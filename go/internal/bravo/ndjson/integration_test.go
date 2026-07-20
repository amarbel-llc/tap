package ndjson_test

import (
	"io"
	"strings"
	"testing"

	"code.linenisgreat.com/tap/go/internal/bravo/ndjson"
	"code.linenisgreat.com/tap/go/internal/bravo/reader"
)

func runReader(t *testing.T, input string) ndjson.Output {
	t.Helper()
	r := reader.NewReader(strings.NewReader(input))
	agg := ndjson.NewAggregator()
	for {
		ev, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reader error: %v", err)
		}
		agg.Consume(ev)
	}
	summary := r.Summary()
	return agg.Finalize(r.Diagnostics(), &summary)
}

func TestIntegrationMinimalTAP(t *testing.T) {
	out := runReader(t, "TAP version 14\n1..2\nok 1 - alpha\nnot ok 2 - beta\n")
	if len(out.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(out.Records))
	}
	if out.Summary.Passed != 1 || out.Summary.Failed != 1 {
		t.Errorf("summary = %+v", out.Summary)
	}
}

func TestIntegrationOutputBlock(t *testing.T) {
	input := "TAP version 14\n" +
		"# Output: 1 - build\n" +
		"    compiling main.rs\n" +
		"    linking binary\n" +
		"ok 1 - build\n" +
		"1..1\n"
	out := runReader(t, input)
	if len(out.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(out.Records))
	}
	if out.Records[0].Output == nil {
		t.Fatal("expected output attached")
	}
	want := "compiling main.rs\nlinking binary\n"
	if *out.Records[0].Output != want {
		t.Errorf("output = %q, want %q", *out.Records[0].Output, want)
	}
}

func TestIntegrationYAMLDiagnostic(t *testing.T) {
	input := "TAP version 14\n1..1\nnot ok 1 - fail\n  ---\n  message: broken\n  severity: fail\n  ...\n"
	out := runReader(t, input)
	if len(out.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(out.Records))
	}
	if msg, _ := out.Records[0].Diagnostic["message"].(string); msg != "broken" {
		t.Errorf("diagnostic = %+v", out.Records[0].Diagnostic)
	}
}

func TestIntegrationYAMLNestedAndTypedScalars(t *testing.T) {
	input := "TAP version 14\n1..1\nnot ok 1 - parser\n" +
		"  ---\n" +
		"  message: assertion failed\n" +
		"  severity: fail\n" +
		"  location:\n" +
		"    file: parser.rs\n" +
		"    line: 87\n" +
		"    column: 4\n" +
		"  expected: 42\n" +
		"  got: 41\n" +
		"  hints:\n" +
		"    - check rounding\n" +
		"    - rerun with --verbose\n" +
		"  ...\n"
	out := runReader(t, input)
	if len(out.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(out.Records))
	}
	diag := out.Records[0].Diagnostic
	if diag == nil {
		t.Fatal("expected diagnostic to be populated")
	}

	// Top-level scalar string
	if msg, _ := diag["message"].(string); msg != "assertion failed" {
		t.Errorf("message = %v, want %q", diag["message"], "assertion failed")
	}

	// Nested mapping must be a JSON object (map), not a stringified blob
	loc, ok := diag["location"].(map[string]any)
	if !ok {
		t.Fatalf("location should be a nested map, got %T: %v", diag["location"], diag["location"])
	}
	if f, _ := loc["file"].(string); f != "parser.rs" {
		t.Errorf("location.file = %v, want %q", loc["file"], "parser.rs")
	}
	if line, _ := toInt(loc["line"]); line != 87 {
		t.Errorf("location.line = %v (%T), want integer 87", loc["line"], loc["line"])
	}

	// Top-level typed integer scalars
	if got, _ := toInt(diag["expected"]); got != 42 {
		t.Errorf("expected = %v (%T), want integer 42", diag["expected"], diag["expected"])
	}
	if got, _ := toInt(diag["got"]); got != 41 {
		t.Errorf("got = %v (%T), want integer 41", diag["got"], diag["got"])
	}

	// Sequence must round-trip as a slice
	hints, ok := diag["hints"].([]any)
	if !ok {
		t.Fatalf("hints should be a slice, got %T: %v", diag["hints"], diag["hints"])
	}
	if len(hints) != 2 {
		t.Fatalf("hints length = %d, want 2", len(hints))
	}
	if h, _ := hints[0].(string); h != "check rounding" {
		t.Errorf("hints[0] = %v, want %q", hints[0], "check rounding")
	}
}

// toInt normalizes the various numeric types yaml.v3 may yield (int, int64,
// uint64) into a single int comparison. Callers must treat the bool result
// as "this value was a recognized integer."
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func TestIntegrationSubtest(t *testing.T) {
	input := "TAP version 14\n1..1\n" +
		"    # Subtest: child\n" +
		"    ok 1 - inner pass\n" +
		"    not ok 2 - inner fail\n" +
		"    1..2\n" +
		"not ok 1 - child\n"
	out := runReader(t, input)
	if len(out.Records) != 1 {
		t.Fatalf("expected 1 top-level, got %d", len(out.Records))
	}
	parent := out.Records[0]
	if parent.OK {
		t.Error("expected parent not ok")
	}
	if len(parent.Subtest) != 2 {
		t.Fatalf("expected 2 children, got %d", len(parent.Subtest))
	}
}

