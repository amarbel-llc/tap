package reformat

//go:generate dagnabit export

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/amarbel-llc/tap/go/internal/0/style"
)

var (
	okLine      = regexp.MustCompile(`^(ok\b)(.*)`)
	notOkLine   = regexp.MustCompile(`^(not ok\b)(.*)`)
	bailOutLine = regexp.MustCompile(`^(Bail out!)(.*)`)
	skipDir     = regexp.MustCompile(`(?i)#\s*skip\b`)
	todoDir     = regexp.MustCompile(`(?i)#\s*todo\b`)
)

// ReformatTAP reads raw TAP (with or without a version line) from r and
// re-emits it as TAP version 14 on w. When color is true, ok/not ok/skip/
// todo/bail out tokens are wrapped in ANSI SGR sequences.
func ReformatTAP(r io.Reader, w io.Writer, color bool) {
	fmt.Fprintln(w, "TAP version 14")

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()

		// Drop any existing version line — we already emitted ours.
		if strings.HasPrefix(line, "TAP version ") {
			continue
		}

		if m := notOkLine.FindStringSubmatchIndex(line); m != nil {
			rest := line[m[4]:m[5]]
			rest = colorizeDirective(rest, todoDir, "# TODO", color, style.Yellow)
			fmt.Fprintf(w, "%s%s\n", colorToken("not ok", color, style.Red), rest)
		} else if m := okLine.FindStringSubmatchIndex(line); m != nil {
			rest := line[m[4]:m[5]]
			rest = colorizeDirective(rest, skipDir, "# SKIP", color, style.Yellow)
			fmt.Fprintf(w, "%s%s\n", colorToken("ok", color, style.Green), rest)
		} else if m := bailOutLine.FindStringSubmatchIndex(line); m != nil {
			rest := line[m[4]:m[5]]
			fmt.Fprintf(w, "%s%s\n", colorToken("Bail out!", color, style.Red), rest)
		} else {
			fmt.Fprintln(w, line)
		}
	}
}

func colorToken(token string, color bool, s lipgloss.Style) string {
	if color {
		return s.Render(token)
	}
	return token
}

func colorizeDirective(rest string, pattern *regexp.Regexp, canonical string, color bool, s lipgloss.Style) string {
	loc := pattern.FindStringIndex(rest)
	if loc == nil {
		return rest
	}
	before := rest[:loc[0]]
	after := rest[loc[1]:]
	directive := colorToken(canonical, color, s)
	return before + directive + after
}
