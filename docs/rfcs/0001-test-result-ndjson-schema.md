---
status: proposed
date: 2026-05-12
---

# TAP Test-Result NDJSON Schema

## Abstract

This RFC specifies a newline-delimited JSON (NDJSON) wire format for
reporting TAP-14 test results in a form optimized for programmatic
consumption. Each test point is emitted as one self-contained JSON
record carrying its verdict, description, parsed diagnostic block,
captured process output, and any embedded subtests; a trailing summary
record reports aggregate counts. The format is the output of the
`tap-dancer format-ndjson` subcommand and provides a stable contract for
agents and downstream tools that consume test results.

## Introduction

TAP-14 is a line-oriented format that interleaves test points with
plans, pragmas, comments, Output Blocks, YAML diagnostics, and subtests
across multiple lines per logical test. Programmatic consumers ---
particularly automated agents extracting failures --- need a format where
one logical test result fits in one parseable unit. NDJSON with parsed
substructures meets that need: every test result is one JSON object,
and `jq`-style queries or single-line readers suffice.

This document specifies the schema of those records. It does not
specify CLI flags, exit codes, or routing semantics --- those are
described in the implementation design document referenced below.

The schema is consumed by:

- Agents reading the failure stream of `tap-dancer format-ndjson --split`
- Tools summarizing test results across runs
- CI integrations that need structured failure data

Background:

- [Design: `tap-dancer format-ndjson`](../plans/2026-05-12-tap-format-ndjson-design.md)
- TAP-14 specification (`tap-version-14-specification.md`)
- Streamed-output amendment (`streamed-output-amendment.md`)

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119.

## Specification

### Document Format

A conforming document is a sequence of records, each encoded as one
JSON object followed by a single line feed (U+000A). Records MUST be
encoded as UTF-8. Producers MUST NOT emit byte order marks. Records
MUST NOT contain unescaped line feeds within their JSON encoding.

Records MUST appear in the following order:

1. Zero or more `test` records, in the order their correlated TAP test
   points appeared in the source stream.
2. At most one `bailout` record, if the source stream contained a
   `Bail out!` directive.
3. Exactly one `summary` record, as the final record of the document.

A document with zero `test` records MUST still emit a `summary` record.

### Record Type Discrimination

Every record MUST contain a `type` field whose value is one of the
strings `"test"`, `"bailout"`, or `"summary"`. Consumers MUST use this
field to discriminate record types. Producers MUST NOT emit records
with any other `type` value.

### Test Record

A `test` record represents one top-level TAP test point and its full
context. Its fields are:

