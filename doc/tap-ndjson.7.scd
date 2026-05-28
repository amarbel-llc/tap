tap-ndjson(7)

# NAME

tap-ndjson - newline-delimited JSON wire format for TAP-14 test results

# DESCRIPTION

This page specifies a newline-delimited JSON (NDJSON) wire format for
reporting TAP-14 test results in a form optimized for programmatic
consumption. Each test point is emitted as one self-contained JSON
record carrying its verdict, description, parsed diagnostic block,
captured process output, and any embedded subtests; a trailing summary
record reports aggregate counts. The format is the output of the
*tap-dancer format-ndjson* subcommand and provides a stable contract
for agents and downstream tools that consume test results.

TAP-14 is a line-oriented format that interleaves test points with
plans, pragmas, comments, Output Blocks, YAML diagnostics, and subtests
across multiple lines per logical test. Programmatic consumers, and
particularly automated agents extracting failures, need a format where
one logical test result fits in one parseable unit. NDJSON with parsed
substructures meets that need: every test result is one JSON object, and
_jq_-style queries or single-line readers suffice.

This page specifies the schema of those records. It does not specify CLI
flags, exit codes, or routing semantics; those belong to the *tap-dancer*
command documentation.

The schema is consumed by:

- Agents reading the failure stream of *tap-dancer format-ndjson --split*.
- Tools summarizing test results across runs.
- CI integrations that need structured failure data.

# REQUIREMENTS LANGUAGE

The key words MUST, MUST NOT, REQUIRED, SHALL, SHALL NOT, SHOULD, SHOULD
NOT, RECOMMENDED, MAY, and OPTIONAL in this page are to be interpreted as
described in RFC 2119.

# DOCUMENT FORMAT

A conforming document is a sequence of records, each encoded as one JSON
object followed by a single line feed (U+000A). Records MUST be encoded
as UTF-8. Producers MUST NOT emit byte order marks. Records MUST NOT
contain unescaped line feeds within their JSON encoding.

Records MUST appear in the following order:

. Zero or one *plan* record. If present, it MUST be the first record,
  before any *test* record.
. Zero or more *test* records, in the order their correlated TAP test
  points appeared in the source stream.
. At most one *bailout* record, if the source stream contained a
  _Bail out!_ directive.
. Exactly one *summary* record, as the final record of the document.

A document with zero *test* records MUST still emit a *summary* record.

# RECORD TYPE DISCRIMINATION

Every record MUST contain a *type* field whose value is one of the
strings _"plan"_, _"test"_, _"bailout"_, or _"summary"_. Consumers MUST
use this field to discriminate record types. Producers MUST NOT emit
records with any other *type* value.

# PLAN RECORD

A *plan* record announces, up front, how many top-level test points the
producer intends to emit. It mirrors a leading TAP _1..N_ plan line.

*type* (string)
	Constant _"plan"_.

*count* (integer)
	The number of top-level test points the producer intends to emit;
	mirrors TAP's _1..N_.

The *plan* record is OPTIONAL. A producer that knows its plan up front
SHOULD emit it as the first record; a producer that only learns the plan
after running — for example a transformer reading a stream whose plan
line is trailing — MAY omit it and report the count in the summary's
*plan_count* field instead.

A document MUST contain at most one *plan* record. If present, it MUST be
the first record, before any *test* record. When both a *plan* record and
a *summary* record are present, *plan_count* MUST equal the plan record's
*count*; this preserves the completeness check of comparing *total*
against *plan_count*.

## Example

```
{"type":"plan","count":2}
```

# TEST RECORD

A *test* record represents one top-level TAP test point and its full
context. Every field below MUST be present on each *test* record; fields
documented as "X or null" MUST appear with a JSON null when not
applicable.

*type* (string)
	Constant _"test"_.

*n* (integer)
	Test point number from the source TAP stream. Producers MUST emit the
	number as it appeared; consumers MUST NOT assume sequential numbering.

*description* (string)
	Test description. MUST be the empty string if the source test point
	had no description.

*ok* (boolean)
	_true_ for a passing test point, _false_ for a failing test point.
	Producers MUST set *ok* based solely on the TAP test point's _ok_ or
	_not ok_ token.

*directive* (object or null)
	_null_ if the test point had no directive. Otherwise an object with
	fields *kind* (string, _"skip"_ or _"todo"_) and *reason* (string;
	MAY be empty).

*diagnostic* (object or null)
	_null_ if the test point had no YAML diagnostic block. Otherwise the
	parsed YAML diagnostic as a JSON object (see *DIAGNOSTIC PARSING*).

*output* (string or null)
	_null_ if the test point had no correlated Output Block. Otherwise the
	concatenated Output Block body with the 4-space indent prefix removed
	from each line. Producers MUST preserve line feeds between body lines
	and MUST preserve trailing blank lines that appeared in the source.

*subtest* (array or null)
	_null_ if the test point was a leaf (no nested subtest). Otherwise an
	array of *test* records, in source order, each representing one child
	test point.

*line* (integer)
	1-indexed line number in the source TAP stream where this test point
	appeared.

