---
name: TAP-14 Output
description: This skill should be used when the user asks to "format output as TAP", "add TAP output", "produce TAP-14", "write TAP test results", "use tap-dancer", or mentions TAP-14, TAP version 14, TAP output format, TAP writer, or test result formatting.
version: 0.1.15
---

# TAP-14 Output with tap-dancer

This skill provides guidance for producing spec-compliant TAP version 14 output using the tap-dancer libraries. TAP-14 is the standard text-based protocol for communicating test results between test modules and harnesses.

## When to Use TAP-14

Use TAP-14 output when building:
- CLI tools that report step-by-step results (build systems, installers, validators)
- Test runners or harnesses
- Recipe executors (justfile runners, task runners)
- Any tool where structured pass/fail output is consumed by other programs

## Go Library

Module: `code.linenisgreat.com/tap/go`
Public package: `code.linenisgreat.com/tap/go/pkgs/writer` (other consumer-facing packages live alongside under `pkgs/`: `reader`, `replay`, `reformat`, `ndjson`, `gotest`, `cargotest`, `diagnostic`, `yaml_diagnostic`).

### Basic Usage

```go
import (
    "os"

    "code.linenisgreat.com/tap/go/pkgs/writer"
)

func main() {
    tw := writer.NewWriter(os.Stdout)  // Emits: TAP version 14
    tw.PlanAhead(3)                    // Emits: 1..3

    tw.Ok("database connected")        // Emits: ok 1 - database connected
    tw.Ok("schema validated")          // Emits: ok 2 - schema validated
    tw.NotOk("migration failed", map[string]string{
        "message":  "column 'name' already exists",
        "severity": "fail",
    })
    // Emits:
    // not ok 3 - migration failed
    //   ---
    //   message: column 'name' already exists
    //   severity: fail
    //   ...
}
```

### API Reference

| Method | Output | Returns |
|--------|--------|---------|
| `NewWriter(w)` | `TAP version 14` | `*Writer` |
| `NewColorWriter(w, color)` | `TAP version 14` (status tokens optionally ANSI-colored) | `*Writer` |
| `NewLocaleWriter(w, locale)` | `TAP version 14` + `pragma +locale-formatting:<locale>` | `*Writer` |
| `tw.Ok(desc)` | `ok N - desc` | test number |
| `tw.NotOk(desc, map[string]string)` | `not ok N - desc` + optional YAML block | test number |
| `tw.Skip(desc, reason)` | `ok N - desc # SKIP reason` | test number |
| `tw.Todo(desc, reason)` | `not ok N - desc # TODO reason` | test number |
| `tw.PlanAhead(n)` | `1..n` (before tests) | — |
| `tw.Plan()` | `1..n` (after tests, n = count; idempotent) | — |
| `tw.BailOut(format, args...)` | `Bail out! <formatted>` | — |
| `tw.Comment(format, args...)` | `# <formatted>` | — |
| `tw.Pragma(key, enabled)` | `pragma +key` / `pragma -key` | — |
| `tw.Subtest(format, args...)` | `# Subtest: <name>` + child writer (4-space indented) | `*Writer` |
| `tw.OutputBlock(desc, fn)` | streamed `# Output:` block + ok/not ok | test number |
| `tw.HasFailures()` | — | `true` if any `NotOk` emitted |

### YAML Diagnostics

Pass a `map[string]string` to `NotOk` for structured failure info. Keys are sorted alphabetically. Multiline values automatically use YAML block scalar (`|`) format:

```go
tw.NotOk("compile failed", map[string]string{
    "exitcode": "1",
    "message":  "syntax error on line 42",
    "output":   "error: unexpected token\n  at main.go:42:5",
})
```

Pass `nil` to omit the YAML block entirely.

### Trailing Plan

When the total test count is unknown upfront, emit the plan after all tests:

```go
tw := writer.NewWriter(os.Stdout)
tw.Ok("step one")
tw.Ok("step two")
tw.Plan()  // Emits: 1..2
```

## Rust Library

Crate: `tap-dancer`

### Basic Usage

