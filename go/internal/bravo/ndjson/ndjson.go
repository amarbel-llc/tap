// Package ndjson aggregates TAP-14 reader events into NDJSON records
// conforming to the tap-ndjson(7) manpage (doc/tap-ndjson.7.scd).
package ndjson

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/amarbel-llc/tap/go/internal/0/diagnostic"
)

//go:generate dagnabit export

// PlanRecord announces, up front, how many top-level test points the
// producer intends to emit. It mirrors a leading TAP `1..N` plan line
// and, when present, is the first record of the document.
type PlanRecord struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// TestRecord is one top-level test point with its full context.
// All fields are emitted unconditionally; nullable fields use
// pointers/slices so they serialize as JSON null when absent.
type TestRecord struct {
	Type        string          `json:"type"`
	N           int             `json:"n"`
	Description string          `json:"description"`
	OK          bool            `json:"ok"`
	Directive   *DirectiveValue `json:"directive"`
	Diagnostic  map[string]any  `json:"diagnostic"`
	Output      *string         `json:"output"`
	Subtest     []TestRecord    `json:"subtest"`
	Line        int             `json:"line"`
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
// Subtests nest to arbitrary depth. When a test point arrives at
// depth N, any pending children at depth N+1 are embedded as that
// record's Subtest array. Records at depth > 0 are themselves
// buffered as pending children of their eventual parent at depth-1.
type Aggregator struct {
	records   []TestRecord
	planCount int
	// planLeading is set when a top-level plan line arrived before any
	// top-level test point, i.e. the producer announced its plan up
	// front. Only then is a leading `plan` record emitted.
	planLeading bool
	bailed      bool
	bailout     *BailoutRecord
	// Accumulating Output Block body for the next top-level test point.
	pendingOutput *string
	// Children buffered at their own depth, keyed by that depth. The
	// parent at depth-1 picks them up when it arrives. Initialized
	// lazily on first append.
	pendingChildren map[int][]TestRecord
}

// Output is the result of finalizing an aggregator.
type Output struct {
	Plan    *PlanRecord
	Records []TestRecord
	Bailout *BailoutRecord
	Summary SummaryRecord
}

// NewAggregator constructs an empty Aggregator ready to consume events.
func NewAggregator() *Aggregator {
	return &Aggregator{
		pendingChildren: map[int][]TestRecord{},
	}
}

// Consume feeds a single reader event into the aggregator.
func (a *Aggregator) Consume(ev diagnostic.Event) {
	switch ev.Type {
	case diagnostic.EventPlan:
		if ev.Depth == 0 && ev.Plan != nil {
			a.planCount = ev.Plan.Count
			a.planLeading = len(a.records) == 0
		}
	case diagnostic.EventTestPoint:
		if ev.TestPoint == nil {
			return
		}
		rec := buildTestRecord(ev)
		if kids := a.pendingChildren[ev.Depth+1]; len(kids) > 0 {
			rec.Subtest = kids
			delete(a.pendingChildren, ev.Depth+1)
		}
		if ev.Depth == 0 {
			if a.pendingOutput != nil {
				rec.Output = a.pendingOutput
				a.pendingOutput = nil
			}
			a.records = append(a.records, rec)
		} else {
			a.pendingChildren[ev.Depth] = append(a.pendingChildren[ev.Depth], rec)
		}
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
	case diagnostic.EventBailOut:
		a.bailed = true
		msg := ""
		if ev.BailOut != nil {
			msg = ev.BailOut.Reason
		}
		a.bailout = &BailoutRecord{Type: "bailout", Message: msg, Line: ev.Line}
	}
}

func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
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

	// Orphan subtest children: any depth>0 records still in
	// pendingChildren at finalization were never claimed by a parent
	// test point. Surface them as parse diagnostics so agents know
	// the stream was malformed; the orphans themselves are dropped
	// from the output rather than synthesized into placeholder
	// records.
	for depth, kids := range a.pendingChildren {
		if len(kids) == 0 {
			continue
		}
		summary.Diagnostics = append(summary.Diagnostics, SummaryDiagnostic{
			Line:     kids[0].Line,
			Severity: "error",
			Rule:     "orphan-subtest-children",
			Message:  fmt.Sprintf("%d subtest child record(s) at depth %d had no parent test point", len(kids), depth),
		})
	}

	summary.Bailed = a.bailed
	// `valid` reports structural sanity: any error-severity diagnostic
	// (from the reader OR from our own orphan detection) drives it
	// false. It is independent of `bailed`; see RFC 0001 §"Summary
	// Record".
	summary.Valid = true
	for _, d := range summary.Diagnostics {
		if d.Severity == "error" {
			summary.Valid = false
			break
		}
	}

	var plan *PlanRecord
	if a.planLeading {
		plan = &PlanRecord{Type: "plan", Count: a.planCount}
	}

	return Output{
		Plan:    plan,
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

// WriteAll emits every record + bailout (if any) + summary to w as
// newline-delimited JSON.
func WriteAll(w io.Writer, out Output) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if out.Plan != nil {
		if err := enc.Encode(out.Plan); err != nil {
			return err
		}
	}
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

	// The plan is a document-level record (like bailout and summary):
	// emit it first to both streams when present.
	if out.Plan != nil {
		if err := failEnc.Encode(out.Plan); err != nil {
			return err
		}
		if passEnc != nil {
			if err := passEnc.Encode(out.Plan); err != nil {
				return err
			}
		}
	}

	for _, rec := range out.Records {
		// Failure stream gets only genuine failures: !ok && directive == nil.
		// SKIP and TODO directives route to the pass stream regardless of ok.
		if !rec.OK && rec.Directive == nil {
			if err := failEnc.Encode(rec); err != nil {
				return err
			}
		} else {
			if passEnc != nil {
				if err := passEnc.Encode(rec); err != nil {
					return err
				}
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
