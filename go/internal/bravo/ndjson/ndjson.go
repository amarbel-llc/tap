// Package ndjson aggregates TAP-14 reader events into NDJSON records
// conforming to docs/rfcs/0001-test-result-ndjson-schema.md.
package ndjson

import "github.com/amarbel-llc/tap/go/internal/0/diagnostic"

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

// Aggregator builds NDJSON records from reader events.
//
// Call Consume for each event in order. Call Finalize once after
// the stream is exhausted (typically at io.EOF from the reader).
//
// v1 limitation: subtests deeper than depth 1 collapse into the
// depth-1 children slice; a follow-up issue tracks proper recursion.
type Aggregator struct {
	records         []TestRecord
	planCount       int
	bailed          bool
	bailout         *BailoutRecord
	pendingOutput   *string      // accumulating Output Block body for the next top-level test point
	pendingChildren []TestRecord // children seen at depth > 0 before parent test point arrives
}

// Output is the result of finalizing an aggregator.
type Output struct {
	Records []TestRecord
	Bailout *BailoutRecord
	Summary SummaryRecord
}

// NewAggregator constructs an empty Aggregator ready to consume events.
func NewAggregator() *Aggregator {
	return &Aggregator{}
}

// Consume feeds a single reader event into the aggregator.
func (a *Aggregator) Consume(ev diagnostic.Event) {
	switch ev.Type {
	case diagnostic.EventPlan:
		if ev.Depth == 0 && ev.Plan != nil {
			a.planCount = ev.Plan.Count
		}
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
	case diagnostic.EventYAMLDiagnostic:
		if ev.Depth == 0 && len(a.records) > 0 {
			a.records[len(a.records)-1].Diagnostic = copyMap(ev.YAML)
		}
	case diagnostic.EventOutputHeader:
		if ev.Depth == 0 {
			empty := ""
			a.pendingOutput = &empty
		}
	case diagnostic.EventOutputLine:
		if ev.Depth == 0 && a.pendingOutput != nil {
			*a.pendingOutput += ev.OutputLine + "\n"
		}
	}
}

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

// Finalize computes the aggregate summary and returns the full Output.
// readerDiags and readerSummary come from reader.Reader's Diagnostics()
// and Summary() methods; both may be nil for synthetic streams.
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
