---
status: proposed
date: 2026-05-23
promotion-criteria: tap-dancer-go renders the viewport on TTY via
  `reformat`, `go-test`, and `cargo-test`; non-TTY pass-through is
  unchanged; the harness-behavior spec amendment lands alongside (or
  before) the implementation
---

# TTY Viewport

## Motivation

Streamed Output Blocks (see `streamed-output-amendment.md`) let TAP-14
producers emit child-process output incrementally inside a test point.
For agents this is great — every byte is structured. For humans on a
TTY watching a long-running test or build, every byte is *noise*: the
terminal scrolls past faster than the user can read, and the next test
point disappears off the top.

tap-dancer already auto-detects TTY for several UX concerns:

- `exec` / `exec-parallel` spawn a spinner on TTY (opt out via
  `--no-spinner`).
- `reformat`, `go-test`, `cargo-test` auto-toggle ANSI color via
  `stdoutIsTerminal()`.

This feature is the natural next step in that line: when a streamed
Output Block body would otherwise scroll past, collapse it into a
fixed-size rolling tail under the in-progress test point. On `ok`,
collapse to the single test-point line in green. On `not ok`, hold
the body visible and render the YAML diagnostic. Non-TTY output is
unchanged.

Inspiration: clown's `cmd/clown/tent_loader.go` already does this for
`podman load`. The dewey-side primitive that owns the bubbletea +
PTY plumbing is documented in
[purse-first FDR 0010 — Operation Viewport][fdr-0010]. This FDR scopes
the *TAP-aware controller* that drives it.

[fdr-0010]: https://github.com/amarbel-llc/purse-first/blob/master/docs/features/0010-operation-viewport.md

## Scope

**v1: tap-dancer-go only.** Rust (`tap-dancer-rust`) and bash
(`tap-dancer.bash`) implementations are deferred. The spec amendment
that describes the harness behavior is language-agnostic; only the
implementation is gated.

## Design

### Architecture

```
                ┌────────────────────────────────────────────┐
                │ tap-dancer-go                              │
                │                                            │
TAP stream ────►│  TAP parser ──► viewport adapter           │
                │                       │                    │
                │                       ▼                    │
                │  ┌─────────────────────────────────────┐   │
                │  │ dewey/pkgs/operation_viewport       │   │
                │  │  raw Model (driven via messages)    │   │
                │  └─────────────────────────────────────┘   │
                └────────────────────────────────────────────┘
                                ▼
                          rendered TTY frame
```

The TAP-aware controller is a thin adapter in tap-dancer-go that:

1. Parses the incoming TAP stream (the existing `reformat` parser).
2. Translates TAP events into `operation_viewport` messages.
3. Owns the TAP-specific UX decisions (collapse-on-`ok`,
   hold-on-`not ok`, subtest depth, SKIP/TODO directives).

The dewey primitive owns everything generic: bubbletea program,
spinner, rolling tail, optional progress bar, PTY allocation for
producer-side wrappers, capture-on-failure.

### Surfaces

This feature has two surfaces, both auto-enabled on TTY:

**Consumer side — `tap-dancer reformat`**

`reformat` already auto-toggles ANSI color on TTY. It learns Output
Blocks: when consuming a TAP stream whose test points carry streamed
Output Blocks, render them through the viewport adapter. Non-TTY: the
current pass-through behavior is unchanged.

**No new `render` subcommand.** `reformat` is the TTY-aware TAP→TAP
renderer; extending it preserves the existing user mental model.

**Producer side — `go-test`, `cargo-test`, `exec`, `exec-parallel`**

These subcommands wrap a child process and emit TAP. On TTY they
auto-enable the viewport. The child is spawned through
`operation_viewport.Run` / `RunBatch` (which allocates a PTY so the
child emits colors, scans output, and renders), and TAP is produced
*to the same stdout*. The viewport renders above the TAP stream;
both flow through cleanly because the viewport region is repainted
in place.

### Flag surface

A unified `--ui` flag controls viewport behavior across all
subcommands:

| Value | Meaning |
|---|---|
| `auto` (default) | enabled iff stdout is a TTY and `NO_COLOR` is unset |
| `always` | force-enabled even on non-TTY (rare; useful for tmux/screen edge cases) |
| `never` | disabled; full pass-through |

`--no-spinner` (existing flag on `exec` / `exec-parallel`) is kept as
a deprecated alias for `--ui=never`. A deprecation notice fires on
stderr when the alias is used; removal is a separate FDR.

`NO_COLOR=1` forces `--ui=never` regardless of explicit flag (matches
the existing color-disable contract).

