package ndjson_test

import (
	"io"
	"strings"
	"testing"

	"github.com/amarbel-llc/tap/go/internal/bravo/ndjson"
	"github.com/amarbel-llc/tap/go/internal/bravo/reader"
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
	if out.Records[0].Diagnostic["message"] != "broken" {
		t.Errorf("diagnostic = %+v", out.Records[0].Diagnostic)
	}
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
