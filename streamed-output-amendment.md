---
layout: default
title: "TAP-14 Amendment: Streamed Output"
---

<!-- SPDX-License-Identifier: Artistic-2.0 -->

# TAP-14 Amendment: Streamed Output

## Problem

TAP-14 captures process output exclusively in YAML diagnostic blocks,
which are emitted after a test point completes. This makes output
invisible to consumers until the entire test finishes. For long-running
tests, build steps, or interactive CI environments, users have no
standard way to observe output in real time.

Common workarounds include sending output to stderr (losing association
with test points) or emitting ad-hoc `#` comment lines (which harnesses
treat as unstructured noise). Neither approach gives harnesses enough
information to associate streamed output with the test point it belongs
to.

## Solution

Define a new pragma, `streamed-output`, that tells harnesses to treat
comment lines between test points as structured output belonging to the
next test point.

## Specification

### Activation

The pragma is activated with:

```tap
pragma +streamed-output
```

This pragma is document-wide. Once set, it applies to all subsequent
test points in the document. Producers _must not_ deactivate it with
`pragma -streamed-output` after activating it.

### Streamed Output Lines

When `streamed-output` is active, any comment line (a line beginning
with `#` preceded by zero or more whitespace characters) that appears
between two test points is a **streamed output line**. Its content is
the text following the `# ` prefix (hash, space).

Streamed output lines are associated with the **next** test point. That
is, all comment lines after test point N (or after the plan, if before
any test points) and before test point N+1 are the streamed output of
test point N+1.

### Example

```tap
TAP version 14
pragma +streamed-output
1..3
# compiling main.rs
# linking binary
ok 1 - build
# running test suite
# FAIL: test_parse expected 42 got 41
not ok 2 - test
  ---
  message: "test_parse assertion failed"
  severity: fail
  exitcode: 1
  ...
# deploying to staging
ok 3 - deploy
```

In this example:

- `compiling main.rs` and `linking binary` are streamed output for test
  point 1 (`build`)
- `running test suite` and `FAIL: test_parse expected 42 got 41` are
  streamed output for test point 2 (`test`)
- `deploying to staging` is streamed output for test point 3 (`deploy`)

### Relationship to YAML Diagnostics

Producers _may_ omit the YAML `output` field when `streamed-output` is
active, since the output has already been delivered via comment lines.

Producers _may_ include the YAML `output` field for backwards
compatibility with harnesses that do not recognize the pragma. In this
case the YAML block contains the complete buffered output, and the
streamed comment lines are the same content delivered incrementally.

### Backwards Compatibility

Harnesses that do not recognize the `streamed-output` pragma _must_
ignore it, per the TAP-14 specification's pragma rules. Comment lines
are already valid TAP and will be treated as normal comments. This
provides graceful degradation: the output is still visible as comments,
just without the semantic association to test points.

Producers targeting maximum compatibility _should_ include both streamed
comment lines and YAML `output` fields.

### Subtests

In a subtest, `pragma +streamed-output` applies only to that subtest's
document, consistent with TAP-14's rule that subtest pragmas do not
affect parent document parsing.

A parent document's `streamed-output` pragma does not automatically
apply to child subtests. Subtests that want streamed output _must_ emit
their own `pragma +streamed-output`.

### Future Extensions

This amendment defines only unstructured text lines prefixed with `# `.
Future amendments _may_ define structured prefixes to distinguish output
streams (e.g., stdout vs stderr) or attach metadata to individual output
lines. Harnesses _should not_ assume any structure beyond the `# `
prefix defined here.

## Authors

This amendment is authored by Sasha F as an extension to the TAP-14
specification by Isaac Z. Schlueter.
