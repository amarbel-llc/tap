# `tap-dancer format-ndjson` Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development to implement this plan task-by-task.

**Goal:** Add a `tap-dancer format-ndjson` subcommand that reads TAP-14 from stdin and emits NDJSON records (one per top-level test point plus a trailing summary), with an optional `--split` mode that routes passes to a side file so agents can read failures end-to-end without filtering.

**Architecture:** A new aggregator package consumes the existing `reader.Reader`'s event stream and builds one in-flight top-level test record at a time, embedding subtests as nested arrays. Output Blocks and YAML diagnostics are buffered onto the record before routing by `ok` verdict. The CLI subcommand and an MCP tool both delegate to the aggregator.

**Tech Stack:** Go 1.26, `encoding/json` (stdlib), existing `reader.Reader` event API, dagnabit code generation for the `pkgs/` façade, `bats-emo` (`require_bin`) for hermetic CLI conformance tests.

**Rollback:** Purely additive — new subcommand, new package, new bats file. Rollback is `git revert` of the implementation commits. No dual-architecture period needed.

**Design and spec:**
- Design: [`docs/plans/2026-05-12-tap-format-ndjson-design.md`](2026-05-12-tap-format-ndjson-design.md)
- RFC: [`docs/rfcs/0001-test-result-ndjson-schema.md`](../rfcs/0001-test-result-ndjson-schema.md)

**Known limitation acknowledged up front:** The existing `reader.Reader` exposes YAML diagnostic blocks as `map[string]string` (flat top-level key-value, no nesting, no scalar typing). For v1 of `format-ndjson` we emit YAML diagnostics as a flat JSON object with string values. The RFC permits this — every scalar is a JSON string. A follow-up issue (filed in Task 11) tracks upgrading to a real YAML parser for nested mappings and typed scalars.

---

## Task 0: Pre-flight check

**Files:** none (read-only).

**Step 1: Confirm we are inside the spinclass worktree**

Run: `pwd`
Expected: `/home/sasha/eng/repos/tap/.worktrees/fresh-sequoia`

**Step 2: Confirm working tree is clean**

Run: `git status --short`
Expected: empty output.

**Step 3: Confirm Go test suite is green at HEAD**

Run: `just test-go`
Expected: PASS for all packages.

**Step 4: Confirm bats lane is green at HEAD**

Run: `just test-bats`
Expected: all tests PASS.

If any step fails, stop and ask the user before continuing.

---

## Task 1: Add record types in `internal/bravo/ndjson`

**Promotion criteria:** N/A — new package.

**Files:**
- Create: `go/internal/bravo/ndjson/ndjson.go`
- Create: `go/internal/bravo/ndjson/ndjson_test.go`

**Step 1: Write the failing test**

Create `go/internal/bravo/ndjson/ndjson_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `cd go && go test ./internal/bravo/ndjson/`
Expected: build failure — `undefined: TestRecord` (and the other types). This proves the test compiles against nothing yet.

**Step 3: Write the record types**

Create `go/internal/bravo/ndjson/ndjson.go`:

```go
// Package ndjson aggregates TAP-14 reader events into NDJSON records
// conforming to docs/rfcs/0001-test-result-ndjson-schema.md.
package ndjson

//go:generate dagnabit export

// TestRecord is one top-level test point with its full context.
// All fields are emitted unconditionally; nullable fields use
// pointers/slices so they serialize as JSON null when absent.
type TestRecord struct {
	Type        string            `json:"type"`
	N           int               `json:"n"`
	Description string            `json:"description"`
	OK          bool              `json:"ok"`
	Directive   *DirectiveValue   `json:"directive"`
	Diagnostic  map[string]string `json:"diagnostic"`
	Output      *string           `json:"output"`
	Subtest     []TestRecord      `json:"subtest"`
	Line        int               `json:"line"`
}

// DirectiveValue is the parsed test point directive.
type DirectiveValue struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

// BailoutRecord indicates the source stream emitted `Bail out!`.
type BailoutRecord struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Line    int    `json:"line"`
}