func TestIntegrationDeepSubtest(t *testing.T) {
	input := "TAP version 14\n1..1\n" +
		"    # Subtest: outer\n" +
		"        # Subtest: inner\n" +
		"        ok 1 - leaf\n" +
		"        1..1\n" +
		"    ok 1 - inner\n" +
		"    1..1\n" +
		"ok 1 - outer\n"
	out := runReader(t, input)
	if len(out.Records) != 1 {
		t.Fatalf("expected 1 top-level record, got %d", len(out.Records))
	}
	outer := out.Records[0]
	if outer.Description != "outer" {
		t.Errorf("outer description = %q, want %q", outer.Description, "outer")
	}
	if len(outer.Subtest) != 1 {
		t.Fatalf("expected outer.Subtest to have 1 child (inner), got %d: %+v", len(outer.Subtest), outer.Subtest)
	}
	inner := outer.Subtest[0]
	if inner.Description != "inner" {
		t.Errorf("inner description = %q, want %q", inner.Description, "inner")
	}
	if len(inner.Subtest) != 1 {
		t.Fatalf("expected inner.Subtest to have 1 child (leaf), got %d: %+v", len(inner.Subtest), inner.Subtest)
	}
	leaf := inner.Subtest[0]
	if leaf.Description != "leaf" {
		t.Errorf("leaf description = %q, want %q", leaf.Description, "leaf")
	}
	if leaf.Subtest != nil {
		t.Errorf("expected leaf.Subtest to be nil, got %+v", leaf.Subtest)
	}
}

func TestIntegrationBailout(t *testing.T) {
	input := "TAP version 14\n1..3\nok 1 - first\nBail out! disk full\n"
	out := runReader(t, input)
	if out.Bailout == nil {
		t.Fatal("expected bailout record")
	}
	if !strings.Contains(out.Bailout.Message, "disk full") {
		t.Errorf("bailout message = %q", out.Bailout.Message)
	}
}

func TestIntegrationDirectives(t *testing.T) {
	input := "TAP version 14\n1..2\nok 1 - a # SKIP not needed\nnot ok 2 - b # TODO later\n"
	out := runReader(t, input)
	if len(out.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(out.Records))
	}
	if out.Records[0].Directive == nil || out.Records[0].Directive.Kind != "skip" {
		t.Errorf("record 0 directive = %+v", out.Records[0].Directive)
	}
	if out.Records[1].Directive == nil || out.Records[1].Directive.Kind != "todo" {
		t.Errorf("record 1 directive = %+v", out.Records[1].Directive)
	}
	if out.Summary.Skipped != 1 || out.Summary.Todo != 1 {
		t.Errorf("summary skipped=%d todo=%d", out.Summary.Skipped, out.Summary.Todo)
	}
}

func TestIntegrationMalformedTAP(t *testing.T) {
	// No version line → reader emits a version-required diagnostic.
	out := runReader(t, "1..1\nok 1 - test\n")

	if len(out.Summary.Diagnostics) == 0 {
		t.Error("expected at least one diagnostic")
	}
	if out.Summary.Valid {
		t.Error("expected Valid=false for malformed input")
	}
}

func TestIntegrationEmptyInput(t *testing.T) {
	out := runReader(t, "")
	if len(out.Records) != 0 {
		t.Errorf("expected 0 records, got %d", len(out.Records))
	}
	// Summary always emitted.
	if out.Summary.Type != "summary" {
		t.Errorf("summary not emitted")
	}
}

func TestIntegrationLeadingPlanEmitsPlanRecord(t *testing.T) {
	// Plan announced up front (before any test point).
	out := runReader(t, "TAP version 14\n1..2\nok 1 - a\nok 2 - b\n")
	if out.Plan == nil {
		t.Fatal("expected a leading plan record")
	}
	if out.Plan.Type != "plan" || out.Plan.Count != 2 {
		t.Errorf("plan = %+v, want {plan 2}", out.Plan)
	}
	if out.Summary.PlanCount != out.Plan.Count {
		t.Errorf("summary.PlanCount = %d, want %d", out.Summary.PlanCount, out.Plan.Count)
	}
}

func TestIntegrationTrailingPlanOmitsPlanRecord(t *testing.T) {
	// Plan reported at the end (after the test points): not an up-front
	// announcement, so no plan record — only summary.plan_count.
	out := runReader(t, "TAP version 14\nok 1 - a\nok 2 - b\n1..2\n")
	if out.Plan != nil {
		t.Errorf("expected no plan record for a trailing plan, got %+v", out.Plan)
	}
	if out.Summary.PlanCount != 2 {
		t.Errorf("summary.PlanCount = %d, want 2", out.Summary.PlanCount)
	}
}
