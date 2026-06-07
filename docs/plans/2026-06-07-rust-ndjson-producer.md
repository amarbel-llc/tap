# Rust tap-ndjson(7) Producer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development to implement this plan task-by-task.

**Goal:** Add a tap-ndjson(7) direct-producer writer (`NdjsonWriter`) and a tty-dispatching `Reporter` enum facade to the Rust `tap-dancer` crate, so `piggy health` and `ssh-agent-mux health` emit TAP-14 text on a tty and ndjson otherwise from one code path.

**Architecture:** Two new modules in `rust/src/` (`ndjson.rs`, `reporter.rs`), purely additive next to the existing `TapWriter`. `NdjsonWriter` emits one spec-conforming JSON record per call (plan → test* → bailout? → summary) with `line: 0` ("no source line" — direct producers have no source TAP stream; the spec gains a clarifying sentence). `Reporter` wraps `TapWriter | NdjsonWriter` and exposes the shared flat-check method subset. Diagnostics are `&[(&str, serde_json::Value)]` so integers stay integers in ndjson and render bare in TAP YAML.

**Design doc:** `docs/plans/2026-06-07-rust-ndjson-producer-design.md` (approved).

**Tech Stack:** Rust (edition 2024), serde + serde_json (new deps), crane via nix (builds from git-tracked files only — always `git add rust/Cargo.lock` after dep changes).

**Rollback:** N/A — purely additive; revert the commits.

**Conventions for every task:**
- Test command is the paved path: `just test-rust` (runs `cargo test` in `rust/` inside the devshell).
- Commit messages end with the Clown sign-off:
  `Signed-off-by: Clown 0.3.10+bb6560d <https://github.com/amarbel-llc/clown/commit/bb6560dd30e00f9a8e16d720fcc60ab9f97c15c1>`
- Do NOT run full `just` before the merge task — the merge hook is the CI lane.

---

### Task 1: Add serde dependencies

**Files:**
- Modify: `rust/Cargo.toml`
- Modify (generated): `rust/Cargo.lock`

**Step 1: Add deps to `rust/Cargo.toml`**

Append to `[dependencies]`:

```toml
# NdjsonWriter (tap-ndjson(7) producer): record structs derive Serialize;
# diagnostics are serde_json::Value so ints stay ints on the wire.
serde = { version = "1", features = ["derive"] }
serde_json = "1"
```

**Step 2: Regenerate the lockfile**

Run: `just build-rust`
Expected: compiles cleanly; `rust/Cargo.lock` gains serde/serde_json entries.

**Step 3: Stage BOTH files and commit**

`git add rust/Cargo.toml rust/Cargo.lock` (lockfile staging is required for the nix/crane build — untracked/unstaged changes are invisible to `nix build`), then commit:

```
deps(rust): add serde + serde_json for the ndjson producer
```

---

### Task 2: NdjsonWriter core — record structs, ok/not_ok

**Files:**
- Create: `rust/src/ndjson.rs`
- Modify: `rust/src/lib.rs` (add `mod ndjson;` + `pub use ndjson::NdjsonWriter;` near the top)

**Step 1: Write failing tests**

In `rust/src/ndjson.rs` (tests at the bottom of the new file):

```rust
#[cfg(test)]
mod tests {
    use super::*;

    fn capture(f: impl FnOnce(&mut NdjsonWriter)) -> String {
        let mut buf = Vec::new();
        let mut w = NdjsonWriter::new(&mut buf);
        f(&mut w);
        String::from_utf8(buf).unwrap()
    }

    #[test]
    fn ok_record() {
        let out = capture(|w| {
            w.ok("socket answers").unwrap();
        });
        assert_eq!(
            out,
            "{\"type\":\"test\",\"n\":1,\"description\":\"socket answers\",\"ok\":true,\"directive\":null,\"diagnostic\":null,\"output\":null,\"subtest\":null,\"line\":0}\n"
        );
    }

    #[test]
    fn not_ok_record() {
        let out = capture(|w| {
            w.not_ok("config parses").unwrap();
        });
        assert!(out.contains("\"ok\":false"));
        assert!(out.contains("\"n\":1"));
    }

    #[test]
    fn counter_and_failures() {
        let mut buf = Vec::new();
        let mut w = NdjsonWriter::new(&mut buf);
        assert_eq!(w.count(), 0);
        assert!(!w.has_failures());
        w.ok("a").unwrap();
        w.not_ok("b").unwrap();
        assert_eq!(w.count(), 2);
        assert!(w.has_failures());
    }
}
```

