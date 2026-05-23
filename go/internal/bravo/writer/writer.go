package writer

//go:generate dagnabit export

import (
	"bytes"
	"fmt"
	"io"
	"iter"
	"sort"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ui"

	"github.com/amarbel-llc/tap/go/internal/0/style"
	"github.com/amarbel-llc/tap/go/internal/alfa/yaml_diagnostic"
)

type Writer struct {
	printer          interfaces.Printer
	n                int
	depth            int
	planEmitted      bool
	failed           bool
	color            bool
	locale           language.Tag
	formatter        *message.Printer
	streamedOutput   bool
	ttyBuildLastLine bool
}

func NewWriter(w io.Writer) *Writer {
	p := ui.MakePrinterFromWriter(w)
	fmt.Fprintln(p, "TAP version 14")
	return &Writer{printer: p}
}

// NewColorWriter creates a Writer that colorizes ok/not ok when color is true.
func NewColorWriter(w io.Writer, color bool) *Writer {
	p := ui.MakePrinterFromWriter(w)
	fmt.Fprintln(p, "TAP version 14")
	return &Writer{printer: p, color: color}
}

func NewLocaleWriter(w io.Writer, locale language.Tag) *Writer {
	p := ui.MakePrinterFromWriter(w)
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "TAP version 14")
	fmt.Fprintf(&buf, "pragma +locale-formatting:%s\n", locale)
	_, _ = buf.WriteTo(p)
	return &Writer{
		printer:   p,
		locale:    locale,
		formatter: message.NewPrinter(locale),
	}
}

func (tw *Writer) formatNumber(n int) string {
	if tw.formatter != nil {
		return tw.formatter.Sprintf("%d", n)
	}
	return fmt.Sprintf("%d", n)
}

func (tw *Writer) colorOk() string {
	if tw.color {
		return style.Green.Render("ok")
	}
	return "ok"
}

func (tw *Writer) colorNotOk() string {
	if tw.color {
		return style.Red.Render("not ok")
	}
	return "not ok"
}

func (tw *Writer) colorSkip() string {
	if tw.color {
		return style.Yellow.Render("# SKIP")
	}
	return "# SKIP"
}

func (tw *Writer) colorTodo() string {
	if tw.color {
		return style.Yellow.Render("# TODO")
	}
	return "# TODO"
}

func (tw *Writer) colorBailOut() string {
	if tw.color {
		return style.Red.Render("Bail out!")
	}
	return "Bail out!"
}

func (tw *Writer) Ok(description string) int {
	tw.n++
	num := tw.formatNumber(tw.n)
	fmt.Fprintf(tw.printer, "%s %s - %s\n", tw.colorOk(), num, description)
	return tw.n
}

// atomicEmit assembles a multi-line TAP record into a buffer and writes
// it to the underlying printer in a single Write call. The single Write
// is atomic against concurrent goroutines sharing the same *os.File
// fd (Go's runtime serializes through pollDesc), so the record's lines
// never interleave with output from elsewhere.
func (tw *Writer) atomicEmit(fn func(io.Writer)) {
	var buf bytes.Buffer
	fn(&buf)
	_, _ = buf.WriteTo(tw.printer)
}

func (tw *Writer) OkDiag(description string, diagnostics *yaml_diagnostic.YAMLDiagnostic) int {
	tw.n++
	num := tw.formatNumber(tw.n)
	tw.atomicEmit(func(w io.Writer) {
		fmt.Fprintf(w, "%s %s - %s\n", tw.colorOk(), num, description)
		yaml_diagnostic.WriteDiagnostics(w, diagnostics, tw.color)
	})
	return tw.n
}

func (tw *Writer) HasFailures() bool {
	return tw.failed
}

func (tw *Writer) NotOk(description string, diagnostics map[string]string) int {
	tw.n++
	tw.failed = true
	num := tw.formatNumber(tw.n)
	tw.atomicEmit(func(w io.Writer) {
		fmt.Fprintf(w, "%s %s - %s\n", tw.colorNotOk(), num, description)
		if len(diagnostics) == 0 {
			return
		}
		fmt.Fprintln(w, "  ---")
		keys := make([]string, 0, len(diagnostics))
		for k := range diagnostics {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := yaml_diagnostic.SanitizeYAMLValue(diagnostics[k], tw.color)
			if strings.Contains(v, "\n") {
				fmt.Fprintf(w, "  %s: |\n", k)
				lines := strings.Split(v, "\n")
				for len(lines) > 0 && lines[len(lines)-1] == "" {
					lines = lines[:len(lines)-1]
				}
				for _, line := range lines {
					fmt.Fprintf(w, "    %s\n", line)
				}
			} else {
				fmt.Fprintf(w, "  %s: %s\n", k, v)
			}
		}
		fmt.Fprintln(w, "  ...")
	})
	return tw.n
}

