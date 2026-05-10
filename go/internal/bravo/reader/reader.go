package reader

//go:generate dagnabit export

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/amarbel-llc/tap/go/internal/0/classify"
	"github.com/amarbel-llc/tap/go/internal/0/diagnostic"
	"github.com/amarbel-llc/tap/go/internal/alfa/parse"
)

type readerState int

const (
	stateStart readerState = iota
	stateHeader
	stateBody
	stateYAML
	stateDone
)

type frame struct {
	depth                  int
	planSeen               bool
	planCount              int
	planLine               int
	testCount              int
	lastTestNumber         int
	streamedOutput         bool
	localeSep              string // grouping separator for active locale, empty = no locale
	inOutputBlock          bool
	outputBlockNumber      int
	outputBlockDescription string
}

// Reader is a streaming TAP-14 parser and validator.
type Reader struct {
	scanner          *bufio.Scanner
	state            readerState
	lineNum          int
	stack            []frame
	diags            []diagnostic.Diagnostic
	done             bool
	bailed           bool
	yamlBuf          map[string]string
	lastWasTestPoint bool
	passed           int
	failed           int
	skipped          int
	todo             int
}

// NewReader creates a new TAP-14 reader from the given input.
func NewReader(r io.Reader) *Reader {
	return &Reader{
		scanner: bufio.NewScanner(r),
		stack:   []frame{{depth: 0}},
	}
}

func (r *Reader) currentFrame() *frame {
	return &r.stack[len(r.stack)-1]
}

func localeGroupingSeparator(tag language.Tag) string {
	p := message.NewPrinter(tag)
	formatted := p.Sprintf("%d", 1234)
	// "1,234" for en-US, "1.234" for de-DE, "1 234" for fr-FR
	runes := []rune(formatted)
	if len(runes) >= 2 {
		return string(runes[1])
	}
	return ""
}

func (r *Reader) addDiag(severity diagnostic.Severity, rule, message string) {
	r.diags = append(r.diags, diagnostic.Diagnostic{
		Line:     r.lineNum,
		Severity: severity,
		Rule:     rule,
		Message:  message,
	})
}