**Step 2: Run tests, verify they fail**

Run: `just test-rust`
Expected: compile error (`NdjsonWriter` not defined).

**Step 3: Implement the module**

`rust/src/ndjson.rs`:

```rust
//! tap-ndjson(7) direct producer.
//!
//! Emits one spec-conforming NDJSON record per call, for producers that
//! generate test results directly rather than transforming a source TAP
//! stream. Direct producers have no source line numbers, so test and
//! bailout records carry `line: 0` ("no source line"), per the
//! tap-ndjson(7) clarification in doc/tap-ndjson.7.scd.

use std::io::{self, Write};

use serde::Serialize;
use serde_json::{Map, Value};

// Field order in these structs is the spec's SHOULD order; serde
// serializes in declaration order.

#[derive(Serialize)]
struct PlanRecord {
    #[serde(rename = "type")]
    type_: &'static str,
    count: usize,
}

#[derive(Serialize)]
struct Directive {
    kind: &'static str,
    reason: String,
}

#[derive(Serialize)]
struct TestRecord {
    #[serde(rename = "type")]
    type_: &'static str,
    n: usize,
    description: String,
    ok: bool,
    directive: Option<Directive>,
    diagnostic: Option<Map<String, Value>>,
    output: Option<String>,
    subtest: Option<Vec<TestRecord>>,
    line: usize,
}

#[derive(Serialize)]
struct BailoutRecord {
    #[serde(rename = "type")]
    type_: &'static str,
    message: String,
    line: usize,
}

#[derive(Serialize)]
struct SummaryRecord {
    #[serde(rename = "type")]
    type_: &'static str,
    passed: usize,
    failed: usize,
    skipped: usize,
    todo: usize,
    total: usize,
    plan_count: usize,
    bailed: bool,
    valid: bool,
    diagnostics: Vec<Value>,
}

fn invalid_input(msg: &str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidInput, msg.to_string())
}

pub struct NdjsonWriter<'a> {
    w: &'a mut dyn Write,
    counter: usize,
    passed: usize,
    failed: usize,
    skipped: usize,
    todo: usize,
    plan_count: usize,
    bailed: bool,
    finished: bool,
}

impl<'a> NdjsonWriter<'a> {
    pub fn new(w: &'a mut dyn Write) -> Self {
        Self {
            w,
            counter: 0,
            passed: 0,
            failed: 0,
            skipped: 0,
            todo: 0,
            plan_count: 0,
            bailed: false,
            finished: false,
        }
    }

    pub fn count(&self) -> usize {
        self.counter
    }

    pub fn has_failures(&self) -> bool {
        self.failed > 0
    }

    fn write_record<T: Serialize>(&mut self, record: &T) -> io::Result<()> {
        serde_json::to_writer(&mut *self.w, record).map_err(io::Error::from)?;
        self.w.write_all(b"\n")
    }

    fn check_open(&self) -> io::Result<()> {
        if self.finished {
            return Err(invalid_input("record after summary"));
        }
        Ok(())
    }

    fn test(
        &mut self,
        description: &str,
        ok: bool,
        directive: Option<Directive>,
        diagnostic: Option<Map<String, Value>>,
    ) -> io::Result<usize> {
        self.check_open()?;
        if self.bailed {
            return Err(invalid_input("test record after bailout"));
        }
        self.counter += 1;
        match &directive {
            Some(d) if d.kind == "skip" => self.skipped += 1,
            Some(_) => self.todo += 1,
            None if ok => self.passed += 1,
            None => self.failed += 1,
        }
        let record = TestRecord {
            type_: "test",
            n: self.counter,
            description: description.to_string(),
            ok,
            directive,
            diagnostic,
            output: None,
            subtest: None,
            line: 0,
        };
        self.write_record(&record)?;
        Ok(self.counter)
    }

    pub fn ok(&mut self, description: &str) -> io::Result<usize> {
        self.test(description, true, None, None)
    }

    pub fn not_ok(&mut self, description: &str) -> io::Result<usize> {
        self.test(description, false, None, None)
    }
}
```

In `rust/src/lib.rs`, after the existing `use` block at the top:

```rust
mod ndjson;
pub use ndjson::NdjsonWriter;
```

**Step 4: Run tests, verify they pass**

Run: `just test-rust`
Expected: all tests PASS (new + the existing ~90).

**Step 5: Commit**

```
feat(rust): NdjsonWriter core with ok/not_ok test records
```

---

### Task 3: Directives and diagnostics (skip/todo/ok_diag/not_ok_diag)

**Files:**
- Modify: `rust/src/ndjson.rs`

**Step 1: Write failing tests** (append inside `mod tests`)

```rust
    #[test]
    fn skip_directive_record() {
        let out = capture(|w| {
            w.skip("network test", "requires network").unwrap();
        });
        assert!(out.contains("\"directive\":{\"kind\":\"skip\",\"reason\":\"requires network\"}"));
        assert!(out.contains("\"ok\":true"));
    }

    #[test]
    fn todo_directive_record() {
        let out = capture(|w| {
            w.todo("future work", "not implemented").unwrap();
        });
        assert!(out.contains("\"directive\":{\"kind\":\"todo\",\"reason\":\"not implemented\"}"));
        assert!(out.contains("\"ok\":false"));
    }

    #[test]
    fn diagnostic_integers_stay_integers() {
        let out = capture(|w| {
            w.ok_diag("upstream answers", &[("keys", serde_json::json!(0))])
                .unwrap();
        });
        assert!(out.contains("\"diagnostic\":{\"keys\":0}"));
    }

    #[test]
    fn not_ok_diag_record() {
        let out = capture(|w| {
            w.not_ok_diag(
                "socket answers",
                &[("message", serde_json::json!("connection refused"))],
            )
            .unwrap();
        });
        assert!(out.contains("\"ok\":false"));
        assert!(out.contains("\"diagnostic\":{\"message\":\"connection refused\"}"));
    }

    #[test]
    fn empty_diag_slice_is_null() {
        let out = capture(|w| {
            w.not_ok_diag("broken", &[]).unwrap();
        });
        assert!(out.contains("\"diagnostic\":null"));
    }
```

**Step 2: Run tests, verify they fail**

Run: `just test-rust`
Expected: compile errors (methods not defined).

**Step 3: Implement** (append to `impl<'a> NdjsonWriter<'a>`, plus the free helper)

```rust
    pub fn ok_diag(
        &mut self,
        description: &str,
        diagnostics: &[(&str, Value)],
    ) -> io::Result<usize> {
        self.test(description, true, None, to_diag_map(diagnostics))
    }

    pub fn not_ok_diag(
        &mut self,
        description: &str,
        diagnostics: &[(&str, Value)],
    ) -> io::Result<usize> {
        self.test(description, false, None, to_diag_map(diagnostics))
    }

    pub fn skip(&mut self, description: &str, reason: &str) -> io::Result<usize> {
        self.test(
            description,
            true,
            Some(Directive {
                kind: "skip",
                reason: reason.to_string(),
            }),
            None,
        )
    }

    pub fn todo(&mut self, description: &str, reason: &str) -> io::Result<usize> {
        self.test(
            description,
            false,
            Some(Directive {
                kind: "todo",
                reason: reason.to_string(),
            }),
            None,
        )
    }
```

Free function at module level (next to `invalid_input`):

```rust
/// Empty slices map to `diagnostic: null`, mirroring TapWriter's
/// not_ok_diag, which emits no YAML block for an empty slice.
fn to_diag_map(diagnostics: &[(&str, Value)]) -> Option<Map<String, Value>> {
    if diagnostics.is_empty() {
        return None;
    }
    Some(
        diagnostics
            .iter()
            .map(|(k, v)| (k.to_string(), v.clone()))
            .collect(),
    )
}
```

**Step 4: Run tests, verify they pass**

Run: `just test-rust`
Expected: PASS.

**Step 5: Commit**

```
feat(rust): ndjson skip/todo directives and Value diagnostics
```

---

### Task 4: plan_ahead, bail_out, comment no-op, ordering errors

**Files:**
- Modify: `rust/src/ndjson.rs`

**Step 1: Write failing tests**