### TAP → viewport message mapping

| TAP event | `operation_viewport` message |
|---|---|
| `1..N` plan line | (held; supplied with first `OperationStarted`) |
| Test point begins (with streamed Output Block) | `OperationStarted{Name: desc, Index: n, Total: planTotal}` |
| Output Block body line | `LogLine{Text: line}` |
| `ok N - <desc>` | `OperationDone{Err: nil}` |
| `ok N - <desc> # SKIP <reason>` | `OperationDone{Err: nil}` (rendered as skipped) |
| `ok N - <desc> # TODO <reason>` | `OperationDone{Err: nil}` (rendered as todo) |
| `not ok N - <desc>` | `OperationDone{Err: <synthetic>}` followed by YAML diagnostic emitted *above* the viewport |
| End of stream | `BatchDone{Err: aggregate}` |
| `Bail out!` | `BatchDone{Err: <bailout>}` |

The viewport adapter is the only place where TAP semantics meet
`operation_viewport`. It is allowed to mutate the rendering style per
message (e.g. green for `ok`, red for `not ok`, dim for `SKIP`) via
opinionated lipgloss styles owned in tap-dancer.

### Collapse / hold behavior

- **`ok N`** — the viewport's body tail is cleared, the rolling tail
  region disappears for the now-completed test point, and the test
  point line is rendered above the viewport position in green. The
  next test point's Output Block (if any) opens a fresh tail at the
  same screen position. This matches clown's
  "collapse to single status line on success" pattern.

- **`not ok N`** — the rolling tail's contents are kept rendered as
  scroll-history above the (now-closing) viewport, and the failing
  test point line is rendered in red, followed by the YAML diagnostic
  block from the streamed-output amendment. Behavior is the same as
  the user scrolling back to see what podman complained about in
  clown's loader — but inline, with the full body and the diagnostic
  side-by-side.

- **`Bail out!`** — render bail-out line and full captured transcript
  to stderr; abandon the viewport.

### Subtest behavior

A single viewport at the **innermost currently-active Output Block**.
When a subtest opens a child Output Block, the viewport switches to
that block's tail. When the subtest closes, the viewport returns to
the parent's active block (if any).

Nested indented viewports (multiple tails on screen simultaneously)
are deferred to a follow-up FDR — neither clown nor the motivating
batch consumers need them, and the simple single-viewport rule is
easier to reason about and to render.

### Non-TTY behavior

When stdout is not a TTY (or `--ui=never` / `NO_COLOR` is set),
`reformat`, `go-test`, `cargo-test`, `exec`, `exec-parallel` pass
TAP through unchanged. No viewport is started, no PTY is allocated,
no ANSI is injected. CI logs and pipe consumers see exactly what
they see today.

### Interaction with `--split` / `format-ndjson`

`--split` (see `docs/plans/2026-05-12-tap-format-ndjson-design.md`)
is the post-hoc aggregation mode: the aggregator buffers Output Block
bodies until each test point's verdict is known, then emits one NDJSON
record per test point. It is explicitly *not* a live mode.

The viewport is the opposite: streaming lines in real time for humans.

These are **orthogonal consumer modes**, not in conflict:

| Mode | Audience | Real-time? | Purpose |
|---|---|---|---|
| `format-ndjson [--split]` | Agents | No (buffered) | Structured triage |
| TTY viewport (this FDR) | Humans | Yes (streaming) | Live tail |

A pipeline that wants both can `tee`. tap-dancer does not fold them
together.

### Spec amendment

This feature lands alongside a new harness-behavior amendment:
`tty-build-tail-viewport-amendment.md`. The amendment describes how a
harness MAY collapse an Output Block body into a fixed-size N-line
viewport on a TTY. Producer behavior is unchanged: producers keep
emitting standard `# Output:` headers + 4-space-indented body lines
per the streamed-output amendment.

The existing `tty-build-last-line-amendment.md` (never adopted by any
producer or harness) is marked **Superseded by tty-build-tail-viewport**
and its content kept in place for external-link stability.

## Interface

### `tap-dancer reformat`

```
tap-dancer reformat [--ui=auto|always|never] [< tap-stream]
```

Reads TAP from stdin, renders Output Blocks as a viewport on TTY,
passes through unchanged on non-TTY.

### `tap-dancer go-test`

```
tap-dancer go-test [--ui=auto|always|never] [go test args...]
```

Wraps `go test`. On TTY: spawns under a PTY, parses the output into
TAP, renders the viewport, emits TAP to stdout. On non-TTY: emits TAP
to stdout unchanged. The existing `--no-spinner` is a deprecated
alias for `--ui=never`.