// SummaryRecord reports aggregate counts and validity.
type SummaryRecord struct {
	Type        string              `json:"type"`
	Passed      int                 `json:"passed"`
	Failed      int                 `json:"failed"`
	Skipped     int                 `json:"skipped"`
	Todo        int                 `json:"todo"`
	Total       int                 `json:"total"`
	PlanCount   int                 `json:"plan_count"`
	Bailed      bool                `json:"bailed"`
	Valid       bool                `json:"valid"`
	Diagnostics []SummaryDiagnostic `json:"diagnostics"`
}

// SummaryDiagnostic is one entry in SummaryRecord.Diagnostics.
type SummaryDiagnostic struct {
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Message  string `json:"message"`
}
```

Note: `Subtest []TestRecord` serializes as JSON `null` when nil, and as `[]` when empty-but-non-nil. RFC requires `null` for "no subtest", which matches Go's nil slice behavior under `encoding/json`. Same for `Diagnostic map[string]string` (nil → `null`).

**Step 4: Run tests to verify they pass**

Run: `cd go && go test ./internal/bravo/ndjson/ -v`
Expected: all three tests PASS.

**Step 5: Format**

Run: `cd go && gofumpt -w internal/bravo/ndjson/`
Expected: no output.

**Step 6: Commit**

```bash
git add go/internal/bravo/ndjson/
git commit -m "feat(ndjson): add record types for tap-dancer format-ndjson

Per RFC 0001 docs/rfcs/0001-test-result-ndjson-schema.md."
```

---

## Task 2: Aggregator skeleton with top-level test routing

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/bravo/ndjson/ndjson.go`
- Modify: `go/internal/bravo/ndjson/ndjson_test.go`

**Step 1: Write the failing test**

Append to `ndjson_test.go`:

```go
import (
	"github.com/amarbel-llc/tap/go/internal/0/diagnostic"
)

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
```

**Step 2: Run test to verify it fails**

Run: `cd go && go test ./internal/bravo/ndjson/`
Expected: build failure — `undefined: NewAggregator`.

**Step 3: Write the aggregator skeleton**

Append to `ndjson.go`:

```go
import "github.com/amarbel-llc/tap/go/internal/0/diagnostic"

// Aggregator builds NDJSON records from reader events.
//
// Call Consume for each event in order. Call Finalize once after
// the stream is exhausted (typically at io.EOF from the reader).
type Aggregator struct {
	records   []TestRecord
	planCount int
	bailed    bool
	bailout   *BailoutRecord
}

// Output is the result of finalizing an aggregator.
type Output struct {
	Records []TestRecord
	Bailout *BailoutRecord
	Summary SummaryRecord
}

func NewAggregator() *Aggregator {
	return &Aggregator{}
}

func (a *Aggregator) Consume(ev diagnostic.Event) {
	switch ev.Type {
	case diagnostic.EventPlan:
		if ev.Depth == 0 && ev.Plan != nil {
			a.planCount = ev.Plan.Count
		}
	case diagnostic.EventTestPoint:
		if ev.Depth == 0 && ev.TestPoint != nil {
			a.records = append(a.records, buildTestRecord(ev))
		}
	}
}

func (a *Aggregator) Finalize(readerDiags []diagnostic.Diagnostic, readerSummary *diagnostic.Summary) Output {
	summary := SummaryRecord{
		Type:        "summary",
		PlanCount:   a.planCount,
		Diagnostics: []SummaryDiagnostic{},
	}

	for _, r := range a.records {
		summary.Total++
		switch {
		case r.Directive != nil && r.Directive.Kind == "skip":
			summary.Skipped++
		case r.Directive != nil && r.Directive.Kind == "todo":
			summary.Todo++
		case r.OK:
			summary.Passed++
		default:
			summary.Failed++
		}
	}

	for _, d := range readerDiags {
		summary.Diagnostics = append(summary.Diagnostics, SummaryDiagnostic{
			Line:     d.Line,
			Severity: d.Severity.String(),
			Rule:     d.Rule,
			Message:  d.Message,
		})
	}

	summary.Bailed = a.bailed
	if readerSummary != nil {
		summary.Valid = readerSummary.Valid
	} else {
		summary.Valid = len(readerDiags) == 0
	}

	return Output{
		Records: a.records,
		Bailout: a.bailout,
		Summary: summary,
	}
}

func buildTestRecord(ev diagnostic.Event) TestRecord {
	tp := ev.TestPoint
	rec := TestRecord{
		Type:        "test",
		N:           tp.Number,
		Description: tp.Description,
		OK:          tp.OK,
		Line:        ev.Line,
	}
	switch tp.Directive {
	case diagnostic.DirectiveSkip:
		rec.Directive = &DirectiveValue{Kind: "skip", Reason: tp.Reason}
	case diagnostic.DirectiveTodo:
		rec.Directive = &DirectiveValue{Kind: "todo", Reason: tp.Reason}
	}
	return rec
}
```

