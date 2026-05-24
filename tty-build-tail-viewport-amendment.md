---
layout: default
title: "TAP-14 Amendment: TTY Build Tail Viewport"
---

<!-- SPDX-License-Identifier: Artistic-2.0 -->

# TAP-14 Amendment: TTY Build Tail Viewport

## Abstract

This amendment describes how a TAP-14 harness MAY collapse a streamed
Output Block body (per the Streamed Output amendment) into a
fixed-size N-line rolling tail when standard output is a terminal
(TTY). The amendment is purely harness-side: it adds no pragma, no
wire format, and no producer obligations. It supersedes the never-
adopted TTY Build Last Line amendment by generalizing its single-line
trailing status to an N-line viewport rendered above the in-progress
test point.

## Problem

The Streamed Output amendment lets producers flush each line of
captured process output the moment it becomes available. This is
ideal for harnesses that triage TAP streams structurally — every byte
is associated with a test point — but on a TTY it is hostile to human
readers: a long-running test scrolls hundreds of body lines past
faster than they can be read, and the next test point's header
disappears off the top of the screen.

Producers cannot solve this themselves. A producer that buffered the
body would defeat the purpose of streaming output. A producer that
truncated the body would deny structural consumers (e.g. NDJSON
aggregators, agents) the full record.

The only place the trade-off can be made is the harness, and it
depends on whether stdout is a TTY: on a pipe or file, the full
record SHOULD be preserved; on a terminal, the harness MAY collapse
the body into a fixed-size rolling tail.

## Solution

Define a recommended pattern for TTY-only harness rendering: collapse
the Output Block body into an N-line tail under the in-progress test
point, with explicit rules for what happens on `ok`, `not ok`, and
`Bail out!`. No pragma is introduced; producers continue to emit
standard `# Output:` headers and 4-space-indented body lines per the
Streamed Output amendment.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119.

## Specification

### Scope

This amendment specifies **harness rendering behavior** only.
Producers do not change. A harness that ignores this amendment
displays the full streamed Output Block body as plain scrolling text,
which is the existing behavior.

### Producer Requirements

None added by this amendment. Producers continue to emit Output
Blocks per the Streamed Output amendment.

### Harness Behavior

When standard output is a terminal and the `NO_COLOR` environment
variable is unset, a harness MAY render an Output Block body as a
fixed-size **viewport tail** instead of as plain scrolling text. The
viewport tail:

1.  MUST render at most N most-recent lines of the Output Block body,
    where N is harness-defined (RECOMMENDED default: 5).
2.  MUST be displayed below the in-progress test point's header (the
    `# Output: <id> - <description>` line, optionally with a
    rendered spinner or progress indicator).
3.  MUST replace its content in place as new body lines arrive
    (e.g., via ANSI cursor positioning), without writing each body
    line to a new terminal row.
4.  MUST NOT discard body lines from any non-TTY-rendered stream the
    harness also exposes (e.g., a `--tee` or sidecar artifact). The
    viewport is a TTY rendering choice; it MUST NOT affect the bytes
    delivered to other consumers of the same TAP stream.

When the correlated test point arrives, the harness MUST take one of
the following actions depending on the test point's status:

- **`ok N - <desc>`** (success): the viewport tail SHOULD collapse.
  The harness SHOULD render the `ok N - <desc>` line above the
  position the viewport occupied. Lines previously visible in the
  tail are no longer displayed.

- **`ok N - <desc> # SKIP <reason>`** and **`ok N - <desc> # TODO
  <reason>`**: same collapse behavior as bare `ok`. The harness MAY
  render the directive in a visually distinct style.

- **`not ok N - <desc>`** (failure): the viewport tail SHOULD be
  kept rendered as scroll-history above the test point line, so the
  user can see the last lines that preceded the failure without
  needing to scroll back through the full body. The `not ok` line
  SHOULD be rendered in a visually distinct style; any YAML
  diagnostic block that follows the test point MUST be rendered per
  standard TAP-14 rules.

- **`Bail out!`**: the harness SHOULD abandon the viewport, render
  the bail-out line, and (if the harness was capturing the full body
  for failure dumps) dump the full captured transcript to stderr.

### Tail Size

The harness chooses N. The RECOMMENDED default is 5 lines, matching
the common case of "enough to see progress for a sub-step without
pushing the user's shell history off-screen". Harnesses MAY expose N
as a configuration option (e.g., a `--tail-lines` flag) and MAY
derive N from terminal height. N MUST be at least 1 if the viewport
is rendered at all.

### ANSI Handling

Output Block body lines MAY contain ANSI SGR sequences per the
Streamed Output amendment, which itself references the ANSI in YAML
Output Blocks amendment. The viewport tail follows the same rules:

- The harness MUST preserve SGR sequences when rendering body lines
  into the tail on a TTY.
- The harness MUST strip non-SGR CSI sequences (cursor positioning,
  erase, scroll regions, etc.) before placing body content into the
  tail, per the security considerations of the Streamed Output and
  ANSI Display Hints amendments.

