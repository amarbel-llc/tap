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

pub struct NdjsonWriter<'a> {
    w: &'a mut dyn Write,
    counter: usize,
    passed: usize,
    failed: usize,
    skipped: usize,
    todo: usize,
    plan_count: usize,
    plan_emitted: bool,
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
            plan_emitted: false,
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

    pub fn skip_diag(
        &mut self,
        description: &str,
        reason: &str,
        diagnostics: &[(&str, Value)],
    ) -> io::Result<usize> {
        self.test(
            description,
            true,
            Some(Directive {
                kind: "skip",
                reason: reason.to_string(),
            }),
            to_diag_map(diagnostics),
        )
    }

    pub fn todo_diag(
        &mut self,
        description: &str,
        reason: &str,
        diagnostics: &[(&str, Value)],
    ) -> io::Result<usize> {
        self.test(
            description,
            false,
            Some(Directive {
                kind: "todo",
                reason: reason.to_string(),
            }),
            to_diag_map(diagnostics),
        )
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

    pub fn plan_ahead(&mut self, n: usize) -> io::Result<()> {
        self.check_open()?;
        if self.counter > 0 {
            return Err(invalid_input("plan record after test records"));
        }
        if self.bailed {
            return Err(invalid_input("plan record after bailout"));
        }
        if self.plan_emitted {
            return Err(invalid_input("second plan record"));
        }
        self.plan_emitted = true;
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

    /// Emit the mandatory trailing summary record. A direct producer has
    /// no parse diagnostics by construction, so `valid` is always true
    /// and `diagnostics` always empty.
    ///
    /// Call this on every normal exit path. If the writer is dropped
    /// without it (early `?` return, panic), [`Drop`] closes the
    /// document best-effort instead — a bailout record (unless one was
    /// already emitted) followed by the summary, so the document stays
    /// spec-valid and is honestly marked incomplete via `bailed: true`.
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
}

/// A document MUST end with exactly one summary record (tap-ndjson(7)),
/// but a `?`-bubbled error or panic in the caller would otherwise drop
/// the writer summary-less. Closing best-effort here keeps the document
/// spec-valid and marks it incomplete the way the spec already provides
/// for: `bailed: true`. Write errors are ignored — Drop cannot
/// propagate them, and the document was abandoned anyway.
impl Drop for NdjsonWriter<'_> {
    fn drop(&mut self) {
        if self.finished {
            return;
        }
        if !self.bailed {
            let _ = self.bail_out("NdjsonWriter dropped without finish()");
        }
        let _ = self.finish();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Result-returning closure: a writer call without `?` (or an
    // explicit unwrap) is a compile error, not a silently dropped error.
    // The writer is scoped so its Drop (which may close the document)
    // releases the buffer borrow before the buffer is consumed.
    fn capture(f: impl FnOnce(&mut NdjsonWriter) -> io::Result<()>) -> String {
        let mut buf = Vec::new();
        {
            let mut w = NdjsonWriter::new(&mut buf);
            f(&mut w).unwrap();
        }
        String::from_utf8(buf).unwrap()
    }

    #[test]
    fn ok_record() {
        let out = capture(|w| {
            w.ok("socket answers")?;
            Ok(())
        });
        // First line only: Drop closes the unfinished document behind it.
        assert_eq!(
            out.lines().next().unwrap(),
            "{\"type\":\"test\",\"n\":1,\"description\":\"socket answers\",\"ok\":true,\"directive\":null,\"diagnostic\":null,\"output\":null,\"subtest\":null,\"line\":0}"
        );
    }

    #[test]
    fn not_ok_record() {
        let out = capture(|w| {
            w.not_ok("config parses")?;
            Ok(())
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

    #[test]
    fn skip_directive_record() {
        let out = capture(|w| {
            w.skip("network test", "requires network")?;
            Ok(())
        });
        assert!(out.contains("\"directive\":{\"kind\":\"skip\",\"reason\":\"requires network\"}"));
        assert!(out.contains("\"ok\":true"));
    }

    #[test]
    fn todo_directive_record() {
        let out = capture(|w| {
            w.todo("future work", "not implemented")?;
            Ok(())
        });
        assert!(out.contains("\"directive\":{\"kind\":\"todo\",\"reason\":\"not implemented\"}"));
        assert!(out.contains("\"ok\":false"));
    }

    #[test]
    fn skip_diag_record() {
        let out = capture(|w| {
            w.skip_diag(
                "pcsc probe",
                "no reader",
                &[("readers", serde_json::json!(0))],
            )?;
            Ok(())
        });
        assert!(out.contains("\"directive\":{\"kind\":\"skip\",\"reason\":\"no reader\"}"));
        assert!(out.contains("\"diagnostic\":{\"readers\":0}"));
        assert!(out.contains("\"ok\":true"));
    }

    #[test]
    fn todo_diag_record() {
        let out = capture(|w| {
            w.todo_diag(
                "ipv6 upstreams",
                "unimplemented",
                &[("tracking", serde_json::json!("tap#37"))],
            )?;
            Ok(())
        });
        assert!(out.contains("\"directive\":{\"kind\":\"todo\",\"reason\":\"unimplemented\"}"));
        assert!(out.contains("\"diagnostic\":{\"tracking\":\"tap#37\"}"));
        assert!(out.contains("\"ok\":false"));
    }

    #[test]
    fn directive_diag_records_count_as_directives() {
        let out = capture(|w| {
            w.skip_diag("s", "r", &[("k", serde_json::json!(1))])?;
            w.todo_diag("t", "r", &[])?;
            w.finish()
        });
        let summary = out.lines().last().unwrap();
        assert!(summary.contains("\"skipped\":1"));
        assert!(summary.contains("\"todo\":1"));
        assert!(summary.contains("\"failed\":0"));
        // Empty diag slice on a directive point still maps to null.
        assert!(out.contains("\"diagnostic\":null"));
    }

    #[test]
    fn diagnostic_integers_stay_integers() {
        let out = capture(|w| {
            w.ok_diag("upstream answers", &[("keys", serde_json::json!(0))])?;
            Ok(())
        });
        assert!(out.contains("\"diagnostic\":{\"keys\":0}"));
    }

    #[test]
    fn not_ok_diag_record() {
        let out = capture(|w| {
            w.not_ok_diag(
                "socket answers",
                &[("message", serde_json::json!("connection refused"))],
            )?;
            Ok(())
        });
        assert!(out.contains("\"ok\":false"));
        assert!(out.contains("\"diagnostic\":{\"message\":\"connection refused\"}"));
    }

    #[test]
    fn empty_diag_slice_is_null() {
        let out = capture(|w| {
            w.not_ok_diag("broken", &[])?;
            Ok(())
        });
        assert!(out.contains("\"diagnostic\":null"));
    }

    #[test]
    fn plan_record_first() {
        let out = capture(|w| {
            w.plan_ahead(2)?;
            w.ok("a")?;
            Ok(())
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
    fn second_plan_errors() {
        let mut buf = Vec::new();
        let mut w = NdjsonWriter::new(&mut buf);
        w.plan_ahead(2).unwrap();
        let err = w.plan_ahead(2).unwrap_err();
        assert_eq!(err.kind(), std::io::ErrorKind::InvalidInput);
    }

    #[test]
    fn bailout_record() {
        let out = capture(|w| {
            w.ok("a")?;
            w.bail_out("database unreachable")
        });
        assert!(
            out.contains(
                "{\"type\":\"bailout\",\"message\":\"database unreachable\",\"line\":0}\n"
            )
        );
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
        // Both captures end with the same Drop-emitted document closure;
        // equality proves comment() itself wrote nothing.
        let baseline = capture(|_| Ok(()));
        let out = capture(|w| w.comment("a note"));
        assert_eq!(out, baseline);
    }

    #[test]
    fn summary_counts_directives_regardless_of_ok() {
        let out = capture(|w| {
            w.ok("p")?;
            w.not_ok("f")?;
            w.skip("s", "r")?;
            w.todo("t", "r")?;
            w.finish()
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
            w.plan_ahead(10)?;
            w.ok("a")?;
            w.bail_out("gone")?;
            w.finish()
        });
        let summary = out.lines().last().unwrap();
        assert!(summary.contains("\"plan_count\":10"));
        assert!(summary.contains("\"bailed\":true"));
    }

    #[test]
    fn empty_document_still_has_summary() {
        let out = capture(|w| w.finish());
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
            w.plan_ahead(1)?;
            w.ok("a")?;
            w.bail_out("gone")?;
            w.finish()
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

    #[test]
    fn drop_unfinished_closes_document() {
        let mut buf = Vec::new();
        {
            let mut w = NdjsonWriter::new(&mut buf);
            w.ok("a").unwrap();
        }
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("\"type\":\"bailout\""));
        assert!(out.contains("dropped without finish"));
        let last = out.lines().last().unwrap();
        assert!(last.contains("\"type\":\"summary\""));
        assert!(last.contains("\"bailed\":true"));
    }

    #[test]
    fn drop_after_finish_emits_nothing_extra() {
        let mut buf = Vec::new();
        {
            let mut w = NdjsonWriter::new(&mut buf);
            w.ok("a").unwrap();
            w.finish().unwrap();
        }
        let out = String::from_utf8(buf).unwrap();
        assert_eq!(out.matches("\"type\":\"summary\"").count(), 1);
        assert!(!out.contains("\"type\":\"bailout\""));
    }

    #[test]
    fn drop_after_explicit_bailout_adds_only_summary() {
        let mut buf = Vec::new();
        {
            let mut w = NdjsonWriter::new(&mut buf);
            w.ok("a").unwrap();
            w.bail_out("card gone").unwrap();
        }
        let out = String::from_utf8(buf).unwrap();
        // The caller's bailout stands; Drop adds only the summary.
        assert_eq!(out.matches("\"type\":\"bailout\"").count(), 1);
        assert!(out.contains("card gone"));
        let last = out.lines().last().unwrap();
        assert!(last.contains("\"type\":\"summary\""));
        assert!(last.contains("\"bailed\":true"));
    }
}