**Step 4: Run tests to verify they pass**

Run: `cd go && go test ./internal/bravo/ndjson/ -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/bravo/ndjson/
git commit -m "feat(ndjson): aggregator skeleton with top-level test routing

Consumes reader.Reader events; emits test records and a summary.
Subtests, output blocks, diagnostics, and bailouts come next."
```

---

## Task 3: YAML diagnostic attachment

**Files:**
- Modify: `go/internal/bravo/ndjson/ndjson.go`
- Modify: `go/internal/bravo/ndjson/ndjson_test.go`

**Step 1: Write the failing test**

Append to `ndjson_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `cd go && go test ./internal/bravo/ndjson/ -run TestAggregatorAttachesYAMLDiagnostic`
Expected: FAIL — diagnostic field is nil.

**Step 3: Implement YAML attachment**

In `Aggregator`, add a case to `Consume`:

```go
case diagnostic.EventYAMLDiagnostic:
    if ev.Depth == 0 && len(a.records) > 0 {
        a.records[len(a.records)-1].Diagnostic = copyMap(ev.YAML)
    }
```

Add helper:

```go
func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
```

**Step 4: Run test to verify it passes**

Run: `cd go && go test ./internal/bravo/ndjson/ -run TestAggregatorAttachesYAMLDiagnostic`
Expected: PASS.

**Step 5: Run full package tests**

Run: `cd go && go test ./internal/bravo/ndjson/`
Expected: all PASS.

**Step 6: Commit**

```bash
git add go/internal/bravo/ndjson/
git commit -m "feat(ndjson): attach YAML diagnostic blocks to the previous record"
```

---

## Task 4: Output Block buffering

**Files:**
- Modify: `go/internal/bravo/ndjson/ndjson.go`
- Modify: `go/internal/bravo/ndjson/ndjson_test.go`

**Step 1: Write the failing test**

Append to `ndjson_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `cd go && go test ./internal/bravo/ndjson/ -run TestAggregatorBuffersOutputBlock`
Expected: FAIL — output is nil.

**Step 3: Implement Output Block buffering**

Add a field to `Aggregator`:

```go
type Aggregator struct {
	records       []TestRecord
	planCount     int
	bailed        bool
	bailout       *BailoutRecord
	pendingOutput *string // accumulating Output Block body for the next top-level test point
}
```

Add cases to `Consume`:

```go
case diagnostic.EventOutputHeader:
    if ev.Depth == 0 {
        empty := ""
        a.pendingOutput = &empty
    }
case diagnostic.EventOutputLine:
    if ev.Depth == 0 && a.pendingOutput != nil {
        *a.pendingOutput += ev.OutputLine + "\n"
    }
```

And modify the `EventTestPoint` case to attach pending output:

```go
case diagnostic.EventTestPoint:
    if ev.Depth == 0 && ev.TestPoint != nil {
        rec := buildTestRecord(ev)
        if a.pendingOutput != nil {
            rec.Output = a.pendingOutput
            a.pendingOutput = nil
        }
        a.records = append(a.records, rec)
    }
```

**Step 4: Run tests to verify they pass**

Run: `cd go && go test ./internal/bravo/ndjson/ -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/bravo/ndjson/
git commit -m "feat(ndjson): buffer Output Block body until correlated test point"
```

---

## Task 5: Subtest embedding

**Files:**
- Modify: `go/internal/bravo/ndjson/ndjson.go`
- Modify: `go/internal/bravo/ndjson/ndjson_test.go`

**Step 1: Write the failing test**