```rust
    #[test]
    fn plan_record_first() {
        let out = capture(|w| {
            w.plan_ahead(2).unwrap();
            w.ok("a").unwrap();
        });
        assert!(out.starts_with("{\"type\":\"plan\",\"count\":2}\n"));
    }

    #[test]
    fn plan_after_test_errors() {
        let mut buf = Vec::new();
        let mut w = NdjsonWriter::new(&mut buf);
        w.ok("a").unwrap();
        let err = w.plan_ahead(2).unwrap_err();
        assert_eq!(err.kind(), std::io::ErrorKind::InvalidInput);
    }

    #[test]
    fn bailout_record() {
        let out = capture(|w| {
            w.ok("a").unwrap();
            w.bail_out("database unreachable").unwrap();
        });
        assert!(out.contains(
            "{\"type\":\"bailout\",\"message\":\"database unreachable\",\"line\":0}\n"
        ));
    }

    #[test]
    fn test_after_bailout_errors() {
        let mut buf = Vec::new();
        let mut w = NdjsonWriter::new(&mut buf);
        w.bail_out("gone").unwrap();
        let err = w.ok("too late").unwrap_err();
        assert_eq!(err.kind(), std::io::ErrorKind::InvalidInput);
    }

    #[test]
    fn comment_is_silent() {
        let out = capture(|w| {
            w.comment("a note").unwrap();
        });
        assert_eq!(out, "");
    }
```

**Step 2: Run tests, verify they fail**

Run: `just test-rust`
Expected: compile errors (methods not defined).

**Step 3: Implement**

```rust
    pub fn plan_ahead(&mut self, n: usize) -> io::Result<()> {
        self.check_open()?;
        if self.counter > 0 {
            return Err(invalid_input("plan record after test records"));
        }
        if self.bailed {
            return Err(invalid_input("plan record after bailout"));
        }
        self.plan_count = n;
        self.write_record(&PlanRecord {
            type_: "plan",
            count: n,
        })
    }

    pub fn bail_out(&mut self, message: &str) -> io::Result<()> {
        self.check_open()?;
        if self.bailed {
            return Err(invalid_input("second bailout record"));
        }
        self.bailed = true;
        self.write_record(&BailoutRecord {
            type_: "bailout",
            message: message.to_string(),
            line: 0,
        })
    }

    pub fn comment(&mut self, _text: &str) -> io::Result<()> {
        // tap-ndjson(7) has no comment record and forbids producers
        // inventing record types; comments are display-only, dropped here.
        Ok(())
    }
```

**Step 4: Run tests, verify they pass**

Run: `just test-rust`
Expected: PASS.

**Step 5: Commit**

```
feat(rust): ndjson plan/bailout records with spec-ordering enforcement
```

---

### Task 5: finish() — the mandatory summary record

**Files:**
- Modify: `rust/src/ndjson.rs`

**Step 1: Write failing tests**

```rust
    #[test]
    fn summary_counts_directives_regardless_of_ok() {
        let out = capture(|w| {
            w.ok("p").unwrap();
            w.not_ok("f").unwrap();
            w.skip("s", "r").unwrap();
            w.todo("t", "r").unwrap();
            w.finish().unwrap();
        });
        let summary = out.lines().last().unwrap();
        assert_eq!(
            summary,
            "{\"type\":\"summary\",\"passed\":1,\"failed\":1,\"skipped\":1,\"todo\":1,\"total\":4,\"plan_count\":0,\"bailed\":false,\"valid\":true,\"diagnostics\":[]}"
        );
    }

    #[test]
    fn summary_reflects_plan_and_bailout() {
        let out = capture(|w| {
            w.plan_ahead(10).unwrap();
            w.ok("a").unwrap();
            w.bail_out("gone").unwrap();
            w.finish().unwrap();
        });
        let summary = out.lines().last().unwrap();
        assert!(summary.contains("\"plan_count\":10"));
        assert!(summary.contains("\"bailed\":true"));
    }

    #[test]
    fn empty_document_still_has_summary() {
        let out = capture(|w| {
            w.finish().unwrap();
        });
        assert!(out.contains("\"type\":\"summary\""));
        assert!(out.contains("\"total\":0"));
    }

    #[test]
    fn double_finish_errors() {
        let mut buf = Vec::new();
        let mut w = NdjsonWriter::new(&mut buf);
        w.finish().unwrap();
        let err = w.finish().unwrap_err();
        assert_eq!(err.kind(), std::io::ErrorKind::InvalidInput);
    }

    #[test]
    fn record_after_finish_errors() {
        let mut buf = Vec::new();
        let mut w = NdjsonWriter::new(&mut buf);
        w.finish().unwrap();
        let err = w.ok("too late").unwrap_err();
        assert_eq!(err.kind(), std::io::ErrorKind::InvalidInput);
    }

    #[test]
    fn document_record_ordering() {
        let out = capture(|w| {
            w.plan_ahead(1).unwrap();
            w.ok("a").unwrap();
            w.bail_out("gone").unwrap();
            w.finish().unwrap();
        });
        let types: Vec<&str> = out
            .lines()
            .map(|l| {
                if l.contains("\"type\":\"plan\"") {
                    "plan"
                } else if l.contains("\"type\":\"test\"") {
                    "test"
                } else if l.contains("\"type\":\"bailout\"") {
                    "bailout"
                } else {
                    "summary"
                }
            })
            .collect();
        assert_eq!(types, ["plan", "test", "bailout", "summary"]);
    }
```

