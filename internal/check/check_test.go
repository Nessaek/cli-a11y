package check

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nessaek/cli-a11y/internal/probe"
	"github.com/Nessaek/cli-a11y/internal/run"
)

// Every rule is pinned to a fixture that provokes it and a fixture that does
// not. A rule that cannot tell the two apart is worse than no rule, because it
// spends the reader's attention on nothing.

var fixtures = map[string]string{}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cli-a11y-fixtures")
	if err != nil {
		panic(err)
	}
	for _, name := range []string{"bad", "good"} {
		bin := filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", bin, "github.com/Nessaek/cli-a11y/internal/fixture/"+name)
		if out, err := cmd.CombinedOutput(); err != nil {
			panic(string(out))
		}
		fixtures[name] = bin
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func audit(t *testing.T, fixture string) map[string]Finding {
	t.Helper()
	set := probe.Collect(probe.Target{Cmd: fixtures[fixture]},
		probe.Options{Timeout: 2 * time.Second})
	byID := map[string]Finding{}
	for _, f := range All(set) {
		byID[f.ID] = f
	}
	return byID
}

var (
	badOnce, goodOnce map[string]Finding
)

func badAudit(t *testing.T) map[string]Finding {
	if badOnce == nil {
		badOnce = audit(t, "bad")
	}
	return badOnce
}

func goodAudit(t *testing.T) map[string]Finding {
	if goodOnce == nil {
		goodOnce = audit(t, "good")
	}
	return goodOnce
}

// mustFailOnBad lists every rule the bad fixture is built to provoke.
var mustFailOnBad = []string{
	"V-PIPE-COLOUR", "V-NO-COLOR", "V-TERM-DUMB", "V-CONTRAST", "V-DIM",
	"V-COLOUR-ONLY", "V-ANIMATION", "V-TERM-STATE", "V-GLYPHS", "V-WIDTH", "V-REFLOW",
	"M-INTERACTIVE-HANG", "M-PIPE-PROMPT", "M-BATCH-FLAG", "M-ARROW-MENU",
	"M-TIMED-PROMPT", "M-MACHINE-OUTPUT",
	"C-HELP-EXIT", "C-HELP-STREAM", "C-VERSION", "C-ERROR-EXIT", "C-ERROR-STREAM",
	"C-ERROR-ACTIONABLE", "C-HELP-STRUCTURE", "C-HELP-EXAMPLES", "C-READABILITY",
}

// mustPassOnGood omits V-CONTRAST on purpose: the ANSI bright red the good
// fixture uses for its failure marker is 4.0:1 on a light terminal, so it
// cannot pass. That is a true finding about the palette, not a defect in the
// fixture.
var mustPassOnGood = []string{
	"V-PIPE-COLOUR", "V-NO-COLOR", "V-TERM-DUMB", "V-DIM", "V-COLOUR-ONLY",
	"V-ANIMATION", "V-TERM-STATE", "V-GLYPHS", "V-PAGER", "V-WIDTH", "V-REFLOW",
	"M-INTERACTIVE-HANG", "M-PIPE-PROMPT", "M-BATCH-FLAG", "M-ARROW-MENU",
	"M-TIMED-PROMPT", "M-MACHINE-OUTPUT",
	"C-HELP-EXISTS", "C-HELP-EXIT", "C-HELP-STREAM", "C-VERSION", "C-ERROR-EXIT",
	"C-ERROR-STREAM", "C-ERROR-ACTIONABLE", "C-HELP-STRUCTURE", "C-HELP-EXAMPLES",
	"C-READABILITY",
}

func TestBadFixtureFailsEveryRuleItProvokes(t *testing.T) {
	got := badAudit(t)
	for _, id := range mustFailOnBad {
		f, ok := got[id]
		if !ok {
			t.Errorf("%s: no finding produced at all", id)
			continue
		}
		if !f.Failed() {
			t.Errorf("%s: status %s, want fail (%s)", id, f.Status, f.Detail)
		}
	}
}

func TestGoodFixtureClearsEveryRule(t *testing.T) {
	got := goodAudit(t)
	for _, id := range mustPassOnGood {
		f, ok := got[id]
		if !ok {
			t.Errorf("%s: no finding produced at all", id)
			continue
		}
		if !f.Passed() {
			t.Errorf("%s: status %s, want pass (%s)", id, f.Status, f.Detail)
		}
	}
}

func TestEveryFailureCarriesARemedy(t *testing.T) {
	for _, set := range []map[string]Finding{badAudit(t), goodAudit(t)} {
		for id, f := range set {
			if f.Failed() && f.Remedy == "" {
				t.Errorf("%s: a failure with no remedy tells the reader nothing to do", id)
			}
		}
	}
}

func TestFindingsAreNeverSilentlyMissing(t *testing.T) {
	// A rule must always speak: pass, fail, or an explicit "not checked".
	got := goodAudit(t)
	for _, id := range mustPassOnGood {
		if f, ok := got[id]; ok && f.Status == "" {
			t.Errorf("%s: empty status", id)
		}
	}
}

// ---------------------------------------------------------------- unit tests

func TestRenderLineAppliesOverwrites(t *testing.T) {
	// "load" is overwritten by "done", leaving the tail behind.
	if got := renderLine("loading 90%\rdone"); got != "doneing 90%" {
		t.Errorf("got %q, want %q", got, "doneing 90%")
	}
}

func TestDisplayWidthCountsWideCharacters(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"ab", 2},
		{"ab✅", 4},
		{"日本", 4},
	}
	for _, c := range cases {
		if got := displayWidth(c.in); got != c.want {
			t.Errorf("displayWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestIsLabelSeparatesStatusTokensFromValues(t *testing.T) {
	for _, s := range []string{"[ok]", "DOWN", "✗", "[FAILED]", "warn"} {
		if !isLabel(s) {
			t.Errorf("%q should read as a status label", s)
		}
	}
	for _, s := range []string{"api-gateway", "worker-queue", "2 replicas", "some/path/here"} {
		if isLabel(s) {
			t.Errorf("%q is a value, not a status label", s)
		}
	}
}

func TestExcerptCollapsesWhitespace(t *testing.T) {
	if got := excerpt("  a   b\n c  ", 20); got != "a b c" {
		t.Errorf("got %q", got)
	}
	if got := excerpt(strings.Repeat("x", 50), 10); len([]rune(got)) != 10 {
		t.Errorf("excerpt not truncated: %q", got)
	}
}

// isPaging is subtle enough to have been wrong once: an earlier version counted
// alternate-screen entries against exits, which inverts the moment the pager
// handles its terminating signal and tidies up on the way out.
func TestIsPagingSurvivesACleanPagerShutdown(t *testing.T) {
	pager := func(enter, leave int, tail string) *run.Result {
		out := ""
		for range enter {
			out += "\x1b[?1049h"
		}
		out += "some help text\r\n"
		for range leave {
			out += "\x1b[?1049l"
		}
		return &run.Result{Raw: out + tail, Output: out + tail, TimedOut: true, TTY: true}
	}

	cases := []struct {
		name string
		r    *run.Result
		want bool
	}{
		{"pager killed before it could tidy up", pager(1, 0, ":"), true},
		{"pager restored the screen on its way out", pager(1, 1, ":"), true},
		{"pager at an (END) prompt", pager(1, 1, "(END)"), true},
		{"pager prompt with a trailing carriage return", pager(1, 1, ":\r"), true},
		{"alternate screen but no pager prompt", pager(1, 1, "Continue? [y/N] "), false},
		{"a real prompt, no alternate screen", pager(0, 0, "Continue? [y/N] "), false},
		{"no alternate screen, colon ending", pager(0, 0, "Enter name:"), false},
	}
	for _, c := range cases {
		if got := isPaging(c.r); got != c.want {
			t.Errorf("%s: isPaging = %v, want %v", c.name, got, c.want)
		}
	}

	// A run that finished is never paging, whatever it printed.
	done := pager(1, 1, ":")
	done.TimedOut = false
	if isPaging(done) {
		t.Error("a run that exited on its own should never count as paging")
	}
}
