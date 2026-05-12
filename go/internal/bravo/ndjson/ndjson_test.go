package ndjson

import (
	"bytes"
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

func TestAggregatorRecordsBailout(t *testing.T) {
	events := []diagnostic.Event{
		{Type: diagnostic.EventVersion, Line: 1},
		{Type: diagnostic.EventPlan, Line: 2, Plan: &diagnostic.PlanResult{Count: 3}},
		{Type: diagnostic.EventTestPoint, Line: 3, Depth: 0, TestPoint: &diagnostic.TestPointResult{Number: 1, OK: true}},
		{Type: diagnostic.EventBailOut, Line: 4, Depth: 0, BailOut: &diagnostic.BailOutResult{Reason: "database unreachable"}},
	}

	agg := NewAggregator()
	for _, ev := range events {
		agg.Consume(ev)
	}
	out := agg.Finalize(nil, nil)

	if out.Bailout == nil {
		t.Fatal("expected bailout record")
	}
	if out.Bailout.Message != "database unreachable" || out.Bailout.Line != 4 {
		t.Errorf("bailout = %+v", out.Bailout)
	}
	if !out.Summary.Bailed {
		t.Error("expected summary.Bailed = true")
	}
}

func TestWriteAllUnified(t *testing.T) {
	rec1 := TestRecord{Type: "test", N: 1, Description: "a", OK: true, Line: 1}
	rec2 := TestRecord{Type: "test", N: 2, Description: "b", OK: false, Line: 2}
	out := Output{
		Records: []TestRecord{rec1, rec2},
		Summary: SummaryRecord{Type: "summary", Passed: 1, Failed: 1, Total: 2, Diagnostics: []SummaryDiagnostic{}},
	}

	var buf bytes.Buffer
	if err := WriteAll(&buf, out); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}

	lines := bytes.Split(bytes.TrimSuffix(buf.Bytes(), []byte("\n")), []byte("\n"))
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %s", len(lines), buf.String())
	}
}

func TestWriteSplitRoutesByOK(t *testing.T) {
	rec1 := TestRecord{Type: "test", N: 1, Description: "a", OK: true, Line: 1}
	rec2 := TestRecord{Type: "test", N: 2, Description: "b", OK: false, Line: 2}
	out := Output{
		Records: []TestRecord{rec1, rec2},
		Summary: SummaryRecord{Type: "summary", Passed: 1, Failed: 1, Total: 2, Diagnostics: []SummaryDiagnostic{}},
	}

	var failBuf, passBuf bytes.Buffer
	if err := WriteSplit(&failBuf, &passBuf, out); err != nil {
		t.Fatalf("WriteSplit: %v", err)
	}

	// Both streams contain the summary; failures gets rec2, passes gets rec1.
	if !bytes.Contains(failBuf.Bytes(), []byte("\"n\":2")) {
		t.Errorf("failure stream missing record 2: %s", failBuf.String())
	}
	if bytes.Contains(failBuf.Bytes(), []byte("\"n\":1")) {
		t.Errorf("failure stream wrongly includes record 1: %s", failBuf.String())
	}
	if !bytes.Contains(passBuf.Bytes(), []byte("\"n\":1")) {
		t.Errorf("pass stream missing record 1: %s", passBuf.String())
	}
	if bytes.Contains(passBuf.Bytes(), []byte("\"n\":2")) {
		t.Errorf("pass stream wrongly includes record 2: %s", passBuf.String())
	}

	// Both streams end with the summary.
	if !bytes.Contains(failBuf.Bytes(), []byte("\"type\":\"summary\"")) {
		t.Errorf("failure stream missing summary")
	}
	if !bytes.Contains(passBuf.Bytes(), []byte("\"type\":\"summary\"")) {
		t.Errorf("pass stream missing summary")
	}
}