func (tw *Writer) Skip(description, reason string) int {
	tw.n++
	num := tw.formatNumber(tw.n)
	fmt.Fprintf(tw.printer, "%s %s - %s %s %s\n", tw.colorOk(), num, description, tw.colorSkip(), reason)
	return tw.n
}

func (tw *Writer) SkipDiag(description, reason string, diagnostics *yaml_diagnostic.YAMLDiagnostic) int {
	tw.n++
	num := tw.formatNumber(tw.n)
	tw.atomicEmit(func(w io.Writer) {
		fmt.Fprintf(w, "%s %s - %s %s %s\n", tw.colorOk(), num, description, tw.colorSkip(), reason)
		yaml_diagnostic.WriteDiagnostics(w, diagnostics, tw.color)
	})
	return tw.n
}

func (tw *Writer) Todo(description, reason string) int {
	tw.n++
	num := tw.formatNumber(tw.n)
	fmt.Fprintf(tw.printer, "%s %s - %s %s %s\n", tw.colorNotOk(), num, description, tw.colorTodo(), reason)
	return tw.n
}

func (tw *Writer) PlanAhead(n int) {
	fmt.Fprintf(tw.printer, "1..%s\n", tw.formatNumber(n))
	tw.planEmitted = true
}

func (tw *Writer) Plan() {
	if tw.planEmitted {
		return
	}
	tw.planEmitted = true
	fmt.Fprintf(tw.printer, "1..%s\n", tw.formatNumber(tw.n))
}

func (tw *Writer) BailOut(format string, args ...any) {
	fmt.Fprintf(tw.printer, "%s %s\n", tw.colorBailOut(), fmt.Sprintf(format, args...))
}

func (tw *Writer) Comment(format string, args ...any) {
	fmt.Fprintf(tw.printer, "# %s\n", fmt.Sprintf(format, args...))
}

func (tw *Writer) Pragma(key string, enabled bool) {
	sign := "-"
	if enabled {
		sign = "+"
	}
	fmt.Fprintf(tw.printer, "pragma %s%s\n", sign, key)
	if key == "streamed-output" && enabled {
		tw.streamedOutput = true
	}
	if key == "tty-build-last-line" && enabled {
		tw.ttyBuildLastLine = true
	}
}

func (tw *Writer) StreamedOutput(text string) {
	fmt.Fprintf(tw.printer, "# %s\n", text)
}

func (tw *Writer) EnableTTYBuildLastLine() {
	tw.ttyBuildLastLine = true
	fmt.Fprintln(tw.printer, "pragma +tty-build-last-line")
}

func (tw *Writer) UpdateLastLine(text string) {
	fmt.Fprintf(tw.printer, "\r\033[2K# %s", text)
}

func (tw *Writer) FinishLastLine() {
	fmt.Fprint(tw.printer, "\r\033[2K")
}

// OutputBlockWriter writes indented body lines inside an Output Block.
// The "# Output:" header is emitted lazily on the first Line call, so a
// block whose callback never writes a body line produces no header at all.
type OutputBlockWriter struct {
	w             io.Writer
	color         bool
	pendingHeader *pendingOutputHeader
}

type pendingOutputHeader struct {
	num, description string
}

// Line writes a single 4-space-indented output line, applying SGR filtering.
// On first invocation it flushes the deferred "# Output:" header.
func (ob *OutputBlockWriter) Line(text string) {
	var buf bytes.Buffer
	if ob.pendingHeader != nil {
		fmt.Fprintf(&buf, "# Output: %s - %s\n", ob.pendingHeader.num, ob.pendingHeader.description)
		ob.pendingHeader = nil
	}
	text = yaml_diagnostic.SanitizeYAMLValue(text, ob.color)
	fmt.Fprintf(&buf, "    %s\n", text)
	_, _ = buf.WriteTo(ob.w)
}