Append to `ndjson_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `cd go && go test ./internal/bravo/ndjson/ -run TestAggregatorEmbedsSubtest`
Expected: FAIL — no subtest array.

**Step 3: Implement subtest buffering**

Add a stack of in-progress child records to `Aggregator`:

```go
type Aggregator struct {
	records       []TestRecord
	planCount     int
	bailed        bool
	bailout       *BailoutRecord
	pendingOutput *string
	pendingChildren []TestRecord // children seen at depth > 0 before parent test point arrives
}
```

Update `Consume` to handle depth > 0 test points and the parent attachment:

```go
case diagnostic.EventTestPoint:
    if ev.TestPoint == nil {
        return
    }
    if ev.Depth > 0 {
        a.pendingChildren = append(a.pendingChildren, buildTestRecord(ev))
        return
    }
    rec := buildTestRecord(ev)
    if a.pendingOutput != nil {
        rec.Output = a.pendingOutput
        a.pendingOutput = nil
    }
    if len(a.pendingChildren) > 0 {
        rec.Subtest = a.pendingChildren
        a.pendingChildren = nil
    }
    a.records = append(a.records, rec)
```

Note: this v1 supports a single nesting level. Nested-nested subtests (depth 2+) collapse into the depth-1 collection. **Filed as a follow-up in Task 11** — the design says "subtests are shallow (1-2 levels) in practice" but proper recursion is correct behavior. For v1, document the limitation.

**Step 4: Run tests to verify they pass**

Run: `cd go && go test ./internal/bravo/ndjson/ -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/bravo/ndjson/
git commit -m "feat(ndjson): embed depth-1 subtests as nested arrays on parent"
```

---

## Task 6: Bailout and integration with reader

**Files:**
- Modify: `go/internal/bravo/ndjson/ndjson.go`
- Create: `go/internal/bravo/ndjson/integration_test.go`

**Step 1: Write the failing test (synthetic events)**

Append to `ndjson_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `cd go && go test ./internal/bravo/ndjson/ -run TestAggregatorRecordsBailout`
Expected: FAIL — bailout is nil.

**Step 3: Implement bailout handling**

Add a case to `Consume`:

```go
case diagnostic.EventBailOut:
    a.bailed = true
    msg := ""
    if ev.BailOut != nil {
        msg = ev.BailOut.Reason
    }
    a.bailout = &BailoutRecord{Type: "bailout", Message: msg, Line: ev.Line}
```

**Step 4: Run tests to verify they pass**

Run: `cd go && go test ./internal/bravo/ndjson/ -v`
Expected: PASS.

**Step 5: Write the integration test (real TAP input through reader)**

Create `go/internal/bravo/ndjson/integration_test.go`:

```go
package ndjson_test

import (
	"io"
	"strings"
	"testing"

	"github.com/amarbel-llc/tap/go/internal/0/diagnostic"
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

	_ = out // avoid unused if we go from here
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
```

Note the `package ndjson_test` — these are black-box tests against the public surface, isolated from the unit tests.

**Step 6: Run integration tests**

Run: `cd go && go test ./internal/bravo/ndjson/ -v`
Expected: all PASS.

**Step 7: Format and commit**

```bash
cd go && gofumpt -w internal/bravo/ndjson/
cd .. && git add go/internal/bravo/ndjson/
git commit -m "feat(ndjson): bailout handling + integration tests against reader"
```

---

## Task 7: NDJSON writer (encoder with split routing)

**Files:**
- Modify: `go/internal/bravo/ndjson/ndjson.go`
- Modify: `go/internal/bravo/ndjson/ndjson_test.go`

**Step 1: Write the failing test**

Append to `ndjson_test.go`:

```go
import (
	"bytes"
)

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
```

**Step 2: Run to verify failure**

Run: `cd go && go test ./internal/bravo/ndjson/ -run TestWrite`
Expected: build failure — `undefined: WriteAll`.

**Step 3: Implement writers**

Append to `ndjson.go`:

```go
import (
	"encoding/json"
	"io"
)

// WriteAll emits every record + bailout (if any) + summary to w as
// newline-delimited JSON.
func WriteAll(w io.Writer, out Output) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, rec := range out.Records {
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	if out.Bailout != nil {
		if err := enc.Encode(out.Bailout); err != nil {
			return err
		}
	}
	return enc.Encode(out.Summary)
}

// WriteSplit emits failures (and any bailout + summary) to failOut,
// and passes (+ bailout + summary) to passOut. passOut may be nil to
// discard passing records.
func WriteSplit(failOut, passOut io.Writer, out Output) error {
	failEnc := json.NewEncoder(failOut)
	failEnc.SetEscapeHTML(false)

	var passEnc *json.Encoder
	if passOut != nil {
		passEnc = json.NewEncoder(passOut)
		passEnc.SetEscapeHTML(false)
	}

	for _, rec := range out.Records {
		if rec.OK {
			if passEnc != nil {
				if err := passEnc.Encode(rec); err != nil {
					return err
				}
			}
		} else {
			if err := failEnc.Encode(rec); err != nil {
				return err
			}
		}
	}

	if out.Bailout != nil {
		if err := failEnc.Encode(out.Bailout); err != nil {
			return err
		}
		if passEnc != nil {
			if err := passEnc.Encode(out.Bailout); err != nil {
				return err
			}
		}
	}

	if err := failEnc.Encode(out.Summary); err != nil {
		return err
	}
	if passEnc != nil {
		if err := passEnc.Encode(out.Summary); err != nil {
			return err
		}
	}
	return nil
}
```

Note: `encoding/json` adds a trailing newline per `Encode` call, so each record becomes one line.

**Step 4: Run tests to verify they pass**

Run: `cd go && go test ./internal/bravo/ndjson/ -v`
Expected: PASS.

**Step 5: Commit**

```bash
cd go && gofumpt -w internal/bravo/ndjson/
cd .. && git add go/internal/bravo/ndjson/
git commit -m "feat(ndjson): WriteAll and WriteSplit encoders"
```

---

## Task 8: dagnabit-generated `pkgs/ndjson` façade

**Files:**
- Create: `go/pkgs/ndjson/main.go`

**Step 1: Check how existing façades are generated**

Run: `cd go && cat pkgs/reader/main.go`
Expected: a short file with a `// Code generated by dagnabit; DO NOT EDIT.` banner that re-exports types and constructors.

**Step 2: Generate the façade**

Run: `cd go && go generate ./internal/bravo/ndjson/`
Expected: `go/pkgs/ndjson/main.go` is created. Re-exports `Aggregator`, `Output`, `TestRecord`, `SummaryRecord`, `BailoutRecord`, `DirectiveValue`, `SummaryDiagnostic` types and `NewAggregator`, `WriteAll`, `WriteSplit` functions.

If `go generate` doesn't produce the file, fall back to writing it by hand:

```go
// Code generated by dagnabit; DO NOT EDIT.

package ndjson

import internal "github.com/amarbel-llc/tap/go/internal/bravo/ndjson"

type (
	Aggregator        = internal.Aggregator
	Output            = internal.Output
	TestRecord        = internal.TestRecord
	SummaryRecord     = internal.SummaryRecord
	BailoutRecord     = internal.BailoutRecord
	DirectiveValue    = internal.DirectiveValue
	SummaryDiagnostic = internal.SummaryDiagnostic
)

var (
	NewAggregator = internal.NewAggregator
	WriteAll      = internal.WriteAll
	WriteSplit    = internal.WriteSplit
)
```

**Step 3: Verify the façade builds**

Run: `cd go && go build ./pkgs/ndjson/`
Expected: no output (success).

**Step 4: Commit**

```bash
git add go/pkgs/ndjson/
git commit -m "feat(ndjson): generate pkgs/ndjson façade"
```

---

## Task 9: Wire `format-ndjson` subcommand into the CLI

**Files:**
- Modify: `go/cmd/tap-dancer/main.go`

**Step 1: Add the new subcommand registration**

In `registerCommands`, add after the `exec-parallel` registration:

```go
app.AddCommand(&command.Command{
    Name:            "format-ndjson",
    Description:     command.Description{Short: "Read TAP from stdin and emit NDJSON; --split routes failures to stdout and passes to a file"},
    PassthroughArgs: true,
    RunCLI:          handleFormatNDJSON,
})
```

Also add the new subcommand to the usage banner near the top of `main()`:

```go
fmt.Fprintf(os.Stderr, "  format-ndjson         Read TAP from stdin and emit NDJSON records\n")
```

**Step 2: Implement the CLI handler**

Append to `main.go`:

```go
import (
	"github.com/amarbel-llc/tap/go/pkgs/ndjson"
)

func handleFormatNDJSON(ctx context.Context, args json.RawMessage) error {
	var pt struct {
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(args, &pt); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}

	fs := flag.NewFlagSet("format-ndjson", flag.ContinueOnError)
	split := fs.Bool("split", false, "")
	passOut := fs.String("pass-out", "", "")
	if err := fs.Parse(pt.Args); err != nil {
		return err
	}

	if *passOut != "" && !*split {
		fmt.Fprintln(os.Stderr, "error: --pass-out requires --split")
		os.Exit(2)
	}

	r := reader.NewReader(os.Stdin)
	agg := ndjson.NewAggregator()
	for {
		ev, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading TAP: %v\n", err)
			os.Exit(2)
		}
		agg.Consume(ev)
	}
	summary := r.Summary()
	out := agg.Finalize(r.Diagnostics(), &summary)

	if !*split {
		if err := ndjson.WriteAll(os.Stdout, out); err != nil {
			fmt.Fprintf(os.Stderr, "error writing ndjson: %v\n", err)
			os.Exit(2)
		}
	} else {
		var passW io.Writer
		if *passOut != "" {
			f, err := os.Create(*passOut)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error opening --pass-out: %v\n", err)
				os.Exit(2)
			}
			defer f.Close()
			passW = f
		}
		if err := ndjson.WriteSplit(os.Stdout, passW, out); err != nil {
			fmt.Fprintf(os.Stderr, "error writing ndjson: %v\n", err)
			os.Exit(2)
		}
	}

	// Exit 1 if any failures or a bailout were seen.
	if out.Summary.Failed > 0 || out.Summary.Bailed {
		os.Exit(1)
	}
	return nil
}
```

**Step 3: Build the CLI and smoke-test by hand**

Run: `just build-cli`
Expected: `result/bin/tap-dancer` exists.

Run: `echo -e 'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n' | ./result/bin/tap-dancer format-ndjson`
Expected: three NDJSON lines on stdout. Process exit code is 1 (one failure). Verify with `echo $?`.

Run: `echo -e 'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n' | ./result/bin/tap-dancer format-ndjson --split --pass-out /tmp/passes.ndjson > /tmp/fails.ndjson; echo exit=$?`
Expected: `/tmp/fails.ndjson` contains the `not ok 2` record + summary; `/tmp/passes.ndjson` contains the `ok 1` record + summary; exit 1.

Inspect both:

```bash
cat /tmp/fails.ndjson | jq '.type'
cat /tmp/passes.ndjson | jq '.type'
```

**Step 4: Commit**

```bash
cd go && gofumpt -w cmd/tap-dancer/main.go
cd .. && git add go/cmd/tap-dancer/main.go
git commit -m "feat(cli): add format-ndjson subcommand

Reads TAP-14 from stdin and emits NDJSON records (one per top-level
test point plus a trailing summary). --split routes failures to
stdout and passing records to --pass-out, so agents can read
failures end-to-end without filtering."
```

---

## Task 10: Bats conformance tests

**Files:**
- Create: `zz-tests_bats/format_ndjson.bats`

**Step 1: Write the bats test file**

Create `zz-tests_bats/format_ndjson.bats`:

