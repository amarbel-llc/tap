//! Format-dispatching reporter: TAP-14 text for humans (tty),
//! tap-ndjson(7) records for programs (non-tty). One call-site code
//! path in consumers like `piggy health` and `ssh-agent-mux health`.

use std::io::{self, Write};

use serde_json::Value;

use crate::ndjson::NdjsonWriter;
use crate::{DiagDirective, TapWriter, TapWriterBuilder};

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

    pub fn skip_diag(
        &mut self,
        description: &str,
        reason: &str,
        diagnostics: &[(&str, Value)],
    ) -> io::Result<usize> {
        match self {
            Self::Tap(t) => t.test_point_directive_diag_values(
                DiagDirective::Skip,
                description,
                reason,
                diagnostics,
            ),
            Self::Ndjson(j) => j.skip_diag(description, reason, diagnostics),
        }
    }

    pub fn todo_diag(
        &mut self,
        description: &str,
        reason: &str,
        diagnostics: &[(&str, Value)],
    ) -> io::Result<usize> {
        match self {
            Self::Tap(t) => t.test_point_directive_diag_values(
                DiagDirective::Todo,
                description,
                reason,
                diagnostics,
            ),
            Self::Ndjson(j) => j.todo_diag(description, reason, diagnostics),
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
        drop(r);
        let out = String::from_utf8(buf).unwrap();
        assert!(out.starts_with("TAP version 14\n"));
        assert!(out.contains("1..4\n"));
        assert!(out.contains("  keys: 0\n"));
        // Bare "SKIP", not "# SKIP": auto color (NO_COLOR absent) wraps
        // the directive word in SGR codes after the "# ".
        assert!(out.contains("SKIP"));
        assert!(out.contains("# a note\n"));
    }

    #[test]
    fn non_tty_dispatches_to_ndjson() {
        let mut buf = Vec::new();
        let mut r = Reporter::auto(&mut buf, false).unwrap();
        run_checks(&mut r);
        assert!(r.has_failures());
        assert_eq!(r.count(), 4);
        drop(r);
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
        drop(r);
        let out = String::from_utf8(buf).unwrap();
        assert!(out.ends_with("1..1\n"));
    }

    #[test]
    fn bail_out_dispatches() {
        let mut buf = Vec::new();
        let mut r = Reporter::auto(&mut buf, false).unwrap();
        r.bail_out("gone").unwrap();
        r.finish().unwrap();
        drop(r);
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("\"type\":\"bailout\""));
        assert!(out.contains("\"bailed\":true"));
    }

    #[test]
    fn bail_out_dispatches_tap() {
        let mut buf = Vec::new();
        let mut r = Reporter::Tap(TapWriterBuilder::new(&mut buf).build().unwrap());
        r.bail_out("gone").unwrap();
        drop(r);
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("Bail out! gone\n"));
    }

    // Explicit-variant construction (the `--format` forcing path) with
    // TapWriterBuilder::new is env-independent: no color, no locale, so
    // the directive token can be asserted byte-precisely — unlike the
    // `auto` tty tests above, which must tolerate SGR wrapping.
    #[test]
    fn explicit_tap_variant_is_deterministic() {
        let mut buf = Vec::new();
        let mut r = Reporter::Tap(TapWriterBuilder::new(&mut buf).build().unwrap());
        r.skip("optional", "not supported").unwrap();
        r.finish().unwrap();
        drop(r);
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("ok 1 - optional # SKIP not supported\n"));
        assert!(out.ends_with("1..1\n"));
    }

    #[test]
    fn directive_diags_dispatch_to_ndjson() {
        let mut buf = Vec::new();
        let mut r = Reporter::auto(&mut buf, false).unwrap();
        r.skip_diag("s", "no reader", &[("readers", serde_json::json!(0))])
            .unwrap();
        r.todo_diag(
            "t",
            "unimplemented",
            &[("tracking", serde_json::json!("tap#37"))],
        )
        .unwrap();
        r.finish().unwrap();
        assert!(!r.has_failures());
        drop(r);
        let out = String::from_utf8(buf).unwrap();
        assert!(out.contains("\"kind\":\"skip\""));
        assert!(out.contains("\"diagnostic\":{\"readers\":0}"));
        assert!(out.contains("\"kind\":\"todo\""));
    }

    #[test]
    fn directive_diags_dispatch_to_tap() {
        let mut buf = Vec::new();
        let mut r = Reporter::auto(&mut buf, true).unwrap();
        r.skip_diag(
            "optional",
            "no reader",
            &[("readers", serde_json::json!(0))],
        )
        .unwrap();
        r.finish().unwrap();
        drop(r);
        let out = String::from_utf8(buf).unwrap();
        // Bare "SKIP": auto color may wrap the directive word in SGR codes.
        assert!(out.contains("SKIP"));
        assert!(out.contains("  readers: 0\n"));
    }
}