Subtest records nested inside the *subtest* array MUST conform to the
*test* record specification recursively. Nesting depth is bounded only by
the source stream's depth.

## Examples

Passing leaf test:

```
{"type":"test","n":1,"description":"loads config","ok":true,"directive":null,"diagnostic":null,"output":null,"subtest":null,"line":3}
```

Failing test with diagnostic and output:

```
{"type":"test","n":2,"description":"parses negative numbers","ok":false,"directive":null,"diagnostic":{"message":"expected 42 got 41","severity":"fail","exitcode":1,"file":"parser.rs","line":87},"output":"running test suite\nFAIL: test_parse expected 42 got 41\n","subtest":null,"line":7}
```

Subtest parent:

```
{"type":"test","n":3,"description":"integration suite","ok":false,"directive":null,"diagnostic":null,"output":null,"subtest":[{"type":"test","n":1,"description":"setup","ok":true,"directive":null,"diagnostic":null,"output":null,"subtest":null,"line":11},{"type":"test","n":2,"description":"db query","ok":false,"directive":null,"diagnostic":{"message":"timeout"},"output":null,"subtest":null,"line":13}],"line":15}
```

Skip directive:

```
{"type":"test","n":4,"description":"network test","ok":true,"directive":{"kind":"skip","reason":"requires network"},"diagnostic":null,"output":null,"subtest":null,"line":17}
```

# BAILOUT RECORD

A *bailout* record indicates the source TAP stream terminated early via a
_Bail out!_ directive. Every field below MUST be present.

*type* (string)
	Constant _"bailout"_.

*message* (string)
	The bailout message text. MUST be the empty string if the source
	_Bail out!_ line had no message.

*line* (integer)
	1-indexed line number of the _Bail out!_ directive in the source TAP
	stream.

A document MUST contain at most one *bailout* record. If present, it MUST
appear after all *test* records and before the *summary* record.

Example:

```
{"type":"bailout","message":"database unreachable","line":42}
```

# SUMMARY RECORD

A *summary* record reports aggregate counts and validity for the source
stream. Every field below MUST be present.

*type* (string)
	Constant _"summary"_.

*passed* (integer)
	Count of top-level test points with _ok: true_ and no skip or todo
	directive.

*failed* (integer)
	Count of top-level test points with _ok: false_ and no skip or todo
	directive.

*skipped* (integer)
	Count of top-level test points with a skip directive, regardless of
	*ok* value.

*todo* (integer)
	Count of top-level test points with a todo directive, regardless of
	*ok* value.

*total* (integer)
	Total count of top-level test points emitted (sum of *passed*,
	*failed*, *skipped*, *todo*).

*plan_count* (integer)
	The count from the TAP plan line (for example, _1..10_ yields 10).
	MUST be 0 if no plan line was present.

*bailed* (boolean)
	_true_ if the source stream contained a _Bail out!_ directive.

*valid* (boolean)
	_true_ if no error-severity parse diagnostics were detected, _false_
	otherwise. *valid* reports structural sanity; it is independent of
	*bailed*. A bailed-out stream MAY be _valid: true_ because the
	_Bail out!_ directive explicitly accounts for any remaining-test
	discrepancy, and the producer suppresses plan-count-mismatch in that
	case. Agents checking for run completeness MUST consult *bailed* (and
	compare *total* to *plan_count*); checking only *valid* is
	insufficient.

*diagnostics* (array)
	Array of parse diagnostics. Each element is an object with *line*
	(integer), *severity* (string), *rule* (string), and *message*
	(string). MUST be empty if *valid* is _true_. Warning-severity
	diagnostics MAY appear without forcing _valid: false_.

Subtest test points MUST NOT be counted in *passed*, *failed*, *skipped*,
*todo*, or *total*. Only top-level test points contribute to summary
counts.

A document MUST contain exactly one *summary* record, and it MUST be the
final record in the document.

## Examples

Valid run:

```
{"type":"summary","passed":7,"failed":3,"skipped":0,"todo":0,"total":10,"plan_count":10,"bailed":false,"valid":true,"diagnostics":[]}
```

Bailed-out run:

```
{"type":"summary","passed":2,"failed":1,"skipped":0,"todo":0,"total":3,"plan_count":10,"bailed":true,"valid":true,"diagnostics":[]}
```

The stream is _valid: true_ because the _Bail out!_ directive explains
the gap between *total* (3) and *plan_count* (10); no error-severity
diagnostic was emitted. _bailed: true_ is the signal that the run did not
complete.

Malformed run:

```
{"type":"summary","passed":0,"failed":0,"skipped":0,"todo":0,"total":0,"plan_count":0,"bailed":false,"valid":false,"diagnostics":[{"line":1,"severity":"error","rule":"version-required","message":"first line must be TAP version 14"}]}
```

# DIAGNOSTIC PARSING

The *diagnostic* field on *test* records carries the parsed contents of
the TAP YAML diagnostic block. Producers MUST parse the YAML block per
TAP-14 rules and emit the result as a JSON object. Specifically:

- Scalar string values MUST be emitted as JSON strings.
- Scalar integer values MUST be emitted as JSON integers when the YAML
  representation is unambiguously integer; otherwise as strings.
