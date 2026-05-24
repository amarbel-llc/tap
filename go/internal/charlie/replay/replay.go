package replay

//go:generate dagnabit export

import (
	"errors"
	"fmt"
	"io"

	"github.com/amarbel-llc/tap/go/internal/0/diagnostic"
	"github.com/amarbel-llc/tap/go/internal/alfa/yaml_diagnostic"
	"github.com/amarbel-llc/tap/go/internal/bravo/reader"
	"github.com/amarbel-llc/tap/go/internal/bravo/writer"
)

// Replay parses a TAP-14 stream from r and re-emits it onto tw, preserving
// test points (with directives), comments, bail outs, and YAML diagnostics.
// The stream's "TAP version 14" line is consumed but not re-emitted (the
// caller's tw owns versioning). The plan line is also consumed and not
// re-emitted; the caller should call tw.Plan() after Replay returns.
//
// Nested subtests in the input (Depth > 0) are not yet supported. The first
// such event triggers a comment warning on tw; subsequent depth>0 events
// are dropped silently to avoid noise.
//
// Output blocks in the input are dropped in v1. Pragmas are passed
// through to the child Writer.
//
// Returns a Summary derived from the test points actually emitted (depth==0
// only) and any read error encountered. EOF is not returned as an error.
func Replay(r io.Reader, tw *writer.Writer) (diagnostic.Summary, error) {
	rd := reader.NewReader(r)
	var (
		summary     diagnostic.Summary
		buffered    *diagnostic.Event
		warnedDepth bool
		readErr     error
	)

	flushBuffered := func() {
		if buffered == nil {
			return
		}
		emitTestPoint(tw, buffered, nil, &summary)
		buffered = nil
	}

	for {
		ev, err := rd.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				readErr = err
			}
			break
		}

		if ev.Depth > 0 {
			flushBuffered()
			if !warnedDepth {
				tw.Comment("tap-dancer: nested subtest replay not yet supported")
				warnedDepth = true
			}
			continue
		}

		switch ev.Type {
		case diagnostic.EventVersion, diagnostic.EventPlan, diagnostic.EventOutputHeader, diagnostic.EventOutputLine, diagnostic.EventUnknown:
			flushBuffered()

		case diagnostic.EventPragma:
			flushBuffered()
			if ev.Pragma != nil {
				tw.Pragma(ev.Pragma.Key, ev.Pragma.Enabled)
			}

		case diagnostic.EventTestPoint:
			flushBuffered()
			ev := ev
			buffered = &ev

		case diagnostic.EventYAMLDiagnostic:
			if buffered != nil {
				emitTestPoint(tw, buffered, ev.YAML, &summary)
				buffered = nil
			}

		case diagnostic.EventComment:
			flushBuffered()
			tw.Comment("%s", ev.Comment)

		case diagnostic.EventBailOut:
			flushBuffered()
			reason := ""
			if ev.BailOut != nil {
				reason = ev.BailOut.Reason
			}
			tw.BailOut("%s", reason)
			summary.BailedOut = true
		}
	}

	flushBuffered()
	return summary, readErr
}

// emitTestPoint writes one test point through tw, optionally with YAML
// diagnostics, and updates summary counts. Description and directive come
// from ev.TestPoint; if ev.TestPoint is nil the call is a no-op.
//
// The yaml map comes from the reader as map[string]any (possibly nested);
// the writer takes map[string]string, so any nested structure is flattened
// to its YAML round-trip via fmt.Sprintf.
func emitTestPoint(tw *writer.Writer, ev *diagnostic.Event, yaml map[string]any, summary *diagnostic.Summary) {
	if ev == nil || ev.TestPoint == nil {
		return
	}
	tp := ev.TestPoint
	flat := flattenYAML(yaml)
	switch tp.Directive {
	case diagnostic.DirectiveSkip:
		if len(yaml) > 0 {
			tw.SkipDiag(tp.Description, tp.Reason, yamlToDiagnostics(yaml))
		} else {
			tw.Skip(tp.Description, tp.Reason)
		}
		summary.Skipped++
	case diagnostic.DirectiveTodo:
		tw.Todo(tp.Description, tp.Reason)
		summary.Todo++
	default:
		if tp.OK {
			if len(yaml) > 0 {
				tw.OkDiag(tp.Description, yamlToDiagnostics(yaml))
			} else {
				tw.Ok(tp.Description)
			}
			summary.Passed++
		} else {
			tw.NotOk(tp.Description, flat)
			summary.Failed++
		}
	}
	summary.TotalTests++
}

// flattenYAML projects the reader's map[string]any onto the writer's
// map[string]string. Scalars become their default Go string form
// (matching the prior reader's behavior); nested mappings and
// sequences become their %v-formatted form. Used only as a bridge to
// the writer's existing API; the original typed map is preserved
// elsewhere.
func flattenYAML(m map[string]any) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch v := v.(type) {
		case string:
			out[k] = v
		default:
			out[k] = fmt.Sprintf("%v", v)
		}
	}
	return out
}

// yamlToDiagnostics converts a parsed YAML map into a *YAMLDiagnostic
// suitable for OkDiag/SkipDiag. Recognized keys populate structured
// fields; every key (including the recognized ones) is also mirrored
// into Extras so the diagnostic round-trips faithfully.
func yamlToDiagnostics(yaml map[string]any) *yaml_diagnostic.YAMLDiagnostic {
	d := &yaml_diagnostic.YAMLDiagnostic{Extras: make(map[string]any, len(yaml))}
	for k, v := range yaml {
		switch k {
		case "message":
			if s, ok := v.(string); ok {
				d.Message = s
			}
		case "severity":
			if s, ok := v.(string); ok {
				d.Severity = s
			}
		case "file":
			if s, ok := v.(string); ok {
				d.File = s
			}
		case "line":
			switch n := v.(type) {
			case int:
				d.Line = n
			case int64:
				d.Line = int(n)
			case uint64:
				d.Line = int(n)
			case float64:
				d.Line = int(n)
			case string:
				fmt.Sscanf(n, "%d", &d.Line)
			}
		}
		d.Extras[k] = v
	}
	return d
}