**Step 2: Run tests, verify they fail**

Run: `just test-rust`
Expected: compile error (`finish` not defined).

**Step 3: Implement**

```rust
    /// Emit the mandatory trailing summary record. A direct producer has
    /// no parse diagnostics by construction, so `valid` is always true
    /// and `diagnostics` always empty.
    pub fn finish(&mut self) -> io::Result<()> {
        self.check_open()?;
        self.finished = true;
        let record = SummaryRecord {
            type_: "summary",
            passed: self.passed,
            failed: self.failed,
            skipped: self.skipped,
            todo: self.todo,
            total: self.passed + self.failed + self.skipped + self.todo,
            plan_count: self.plan_count,
            bailed: self.bailed,
            valid: true,
            diagnostics: Vec::new(),
        };
        self.write_record(&record)
    }
```

**Step 4: Run tests, verify they pass**

Run: `just test-rust`
Expected: PASS.

**Step 5: Commit**

```
feat(rust): ndjson summary record completes the producer
```

---

### Task 6: TapWriter Value-diagnostic rendering (crate-internal)

The facade needs the TAP side to render `serde_json::Value` diagnostics:
strings through the existing sanitizing/quoting path, numbers/bools bare
(like today's `exitcode: 1`). Crate-internal — the public TapWriter API
is unchanged.

**Files:**
- Modify: `rust/src/lib.rs`

**Step 1: Write failing tests** (append inside the existing `mod tests` in `lib.rs`)

```rust
    // --- Value-diagnostic rendering (used by the Reporter facade) ---

    #[test]
    fn diag_values_int_renders_bare() {
        let mut buf = Vec::new();
        let mut tw = TapWriterBuilder::new(&mut buf).build().unwrap();
        tw.test_point_diag_values(true, "upstream answers", &[("keys", serde_json::json!(0))])
            .unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("ok 1 - upstream answers\n"));
        assert!(out.contains("  keys: 0\n"));
        assert!(out.contains("  ---\n"));
        assert!(out.contains("  ...\n"));
    }

    #[test]
    fn diag_values_string_renders_quoted() {
        let mut buf = Vec::new();
        let mut tw = TapWriterBuilder::new(&mut buf).build().unwrap();
        tw.test_point_diag_values(
            false,
            "broken",
            &[("message", serde_json::json!("connection refused"))],
        )
        .unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("not ok 1 - broken\n"));
        assert!(out.contains("  message: \"connection refused\"\n"));
        assert!(tw.has_failures());
    }

    #[test]
    fn diag_values_empty_no_block() {
        let mut buf = Vec::new();
        let mut tw = TapWriterBuilder::new(&mut buf).build().unwrap();
        tw.test_point_diag_values(true, "plain", &[]).unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(!out.contains("---"));
    }
```

**Step 2: Run tests, verify they fail**

Run: `just test-rust`
Expected: compile error (`test_point_diag_values` not defined).

**Step 3: Implement**

Add `use serde_json::Value;` to the imports in `lib.rs`. Next to
`write_yaml_field`:

```rust
fn write_yaml_value_field(
    w: &mut (impl Write + ?Sized),
    key: &str,
    value: &Value,
    color: bool,
) -> io::Result<()> {
    match value {
        // Strings go through the existing sanitizing/quoting path.
        Value::String(s) => write_yaml_field(w, key, s, color),
        // Numbers, bools, and null render bare (like `exitcode: 1`);
        // arrays/objects render as compact JSON, valid YAML flow syntax.
        other => writeln!(w, "  {key}: {other}"),
    }
}
```

In `impl<'a> TapWriter<'a>` (next to `not_ok_diag`):

```rust
    /// Test point with serde_json::Value diagnostics. Crate-internal:
    /// the Reporter facade's TAP-side rendering of the shared
    /// `&[(&str, Value)]` diagnostic shape.
    pub(crate) fn test_point_diag_values(
        &mut self,
        ok: bool,
        desc: &str,
        diagnostics: &[(&str, Value)],
    ) -> io::Result<usize> {
        self.counter += 1;
        if !ok {
            self.failed = true;
        }
        let status = if ok {
            status_ok(self.config.color())
        } else {
            status_not_ok(self.config.color())
        };
        let num = self.config.format_number(self.counter);
        writeln!(self.w, "{} {} - {}", status, num, desc)?;
        if !diagnostics.is_empty() {
            writeln!(self.w, "  ---")?;
            for (key, value) in diagnostics {
                write_yaml_value_field(&mut *self.w, key, value, self.config.color())?;
            }
            writeln!(self.w, "  ...")?;
        }
        Ok(self.counter)
    }
```

**Step 4: Run tests, verify they pass**

Run: `just test-rust`
Expected: PASS.

**Step 5: Commit**

```
feat(rust): TapWriter Value-diagnostic rendering for the facade
```

---

### Task 7: Reporter enum facade

**Files:**
- Create: `rust/src/reporter.rs`
- Modify: `rust/src/lib.rs` (add `mod reporter;` + `pub use reporter::Reporter;`)

**Step 1: Write failing tests** (bottom of `rust/src/reporter.rs`)

```rust
#[cfg(test)]
mod tests {
    use super::*;

    fn run_checks(r: &mut Reporter) {
        r.plan_ahead(4).unwrap();
        r.ok("config parses").unwrap();
        r.ok_diag("upstream answers", &[("keys", serde_json::json!(0))])
            .unwrap();
        r.skip("optional", "not supported").unwrap();
        r.not_ok_diag("listener", &[("message", serde_json::json!("refused"))])
            .unwrap();
        r.comment("a note").unwrap();
        r.finish().unwrap();
    }

    #[test]
    fn tty_dispatches_to_tap() {
        let mut buf = Vec::new();
        let mut r = Reporter::auto(&mut buf, true).unwrap();
        run_checks(&mut r);
        assert!(r.has_failures());
        assert_eq!(r.count(), 4);
        let out = String::from_utf8(buf).unwrap();
        assert!(out.starts_with("TAP version 14\n"));
        assert!(out.contains("1..4\n"));
        assert!(out.contains("  keys: 0\n"));
        assert!(out.contains("# SKIP"));
        assert!(out.contains("# a note\n"));
    }

    #[test]
    fn non_tty_dispatches_to_ndjson() {
        let mut buf = Vec::new();
        let mut r = Reporter::auto(&mut buf, false).unwrap();
        run_checks(&mut r);
        assert!(r.has_failures());
        assert_eq!(r.count(), 4);
        let out = String::from_utf8(buf).unwrap();
        assert!(out.starts_with("{\"type\":\"plan\",\"count\":4}\n"));
        assert!(out.contains("\"diagnostic\":{\"keys\":0}"));
        assert!(out.lines().last().unwrap().contains("\"type\":\"summary\""));
        // Comment is dropped: 1 plan + 4 tests + 1 summary.
        assert_eq!(out.lines().count(), 6);
    }

    #[test]
    fn tap_finish_is_trailing_plan() {
        let mut buf = Vec::new();
        let mut r = Reporter::auto(&mut buf, true).unwrap();
        r.ok("only").unwrap();
        r.finish().unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.ends_with("1..1\n"));
    }

    #[test]
    fn bail_out_dispatches() {
        let mut buf = Vec::new();
        let mut r = Reporter::auto(&mut buf, false).unwrap();
        r.bail_out("gone").unwrap();
        r.finish().unwrap();
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("\"type\":\"bailout\""));
        assert!(out.contains("\"bailed\":true"));
    }
}
```

Note: `tty_dispatches_to_tap` must be robust against the ambient
`NO_COLOR`/locale env of the test runner — `Reporter::auto` on the tty
side uses `TapWriterBuilder::auto`, which reads env vars. The existing
lib.rs tests serialize env access with `ENV_MUTEX`; these facade tests
assert only on substrings that are stable across color on/off
(`keys: 0`, `# SKIP`, `1..4`) — `ok`/`not ok` tokens may carry SGR
wrappers, so do NOT assert on bare `ok 1 -` here.

**Step 2: Run tests, verify they fail**

Run: `just test-rust`
Expected: compile error (`Reporter` not defined).

**Step 3: Implement**

`rust/src/reporter.rs`:

```rust
//! Format-dispatching reporter: TAP-14 text for humans (tty),
//! tap-ndjson(7) records for programs (non-tty). One call-site code
//! path in consumers like `piggy health` and `ssh-agent-mux health`.

use std::io::{self, Write};

use serde_json::Value;

use crate::ndjson::NdjsonWriter;
use crate::{TapWriter, TapWriterBuilder};

pub enum Reporter<'a> {
    Tap(TapWriter<'a>),
    Ndjson(NdjsonWriter<'a>),
}

impl<'a> Reporter<'a> {
    /// Pick the output format from the caller-supplied tty-ness:
    /// `Reporter::auto(&mut out, std::io::stdout().is_terminal())`.
    /// The tty side gets TapWriterBuilder::auto defaults (NO_COLOR,
    /// locale); the crate never sniffs file descriptors itself.
    pub fn auto(w: &'a mut dyn Write, is_tty: bool) -> io::Result<Self> {
        if is_tty {
            Ok(Self::Tap(TapWriterBuilder::auto(w).build()?))
        } else {
            Ok(Self::Ndjson(NdjsonWriter::new(w)))
        }
    }

    pub fn plan_ahead(&mut self, n: usize) -> io::Result<()> {
        match self {
            Self::Tap(t) => t.plan_ahead(n),
            Self::Ndjson(j) => j.plan_ahead(n),
        }
    }

    pub fn ok(&mut self, description: &str) -> io::Result<usize> {
        match self {
            Self::Tap(t) => t.ok(description),
            Self::Ndjson(j) => j.ok(description),
        }
    }

    pub fn not_ok(&mut self, description: &str) -> io::Result<usize> {
        match self {
            Self::Tap(t) => t.not_ok(description),
            Self::Ndjson(j) => j.not_ok(description),
        }
    }

    pub fn ok_diag(
        &mut self,
        description: &str,
        diagnostics: &[(&str, Value)],
    ) -> io::Result<usize> {
        match self {
            Self::Tap(t) => t.test_point_diag_values(true, description, diagnostics),
            Self::Ndjson(j) => j.ok_diag(description, diagnostics),
        }
    }

    pub fn not_ok_diag(
        &mut self,
        description: &str,
        diagnostics: &[(&str, Value)],
    ) -> io::Result<usize> {
        match self {
            Self::Tap(t) => t.test_point_diag_values(false, description, diagnostics),
            Self::Ndjson(j) => j.not_ok_diag(description, diagnostics),
        }
    }

    pub fn skip(&mut self, description: &str, reason: &str) -> io::Result<usize> {
        match self {
            Self::Tap(t) => t.skip(description, reason),
            Self::Ndjson(j) => j.skip(description, reason),
        }
    }

    pub fn todo(&mut self, description: &str, reason: &str) -> io::Result<usize> {
        match self {
            Self::Tap(t) => t.todo(description, reason),
            Self::Ndjson(j) => j.todo(description, reason),
        }
    }

    pub fn comment(&mut self, text: &str) -> io::Result<()> {
        match self {
            Self::Tap(t) => t.comment(text),
            Self::Ndjson(j) => j.comment(text),
        }
    }

    pub fn bail_out(&mut self, message: &str) -> io::Result<()> {
        match self {
            Self::Tap(t) => t.bail_out(message),
            Self::Ndjson(j) => j.bail_out(message),
        }
    }

    /// Close the document: trailing `1..N` plan on the TAP side
    /// (idempotent), the mandatory summary record on the ndjson side.
    pub fn finish(&mut self) -> io::Result<()> {
        match self {
            Self::Tap(t) => t.plan(),
            Self::Ndjson(j) => j.finish(),
        }
    }

    pub fn count(&self) -> usize {
        match self {
            Self::Tap(t) => t.count(),
            Self::Ndjson(j) => j.count(),
        }
    }

    pub fn has_failures(&self) -> bool {
        match self {
            Self::Tap(t) => t.has_failures(),
            Self::Ndjson(j) => j.has_failures(),
        }
    }
}
```

In `rust/src/lib.rs` next to the ndjson module line:

```rust
mod reporter;
pub use reporter::Reporter;
```

**Step 4: Run tests, verify they pass**

Run: `just test-rust`
Expected: PASS.

**Step 5: Commit**

```
feat(rust): Reporter facade — tty→TAP text, non-tty→ndjson
```

---

### Task 8: tap-ndjson(7) clarification — line: 0 for direct producers

**Files:**
- Modify: `doc/tap-ndjson.7.scd`

**Step 1: Amend the TEST RECORD `line` field** (currently lines 141–143)

Replace:

```
*line* (integer)
	1-indexed line number in the source TAP stream where this test point
	appeared.
```

with:

```
*line* (integer)
	1-indexed line number in the source TAP stream where this test point
	appeared. Producers that emit records directly, without transforming
	a source TAP stream, MUST emit 0, meaning no source line exists.
	Consumers MUST treat a *line* of 0 as "no source position".
```

**Step 2: Amend the BAILOUT RECORD `line` field** (currently lines 187–189)

Replace:

```
*line* (integer)
	1-indexed line number of the _Bail out!_ directive in the source TAP
	stream.
```

with:

```
*line* (integer)
	1-indexed line number of the _Bail out!_ directive in the source TAP
	stream, or 0 when the producer has no source TAP stream (see the
	*line* field of the test record).
```

**Step 3: Verify the manpage still compiles**

Run: `just build-doc`
Expected: builds without scdoc errors.

**Step 4: Commit**

```
doc: tap-ndjson(7) — line is 0 for direct producers

Direct producers (e.g. the Rust NdjsonWriter) emit records without a
source TAP stream; 0 now officially means "no source line" instead of
being a private convention. Backwards-compatible under the page's
COMPATIBILITY rules (no type change, no field removal).
```

---

### Task 9: Merge (main session — not a subagent task)

1. Run the pre-merge skills against the full diff and attest:
   `simplify`, `review` (/code-review), `eng:loose-ends`, `eng:doc-drift`
   (check CLAUDE.md's overview line and README against the new public
   API), then `mcp__plugin_spinclass_spinclass__nothing-but-the-truth`.
2. Do NOT run full `just` first — the merge hook is the CI lane.
3. `mcp__plugin_spinclass_spinclass__merge-this-session` with
   `git_sync: true` (release needs origin/master to contain this work).

### Task 10: Release v0.1.12 (main session)

Run: `just release 0.1.12`

The recipe is self-contained: bumps `version.env` + `rust/Cargo.toml` +
`skills/tap14/SKILL.md`, runs `just test`, commits, creates signed
`v0.1.12` + `go/v0.1.12` tags, pushes master + tags, creates the GitHub
release. Pre-flights require a clean tree and HEAD ⊇ origin/master —
run it after the merge from the worktree branch (post-merge,
fast-forwarded master equals the branch tip). If tag signing fails on
gpg, STOP and ask the user to unlock piggy-agent — never retag
unsigned.

### Task 11: Hand off to the downstream sessions (main session)

ssh-agent-mux now has a spinclass peer session
(`ssh-agent-mux/noble-banyan`) — notify it directly via
`mcp__plugin_spinclass_spinclass__chat-send`. The piggy session
(`d078c0b7…`) is a plain Claude session with no chat path (unless a
piggy spinclass peer appears too) — report to the user for relaying.
Hand-off content for both:

- the tag: `v0.1.12`
- the dep line:
  `tap-dancer = { git = "https://github.com/amarbel-llc/tap", tag = "v0.1.12" }`
  (cargo finds the crate in the `rust/` subdir by package name)
- the call pattern:
  `Reporter::auto(&mut stdout, std::io::stdout().is_terminal())`, then
  `plan_ahead` / `ok` / `ok_diag` / `not_ok_diag` / `skip` / `bail_out`
  / `finish`, exit code from `has_failures()`
