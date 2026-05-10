package classify

import "testing"

func TestClassifyVersion(t *testing.T) {
	tests := []struct {
		line string
		want LineKind
	}{
		{"TAP version 14", LineVersion},
		{"TAP version 13", LineUnknown},
		{"TAP version 14 ", LineUnknown},
		{"tap version 14", LineUnknown},
	}
	for _, tt := range tests {
		if got := ClassifyLine(tt.line); got != tt.want {
			t.Errorf("ClassifyLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestClassifyPlan(t *testing.T) {
	tests := []struct {
		line string
		want LineKind
	}{
		{"1..5", LinePlan},
		{"1..0", LinePlan},
		{"1..0 # skip all", LinePlan},
		{"1..100", LinePlan},
		{"2..5", LineUnknown},
		{"1..", LineUnknown},
	}
	for _, tt := range tests {
		if got := ClassifyLine(tt.line); got != tt.want {
			t.Errorf("ClassifyLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestClassifyTestPoint(t *testing.T) {
	tests := []struct {
		line string
		want LineKind
	}{
		{"ok", LineTestPoint},
		{"ok 1", LineTestPoint},
		{"ok 1 - description", LineTestPoint},
		{"not ok", LineTestPoint},
		{"not ok 2 - failing", LineTestPoint},
		{"ok 1 - desc # SKIP reason", LineTestPoint},
		{"not ok 3 - desc # TODO reason", LineTestPoint},
		{"okay", LineUnknown},
		{"not okay", LineUnknown},
	}
	for _, tt := range tests {
		if got := ClassifyLine(tt.line); got != tt.want {
			t.Errorf("ClassifyLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestClassifyYAMLMarkers(t *testing.T) {
	tests := []struct {
		line string
		want LineKind
	}{
		{"---", LineYAMLStart},
		{"...", LineYAMLEnd},
	}
	for _, tt := range tests {
		if got := ClassifyLine(tt.line); got != tt.want {
			t.Errorf("ClassifyLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestClassifyBailOut(t *testing.T) {
	tests := []struct {
		line string
		want LineKind
	}{
		{"Bail out!", LineBailOut},
		{"Bail out! reason", LineBailOut},
		{"bail out!", LineUnknown},
		{"Bail out", LineUnknown},
	}
	for _, tt := range tests {
		if got := ClassifyLine(tt.line); got != tt.want {
			t.Errorf("ClassifyLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestClassifyPragma(t *testing.T) {
	tests := []struct {
		line string
		want LineKind
	}{
		{"pragma +strict", LinePragma},
		{"pragma -strict", LinePragma},
		{"pragma strict", LineUnknown},
	}
	for _, tt := range tests {
		if got := ClassifyLine(tt.line); got != tt.want {
			t.Errorf("ClassifyLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ok 1 - plain", "ok 1 - plain"},
		{"\033[32mok\033[0m 1 - colored", "ok 1 - colored"},
		{"\033[31mnot ok\033[0m 2 - fail", "not ok 2 - fail"},
		{"\033[32mok\033[0m 1 - desc \033[33m# SKIP\033[0m reason", "ok 1 - desc # SKIP reason"},
		{"\033[31mBail out!\033[0m reason", "Bail out! reason"},
		// Non-SGR CSI sequences should also be stripped
		{"\033[2Jok 1 - after clear", "ok 1 - after clear"},
		{"no escapes", "no escapes"},
	}
	for _, tt := range tests {
		if got := StripANSI(tt.input); got != tt.want {
			t.Errorf("StripANSI(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripNonSGR(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// SGR sequences preserved
		{"\033[32mok\033[0m", "\033[32mok\033[0m"},
		{"\033[31;1mbold red\033[0m", "\033[31;1mbold red\033[0m"},
		// Non-SGR CSI sequences stripped
		{"\033[2Jcleared", "cleared"},
		{"\033[Hcursor home", "cursor home"},
		{"\033[3Aup three", "up three"},
		// Mixed: SGR preserved, non-SGR stripped
		{"\033[2J\033[31merror\033[0m text", "\033[31merror\033[0m text"},
		// Plain text unchanged
		{"no escapes", "no escapes"},
	}
	for _, tt := range tests {
		if got := StripNonSGR(tt.input); got != tt.want {
			t.Errorf("StripNonSGR(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestClassifyComment(t *testing.T) {
	tests := []struct {
		line string
		want LineKind
	}{
		{"# comment", LineComment},
		{"# Subtest: name", LineSubtestComment},
		{"#comment", LineComment},
	}
	for _, tt := range tests {
		if got := ClassifyLine(tt.line); got != tt.want {
			t.Errorf("ClassifyLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}
