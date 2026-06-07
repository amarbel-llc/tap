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
}

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
}