```bash
#! /usr/bin/env bats
# bats file_tags=format-ndjson

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
  tap_dancer="${TAP_DANCER_BIN:-tap-dancer}"
}

teardown() {
  teardown_test_home
}

function format_ndjson_emits_one_record_per_test { # @test
  local input=$'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n'
  run bash -c "echo '$input' | $tap_dancer format-ndjson"
  # 2 test records + 1 summary = 3 lines
  local count=$(echo "$output" | wc -l)
  [ "$count" -eq 3 ]
  # Last line is summary
  echo "$output" | tail -1 | jq -e '.type == "summary"'
}

function format_ndjson_exit_1_on_failures { # @test
  run bash -c "printf 'TAP version 14\n1..1\nnot ok 1 - fail\n' | $tap_dancer format-ndjson"
  [ "$status" -eq 1 ]
}

function format_ndjson_exit_0_on_all_pass { # @test
  run bash -c "printf 'TAP version 14\n1..1\nok 1 - pass\n' | $tap_dancer format-ndjson"
  [ "$status" -eq 0 ]
}

function format_ndjson_split_routes_by_verdict { # @test
  local passfile="$BATS_TEST_TMPDIR/pass.ndjson"
  local failfile="$BATS_TEST_TMPDIR/fail.ndjson"
  printf 'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n' \
    | "$tap_dancer" format-ndjson --split --pass-out "$passfile" > "$failfile" || true

  # Failure stream: 1 test (n=2) + summary
  run jq -s 'length' "$failfile"
  assert_output "2"
  run jq -r 'select(.type == "test") | .n' "$failfile"
  assert_output "2"

  # Pass stream: 1 test (n=1) + summary
  run jq -s 'length' "$passfile"
  assert_output "2"
  run jq -r 'select(.type == "test") | .n' "$passfile"
  assert_output "1"
}

function format_ndjson_split_without_pass_out_drops_passes { # @test
  run bash -c "printf 'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n' | $tap_dancer format-ndjson --split"
  # Should contain only the failing record + summary
  local count=$(echo "$output" | wc -l)
  [ "$count" -eq 2 ]
  echo "$output" | head -1 | jq -e '.type == "test" and .ok == false'
}

function format_ndjson_pass_out_without_split_fails { # @test
  run bash -c "printf 'TAP version 14\n1..1\nok 1 - a\n' | $tap_dancer format-ndjson --pass-out /tmp/x.ndjson"
  [ "$status" -eq 2 ]
  assert_output --partial "--pass-out requires --split"
}

function format_ndjson_attaches_yaml_diagnostic { # @test
  local input
  input=$'TAP version 14\n1..1\nnot ok 1 - fail\n  ---\n  message: broken\n  severity: fail\n  ...\n'
  local fails="$BATS_TEST_TMPDIR/fails.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson > "$fails" || true
  run jq -r 'select(.type == "test") | .diagnostic.message' "$fails"
  assert_output "broken"
}

function format_ndjson_embeds_subtest { # @test
  local input
  input=$'TAP version 14\n1..1\n    # Subtest: child\n    ok 1 - inner pass\n    not ok 2 - inner fail\n    1..2\nnot ok 1 - child\n'
  local fails="$BATS_TEST_TMPDIR/fails.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson > "$fails" || true
  run jq -r 'select(.type == "test") | .subtest | length' "$fails"
  assert_output "2"
}

function format_ndjson_attaches_output_block { # @test
  local input
  input=$'TAP version 14\n# Output: 1 - build\n    compiling main.rs\n    linking binary\nok 1 - build\n1..1\n'
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson > "$out_file"
  run jq -r 'select(.type == "test") | .output' "$out_file"
  assert_output --partial "compiling main.rs"
  assert_output --partial "linking binary"
}

function format_ndjson_emits_bailout_record { # @test
  local input=$'TAP version 14\n1..3\nok 1 - first\nBail out! disk full\n'
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson > "$out_file" || true
  run jq -r 'select(.type == "bailout") | .message' "$out_file"
  assert_output --partial "disk full"
}

function format_ndjson_summary_has_required_fields { # @test
  local input=$'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n'
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson > "$out_file" || true
  run jq -r 'select(.type == "summary") | [.passed, .failed, .total, .plan_count, .bailed, .valid] | @csv' "$out_file"
  assert_output "1,1,2,2,false,true"
}

function format_ndjson_empty_input_emits_summary_only { # @test
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  printf '' | "$tap_dancer" format-ndjson > "$out_file" || true
  local count=$(wc -l < "$out_file")
  [ "$count" -eq 1 ]
  run jq -r '.type' "$out_file"
  assert_output "summary"
}

function format_ndjson_produces_valid_ndjson_each_line { # @test
  local input=$'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n'
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson > "$out_file" || true
  # Each line MUST be a parseable JSON value
  while IFS= read -r line; do
    echo "$line" | jq -e '.type' > /dev/null || { echo "bad line: $line"; return 1; }
  done < "$out_file"
}
```

**Step 2: Run the bats suite locally**

Run: `just test-bats`
Expected: all `format_ndjson_*` tests PASS.

If `jq` isn't on `$PATH` in the bats devshell, stop and ask the user — that's a flake-input concern, not something to silently work around.

**Step 3: Run the hermetic bats lane**

Run: `just test-bats-tags format-ndjson`
Expected: lane builds and PASSes.