// OutputBlock emits an Output Block per the streamed-output amendment.
// The callback receives an OutputBlockWriter for streaming body lines.
// Returning nil emits "ok"; returning non-nil emits "not ok" with YAML diagnostics.
func (tw *Writer) OutputBlock(description string, fn func(*OutputBlockWriter) *yaml_diagnostic.YAMLDiagnostic) int {
	tw.n++
	num := tw.formatNumber(tw.n)
	ob := &OutputBlockWriter{
		w:             tw.printer,
		color:         tw.color,
		pendingHeader: &pendingOutputHeader{num: num, description: description},
	}
	diag := fn(ob)
	if diag != nil {
		tw.failed = true
	}
	tw.atomicEmit(func(w io.Writer) {
		if diag != nil {
			fmt.Fprintf(w, "%s %s - %s\n", tw.colorNotOk(), num, description)
			yaml_diagnostic.WriteDiagnostics(w, diag, tw.color)
		} else {
			fmt.Fprintf(w, "%s %s - %s\n", tw.colorOk(), num, description)
		}
	})
	return tw.n
}

// indentWriter prefixes every written line with prefix, used by Subtest to
// indent the entire child Writer's output by 4 spaces under its
// "# Subtest:" header. It buffers all prefixed lines from a single Write
// call into one downstream Write so atomic records assembled upstream
// remain atomic after indentation.
type indentWriter struct {
	w      io.Writer
	prefix string
}

func (iw *indentWriter) Write(p []byte) (int, error) {
	var buf bytes.Buffer
	prefix := []byte(iw.prefix)
	rest := p
	for len(rest) > 0 {
		nl := bytes.IndexByte(rest, '\n')
		if nl < 0 {
			buf.Write(prefix)
			buf.Write(rest)
			break
		}
		buf.Write(prefix)
		buf.Write(rest[:nl+1])
		rest = rest[nl+1:]
	}
	if _, err := iw.w.Write(buf.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (tw *Writer) Subtest(format string, args ...any) *Writer {
	prefix := "    "
	fmt.Fprintf(tw.printer, "%s# Subtest: %s\n", prefix, fmt.Sprintf(format, args...))
	iw := &indentWriter{w: tw.printer, prefix: prefix}
	childPrinter := ui.MakePrinterFromWriter(iw)
	child := &Writer{
		printer:   childPrinter,
		depth:     tw.depth + 1,
		color:     tw.color,
		locale:    tw.locale,
		formatter: tw.formatter,
	}
	if tw.formatter != nil {
		fmt.Fprintf(childPrinter, "pragma +locale-formatting:%s\n", tw.locale)
	}
	if tw.streamedOutput {
		child.streamedOutput = true
		fmt.Fprintln(childPrinter, "pragma +streamed-output")
	}
	return child
}

type TestPoint struct {
	Description string
	Ok          bool
	Skip        string
	Todo        string
	Diagnostics *yaml_diagnostic.YAMLDiagnostic
	Subtests    func(*Writer)
	OutputBlock func(*OutputBlockWriter) *yaml_diagnostic.YAMLDiagnostic
}

func (tw *Writer) WriteAll(tests iter.Seq[TestPoint]) {
	for tp := range tests {
		if tp.OutputBlock != nil {
			tw.OutputBlock(tp.Description, tp.OutputBlock)
		} else if tp.Subtests != nil {
			child := tw.Subtest("%s", tp.Description)
			tp.Subtests(child)
			if !child.planEmitted {
				child.Plan()
			}
			tw.Ok(tp.Description)
		} else if tp.Skip != "" {
			tw.SkipDiag(tp.Description, tp.Skip, tp.Diagnostics)
		} else if tp.Todo != "" {
			tw.Todo(tp.Description, tp.Todo)
		} else if tp.Ok {
			tw.n++
			num := tw.formatNumber(tw.n)
			tw.atomicEmit(func(w io.Writer) {
				fmt.Fprintf(w, "%s %s - %s\n", tw.colorOk(), num, tp.Description)
				yaml_diagnostic.WriteDiagnostics(w, tp.Diagnostics, tw.color)
			})
		} else {
			tw.n++
			tw.failed = true
			num := tw.formatNumber(tw.n)
			tw.atomicEmit(func(w io.Writer) {
				fmt.Fprintf(w, "%s %s - %s\n", tw.colorNotOk(), num, tp.Description)
				yaml_diagnostic.WriteDiagnostics(w, tp.Diagnostics, tw.color)
			})
		}
	}
	if !tw.planEmitted {
		tw.Plan()
	}
}