// Next returns the next parsed event from the TAP stream.
// Returns io.EOF when the stream is exhausted.
func (r *Reader) Next() (diagnostic.Event, error) {
	for r.scanner.Scan() {
		r.lineNum++
		original := r.scanner.Text()

		// Strip ANSI CSI escape sequences before parsing, per the
		// ANSI Display Hints amendment. This ensures colored TAP
		// streams parse identically to uncolored streams.
		raw := classify.StripANSI(original)

		// Determine indentation depth
		trimmed := strings.TrimLeft(raw, " ")
		indent := len(raw) - len(trimmed)
		depth := indent / 4

		// Handle YAML block state
		if r.state == stateYAML {
			expectedIndent := (r.currentFrame().depth * 4) + 2
			if raw == strings.Repeat(" ", expectedIndent)+"..." {
				r.state = stateBody
				yaml := r.yamlBuf
				r.yamlBuf = nil
				return diagnostic.Event{
					Type:  diagnostic.EventYAMLDiagnostic,
					Line:  r.lineNum,
					Depth: r.currentFrame().depth,
					Raw:   raw,
					YAML:  yaml,
				}, nil
			}
			// Accumulate YAML content using the original line to
			// preserve ANSI SGR sequences in values, per the ANSI
			// in YAML Output Blocks amendment.
			content := original
			if len(content) >= expectedIndent {
				content = content[expectedIndent:]
			}
			parts := strings.SplitN(content, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				r.yamlBuf[key] = val
			}
			continue
		}

		// Handle output block body lines (4-space indent at current depth)
		// Must check before depth-change handling, since 4-space indent
		// would otherwise trigger a subtest frame push.
		if r.currentFrame().inOutputBlock && indent == (r.currentFrame().depth*4)+4 {
			content := raw[(r.currentFrame().depth*4)+4:]
			r.lastWasTestPoint = false
			return diagnostic.Event{
				Type:       diagnostic.EventOutputLine,
				Line:       r.lineNum,
				Depth:      r.currentFrame().depth,
				Raw:        raw,
				OutputLine: content,
			}, nil
		}

		// Handle depth changes for subtests
		if depth > r.currentFrame().depth {
			r.stack = append(r.stack, frame{depth: depth})
		}
		for depth < r.currentFrame().depth && len(r.stack) > 1 {
			completed := r.stack[len(r.stack)-1]
			r.stack = r.stack[:len(r.stack)-1]
			if completed.planSeen && completed.testCount != completed.planCount && !r.bailed {
				r.addDiag(diagnostic.SeverityError, "plan-count-mismatch",
					"subtest plan count mismatch: plan declared "+
						strconv.Itoa(completed.planCount)+
						" tests but "+strconv.Itoa(completed.testCount)+" ran")
			}
		}

		kind := classify.ClassifyLine(trimmed)

		switch kind {
		case classify.LineVersion:
			if r.state != stateStart {
				if r.currentFrame().depth > 0 {
					r.addDiag(diagnostic.SeverityWarning, "subtest-version",
						"subtests should omit version line for TAP13 compatibility")
				}
			}
			r.state = stateHeader
			r.lastWasTestPoint = false
			return diagnostic.Event{Type: diagnostic.EventVersion, Line: r.lineNum, Depth: depth, Raw: raw}, nil

		case classify.LinePlan:
			f := r.currentFrame()
			if f.planSeen {
				r.addDiag(diagnostic.SeverityError, "plan-duplicate", "duplicate plan line")
			}
			plan, _ := parse.ParsePlanWithSep(trimmed, f.localeSep)
			f.planSeen = true
			f.planCount = plan.Count
			f.planLine = r.lineNum
			if r.state == stateStart {
				r.addDiag(diagnostic.SeverityError, "version-required", "first line must be TAP version 14")
			}
			if r.state == stateHeader {
				r.state = stateBody
			}
			r.lastWasTestPoint = false
			return diagnostic.Event{Type: diagnostic.EventPlan, Line: r.lineNum, Depth: depth, Raw: raw, Plan: &plan}, nil

		case classify.LineTestPoint:
			if r.state == stateStart {
				r.addDiag(diagnostic.SeverityError, "version-required", "first line must be TAP version 14")
			}
			r.state = stateBody
			f := r.currentFrame()
			tp, tpDiags := parse.ParseTestPointWithSep(trimmed, f.localeSep)
			r.diags = append(r.diags, tpDiags...)
			f.testCount++

			if tp.Number == 0 {
				r.addDiag(diagnostic.SeverityWarning, "test-number-missing", "test point without explicit number")
			} else {
				if tp.Number != f.lastTestNumber+1 {
					r.addDiag(diagnostic.SeverityWarning, "test-number-sequence",
						"test number "+strconv.Itoa(tp.Number)+" out of sequence, expected "+strconv.Itoa(f.lastTestNumber+1))
				}
				f.lastTestNumber = tp.Number
			}

			if f.inOutputBlock {
				f.inOutputBlock = false
				if tp.Number != f.outputBlockNumber {
					r.addDiag(diagnostic.SeverityError, "output-block-id-mismatch",
						"output block header declared test "+strconv.Itoa(f.outputBlockNumber)+
							" but correlated test point is "+strconv.Itoa(tp.Number))
				}
				if tp.Description != f.outputBlockDescription {
					r.addDiag(diagnostic.SeverityWarning, "output-block-description-mismatch",
						"output block header description "+strconv.Quote(f.outputBlockDescription)+
							" differs from test point description "+strconv.Quote(tp.Description))
				}
			}

			// Track pass/fail/skip/todo
			switch tp.Directive {
			case diagnostic.DirectiveSkip:
				r.skipped++
			case diagnostic.DirectiveTodo:
				r.todo++
			default:
				if tp.OK {
					r.passed++
				} else {
					r.failed++
				}
			}

			r.lastWasTestPoint = true
			return diagnostic.Event{Type: diagnostic.EventTestPoint, Line: r.lineNum, Depth: depth, Raw: raw, TestPoint: &tp}, nil

		case classify.LineYAMLStart:
			if !r.lastWasTestPoint {
				r.addDiag(diagnostic.SeverityWarning, "yaml-orphan", "YAML block not following a test point")
			}
			expectedIndent := (r.currentFrame().depth * 4) + 2
			if indent != expectedIndent {
				r.addDiag(diagnostic.SeverityError, "yaml-indent",
					"YAML block must be indented by "+strconv.Itoa(expectedIndent)+" spaces")
			}
			r.state = stateYAML
			r.yamlBuf = make(map[string]string)
			r.lastWasTestPoint = false
			continue

		case classify.LineYAMLEnd:
			r.addDiag(diagnostic.SeverityError, "yaml-unclosed", "unexpected YAML end marker without opening ---")
			r.lastWasTestPoint = false
			continue

		case classify.LineBailOut:
			b := parse.ParseBailOut(trimmed)
			r.bailed = true
			r.lastWasTestPoint = false
			return diagnostic.Event{Type: diagnostic.EventBailOut, Line: r.lineNum, Depth: depth, Raw: raw, BailOut: &b}, nil

		case classify.LinePragma:
			p := parse.ParsePragma(trimmed)
			if p.Key == "streamed-output" {
				if p.Enabled {
					r.currentFrame().streamedOutput = true
				} else if r.currentFrame().streamedOutput {
					r.addDiag(diagnostic.SeverityError, "streamed-output-deactivation",
						"pragma -streamed-output is not permitted after activation")
				}
			}
			if strings.HasPrefix(p.Key, "locale-formatting:") {
				tag := strings.TrimPrefix(p.Key, "locale-formatting:")
				langTag, err := language.Parse(tag)
				if err == nil {
					r.currentFrame().localeSep = localeGroupingSeparator(langTag)
				}
			}
			r.lastWasTestPoint = false
			return diagnostic.Event{Type: diagnostic.EventPragma, Line: r.lineNum, Depth: depth, Raw: raw, Pragma: &p}, nil

		case classify.LineOutputHeader:
			m := classify.OutputHeaderRegexp.FindStringSubmatch(trimmed)
			num, _ := strconv.Atoi(m[1])
			desc := strings.TrimSpace(m[2])
			f := r.currentFrame()
			f.inOutputBlock = true
			f.outputBlockNumber = num
			f.outputBlockDescription = desc
			r.lastWasTestPoint = false
			return diagnostic.Event{
				Type:  diagnostic.EventOutputHeader,
				Line:  r.lineNum,
				Depth: depth,
				Raw:   raw,
				OutputHeader: &diagnostic.OutputHeaderResult{
					Number:      num,
					Description: desc,
				},
			}, nil

		case classify.LineSubtestComment:
			comment := strings.TrimPrefix(trimmed, "#")
			comment = strings.TrimSpace(comment)
			r.lastWasTestPoint = false
			return diagnostic.Event{Type: diagnostic.EventComment, Line: r.lineNum, Depth: depth, Raw: raw, Comment: comment}, nil

		case classify.LineComment:
			comment := strings.TrimPrefix(trimmed, "#")
			comment = strings.TrimSpace(comment)
			r.lastWasTestPoint = false
			return diagnostic.Event{Type: diagnostic.EventComment, Line: r.lineNum, Depth: depth, Raw: raw, Comment: comment, StreamedOutput: r.currentFrame().streamedOutput}, nil

		case classify.LineEmpty:
			r.lastWasTestPoint = false
			continue

		default:
			r.lastWasTestPoint = false
			return diagnostic.Event{Type: diagnostic.EventUnknown, Line: r.lineNum, Depth: depth, Raw: raw}, nil
		}
	}

	if !r.done {
		r.done = true
		r.finalize()
	}
	return diagnostic.Event{}, io.EOF
}

