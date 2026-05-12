package ndjson

import (
	"encoding/json"
	"testing"
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
