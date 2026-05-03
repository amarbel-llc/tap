package tap

import (
	"bytes"
	"strings"
	"testing"
)

// replayInto runs Replay on the given input against a fresh Writer wrapping
// a buffer, and returns the bytes Replay caused to be emitted (i.e. the
// buffer with the leading "TAP version 14\n" header from NewWriter stripped).
func replayInto(t *testing.T, input string) (string, Summary, *Writer) {
	t.Helper()
	var buf bytes.Buffer
	tw := NewWriter(&buf)
	header := buf.Len()
	summary, err := Replay(strings.NewReader(input), tw)
	if err != nil {
		t.Fatalf("Replay returned error: %v", err)
	}
	return buf.String()[header:], summary, tw
}

func TestReplaySkipsVersionLine(t *testing.T) {
	out, _, _ := replayInto(t, "TAP version 14\nok 1 - hello\n1..1\n")
	if strings.Contains(out, "TAP version 14") {
		t.Errorf("Replay must not re-emit version line; got: %q", out)
	}
}

func TestReplaySkipsPlanLine(t *testing.T) {
	out, _, _ := replayInto(t, "TAP version 14\nok 1 - hello\n1..1\n")
	if strings.Contains(out, "1..1") {
		t.Errorf("Replay must not re-emit plan line; caller owns Plan(); got: %q", out)
	}
}

func TestReplayPassingTestPoint(t *testing.T) {
	out, summary, _ := replayInto(t, "TAP version 14\nok 1 - hello\n1..1\n")
	if !strings.Contains(out, "ok 1 - hello\n") {
		t.Errorf("expected `ok 1 - hello`, got: %q", out)
	}
	if summary.Passed != 1 {
		t.Errorf("expected summary.Passed=1, got %d", summary.Passed)
	}
	if summary.Failed != 0 {
		t.Errorf("expected summary.Failed=0, got %d", summary.Failed)
	}
}

func TestReplayFailingTestPoint(t *testing.T) {
	out, summary, tw := replayInto(t, "TAP version 14\nnot ok 1 - boom\n1..1\n")
	if !strings.Contains(out, "not ok 1 - boom\n") {
		t.Errorf("expected `not ok 1 - boom`, got: %q", out)
	}
	if summary.Failed != 1 {
		t.Errorf("expected summary.Failed=1, got %d", summary.Failed)
	}
	if !tw.HasFailures() {
		t.Error("expected tw.HasFailures() to be true after replaying a failing test point")
	}
}

func TestReplayMultipleTestPoints(t *testing.T) {
	input := "TAP version 14\nok 1 - first\nnot ok 2 - second\nok 3 - third\n1..3\n"
	out, summary, _ := replayInto(t, input)
	for _, want := range []string{"ok 1 - first\n", "not ok 2 - second\n", "ok 3 - third\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %q", want, out)
		}
	}
	if summary.Passed != 2 || summary.Failed != 1 {
		t.Errorf("expected 2 passed, 1 failed; got passed=%d failed=%d", summary.Passed, summary.Failed)
	}
}

func TestReplaySkipDirective(t *testing.T) {
	out, summary, _ := replayInto(t, "TAP version 14\nok 1 - skipped # SKIP not relevant\n1..1\n")
	if !strings.Contains(out, "# SKIP") || !strings.Contains(out, "not relevant") {
		t.Errorf("expected SKIP directive with reason, got: %q", out)
	}
	if summary.Skipped != 1 {
		t.Errorf("expected summary.Skipped=1, got %d", summary.Skipped)
	}
}

func TestReplayTodoDirective(t *testing.T) {
	out, summary, _ := replayInto(t, "TAP version 14\nnot ok 1 - todo # TODO not implemented\n1..1\n")
	if !strings.Contains(out, "# TODO") || !strings.Contains(out, "not implemented") {
		t.Errorf("expected TODO directive with reason, got: %q", out)
	}
	if summary.Todo != 1 {
		t.Errorf("expected summary.Todo=1, got %d", summary.Todo)
	}
}

func TestReplayYAMLDiagnosticsAttachToPriorTestPoint(t *testing.T) {
	input := strings.Join([]string{
		"TAP version 14",
		"not ok 1 - boom",
		"  ---",
		"  message: assertion failed",
		"  severity: fail",
		"  ...",
		"1..1",
		"",
	}, "\n")
	out, _, _ := replayInto(t, input)
	if !strings.Contains(out, "not ok 1 - boom\n") {
		t.Errorf("expected test point line, got: %q", out)
	}
	if !strings.Contains(out, "  ---\n") {
		t.Errorf("expected YAML diag opener, got: %q", out)
	}
	if !strings.Contains(out, "message: assertion failed") {
		t.Errorf("expected message diag, got: %q", out)
	}
	if !strings.Contains(out, "  ...\n") {
		t.Errorf("expected YAML diag closer, got: %q", out)
	}
}

