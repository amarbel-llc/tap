package classify

import (
	"regexp"
	"strings"
)

type LineKind int

const (
	LineUnknown LineKind = iota
	LineVersion
	LinePlan
	LineTestPoint
	LineYAMLStart
	LineYAMLEnd
	LineBailOut
	LinePragma
	LineComment
	LineSubtestComment
	LineEmpty
	LineOutputHeader
)

var (
	PlanRegexp         = regexp.MustCompile(`^1\.\.([\d,.\x{00a0}\x{202f} ]+)(\s+#\s+(.*))?$`)
	TestPointRegexp    = regexp.MustCompile(`^(not )?ok\b`)
	PragmaRegexp       = regexp.MustCompile(`^pragma\s+[+-]\w`)
	OutputHeaderRegexp = regexp.MustCompile(`^# Output:\s+(\d+)\s*-\s*(.+?)(?:\s+#.*)?$`)
	// csiRegexp matches all CSI escape sequences (ESC [ ... <final byte>),
	// not just SGR, per the ANSI Display Hints amendment security guidance.
	csiRegexp = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")
	// nonSGRRegexp matches CSI sequences whose final byte is anything except
	// 'm' (SGR), per the ANSI in YAML Output Blocks amendment.
	nonSGRRegexp = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-ln-z]")
)

// StripANSI removes all CSI escape sequences from a string.
func StripANSI(s string) string {
	return csiRegexp.ReplaceAllString(s, "")
}

// StripNonSGR removes non-SGR CSI sequences, preserving SGR (ESC[...m) color codes.
func StripNonSGR(s string) string {
	return nonSGRRegexp.ReplaceAllString(s, "")
}

func ClassifyLine(line string) LineKind {
	if line == "TAP version 14" {
		return LineVersion
	}

	if PlanRegexp.MatchString(line) {
		return LinePlan
	}

	if TestPointRegexp.MatchString(line) {
		return LineTestPoint
	}

	if line == "---" {
		return LineYAMLStart
	}

	if line == "..." {
		return LineYAMLEnd
	}

	if strings.HasPrefix(line, "Bail out!") {
		return LineBailOut
	}

	if PragmaRegexp.MatchString(line) {
		return LinePragma
	}

	if OutputHeaderRegexp.MatchString(line) {
		return LineOutputHeader
	}

	if strings.HasPrefix(line, "# Subtest") {
		return LineSubtestComment
	}

	if strings.HasPrefix(line, "#") {
		return LineComment
	}

	if strings.TrimSpace(line) == "" {
		return LineEmpty
	}

	return LineUnknown
}