### `tap-dancer cargo-test`

Same shape as `go-test`, wraps `cargo test`.

### `tap-dancer exec` / `exec-parallel`

Same shape as `go-test`, wraps an arbitrary command (`exec`) or a
fan-out of commands (`exec-parallel`). Each command becomes one
`OperationStarted` event; with `exec-parallel`'s sequential mode
the viewport switches between them.

## Examples

### Streaming `go test` output through a viewport

A TAP-14 stream from `tap-dancer go-test` for three tests, mid-run.
On TTY the screen shows:

```
⠹ go test                                       [████░░░░░░░░] 1/3
│ === RUN   TestParseHeader
│ --- PASS: TestParseHeader (0.00s)
│ === RUN   TestParseBody
│     parser_test.go:42: case "long":  expected len=3, got 2
│ --- FAIL: TestParseBody (0.01s)
```

When `not ok 2 - TestParseBody` arrives, the tail freezes above and
the diagnostic renders:

```
ok 1 - TestParseHeader
not ok 2 - TestParseBody
  ---
  message: |
    parser_test.go:42: case "long": expected len=3, got 2
  ...
⠹ go test                                       [████████░░░░] 2/3
│ === RUN   TestParseFooter
│ --- PASS: TestParseFooter (0.00s)
```

### Non-TTY: unchanged pass-through

```
$ tap-dancer go-test ./... | grep -E '^(ok|not ok)'
ok 1 - TestParseHeader
not ok 2 - TestParseBody
ok 3 - TestParseFooter
```

### Force viewport off

```
tap-dancer go-test --ui=never ./...
```

Or:

```
NO_COLOR=1 tap-dancer go-test ./...
```

## Limitations

- **Go implementation only in v1.** Rust and bash tap-dancers do not
  carry the viewport. They consume TAP and emit TAP; users of those
  implementations can still pipe through `tap-dancer-go reformat` if
  they want viewport rendering.

- **Single innermost viewport.** Subtests' Output Blocks render
  serially, not as side-by-side indented tails. Multi-viewport layout
  is a follow-up FDR.

- **Sequential test points only.** Inherited from
  `operation_viewport` v0. Parallel producers (`exec-parallel` with
  fan-out, parallel `go test -parallel`) serialize their test point
  outputs through the viewport — only the most-recently-started point
  has a visible tail. Parallel tails are a follow-up FDR.

- **CR-overwrite / cursor sequences are stripped in v0.** Until
  `operation_viewport` exposes its v1 pane mode (vt-emulator backing
  for the tail), commands inside Output Blocks that use `\r`-overwrite
  progress bars render with last-line-wins.

- **Sidecar artifact capture is out of scope for this FDR.** When a
  user runs `tap-dancer go-test --ui=auto > run.tap`, the redirection
  flips stdout to non-TTY and the viewport is off — they get a clean
  TAP file but no live tail. Concurrent viewport + sidecar file
  capture (a `--tee=run.tap` flag or similar) is left for a follow-up
  if it actually shows up in real workflows.

- **Spec amendment is normative-only for harnesses.** Producers do not
  need to change; the amendment describes harness UX, not a wire
  format. tap-dancer is the reference implementation; other harnesses
  MAY adopt the same conventions or ignore them.

## More Information

- **Driving issue:** `amarbel-llc/tap` issue
  [#21][issue-21] — "TTY viewport for Output Block tail
  (charmbracelet/bubbletea)"
- **Dewey primitive:** [purse-first FDR 0010 — Operation Viewport][fdr-0010]
- **Reference implementation pattern:**
  [`amarbel-llc/clown:cmd/clown/tent_loader.go`][tent-loader]
- **Relevant existing amendments:**
  - `streamed-output-amendment.md` — defines Output Blocks (the content
    this feature collapses into a viewport)
  - `ansi-yaml-output-amendment.md` — ANSI rules in Output Block bodies
    (SGR pass-through, non-SGR CSI stripping; this FDR follows it)
  - `tty-build-last-line-amendment.md` — to be marked Superseded by
    `tty-build-tail-viewport-amendment.md` when the new amendment lands
- **Adjacent design:**
  `docs/plans/2026-05-12-tap-format-ndjson-design.md` — `--split`
  buffered consumer mode (orthogonal to this FDR; documented above)

[issue-21]: https://github.com/amarbel-llc/tap/issues/21
[tent-loader]: https://github.com/amarbel-llc/clown/blob/HEAD/cmd/clown/tent_loader.go