func (r *Reader) finalize() {
	if r.state == stateStart {
		r.addDiag(diagnostic.SeverityError, "version-required", "first line must be TAP version 14")
	}
	if r.state == stateYAML {
		r.addDiag(diagnostic.SeverityError, "yaml-unclosed", "YAML block not closed at end of input")
	}

	// Validate all remaining stack frames
	for i := len(r.stack) - 1; i >= 0; i-- {
		f := r.stack[i]
		if !f.planSeen && !r.bailed {
			if f.depth == 0 {
				r.addDiag(diagnostic.SeverityError, "plan-required", "no plan line found")
			}
		}
		if f.planSeen && f.testCount != f.planCount && !r.bailed {
			r.addDiag(diagnostic.SeverityError, "plan-count-mismatch",
				"plan declared "+strconv.Itoa(f.planCount)+" tests but "+strconv.Itoa(f.testCount)+" ran")
		}
	}
}

// Diagnostics returns all validation problems found so far.
func (r *Reader) Diagnostics() []diagnostic.Diagnostic {
	if !r.done {
		for {
			if _, err := r.Next(); err != nil {
				break
			}
		}
	}
	return r.diags
}

// Summary returns aggregate results after the stream is fully consumed.
func (r *Reader) Summary() diagnostic.Summary {
	if !r.done {
		for {
			if _, err := r.Next(); err != nil {
				break
			}
		}
	}

	s := diagnostic.Summary{
		Version:   14,
		BailedOut: r.bailed,
		Passed:    r.passed,
		Failed:    r.failed,
		Skipped:   r.skipped,
		Todo:      r.todo,
	}

	if len(r.stack) > 0 {
		root := r.stack[0]
		s.PlanCount = root.planCount
		s.TotalTests = root.testCount
	}

	hasErrors := false
	for _, d := range r.diags {
		if d.Severity == diagnostic.SeverityError {
			hasErrors = true
			break
		}
	}
	s.Valid = !hasErrors

	return s
}

// ReadFrom reads the entire TAP stream, consuming all events and
// collecting diagnostics.
func (r *Reader) ReadFrom(src io.Reader) (int64, error) {
	r.scanner = bufio.NewScanner(src)
	r.lineNum = 0
	r.state = stateStart
	r.stack = []frame{{depth: 0}}
	r.diags = nil
	r.done = false

	for {
		if _, err := r.Next(); err != nil {
			break
		}
	}
	return int64(r.lineNum), nil
}

// WriteTo writes the validation report to the given writer.
func (r *Reader) WriteTo(w io.Writer) (int64, error) {
	if !r.done {
		for {
			if _, err := r.Next(); err != nil {
				break
			}
		}
	}

	var total int64
	summary := r.Summary()

	for _, d := range r.diags {
		line := fmt.Sprintf("line %d: %s: [%s] %s\n", d.Line, d.Severity, d.Rule, d.Message)
		n, err := io.WriteString(w, line)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	status := "valid"
	if !summary.Valid {
		status = "invalid"
	}
	line := fmt.Sprintf("\n%s: %d tests (%d passed, %d failed, %d skipped, %d todo)\n",
		status, summary.TotalTests, summary.Passed, summary.Failed, summary.Skipped, summary.Todo)
	n, err := io.WriteString(w, line)
	total += int64(n)
	return total, err
}