func TestReplayComment(t *testing.T) {
	out, _, _ := replayInto(t, "TAP version 14\n# a comment\nok 1 - x\n1..1\n")
	if !strings.Contains(out, "# a comment\n") {
		t.Errorf("expected comment, got: %q", out)
	}
}

func TestReplayBailOut(t *testing.T) {
	out, summary, _ := replayInto(t, "TAP version 14\nBail out! database gone\n")
	if !strings.Contains(out, "Bail out! database gone\n") {
		t.Errorf("expected bail out line, got: %q", out)
	}
	if !summary.BailedOut {
		t.Error("expected summary.BailedOut to be true")
	}
}

func TestReplayDepthGreaterThanZeroEmitsWarning(t *testing.T) {
	input := strings.Join([]string{
		"TAP version 14",
		"# Subtest: nested",
		"    ok 1 - inner",
		"    1..1",
		"ok 1 - nested",
		"1..1",
		"",
	}, "\n")
	out, _, _ := replayInto(t, input)
	if !strings.Contains(out, "tap-dancer: nested subtest replay not yet supported") {
		t.Errorf("expected nested-subtest warning comment, got: %q", out)
	}
	if !strings.Contains(out, "ok 1 - nested\n") {
		t.Errorf("expected outer test point at depth 0 to still be emitted, got: %q", out)
	}
}

func TestReplayEmptyInput(t *testing.T) {
	out, summary, _ := replayInto(t, "")
	if out != "" {
		t.Errorf("expected empty output for empty input, got: %q", out)
	}
	if summary.Failed != 0 || summary.Passed != 0 {
		t.Errorf("expected zero-counts summary, got: %+v", summary)
	}
}

func TestReplayOnlyVersionLine(t *testing.T) {
	out, _, _ := replayInto(t, "TAP version 14\n")
	if out != "" {
		t.Errorf("expected no emission for version-only input, got: %q", out)
	}
}

func TestReplayBatsLikeOutput(t *testing.T) {
	// What `bats --tap` typically produces for a 2-test file with one failure.
	input := strings.Join([]string{
		"TAP version 14",
		"1..2",
		"ok 1 first test",
		"not ok 2 second test",
		"# (in test file foo.bats, line 12)",
		"#   `assert_success' failed",
		"",
	}, "\n")
	out, summary, tw := replayInto(t, input)
	// Bats omits the "-" separator; Replay normalizes to TAP-14 canonical form.
	if !strings.Contains(out, "ok 1 - first test\n") {
		t.Errorf("expected first test point, got: %q", out)
	}
	if !strings.Contains(out, "not ok 2 - second test\n") {
		t.Errorf("expected failing test point, got: %q", out)
	}
	if !strings.Contains(out, "# (in test file foo.bats") {
		t.Errorf("expected bats failure-comment passthrough, got: %q", out)
	}
	if summary.Failed != 1 {
		t.Errorf("expected 1 failure in summary, got %d", summary.Failed)
	}
	if !tw.HasFailures() {
		t.Error("expected tw.HasFailures() to be true")
	}
}

func TestReplayInsideSubtestProducesFirstClassChildTAP(t *testing.T) {
	// End-to-end: parent Writer creates a Subtest, Replay fills it, parent
	// emits its own ok/not-ok and Plan. The result must be valid TAP-14
	// that an outer Reader can parse and report inner failure counts.
	var buf bytes.Buffer
	parent := NewWriter(&buf)
	child := parent.Subtest("inner")
	innerInput := "TAP version 14\nok 1 - a\nnot ok 2 - b\n1..2\n"
	summary, err := Replay(strings.NewReader(innerInput), child)
	if err != nil {
		t.Fatalf("Replay error: %v", err)
	}
	child.Plan()
	if summary.Failed != 1 {
		t.Errorf("expected 1 inner failure, got %d", summary.Failed)
	}
	if !child.HasFailures() {
		t.Error("expected child writer to have failures")
	}
	parent.NotOk("inner", nil)
	parent.Plan()

	out := buf.String()
	// Indented inner test points appear under the subtest.
	if !strings.Contains(out, "    ok 1 - a\n") {
		t.Errorf("expected indented inner pass, got: %q", out)
	}
	if !strings.Contains(out, "    not ok 2 - b\n") {
		t.Errorf("expected indented inner failure, got: %q", out)
	}
	if !strings.Contains(out, "    1..2\n") {
		t.Errorf("expected indented inner plan, got: %q", out)
	}
	if !strings.Contains(out, "not ok 1 - inner\n") {
		t.Errorf("expected outer not ok, got: %q", out)
	}
	// Re-parse via Reader and confirm summary sees one outer test point that's failing.
	r := NewReader(strings.NewReader(out))
	s := r.Summary()
	if s.Failed == 0 {
		t.Errorf("expected outer reader to count at least one failure, got summary: %+v", s)
	}
}
