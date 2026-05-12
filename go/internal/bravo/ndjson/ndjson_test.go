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