If the lane wasn't auto-discovered, run `nix flake show` and confirm `bats-format-ndjson` appears under `checks`.

**Step 4: Commit**

```bash
git add zz-tests_bats/format_ndjson.bats
git commit -m "test(bats): conformance suite for format-ndjson

Covers: record-per-test layout, split routing, exit codes, diagnostic
attachment, subtest embedding, output block buffering, bailout, summary
fields, empty input, NDJSON validity. Hermetic via batslanes."
```

---

## Task 11: File follow-up issues and run the full suite

**Files:** none (issue filing only).

**Step 1: File the nested-YAML follow-up**

Use the `eng:file-issue` skill (do not call gh by hand) to file an issue with these contents:

- **Title:** `format-ndjson: upgrade YAML diagnostic parsing to support nested mappings`
- **Body:** The current implementation emits TAP-14 YAML diagnostic blocks as a flat JSON object with string values, because `internal/bravo/reader` parses YAML line-by-line into `map[string]string`. The RFC `docs/rfcs/0001-test-result-ndjson-schema.md` permits this for v1 (every scalar is a string), but a proper YAML parser (e.g. `gopkg.in/yaml.v3`) would enable nested mappings and typed scalars (integers as JSON numbers, sequences as arrays). Touchpoints: `internal/bravo/reader/reader.go` YAML state, `internal/bravo/ndjson/ndjson.go` `Diagnostic` field type, RFC §"Diagnostic Parsing".

**Step 2: File the deep-subtest follow-up**

- **Title:** `format-ndjson: support arbitrarily-deep subtest nesting`
- **Body:** v1 of `format-ndjson` flattens subtest events deeper than depth 1 into the depth-1 `subtest` array on the immediate parent. In practice TAP-14 subtests are 1–2 levels deep, but a correct implementation should recurse using a stack of in-progress parent records. Touchpoints: `internal/bravo/ndjson/ndjson.go` `Aggregator.pendingChildren` (replace flat slice with a stack of `[]TestRecord` indexed by depth).

**Step 3: Run the full test matrix**

Run: `just test`
Expected: `test-go`, `test-rust`, `test-bats` all PASS.

**Step 4: Run lint**

Run: `just lint`
Expected: clean (no `go vet` or `cargo clippy` warnings).

**Step 5: Final commit (if anything moved)**

If `gofumpt`, `go.sum`, or `gomod2nix.toml` changed during the suite run, commit them now:

```bash
git status
git add <any-stragglers>
git commit -m "chore: formatting and dependency lock updates from format-ndjson work"
```

If nothing changed, skip this step.

---

## Task 12: Merge

**Step 1: Confirm clean tree and green tests**

Run: `git status --short && just test`
Expected: clean tree, all green.

**Step 2: Merge via spinclass**

Use the spinclass MCP tool:

```
mcp__plugin_spinclass_spinclass__merge-this-session with git_sync=true
```

This runs the configured pre-merge hook (`just build-nix`) and merges to `master`.

**Step 3: Confirm the merge landed**

Run: `git log --oneline -5 master`
Expected: the format-ndjson commits are at the top of master.

---

## Acceptance checklist

When the plan is complete, all of these should be true:

- [ ] `tap-dancer format-ndjson` is documented in `tap-dancer --help` output.
- [ ] `echo 'TAP version 14\n1..1\nok 1 - x\n' | tap-dancer format-ndjson` emits two NDJSON lines (test + summary) and exits 0.
- [ ] `--split` + `--pass-out FILE` routes failures to stdout and passes to FILE; both streams end with the same summary record.
- [ ] `--pass-out` without `--split` exits 2 with a usage error.
- [ ] `Bail out!` in the input yields a `{"type":"bailout"}` record in both streams and exit 1.
- [ ] Output Block bodies appear in the record's `output` field with the 4-space indent stripped.
- [ ] YAML diagnostics appear in the record's `diagnostic` field as a JSON object.
- [ ] Subtests appear in the parent record's `subtest` array.
- [ ] All bats conformance tests in `zz-tests_bats/format_ndjson.bats` pass under both `just test-bats` and `nix build .#bats-format-ndjson`.
- [ ] Go unit + integration tests pass under `just test-go`.
- [ ] Issues filed for nested-YAML and deep-subtest follow-ups.
