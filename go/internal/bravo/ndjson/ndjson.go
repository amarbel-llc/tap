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
