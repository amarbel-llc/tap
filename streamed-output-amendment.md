---
layout: default
title: "TAP-14 Amendment: Streamed Output"
---

<!-- SPDX-License-Identifier: Artistic-2.0 -->

# TAP-14 Amendment: Streamed Output

## Abstract

This amendment defines how TAP-14 producers MAY stream process output in real
time, before a test point's pass/fail status is known. It introduces the Output
Block: a `# Output:` comment header followed by 4-space indented output lines,
terminated by the correlated test point. The `streamed-output` pragma signals
that a TAP document uses this pattern.

## Problem

TAP-14 captures process output in YAML diagnostic blocks, which are attached to
the test point that precedes them. Because the test point line (`ok` or
`not ok`) requires knowing the pass/fail status of the test, producers must wait
for the process to finish before emitting the test point --- and therefore
before emitting the YAML diagnostic block that carries the output.

This creates two problems:

1.  **Output is invisible during execution.** For long-running tests, build
    steps, or interactive CI environments, no output appears until the test
    finishes. Developers cannot observe progress, diagnose hangs, or detect
    early failures.

2.  **Producers must buffer all output.** Even though the YAML block is emitted
    after the test point, the producer must capture and hold all process output
    in memory until the process exits, then write it into the YAML block. For
    processes that produce large amounts of output, this is wasteful.

Common workarounds include sending output to stderr (losing association with
test points) or emitting ad-hoc `#` comment lines (which harnesses treat as
unstructured noise). Neither approach preserves the structured association
between output and test points that TAP diagnostics provide.

## Solution

Define a new pragma, `streamed-output`, and a new structural element, the Output
Block, that allows producers to stream process output *before* the correlated
test point. The Output Block mirrors the subtest pattern: a comment header
introduces the block, 4-space indented content follows, and the correlated test
point terminates it.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD",
"SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be
interpreted as described in RFC 2119.

## Specification

### Activation

The pragma is activated with:

``` tap
pragma +streamed-output
```

This pragma is document-wide. Once set, it applies for the remainder of the
document. Producers MUST NOT deactivate it with `pragma -streamed-output` after
activating it.

### Output Block Structure

An Output Block consists of three parts:

1.  **Header:** A comment line of the form `# Output: <id> - <description>`,
    where `<id>` is the test point ID and `<description>` is the test point
    description. The header MAY include a trailing comment (after an unescaped
    `#`) forwarding the test point's directive or documentation comment.

2.  **Body:** Zero or more lines, each indented by exactly 4 spaces. These lines
    are plain text representing captured process output --- they are not YAML,
    not TAP, and MUST NOT be parsed as either. Producers MAY flush each line as
    it becomes available.

3.  **Correlated Test Point:** A test point at the parent indentation level
    whose ID matches the Output Block header's ID. This terminates the Output
    Block, exactly as a correlated test point terminates a subtest.

Its grammar is:

``` ebnf
OutputBlock   := OutputHeader OutputBody TestPoint
OutputHeader  := "# Output: " Number (" -" (" " Description)?)? (" " Directive)? "\n"
OutputBody    := (OutputLine | Empty)*
OutputLine    := "    " [^\n]* "\n"
```

The correlated test point's ID MUST match the Output Block header's ID. The
correlated test point's description SHOULD match the header's description.

Producers MAY omit the Output Block entirely for test points that produce no
output.

### Producer Requirements

Producers SHOULD emit the `# Output:` header before starting the process
associated with the test point. This allows harnesses to display the test
point's name and description immediately.

Producers MAY flush each indented output line as it is produced by the child
process, without waiting for the process to exit. This is the primary benefit of
the Output Block pattern: output is visible in real time during execution.

Producers MUST emit the correlated test point after the process completes and
its pass/fail status is known.

After the correlated test point, producers MAY emit a YAML diagnostic block
carrying structured diagnostic data (`message`, `severity`, `exitcode`, etc.).
This YAML block follows the standard TAP-14 rules for diagnostic blocks --- it
is attached to the preceding test point. Producers SHOULD NOT duplicate the
process output in this YAML block, as it was already delivered in the Output
Block body.

### Harness Behavior

When `streamed-output` is active, harnesses SHOULD:

1.  Recognize the `# Output: <id>` header as introducing an Output Block.
2.  Display each indented body line as it arrives, stripping the 4-space indent
    prefix.
3.  Associate the output with the test point whose ID matches the header's ID.
4.  Treat the correlated test point as the end of the Output Block.

Harnesses MUST still parse any YAML diagnostic block that follows the correlated
test point, per standard TAP-14 rules.