func TestWriteSplitRoutesSkipToPassStream(t *testing.T) {
	// A `# SKIP` record has ok:true and a skip directive. It should go to passes.
	rec := TestRecord{
		Type:        "test",
		N:           1,
		Description: "skipped",
		OK:          true,
		Directive:   &DirectiveValue{Kind: "skip", Reason: "not implemented"},
		Line:        1,
	}
	out := Output{
		Records: []TestRecord{rec},
		Summary: SummaryRecord{Type: "summary", Skipped: 1, Total: 1, Diagnostics: []SummaryDiagnostic{}},
	}

	var failBuf, passBuf bytes.Buffer
	if err := WriteSplit(&failBuf, &passBuf, out); err != nil {
		t.Fatalf("WriteSplit: %v", err)
	}

	if !bytes.Contains(passBuf.Bytes(), []byte("\"n\":1")) {
		t.Errorf("pass stream missing skip record: %s", passBuf.String())
	}
	if bytes.Contains(failBuf.Bytes(), []byte("\"n\":1")) {
		t.Errorf("failure stream wrongly includes skip record: %s", failBuf.String())
	}
}

func TestWriteSplitRoutesTodoToPassStream(t *testing.T) {
	// A `# TODO` record typically has ok:false and a todo directive. It should
	// go to the pass stream (NOT the failure stream) — TODOs are not genuine
	// failures.
	rec := TestRecord{
		Type:        "test",
		N:           1,
		Description: "todo work",
		OK:          false,
		Directive:   &DirectiveValue{Kind: "todo", Reason: "not yet implemented"},
		Line:        1,
	}
	out := Output{
		Records: []TestRecord{rec},
		Summary: SummaryRecord{Type: "summary", Todo: 1, Total: 1, Diagnostics: []SummaryDiagnostic{}},
	}

	var failBuf, passBuf bytes.Buffer
	if err := WriteSplit(&failBuf, &passBuf, out); err != nil {
		t.Fatalf("WriteSplit: %v", err)
	}

	if !bytes.Contains(passBuf.Bytes(), []byte("\"n\":1")) {
		t.Errorf("pass stream missing todo record: %s", passBuf.String())
	}
	if bytes.Contains(failBuf.Bytes(), []byte("\"n\":1")) {
		t.Errorf("failure stream wrongly includes todo record: %s", failBuf.String())
	}
}

func TestWriteSplitRoutesFailureToFailStream(t *testing.T) {
	// A plain not-ok record with no directive is a genuine failure and goes
	// to the failure stream.
	rec := TestRecord{
		Type:        "test",
		N:           1,
		Description: "real failure",
		OK:          false,
		Directive:   nil,
		Line:        1,
	}
	out := Output{
		Records: []TestRecord{rec},
		Summary: SummaryRecord{Type: "summary", Failed: 1, Total: 1, Diagnostics: []SummaryDiagnostic{}},
	}

	var failBuf, passBuf bytes.Buffer
	if err := WriteSplit(&failBuf, &passBuf, out); err != nil {
		t.Fatalf("WriteSplit: %v", err)
	}

	if !bytes.Contains(failBuf.Bytes(), []byte("\"n\":1")) {
		t.Errorf("failure stream missing failure record: %s", failBuf.String())
	}
	if bytes.Contains(passBuf.Bytes(), []byte("\"n\":1")) {
		t.Errorf("pass stream wrongly includes failure record: %s", passBuf.String())
	}
}

func TestWriteSplitNilPassOut(t *testing.T) {
	rec1 := TestRecord{Type: "test", N: 1, OK: true, Line: 1}
	rec2 := TestRecord{Type: "test", N: 2, OK: false, Line: 2}
	out := Output{
		Records: []TestRecord{rec1, rec2},
		Summary: SummaryRecord{Type: "summary", Total: 2, Diagnostics: []SummaryDiagnostic{}},
	}

	var failBuf bytes.Buffer
	if err := WriteSplit(&failBuf, nil, out); err != nil {
		t.Fatalf("WriteSplit nil passOut: %v", err)
	}
	if bytes.Contains(failBuf.Bytes(), []byte("\"n\":1")) {
		t.Errorf("passing record leaked into failure stream: %s", failBuf.String())
	}
}