A consequence: body lines that use `\r` carriage-return overwrites
(e.g., a child process's progress bar) will render as "last line
wins" in the tail rather than as a live in-place overwrite within the
tail. This is an acceptable degradation; harnesses that want true
in-tail terminal emulation MAY route body bytes through a virtual
terminal emulator before feeding the visible region into the tail.

### `NO_COLOR` and Non-TTY

When `NO_COLOR` is set to a non-empty value, the harness SHOULD NOT
render a viewport — it SHOULD pass the body through as plain
scrolling text. This matches the contract that `NO_COLOR` disables
*all* ANSI-driven UX, not only color.

When stdout is not a terminal, the harness MUST NOT render a
viewport. The full body MUST pass through verbatim. This preserves
the pipe-and-redirect contract: a TAP consumer downstream sees the
same bytes regardless of whether the producing harness *could* have
rendered a viewport.

### Subtests

When an Output Block appears inside a subtest, the harness MAY render
its body in a viewport using the same rules as the parent document.

A harness that supports viewport rendering MUST display **at most one
viewport at a time**: the one whose Output Block is currently
innermost-active. When a subtest opens a child Output Block, the
viewport switches to the child's body. When the subtest closes (its
correlated test point arrives), the viewport returns to the parent's
Output Block, if one is still active.

Nested simultaneous viewports (indented tails for parent and child
shown side-by-side) are NOT REQUIRED by this amendment. Harnesses
MAY implement them as an extension.

### Interaction with the Streamed Output Amendment

This amendment refines the harness side of the Streamed Output
amendment's "Harness Behavior" section. The Streamed Output amendment
says a harness SHOULD display each indented body line as it arrives;
this amendment qualifies that as "on a TTY, the harness MAY render a
fixed-size N-line tail in place of unbounded scrolling". The two are
not in conflict.

This amendment does not change any structural rules of the Streamed
Output amendment: the `# Output:` header, the 4-space-indented body
lines, the correlated test point, and the optional YAML diagnostic
block are all parsed exactly as the Streamed Output amendment
prescribes.

### Interaction with the ANSI Display Hints Amendment

This amendment respects the security considerations of the ANSI
Display Hints amendment: harnesses that render any TAP content to a
terminal MUST sanitize non-SGR CSI sequences to prevent terminal
injection. The viewport tail is no exception.

## Example

A producer emits standard streamed output for a build step (per the
Streamed Output amendment):

``` tap
TAP version 14
pragma +streamed-output
1..2
# Output: 1 - build
    compiling main.rs
    compiling lib.rs
    linking binary
ok 1 - build
# Output: 2 - test
    running test suite
    FAIL: test_parse expected 42 got 41
not ok 2 - test
  ---
  message: "test_parse assertion failed"
  severity: fail
  exitcode: 1
  ...
```

A non-TTY consumer (CI log, file, pipe) sees the bytes above
verbatim.

A TTY harness that implements this amendment, rendering with a
5-line tail, shows roughly:

```
⠹ 1 - build
│ compiling main.rs
│ compiling lib.rs
│ linking binary
```

When `ok 1 - build` arrives, the tail collapses:

```
ok 1 - build
⠹ 2 - test
│ running test suite
│ FAIL: test_parse expected 42 got 41
```

When `not ok 2 - test` arrives, the tail is kept rendered above the
diagnostic:

```
ok 1 - build
2 - test
│ running test suite
│ FAIL: test_parse expected 42 got 41
not ok 2 - test
  ---
  message: "test_parse assertion failed"
  severity: fail
  exitcode: 1
  ...
```

The exact rendering style (spinner glyph, indent character, color)
is harness-defined.

## Relationship to the TTY Build Last Line Amendment

This amendment supersedes the never-adopted **TTY Build Last Line**
amendment. That amendment introduced a producer-side pragma
(`+tty-build-last-line`) for a single trailing comment line that
producers could rewrite in place using ANSI cursor control.

This amendment takes a different approach to the same underlying
problem space (live TTY UX for long-running TAP streams):

- The decision is moved from the producer to the harness. Producers
  do not need to know whether their consumer is a TTY.
- The single trailing line is generalized to an N-line tail.
- No pragma is required; harness rendering is opt-in via TTY
  detection and harness configuration.

Producers SHOULD NOT emit `pragma +tty-build-last-line` going forward.
Harnesses MAY continue to honor it for backwards compatibility, but
new producer and harness work SHOULD use the Streamed Output
amendment together with this amendment.

## Backwards Compatibility

This amendment introduces no new wire syntax, no pragma, and no
producer obligations. A harness that does not implement viewport
rendering displays the full Output Block body as plain scrolling text
— the existing behavior. There is no version skew between producers
and harnesses; the TAP stream they exchange is identical in either
case.

## Security Considerations

The viewport tail renders body bytes that originated from a child
process. Per the Streamed Output amendment's security considerations,
harnesses MUST strip non-SGR CSI sequences from these bytes before
rendering, to prevent terminal injection. This amendment does not
relax that requirement.

A harness that captures the full body for failure dumps MUST apply
the same sanitization when writing the captured buffer to stderr or
to a sidecar artifact.

## Authors

This amendment is authored by Sasha F as an extension to the TAP-14
specification by Isaac Z. Schlueter.
