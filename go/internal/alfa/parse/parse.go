package parse

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amarbel-llc/tap/go/internal/0/classify"
	"github.com/amarbel-llc/tap/go/internal/0/diagnostic"
)

func ParsePlan(line string) (diagnostic.PlanResult, error) {
	return ParsePlanWithSep(line, "")
}

func ParsePlanWithSep(line, sep string) (diagnostic.PlanResult, error) {
	m := classify.PlanRegexp.FindStringSubmatch(line)
	if m == nil {
		return diagnostic.PlanResult{}, fmt.Errorf("invalid plan line: %q", line)
	}

	countStr := strings.TrimSpace(m[1])
	if sep != "" {
		countStr = strings.ReplaceAll(countStr, sep, "")
	}
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return diagnostic.PlanResult{}, fmt.Errorf("invalid plan count: %v", err)
	}

	return diagnostic.PlanResult{
		Count:  count,
		Reason: strings.TrimSpace(m[3]),
	}, nil
}

func ParseTestPoint(line string) (diagnostic.TestPointResult, []diagnostic.Diagnostic) {
	return ParseTestPointWithSep(line, "")
}

func ParseTestPointWithSep(line, sep string) (diagnostic.TestPointResult, []diagnostic.Diagnostic) {
	var tp diagnostic.TestPointResult
	var diags []diagnostic.Diagnostic

	rest := line
	if strings.HasPrefix(rest, "not ok") {
		tp.OK = false
		rest = rest[6:]
	} else if strings.HasPrefix(rest, "ok") {
		tp.OK = true
		rest = rest[2:]
	}

	rest = strings.TrimLeft(rest, " ")

	// Parse optional test number, accepting locale grouping separators
	if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
		numEnd := 0
		for numEnd < len(rest) {
			b := rest[numEnd]
			if b >= '0' && b <= '9' {
				numEnd++
				continue
			}
			// Check for multi-byte separator at this position
			if sep != "" && strings.HasPrefix(rest[numEnd:], sep) {
				numEnd += len(sep)
				continue
			}
			break
		}
		numStr := rest[:numEnd]
		if sep != "" {
			numStr = strings.ReplaceAll(numStr, sep, "")
		}
		tp.Number, _ = strconv.Atoi(numStr)
		rest = rest[numEnd:]
	}

	// Parse optional description separator " - " or "- "
	if strings.HasPrefix(rest, " - ") {
		rest = rest[3:]
	} else if strings.HasPrefix(rest, "- ") {
		rest = rest[2:]
	} else if strings.HasPrefix(rest, " ") {
		rest = rest[1:]
	}

	// Find unescaped # for directive
	desc, directive, reason := splitDirective(rest)
	tp.Description = unescapeDescription(strings.TrimSpace(desc))
	tp.Directive = directive
	tp.Reason = reason

	return tp, diags
}

func splitDirective(s string) (desc string, directive diagnostic.Directive, reason string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++ // skip escaped char
			continue
		}
		if s[i] == '#' {
			// Check for directive pattern: " # TODO" or " # SKIP"
			if i > 0 && s[i-1] == ' ' {
				after := strings.TrimSpace(s[i+1:])
				upper := strings.ToUpper(after)
				if strings.HasPrefix(upper, "TODO") {
					reason := strings.TrimSpace(after[4:])
					return s[:i-1], diagnostic.DirectiveTodo, reason
				}
				if strings.HasPrefix(upper, "SKIP") {
					reason := strings.TrimSpace(after[4:])
					return s[:i-1], diagnostic.DirectiveSkip, reason
				}
			}
		}
	}
	return s, diagnostic.DirectiveNone, ""
}

func unescapeDescription(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			next := s[i+1]
			if next == '#' || next == '\\' {
				b.WriteByte(next)
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func ParseBailOut(line string) diagnostic.BailOutResult {
	reason := strings.TrimPrefix(line, "Bail out!")
	return diagnostic.BailOutResult{Reason: strings.TrimSpace(reason)}
}

func ParsePragma(line string) diagnostic.PragmaResult {
	rest := strings.TrimPrefix(line, "pragma ")
	enabled := rest[0] == '+'
	key := rest[1:]
	return diagnostic.PragmaResult{Key: key, Enabled: enabled}
}
