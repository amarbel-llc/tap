package ndjson

import (
	"encoding/json"
	"testing"

	"github.com/amarbel-llc/tap/go/internal/0/diagnostic"
)

func TestTestRecordMarshalsRequiredFields(t *testing.T) {
	rec := TestRecord{
		Type:        "test",
		N:           3,
		Description: "parses negative numbers",
		OK:          false,
		Directive:   nil,
		Diagnostic:  nil,
		Output:      nil,
		Subtest:     nil,
		Line:        7,
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	required := []string{"type", "n", "description", "ok", "directive", "diagnostic", "output", "subtest", "line"}
	for _, f := range required {
		if _, ok := got[f]; !ok {
			t.Errorf("missing required field %q in %s", f, data)
		}
	}
	if got["type"] != "test" {
		t.Errorf("type = %v, want %q", got["type"], "test")
	}
	if got["ok"] != false {
		t.Errorf("ok = %v, want false", got["ok"])
	}
}

func TestSummaryRecordMarshalsRequiredFields(t *testing.T) {
	rec := SummaryRecord{
		Type:        "summary",
		Passed:      7,
		Failed:      3,
		Skipped:     0,
		Todo:        0,
		Total:       10,
		PlanCount:   10,
		Bailed:      false,
		Valid:       true,
		Diagnostics: []SummaryDiagnostic{},
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	required := []string{"type", "passed", "failed", "skipped", "todo", "total", "plan_count", "bailed", "valid", "diagnostics"}
	for _, f := range required {
		if _, ok := got[f]; !ok {
			t.Errorf("missing required field %q in %s", f, data)
		}
	}
}

func TestBailoutRecordMarshalsRequiredFields(t *testing.T) {
	rec := BailoutRecord{
		Type:    "bailout",
		Message: "database unreachable",
		Line:    42,
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	required := []string{"type", "message", "line"}
	for _, f := range required {
		if _, ok := got[f]; !ok {
			t.Errorf("missing required field %q in %s", f, data)
		}
	}
}

func TestAggregatorRoutesOkAndNotOk(t *testing.T) {
	events := []diagnostic.Event{
		{Type: diagnostic.EventVersion, Line: 1, Depth: 0},
		{Type: diagnostic.EventPlan, Line: 2, Depth: 0, Plan: &diagnostic.PlanResult{Count: 2}},
		{Type: diagnostic.EventTestPoint, Line: 3, Depth: 0, TestPoint: &diagnostic.TestPointResult{Number: 1, Description: "alpha", OK: true}},
		{Type: diagnostic.EventTestPoint, Line: 4, Depth: 0, TestPoint: &diagnostic.TestPointResult{Number: 2, Description: "beta", OK: false}},
	}

	agg := NewAggregator()
	for _, ev := range events {
		agg.Consume(ev)
	}
	out := agg.Finalize(nil, nil)

	if len(out.Records) != 2 {
		t.Fatalf("expected 2 test records, got %d", len(out.Records))
	}

	if out.Records[0].N != 1 || !out.Records[0].OK || out.Records[0].Description != "alpha" {
		t.Errorf("record 0 = %+v", out.Records[0])
	}
	if out.Records[1].N != 2 || out.Records[1].OK || out.Records[1].Description != "beta" {
		t.Errorf("record 1 = %+v", out.Records[1])
	}

	if out.Summary.Passed != 1 || out.Summary.Failed != 1 || out.Summary.Total != 2 || out.Summary.PlanCount != 2 {
		t.Errorf("summary = %+v", out.Summary)
	}
	if !out.Summary.Valid {
		t.Errorf("expected valid")
	}
}

func TestAggregatorAttachesYAMLDiagnostic(t *testing.T) {
	events := []diagnostic.Event{
		{Type: diagnostic.EventVersion, Line: 1, Depth: 0},
		{Type: diagnostic.EventPlan, Line: 2, Depth: 0, Plan: &diagnostic.PlanResult{Count: 1}},
		{Type: diagnostic.EventTestPoint, Line: 3, Depth: 0, TestPoint: &diagnostic.TestPointResult{Number: 1, Description: "fail", OK: false}},
		{Type: diagnostic.EventYAMLDiagnostic, Line: 6, Depth: 0, YAML: map[string]string{"message": "broken", "severity": "fail"}},
	}

	agg := NewAggregator()
	for _, ev := range events {
		agg.Consume(ev)
	}
	out := agg.Finalize(nil, nil)

	if len(out.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(out.Records))
	}
	got := out.Records[0].Diagnostic
	if got["message"] != "broken" {
		t.Errorf("diagnostic message = %q, want %q", got["message"], "broken")
	}
	if got["severity"] != "fail" {
		t.Errorf("diagnostic severity = %q", got["severity"])
	}
}

func TestAggregatorBuffersOutputBlock(t *testing.T) {
	events := []diagnostic.Event{
		{Type: diagnostic.EventVersion, Line: 1},
		{Type: diagnostic.EventOutputHeader, Line: 2, Depth: 0, OutputHeader: &diagnostic.OutputHeaderResult{Number: 1, Description: "build"}},
		{Type: diagnostic.EventOutputLine, Line: 3, Depth: 0, OutputLine: "compiling main.rs"},
		{Type: diagnostic.EventOutputLine, Line: 4, Depth: 0, OutputLine: "linking binary"},
		{Type: diagnostic.EventTestPoint, Line: 5, Depth: 0, TestPoint: &diagnostic.TestPointResult{Number: 1, Description: "build", OK: true}},
		{Type: diagnostic.EventPlan, Line: 6, Depth: 0, Plan: &diagnostic.PlanResult{Count: 1}},
	}

	agg := NewAggregator()
	for _, ev := range events {
		agg.Consume(ev)
	}
	out := agg.Finalize(nil, nil)

	if len(out.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(out.Records))
	}
	if out.Records[0].Output == nil {
		t.Fatal("expected output to be attached")
	}
	want := "compiling main.rs\nlinking binary\n"
	if *out.Records[0].Output != want {
		t.Errorf("output = %q, want %q", *out.Records[0].Output, want)
	}
}

func TestAggregatorEmbedsSubtest(t *testing.T) {
	events := []diagnostic.Event{
		{Type: diagnostic.EventVersion, Line: 1, Depth: 0},
		{Type: diagnostic.EventPlan, Line: 2, Depth: 0, Plan: &diagnostic.PlanResult{Count: 1}},
		// Subtest body at depth 1
		{Type: diagnostic.EventTestPoint, Line: 3, Depth: 1, TestPoint: &diagnostic.TestPointResult{Number: 1, Description: "child a", OK: true}},
		{Type: diagnostic.EventTestPoint, Line: 4, Depth: 1, TestPoint: &diagnostic.TestPointResult{Number: 2, Description: "child b", OK: false}},
		{Type: diagnostic.EventPlan, Line: 5, Depth: 1, Plan: &diagnostic.PlanResult{Count: 2}},
		// Parent test point at depth 0
		{Type: diagnostic.EventTestPoint, Line: 6, Depth: 0, TestPoint: &diagnostic.TestPointResult{Number: 1, Description: "parent", OK: false}},
	}

	agg := NewAggregator()
	for _, ev := range events {
		agg.Consume(ev)
	}
	out := agg.Finalize(nil, nil)

	if len(out.Records) != 1 {
		t.Fatalf("expected 1 top-level record, got %d", len(out.Records))
	}
	parent := out.Records[0]
	if parent.Description != "parent" || parent.OK {
		t.Errorf("parent record = %+v", parent)
	}
	if len(parent.Subtest) != 2 {
		t.Fatalf("expected 2 subtest children, got %d", len(parent.Subtest))
	}
	if parent.Subtest[0].Description != "child a" || !parent.Subtest[0].OK {
		t.Errorf("child 0 = %+v", parent.Subtest[0])
	}
	if parent.Subtest[1].Description != "child b" || parent.Subtest[1].OK {
		t.Errorf("child 1 = %+v", parent.Subtest[1])
	}
	if out.Summary.Total != 1 {
		t.Errorf("summary.Total = %d, want 1 (subtests do not count toward total)", out.Summary.Total)
	}
}
