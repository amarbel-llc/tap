package operation_writer

//go:generate dagnabit export

import (
	"fmt"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/operation"

	"github.com/amarbel-llc/tap/go/internal/alfa/yaml_diagnostic"
	"github.com/amarbel-llc/tap/go/internal/bravo/writer"
)

var _ operation.Writer = (*OperationWriter)(nil)

type opLevel struct {
	writer      *writer.Writer
	description string
	hasChildren bool
}

// OperationWriter bridges operation.Writer to tap-dancer's Writer,
// converting operation lifecycle events into TAP-14 test output.
type OperationWriter struct {
	levels []opLevel
}

func NewOperationWriter(tw *writer.Writer) *OperationWriter {
	return &OperationWriter{
		levels: []opLevel{{writer: tw}},
	}
}

func (ow *OperationWriter) BeginOperation(depth int, op *operation.OperationEvent) {
	idx := depth - 1

	if idx < len(ow.levels) {
		ow.levels[idx].description = op.Description
		ow.levels[idx].hasChildren = false
	} else {
		parent := &ow.levels[idx-1]
		parent.hasChildren = true
		child := parent.writer.Subtest("%s", parent.description)
		ow.levels = append(ow.levels, opLevel{
			writer:      child,
			description: op.Description,
		})
	}
}

func (ow *OperationWriter) EndOperation(depth int, op *operation.OperationEvent) {
	idx := depth - 1
	level := &ow.levels[idx]

	if level.hasChildren && idx+1 < len(ow.levels) {
		ow.levels[idx+1].writer.Plan()
		ow.levels = ow.levels[:idx+1]
	}

	tw := level.writer

	switch op.Outcome {
	case operation.Success:
		if op.Diagnostic != nil {
			tw.OkDiag(op.Description, opDiagToTap(op.Diagnostic))
		} else {
			tw.Ok(op.Description)
		}

	case operation.Failure:
		diags := buildOpDiagMap(op)
		tw.NotOk(op.Description, diags)

	case operation.Skipped:
		reason := ""
		if op.Diagnostic != nil {
			reason = op.Diagnostic.Message
		}
		tw.Skip(op.Description, reason)

	case operation.Aborted:
		diags := buildOpDiagMap(op)
		diags["aborted"] = "true"
		tw.NotOk(op.Description, diags)
	}
}

func opDiagToTap(d *operation.Diagnostic) *yaml_diagnostic.YAMLDiagnostic {
	td := &yaml_diagnostic.YAMLDiagnostic{
		File:     d.File,
		Line:     d.Line,
		Message:  d.Message,
		Severity: d.Severity,
		Extras:   make(map[string]any),
	}

	if d.Source != "" {
		td.Extras["source"] = d.Source
	}

	for k, v := range d.Extras {
		td.Extras[k] = v
	}

	return td
}

func buildOpDiagMap(op *operation.OperationEvent) map[string]string {
	diags := make(map[string]string)

	if op.Diagnostic != nil {
		if op.Diagnostic.File != "" {
			diags["file"] = op.Diagnostic.File
		}
		if op.Diagnostic.Line != 0 {
			diags["line"] = fmt.Sprintf("%d", op.Diagnostic.Line)
		}
		if op.Diagnostic.Message != "" {
			diags["message"] = op.Diagnostic.Message
		}
		if op.Diagnostic.Severity != "" {
			diags["severity"] = op.Diagnostic.Severity
		}
		if op.Diagnostic.Source != "" {
			diags["source"] = op.Diagnostic.Source
		}
		for k, v := range op.Diagnostic.Extras {
			diags[k] = fmt.Sprintf("%v", v)
		}
	}

	for i, err := range op.MustErrors {
		key := "must_error"
		if len(op.MustErrors) > 1 {
			key = fmt.Sprintf("must_error_%d", i+1)
		}
		diags[key] = err.Error()
	}

	return diags
}
