package yaml_diagnostic

//go:generate dagnabit export

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"code.linenisgreat.com/tap/go/internal/0/classify"
)

// YAMLDiagnostic models the YAML Diagnostic block under a TAP-14 Test Point
// (see TAP-14 spec §"YAML Diagnostics").
type YAMLDiagnostic struct {
	Message  string
	Severity string
	File     string
	Line     int
	Extras   map[string]any
}

// SanitizeYAMLValue strips ANSI/CSI escape sequences from a YAML value, per
// the TAP-14 ANSI Display Hints amendment. When color is true, SGR (color)
// sequences are preserved and only non-SGR CSI is stripped.
func SanitizeYAMLValue(value string, color bool) string {
	if color {
		return classify.StripNonSGR(value)
	}
	return classify.StripANSI(value)
}

// WriteDiagnostics serializes a YAMLDiagnostic as a TAP-14 YAML diagnostic
// block (between "  ---" and "  ...") to w. A nil or empty diagnostic
// produces no output.
func WriteDiagnostics(w io.Writer, d *YAMLDiagnostic, color bool) {
	if d == nil {
		return
	}

	entries := make([]struct{ k, v string }, 0, 8)

	if d.File != "" {
		entries = append(entries, struct{ k, v string }{"file", d.File})
	}
	if d.Line != 0 {
		entries = append(entries, struct{ k, v string }{"line", fmt.Sprintf("%d", d.Line)})
	}
	if d.Message != "" {
		entries = append(entries, struct{ k, v string }{"message", SanitizeYAMLValue(d.Message, color)})
	}
	if d.Severity != "" {
		entries = append(entries, struct{ k, v string }{"severity", d.Severity})
	}

	if len(d.Extras) > 0 {
		keys := make([]string, 0, len(d.Extras))
		for k := range d.Extras {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			entries = append(entries, struct{ k, v string }{k, SanitizeYAMLValue(fmt.Sprintf("%v", d.Extras[k]), color)})
		}
	}

	if len(entries) == 0 {
		return
	}

	fmt.Fprintln(w, "  ---")
	for _, e := range entries {
		if strings.Contains(e.v, "\n") {
			fmt.Fprintf(w, "  %s: |\n", e.k)
			lines := strings.Split(e.v, "\n")
			for len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			for _, line := range lines {
				fmt.Fprintf(w, "    %s\n", line)
			}
		} else {
			fmt.Fprintf(w, "  %s: %s\n", e.k, e.v)
		}
	}
	fmt.Fprintln(w, "  ...")
}
