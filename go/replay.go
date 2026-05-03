package tap

import (
	"errors"
	"fmt"
	"io"
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
// Pragmas and output blocks in the input are dropped in v1.
//
// Returns a Summary derived from the test points actually emitted (depth==0
// only) and any read error encountered. EOF is not returned as an error.
func Replay(r io.Reader, tw *Writer) (Summary, error) {
	rd := NewReader(r)
	var (
		summary     Summary
		buffered    *Event
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
		case EventVersion, EventPlan, EventPragma, EventOutputHeader, EventOutputLine, EventUnknown:
			// Skip in v1: version/plan are owned by caller; pragma/output
			// would need writer-state propagation we don't do yet.
			flushBuffered()

		case EventTestPoint:
			flushBuffered()
			ev := ev
			buffered = &ev

		case EventYAMLDiagnostic:
			if buffered != nil {
				emitTestPoint(tw, buffered, ev.YAML, &summary)
				buffered = nil
			}
			// YAML with no prior test point: silently drop.

		case EventComment:
			flushBuffered()
			tw.Comment(ev.Comment)

		case EventBailOut:
			flushBuffered()
			reason := ""
			if ev.BailOut != nil {
				reason = ev.BailOut.Reason
			}
			tw.BailOut(reason)
			summary.BailedOut = true
		}
	}

	flushBuffered()
	return summary, readErr
}

// emitTestPoint writes one test point through tw, optionally with YAML
// diagnostics, and updates summary counts. Description and directive come
// from ev.TestPoint; if ev.TestPoint is nil the call is a no-op.
func emitTestPoint(tw *Writer, ev *Event, yaml map[string]string, summary *Summary) {
	if ev == nil || ev.TestPoint == nil {
		return
	}
	tp := ev.TestPoint
	switch tp.Directive {
	case DirectiveSkip:
		if len(yaml) > 0 {
			tw.SkipDiag(tp.Description, tp.Reason, yamlToDiagnostics(yaml))
		} else {
			tw.Skip(tp.Description, tp.Reason)
		}
		summary.Skipped++
	case DirectiveTodo:
		tw.Todo(tp.Description, tp.Reason)
		summary.Todo++
		if !tp.OK {
			// TODO directives don't count as failures by TAP semantics,
			// but we still surface the underlying ok/not ok via Todo.
		}
	default:
		if tp.OK {
			if len(yaml) > 0 {
				tw.OkDiag(tp.Description, yamlToDiagnostics(yaml))
			} else {
				tw.Ok(tp.Description)
			}
			summary.Passed++
		} else {
			tw.NotOk(tp.Description, yaml)
			summary.Failed++
		}
	}
	summary.TotalTests++
}

// yamlToDiagnostics converts a parsed YAML map (key→string) into a *Diagnostics
// suitable for OkDiag/SkipDiag. Recognized keys populate structured fields;
// every key (including the recognized ones) is also mirrored into Extras so
// the diagnostic round-trips faithfully.
func yamlToDiagnostics(yaml map[string]string) *Diagnostics {
	d := &Diagnostics{Extras: make(map[string]any, len(yaml))}
	for k, v := range yaml {
		switch k {
		case "message":
			d.Message = v
		case "severity":
			d.Severity = v
		case "file":
			d.File = v
		case "line":
			var n int
			fmt.Sscanf(v, "%d", &n)
			d.Line = n
		}
		d.Extras[k] = v
	}
	return d
}