- Nested mappings MUST be emitted as JSON objects.
- Sequences MUST be emitted as JSON arrays.
- ANSI SGR sequences in YAML scalar values SHOULD be stripped before
  emission. This is a deliberate divergence from the ANSI in YAML Output
  Blocks amendment: YAML 1.2 parsers reject raw ESC (0x1B) bytes as
  disallowed control characters, so preserving them through programmatic
  YAML parsing is impractical. Producers consuming the TAP wire format
  for display (for example, *tap-dancer reformat*) MAY preserve ANSI;
  producers emitting NDJSON for programmatic consumption MUST strip ANSI
  to avoid downstream JSON consumers needing to handle escape sequences
  in display logic.

Producers MUST NOT emit a *diagnostic* field of type other than object or
null. If a diagnostic block fails to parse as valid YAML, the producer
SHOULD emit _null_ for *diagnostic* and SHOULD record a parse diagnostic
in the summary's *diagnostics* array.

# FIELD ORDERING

Producers SHOULD emit fields in the order specified above. Consumers MUST
NOT depend on field order, since JSON object member order is not
significant per RFC 8259.

# ENCODING OF NON-UTF-8 BYTES

If the source TAP stream contains non-UTF-8 byte sequences in Output
Block bodies or YAML diagnostic values, producers MUST replace invalid
sequences with the Unicode replacement character U+FFFD. Producers MUST
NOT emit invalid UTF-8 in the resulting NDJSON.

# UNKNOWN FIELDS

Future revisions of this schema MAY add fields to existing record types.
Consumers MUST ignore unknown fields they do not recognize. Consumers
MUST NOT reject records on the basis of unknown fields.

Producers MUST NOT emit fields not specified by this page or by a future
revision that supersedes it.

# SECURITY CONSIDERATIONS

The schema preserves arbitrary text captured from child processes in the
*output* field and arbitrary scalar values in the *diagnostic* field.
This text MAY contain ANSI SGR sequences as permitted by the ANSI in YAML
Output Blocks amendment.

Consumers that display NDJSON record contents to a terminal SHOULD strip
all ESC [ CSI sequences (not just SGR) before display to prevent
injection of terminal control codes, consistent with the ANSI Display
Hints amendment.

Consumers that pass *output* or *diagnostic* contents into shell
commands, file paths, or other security-sensitive contexts MUST sanitize
the data per the conventions of the target context. The schema makes no
claims about the safety of its string contents; producers faithfully
relay whatever the source TAP stream contained, including potentially
adversarial content from test fixtures or process output.

The schema does not include authentication, integrity, or confidentiality
protections. Consumers requiring these properties MUST apply them at the
transport layer (for example, signed archives, TLS for network
transmission).

# CONFORMANCE

Conformance tests for this schema live in _zz-tests\_bats/_, specifically
_zz-tests\_bats/format\_ndjson.bats_. Tests use binary injection via
bats-emo (the *require\_bin TAP\_DANCER\_BIN tap-dancer* helper), so the
conformance suite runs against any implementation that produces this
schema, not only the reference *tap-dancer* implementation.

Covered requirements include:

- Document Format: record ordering (*plan* precedes *test* precedes
  *bailout* precedes *summary*).
- Document Format: a trailing *summary* record is always present, even
  for empty input.
- Plan Record: a leading TAP plan emits a first *plan* record whose
  *count* equals the plan, and *summary.plan_count* matches it.
- Plan Record: a trailing TAP plan emits no *plan* record.
- Test Record: all required fields present.
- Test Record: subtest nesting matches source structure.
- Test Record: *directive* shape (null, or *kind* and *reason*).
- Test Record: *diagnostic* parsed as an object, not a string.
- Test Record: Output Block body extraction (4-space indent stripped,
  line feeds preserved).
- Bailout Record: emitted when the source contains _Bail out!_.
- Summary Record: counts exclude subtests.
- Summary Record: *valid* reflects parse errors.

# COMPATIBILITY

This is the initial version of the schema. No backwards-compatibility
constraints apply to it. Future revisions MUST be backwards-compatible
according to the following rules:

- New fields MAY be added to any record type.
- Existing fields MUST NOT be removed.
- Existing fields' types MUST NOT change.
- New record types MAY be added; consumers MUST ignore record types they
  do not recognize.

Incompatible changes MUST be specified in a new revision that supersedes
this page.

# SEE ALSO

The TAP-14 specification and its amendments, distributed with this
repository:

- _tap-version-14-specification.md_ - the base TAP-14 grammar.
- _streamed-output-amendment.md_ - Output Block structure.
- _ansi-yaml-output-amendment.md_ - ANSI in YAML Output Blocks.
- _ansi-display-amendment.md_ - ANSI display hints.

Normative external references: RFC 2119 (requirement-level key words) and
RFC 8259 (the JSON data interchange format).

# HISTORY

This page supersedes the proposed RFC formerly maintained at
_docs/rfcs/0001-test-result-ndjson-schema.md_, which is retained only as
a redirect stub. The manpage is now the sole normative specification of
the wire format.
