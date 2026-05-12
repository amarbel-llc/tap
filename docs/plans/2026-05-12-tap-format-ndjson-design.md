# Design: `tap-dancer format-ndjson`

**Status:** Approved
**Date:** 2026-05-12
**Author:** Sasha F (with Clown :clown:)

## Problem

Agents consuming TAP-14 streams to identify failures must do non-trivial
parsing: TAP is a streaming line-oriented format with subtests, YAML
diagnostic blocks, Output Blocks, pragmas, and comments interleaved with
test points. Filtering for failures requires:

- A TAP-14 parser that understands subtest nesting and pragmas.
- Reassembling Output Block bodies, YAML diagnostics, and test points into
  one logical unit.
- Filtering by the `not ok` test points without losing context.

Today no tool in this repo does that for agents. They either consume raw
TAP (and shoulder the parsing burden) or run the test runner in some
runner-specific verbose mode (which loses the unified format).

## Solution

Add a new `tap-dancer format-ndjson` subcommand that reads TAP-14 from
stdin and emits NDJSON: one JSON record per top-level test point plus a
trailing summary record. An optional `--split` mode routes passes to one
sink and failures to another so agents can read failures end-to-end
without filtering.

A formal schema for the NDJSON records is captured in a separate RFC
(see [RFC: TAP test-result NDJSON
schema](../rfcs/0001-test-result-ndjson-schema.md)). This design references
the schema but does not duplicate it.

## CLI surface

```sh
# Unified NDJSON stream
... | tap-dancer format-ndjson > all.ndjson

# Split: failures to stdout, passes to side file
... | tap-dancer format-ndjson --split --pass-out passes.ndjson > failures.ndjson

# Split, dropping passes
... | tap-dancer format-ndjson --split > failures.ndjson
```

Flags:

| Flag | Description |
|---|---|
| `--split` | Enable split mode. Failures to stdout, passes to `--pass-out`. |
| `--pass-out <path>` | File path for passing records. Only valid with `--split`. Omit to drop passes. |

Exit codes:

- `0` --- no failures and no bailout
- `1` --- at least one failure seen, or `Bail out!` was emitted
- `2` --- tool-internal error (I/O failure, invalid flags)

## MCP exposure

A parallel MCP tool accepts a TAP input string and returns
`{failures: [...], summary: {...}}` --- failures only, no pass list. The
streaming `--pass-out` side-channel does not map to MCP's
request/response model and is omitted.

## Routing rules

**Whole-subtree by parent verdict.** A top-level test point routes to
the failure stream only when it is a *genuine* failure: `!ok &&
directive == nil`. All other records --- ok:true passes, SKIPs (any
ok), and TODOs (`ok:false` per TAP convention) --- route to the pass
stream. The embedded `subtest` array goes with the parent regardless
of children's verdicts. An NDJSON record is never split across
streams.

**Shared records.** Bail-out and summary records are emitted to both
streams.

**Output Block routing.** Output Blocks (`# Output:` header + indented
body) arrive *before* the correlated test point's verdict. The
aggregator buffers them until the test point arrives, then attaches the
body as the record's `output` field. This loses real-time streaming but
preserves valid record boundaries; agents read these streams post-hoc
anyway.

## Data flow

```
stdin (TAP-14)
    │
    ▼
reader.Reader.Next()        ← existing event API
    │
    ▼
ndjson.Aggregator           ← NEW
    │  (one in-progress top-level record at a time)
    │  (subtest events push into parent's subtest array)
    │  (Output Block + YAML diagnostic buffered onto record)
    ▼
JSON encoder per stream
    │             │
    ▼             ▼
failures.ndjson  passes.ndjson
(stdout)         (--pass-out, optional)
```

The aggregator consumes the reader's existing event stream and emits
complete records on each top-level test point.

## Error handling

| Scenario | Behavior |
|---|---|
| Malformed TAP | Aggregator continues through EOF; reader diagnostics surface in the summary's `diagnostics` array; `valid: false`. |
| Bail-out | Flush in-progress record (`ok: false`), emit bailout record to both streams, emit summary with `bailed: true`, exit 1. |
| Partial Output Block at EOF | Synthetic failure record with `description: "<unterminated output block>"` and a diagnostic noting truncation. Routes to failure stream. |
| I/O failure on `--pass-out` | Exit 2 with error to stderr. |
| `--pass-out` without `--split` | Exit 2 with usage error. |
| Empty input | Emit only the summary record; exit 0. |

All strings in records are UTF-8; non-UTF-8 bytes are replaced with
U+FFFD on emit.

## Code layout

```
go/
  internal/
    bravo/
      ndjson/                NEW: aggregator, record types, marshalling
        ndjson.go
        ndjson_test.go
  pkgs/
    ndjson/                  NEW: façade re-export via dagnabit
  cmd/tap-dancer/
    main.go                  ← add format-ndjson subcommand
```

Lives at the `bravo/` level alongside `reader/` and `writer/` because it
consumes reader events directly.

## Testing

| Layer | Coverage |
|---|---|
| `internal/bravo/ndjson` unit tests | Aggregator state machine driven by synthetic event streams. |
| `internal/bravo/ndjson` integration tests | End-to-end: TAP-14 strings → reader → aggregator → JSON. |
| `zz-tests_bats/format_ndjson.bats` | Black-box CLI: flag handling, exit codes, stream routing, asserted via `jq`. |

## Out of scope

- Rust parity: the Rust crate is writer-only; no CLI to extend.
- Bash parity: writer-only.
- `--format=ndjson` flag on `cargo-test` / `go-test`: filed as
  [#13](https://github.com/amarbel-llc/tap/issues/13) for follow-up.

## Rollback

`format-ndjson` is purely additive --- a new subcommand alongside
existing ones. Rollback is `git revert` of the introducing commit; no
dual-architecture period needed because nothing is being replaced.

## References

- [RFC: TAP test-result NDJSON schema](../rfcs/0001-test-result-ndjson-schema.md)
- TAP-14 specification (root of repo)
- Streamed-output amendment (`streamed-output-amendment.md`)
- Issue [#13](https://github.com/amarbel-llc/tap/issues/13) ---
  `--format=ndjson` on test-runner subcommands (follow-up)