| Field | Type | Required | Description |
|---|---|---|---|
| `type` | string | MUST | Constant `"test"`. |
| `n` | integer | MUST | Test point number from the source TAP stream. Producers MUST emit the number as it appeared; consumers MUST NOT assume sequential numbering. |
| `description` | string | MUST | Test description. MUST be the empty string if the source test point had no description. |
| `ok` | boolean | MUST | `true` for a passing test point, `false` for a failing test point. Producers MUST set `ok` based solely on the TAP test point's `ok` / `not ok` token. |
| `directive` | object \| null | MUST | `null` if the test point had no directive. Otherwise an object with fields `kind` (string, `"skip"` or `"todo"`) and `reason` (string; MAY be empty). |
| `diagnostic` | object \| null | MUST | `null` if the test point had no YAML diagnostic block. Otherwise, the parsed YAML diagnostic as a JSON object (see [Diagnostic Parsing](#diagnostic-parsing)). |
| `output` | string \| null | MUST | `null` if the test point had no correlated Output Block. Otherwise, the concatenated Output Block body with the 4-space indent prefix removed from each line. Producers MUST preserve line feeds between body lines and MUST preserve trailing blank lines that appeared in the source. |
| `subtest` | array \| null | MUST | `null` if the test point was a leaf (no nested subtest). Otherwise, an array of `test` records, in source order, each representing one child test point. |
| `line` | integer | MUST | 1-indexed line number in the source TAP stream where this test point appeared. |

Subtest records nested inside the `subtest` array MUST conform to the
`test` record specification recursively. Nesting depth is bounded only
by the source stream's depth.

#### Example: Passing Leaf Test

```json
{"type":"test","n":1,"description":"loads config","ok":true,"directive":null,"diagnostic":null,"output":null,"subtest":null,"line":3}
```

#### Example: Failing Test With Diagnostic and Output

```json
{"type":"test","n":2,"description":"parses negative numbers","ok":false,"directive":null,"diagnostic":{"message":"expected 42 got 41","severity":"fail","exitcode":1,"file":"parser.rs","line":87},"output":"running test suite\nFAIL: test_parse expected 42 got 41\n","subtest":null,"line":7}
```

#### Example: Subtest Parent

```json
{"type":"test","n":3,"description":"integration suite","ok":false,"directive":null,"diagnostic":null,"output":null,"subtest":[{"type":"test","n":1,"description":"setup","ok":true,"directive":null,"diagnostic":null,"output":null,"subtest":null,"line":11},{"type":"test","n":2,"description":"db query","ok":false,"directive":null,"diagnostic":{"message":"timeout"},"output":null,"subtest":null,"line":13}],"line":15}
```

#### Example: Skip Directive

```json
{"type":"test","n":4,"description":"network test","ok":true,"directive":{"kind":"skip","reason":"requires network"},"diagnostic":null,"output":null,"subtest":null,"line":17}
```

### Bailout Record

A `bailout` record indicates the source TAP stream terminated early via
a `Bail out!` directive. Its fields are:

| Field | Type | Required | Description |
|---|---|---|---|
| `type` | string | MUST | Constant `"bailout"`. |
| `message` | string | MUST | The bailout message text. MUST be the empty string if the source `Bail out!` line had no message. |
| `line` | integer | MUST | 1-indexed line number of the `Bail out!` directive in the source TAP stream. |

A document MUST contain at most one `bailout` record. If present, it
MUST appear after all `test` records and before the `summary` record.

#### Example

```json
{"type":"bailout","message":"database unreachable","line":42}
```

### Summary Record

A `summary` record reports aggregate counts and validity for the source
stream. Its fields are:

| Field | Type | Required | Description |
|---|---|---|---|
| `type` | string | MUST | Constant `"summary"`. |
| `passed` | integer | MUST | Count of top-level test points with `ok: true` and no `skip`/`todo` directive. |
| `failed` | integer | MUST | Count of top-level test points with `ok: false` and no `skip`/`todo` directive. |
| `skipped` | integer | MUST | Count of top-level test points with a `skip` directive, regardless of `ok` value. |
| `todo` | integer | MUST | Count of top-level test points with a `todo` directive, regardless of `ok` value. |
| `total` | integer | MUST | Total count of top-level test points emitted (sum of `passed`, `failed`, `skipped`, `todo`). |
| `plan_count` | integer | MUST | The count from the TAP plan line (e.g., `1..10` yields `10`). MUST be `0` if no plan line was present. |
| `bailed` | boolean | MUST | `true` if the source stream contained a `Bail out!` directive. |
| `valid` | boolean | MUST | `true` if no error-severity parse diagnostics were detected. `false` otherwise. `valid` reports structural sanity; it is independent of `bailed`. A bailed-out stream MAY be `valid: true` because the `Bail out!` directive explicitly accounts for any remaining-test discrepancy, and the producer suppresses `plan-count-mismatch` in that case. Agents checking for run completeness MUST consult `bailed` (and compare `total` to `plan_count`); checking only `valid` is insufficient. |
| `diagnostics` | array | MUST | Array of parse diagnostics. Each element is an object with `line` (integer), `severity` (string), `rule` (string), and `message` (string). MUST be empty if `valid` is `true`. Warning-severity diagnostics MAY appear without forcing `valid: false`. |

Subtest test points MUST NOT be counted in `passed`, `failed`,
`skipped`, `todo`, or `total`. Only top-level test points contribute to
summary counts.

A document MUST contain exactly one `summary` record, and it MUST be
the final record in the document.

#### Example: Valid Run

```json
{"type":"summary","passed":7,"failed":3,"skipped":0,"todo":0,"total":10,"plan_count":10,"bailed":false,"valid":true,"diagnostics":[]}
```

#### Example: Bailed-Out Run

```json
{"type":"summary","passed":2,"failed":1,"skipped":0,"todo":0,"total":3,"plan_count":10,"bailed":true,"valid":true,"diagnostics":[]}
```

The stream is `valid: true` because the `Bail out!` directive
explains the gap between `total` (3) and `plan_count` (10); no
error-severity diagnostic was emitted. `bailed: true` is the signal
that the run did not complete.

#### Example: Malformed Run

```json
{"type":"summary","passed":0,"failed":0,"skipped":0,"todo":0,"total":0,"plan_count":0,"bailed":false,"valid":false,"diagnostics":[{"line":1,"severity":"error","rule":"version-required","message":"first line must be TAP version 14"}]}
```

### Diagnostic Parsing

The `diagnostic` field on `test` records carries the parsed contents of
the TAP YAML diagnostic block. Producers MUST parse the YAML block per
TAP-14 rules and emit the result as a JSON object.

Specifically:

- Scalar string values MUST be emitted as JSON strings.
- Scalar integer values MUST be emitted as JSON integers when the YAML
  representation is unambiguously integer; otherwise as strings.
- Nested mappings MUST be emitted as JSON objects.
- Sequences MUST be emitted as JSON arrays.
- ANSI SGR sequences in YAML scalar values MUST be preserved verbatim
  in the emitted JSON strings (per the ANSI in YAML Output Blocks
  amendment).

Producers MUST NOT emit a `diagnostic` field of type other than
`object` or `null`. If a diagnostic block fails to parse as valid YAML,
the producer SHOULD emit `null` for `diagnostic` and SHOULD record a
parse diagnostic in the summary's `diagnostics` array.

### Field Ordering

Producers SHOULD emit fields in the order specified by the tables
above. Consumers MUST NOT depend on field order, since JSON object
member order is not significant per RFC 8259.

### Encoding of Non-UTF-8 Bytes

If the source TAP stream contains non-UTF-8 byte sequences in Output
Block bodies or YAML diagnostic values, producers MUST replace invalid
sequences with the Unicode replacement character U+FFFD. Producers
MUST NOT emit invalid UTF-8 in the resulting NDJSON.

### Unknown Fields

Future revisions of this schema MAY add fields to existing record
types. Consumers MUST ignore unknown fields they do not recognize.
Consumers MUST NOT reject records on the basis of unknown fields.

Producers MUST NOT emit fields not specified by this RFC or by a
future revision that supersedes it.

## Security Considerations

The schema preserves arbitrary text captured from child processes in
the `output` field and arbitrary scalar values in the `diagnostic`
field. This text MAY contain ANSI SGR sequences as permitted by the
ANSI in YAML Output Blocks amendment.

Consumers that display NDJSON record contents to a terminal SHOULD
strip all `ESC [` CSI sequences (not just SGR) before display to
prevent injection of terminal control codes, consistent with the
guidance in the ANSI Display Hints amendment.

Consumers that pass `output` or `diagnostic` contents into shell
commands, file paths, or other security-sensitive contexts MUST
sanitize the data per the conventions of the target context. The
schema makes no claims about the safety of its string contents ---
producers faithfully relay whatever the source TAP stream contained,
including potentially adversarial content from test fixtures or
process output.

The schema does not include authentication, integrity, or
confidentiality protections. Consumers requiring these properties MUST
apply them at the transport layer (e.g., signed archives, TLS for
network transmission).

## Conformance Testing

Conformance tests for this specification live in `zz-tests_bats/`
(specifically `zz-tests_bats/format_ndjson.bats`).

Tests use binary injection via `bats-emo`:

    require_bin TAP_DANCER_BIN tap-dancer

This allows the conformance suite to run against any implementation
that produces this schema, not only the reference `tap-dancer`
implementation.

### Covered Requirements

| Requirement | Test File | Description |
|---|---|---|
| Document Format: record ordering | `format_ndjson.bats` | Verifies `test` records precede `bailout`, which precedes `summary`. |
| Document Format: trailing summary always present | `format_ndjson.bats` | Verifies empty input still emits a summary record. |
| Test Record: required fields | `format_ndjson.bats` | Verifies all MUST fields present on test records via `jq` assertions. |
| Test Record: subtest nesting | `format_ndjson.bats` | Verifies subtest arrays match source structure for nested TAP. |
| Test Record: directive shape | `format_ndjson.bats` | Verifies `directive` is `null` or has `kind` and `reason`. |
| Test Record: diagnostic parsed as object | `format_ndjson.bats` | Verifies YAML diagnostic blocks become JSON objects, not strings. |
| Test Record: output block body extraction | `format_ndjson.bats` | Verifies 4-space indent is stripped and line feeds preserved. |
| Bailout Record: emitted on `Bail out!` | `format_ndjson.bats` | Verifies bailout record appears when source contains the directive. |
| Summary Record: counts exclude subtests | `format_ndjson.bats` | Verifies subtest test points do not contribute to summary counts. |
| Summary Record: `valid` reflects parse errors | `format_ndjson.bats` | Verifies malformed input yields `valid: false` with populated `diagnostics`. |

## Compatibility

This is the initial version of the schema. No backwards-compatibility
constraints apply.

Future revisions MUST be backwards-compatible according to the
following rules:

- New fields MAY be added to any record type.
- Existing fields MUST NOT be removed.
- Existing fields' types MUST NOT change.
- New record types MAY be added; consumers MUST ignore record types
  they do not recognize.

Incompatible changes MUST be specified in a new RFC that supersedes
this one.

## References

### Normative

- TAP-14 specification (`tap-version-14-specification.md` in this repo)
- Streamed-output amendment (`streamed-output-amendment.md` in this
  repo) --- defines Output Block structure
- ANSI in YAML Output Blocks amendment
  (`ansi-yaml-output-amendment.md` in this repo)
- ANSI Display Hints amendment (`ansi-display-amendment.md` in this
  repo)
- [RFC 2119] Bradner, S., "Key words for use in RFCs to Indicate
  Requirement Levels", BCP 14, RFC 2119, March 1997
- [RFC 8259] Bray, T., Ed., "The JavaScript Object Notation (JSON)
  Data Interchange Format", STD 90, RFC 8259, December 2017

### Informative

- [Design: `tap-dancer format-ndjson`](../plans/2026-05-12-tap-format-ndjson-design.md)
- Issue [#13](https://github.com/amarbel-llc/tap/issues/13) ---
  follow-up to add `--format=ndjson` flag to test-runner subcommands