```rust
use std::io;
use tap_dancer::TapWriterBuilder;

fn main() -> io::Result<()> {
    let stdout = io::stdout();
    let mut handle = stdout.lock();
    let mut tw = TapWriterBuilder::new(&mut handle).build()?;  // Emits: TAP version 14
    tw.plan_ahead(3)?;                                          // Emits: 1..3

    tw.ok("database connected")?;                               // Emits: ok 1 - database connected
    tw.ok("schema validated")?;                                 // Emits: ok 2 - schema validated
    tw.not_ok_diag("migration failed", &[
        ("message", "column 'name' already exists"),
        ("severity", "fail"),
    ])?;
    // Emits:
    // not ok 3 - migration failed
    //   ---
    //   message: column 'name' already exists
    //   severity: fail
    //   ...
    Ok(())
}
```

### API Reference

| Method | Output | Returns |
|--------|--------|---------|
| `TapWriterBuilder::new(w).build()?` | `TAP version 14` | `TapWriter` |
| `TapWriterBuilder::auto(w).build()?` | version + locale/color from env | `TapWriter` |
| `tw.ok(desc)?` | `ok N - desc` | test number |
| `tw.not_ok(desc)?` | `not ok N - desc` | test number |
| `tw.not_ok_diag(desc, &[(k, v), ...])?` | `not ok N - desc` + YAML block | test number |
| `tw.skip(desc, reason)?` | `ok N - desc # SKIP reason` | test number |
| `tw.todo(desc, reason)?` | `not ok N - desc # TODO reason` | test number |
| `tw.plan_ahead(n)?` | `1..n` (before tests) | — |
| `tw.plan()?` | `1..n` (after tests, n = count) | — |
| `tw.bail_out(reason)?` | `Bail out! reason` | — |
| `tw.comment(text)?` | `# text` | — |
| `tw.pragma(key, enabled)?` | `pragma +key` / `pragma -key` | — |
| `tw.count()` | — | current test count |
| `tw.has_failures()` | — | `true` if any `not_ok` emitted |

### Builder Options

`TapWriterBuilder` is chainable:

```rust
let mut tw = TapWriterBuilder::new(&mut w)
    .color(true)                // wrap status tokens in ANSI SGR
    .locale("de-DE".parse()?)   // BCP 47 locale for number formatting
    .tty_build_last_line(true)  // emit pragma +tty-build-last-line
    .build()?;
```

`TapWriterBuilder::auto(w)` is shorthand for `new(w).default_color().default_locale()`, which respects `NO_COLOR` and `LC_ALL` / `LC_NUMERIC` / `LANG`.

`build_without_printing()` produces a writer without emitting the version line, useful for testing or for stitching into an existing stream.

### YAML Diagnostics

Pass `&[(key, value), ...]` to `not_ok_diag` for structured failure info. Keys are emitted in the order given. Multiline values automatically use YAML block scalar (`|`) format:

```rust
tw.not_ok_diag("compile failed", &[
    ("exitcode", "1"),
    ("message",  "syntax error on line 42"),
    ("output",   "error: unexpected token\n  at main.go:42:5"),
])?;
```

Use `not_ok` (no `_diag`) to omit the YAML block entirely.

### Trailing Plan

When the total test count is unknown upfront, emit the plan after all tests:

```rust
let mut tw = TapWriterBuilder::new(&mut w).build()?;
tw.ok("step one")?;
tw.ok("step two")?;
tw.plan()?;  // Emits: 1..2
```

## TAP-14 Quick Reference

```
TAP version 14
1..N                          # Plan (before or after tests)
ok 1 - description            # Passing test
not ok 2 - description        # Failing test
  ---                         # YAML diagnostic start
  message: "error text"       # Diagnostic field
  severity: fail              # Severity indicator
  exitcode: 1                 # Exit code
  output: |                   # Multiline output
    line one
    line two
  ...                         # YAML diagnostic end
ok 3 - desc # SKIP reason    # Skipped test
not ok 4 - desc # TODO reason # Todo test
Bail out! reason              # Emergency halt
# comment text                # Comment
```

For the complete TAP-14 specification including subtests, pragmas, escaping, and parsing rules, see `references/tap14-spec.md`.