Harnesses that do not recognize the `streamed-output` pragma will see the
`# Output:` header as an ordinary comment (ignored per TAP-14) and the indented
body lines as non-TAP (ignored or passed through per TAP-14). The correlated
test point is parsed normally. This provides graceful degradation.

### Interaction with YAML Diagnostics

The Output Block and YAML diagnostic blocks serve complementary roles:

- The **Output Block** carries raw process output, streamed in real time before
  the test point's status is known.
- The **YAML diagnostic block** (after the correlated test point) carries
  structured metadata about the test result: `message`, `severity`, `exitcode`,
  `file`, `line`, etc.

Producers SHOULD NOT include an `output` field in the YAML diagnostic block when
an Output Block was emitted for the same test point, as this would duplicate the
content.

### Interaction with ANSI Amendments

Indented output lines in the Output Block body MAY contain ANSI SGR sequences
when standard output is a terminal (TTY), subject to the same rules defined by
the ANSI in YAML Output Blocks amendment:

- Producers MUST limit ANSI sequences to SGR only (`ESC [ <params> m`).
- Producers SHOULD emit ANSI sequences only when stdout is a TTY.
- Producers MAY respect the `NO_COLOR` environment variable.

Harnesses that display output lines SHOULD pass through SGR sequences when
writing to a terminal and strip them when writing to a non-terminal.

The `# Output:` header line itself follows the ANSI Display Hints amendment
rules for comment lines: producers MUST NOT include ANSI sequences in the
header.

### Interaction with Subtests

In a subtest, `pragma +streamed-output` applies only to that subtest's document,
consistent with TAP-14's rule that subtest pragmas do not affect parent document
parsing.

A parent document's `streamed-output` pragma does not automatically apply to
child subtests. Subtests that want streamed output MUST emit their own
`pragma +streamed-output`.

When an Output Block appears inside a subtest, the `# Output:` header and
indented body lines are at the subtest's indentation level (i.e., 4 spaces
deeper than the parent for a first-level subtest). The correlated test point is
also at the subtest's level.

### Blank Lines in Output Body

Blank lines within the Output Block body MUST be preserved. Unlike TAP-14's rule
that blank lines outside YAML blocks are ignored, blank lines in the Output
Block body are part of the captured process output and carry semantic meaning
(e.g., paragraph breaks in compiler output).

A blank line in the body MUST still be indented by 4 spaces to remain within the
Output Block. An unindented blank line terminates the body (the next line must
be the correlated test point or non-TAP).

## Example

A build step that compiles a project, then a test step that fails. Output lines
are flushed as the processes run:

``` tap
TAP version 14
pragma +streamed-output
1..2
# Output: 1 - build # Build the project
    compiling main.rs
    compiling lib.rs
    linking binary
ok 1 - build # Build the project
# Output: 2 - test # Run the test suite
    running test suite
    FAIL: test_parse expected 42 got 41
not ok 2 - test # Run the test suite
  ---
  message: "test_parse assertion failed"
  severity: fail
  exitcode: 1
  ...
```

A harness implementing this amendment displays `compiling main.rs` as soon as it
arrives, followed by each subsequent line. When the process finishes and the
producer knows the result, `ok 1 - build` appears. The second test point's
output streams similarly, and the failing `not ok` line is followed by a YAML
diagnostic block with structured failure information.

Without the pragma, a harness sees `# Output: 1 - build` as a comment (ignored),
the indented lines as non-TAP (ignored or passed through), and parses the test
points normally.

## Backwards Compatibility

Harnesses that do not recognize the `streamed-output` pragma MUST ignore it, per
the TAP-14 specification's pragma rules. The structural elements of an Output
Block degrade gracefully:

- The `# Output:` header is a valid TAP comment and is ignored.
- The 4-space indented body lines are not valid TAP at the parent indentation
  level. Harnesses that treat indentation as subtest introduction will attempt
  to parse the body as a subtest TAP document, which will fail (the body is not
  valid TAP). Per TAP-14's subtest rules, harnesses SHOULD treat unterminated
  subtests as non-TAP.
- The correlated test point is a standard test point and is parsed normally.

For maximum compatibility with TAP-13 and TAP-14 harnesses that do not support
this amendment, producers SHOULD only activate `streamed-output` when they know
the consuming harness supports it, or when graceful degradation (output lines
ignored) is acceptable.

## Security Considerations

Output Block body lines may contain arbitrary text captured from child
processes. Harnesses that display these lines to a terminal SHOULD strip all
`ESC [` CSI sequences (not just SGR) to prevent injection of terminal control
codes, consistent with the guidance in the ANSI Display Hints amendment.

## Authors

This amendment is authored by Sasha F as an extension to the TAP-14
specification by Isaac Z. Schlueter.
