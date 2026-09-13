// Package check holds the accessibility rules and the helpers they share.
//
// A rule reads recorded probes and returns findings. It never runs anything
// itself, so adding a rule costs nothing at audit time.
package check

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/Nessaek/cli-a11y/internal/ansi"
	"github.com/Nessaek/cli-a11y/internal/probe"
	"github.com/Nessaek/cli-a11y/internal/run"
)

// Status is the outcome of one rule.
type Status int

const (
	// Fail means the rule found a real problem.
	Fail Status = iota
	// Pass means the rule ran and found nothing.
	Pass
	// Skip means the rule could not be judged, which is never the same as a pass.
	Skip
)

func (s Status) String() string {
	switch s {
	case Pass:
		return "pass"
	case Skip:
		return "skip"
	default:
		return "fail"
	}
}

// Severity ranks a failure.
type Severity int

const (
	// None is used by passes and skips.
	None Severity = iota
	// Critical means the tool is unusable for the affected group.
	Critical
	// Serious means a major barrier with no easy workaround.
	Serious
	// Moderate means a real barrier with a workaround.
	Moderate
	// Minor means friction rather than a barrier.
	Minor
)

func (s Severity) String() string {
	switch s {
	case Critical:
		return "critical"
	case Serious:
		return "serious"
	case Moderate:
		return "moderate"
	case Minor:
		return "minor"
	default:
		return ""
	}
}

// Weight is what a failure costs against a score out of 100.
func (s Severity) Weight() int {
	switch s {
	case Critical:
		return 25
	case Serious:
		return 10
	case Moderate:
		return 4
	case Minor:
		return 1
	default:
		return 0
	}
}

// ParseSeverity reads a severity name, reporting whether it was valid.
func ParseSeverity(s string) (Severity, bool) {
	for _, v := range []Severity{Critical, Serious, Moderate, Minor} {
		if v.String() == s {
			return v, true
		}
	}
	return None, false
}

// Dimensions, in the order a report shows them.
const (
	Vision  = "vision"
	Motor   = "motor"
	Clarity = "clarity"
)

// Finding is one rule's verdict.
type Finding struct {
	ID        string   `json:"id"`
	Dimension string   `json:"dimension"`
	Status    string   `json:"status"`
	Severity  string   `json:"severity,omitempty"`
	Title     string   `json:"title"`
	Detail    string   `json:"detail"`
	Evidence  []string `json:"evidence,omitempty"`
	Remedy    string   `json:"remedy,omitempty"`

	status Status
	sev    Severity
}

// Failed reports whether this finding is a failure.
func (f Finding) Failed() bool { return f.status == Fail }

// Passed reports whether this finding is a pass.
func (f Finding) Passed() bool { return f.status == Pass }

// Sev is the parsed severity.
func (f Finding) Sev() Severity { return f.sev }

func fail(id, dim, title string, sev Severity, detail string, evidence []string, remedy string) Finding {
	return Finding{
		ID: id, Dimension: dim, Title: title, Detail: detail,
		Evidence: evidence, Remedy: remedy,
		Status: Fail.String(), Severity: sev.String(),
		status: Fail, sev: sev,
	}
}

func pass(id, dim, title, detail string, evidence ...string) Finding {
	return Finding{
		ID: id, Dimension: dim, Title: title, Detail: detail, Evidence: evidence,
		Status: Pass.String(), status: Pass,
	}
}

func skip(id, dim, title, detail string) Finding {
	return Finding{
		ID: id, Dimension: dim, Title: title, Detail: detail,
		Status: Skip.String(), status: Skip,
	}
}

// All runs every rule against a probe set.
func All(s *probe.Set) []Finding {
	var out []Finding
	out = append(out, visionChecks(s)...)
	out = append(out, motorChecks(s)...)
	out = append(out, clarityChecks(s)...)
	return out
}

// ---------------------------------------------------------------- shared helpers

// renderLine draws a line the way a terminal would: a carriage return sends the
// cursor back to column zero and what follows overwrites what was there.
// Without this a spinner looks like a hundred-character line rather than the
// eight the user sees.
func renderLine(line string) string {
	var buf []rune
	col := 0
	for _, ch := range line {
		if ch == '\r' {
			col = 0
			continue
		}
		if col < len(buf) {
			buf[col] = ch
		} else {
			buf = append(buf, ch)
		}
		col++
	}
	return string(buf)
}

// visibleLines is what a reader would see: escapes removed, overwrites applied.
func visibleLines(raw string) []string {
	lines := strings.Split(ansi.Strip(raw), "\n")
	for i, l := range lines {
		lines[i] = renderLine(l)
	}
	return lines
}

func visibleText(raw string) string { return strings.Join(visibleLines(raw), "\n") }

// The scattered wide characters in the symbol blocks — the ticks, crosses and
// lightning bolts — which terminals draw two cells wide despite sitting below
// U+1F300.
var wideSymbols = [][2]rune{
	{0x231a, 0x231b}, {0x23e9, 0x23ec}, {0x23f0, 0x23f0}, {0x23f3, 0x23f3},
	{0x25fd, 0x25fe}, {0x2614, 0x2615}, {0x2648, 0x2653}, {0x267f, 0x267f},
	{0x2693, 0x2693}, {0x26a1, 0x26a1}, {0x26aa, 0x26ab}, {0x26bd, 0x26be},
	{0x26c4, 0x26c5}, {0x26ce, 0x26ce}, {0x26d4, 0x26d4}, {0x26ea, 0x26ea},
	{0x26f2, 0x26f3}, {0x26f5, 0x26f5}, {0x26fa, 0x26fa}, {0x26fd, 0x26fd},
	{0x2705, 0x2705}, {0x270a, 0x270b}, {0x2728, 0x2728}, {0x274c, 0x274c},
	{0x274e, 0x274e}, {0x2753, 0x2755}, {0x2757, 0x2757}, {0x2795, 0x2797},
	{0x27b0, 0x27b0}, {0x27bf, 0x27bf}, {0x2b1b, 0x2b1c}, {0x2b50, 0x2b50},
	{0x2b55, 0x2b55},
}

// displayWidth approximates terminal columns: emoji and East Asian wide
// characters occupy two cells, combining marks none.
func displayWidth(s string) int {
	w := 0
	for _, c := range s {
		if c == 0x200d || (c >= 0xfe00 && c <= 0xfe0f) || unicode.Is(unicode.Mn, c) {
			continue
		}
		wide := (c >= 0x1100 && c <= 0x115f) || (c >= 0x2e80 && c <= 0xa4cf) ||
			(c >= 0xac00 && c <= 0xd7a3) || (c >= 0xf900 && c <= 0xfaff) ||
			(c >= 0xfe30 && c <= 0xfe6f) || (c >= 0xff00 && c <= 0xff60) ||
			(c >= 0xffe0 && c <= 0xffe6) || (c >= 0x1f300 && c <= 0x1faff) ||
			(c >= 0x1f000 && c <= 0x1f0ff)
		if !wide {
			for _, r := range wideSymbols {
				if c >= r[0] && c <= r[1] {
					wide = true
					break
				}
			}
		}
		if wide {
			w += 2
		} else {
			w++
		}
	}
	return w
}

var spaces = regexp.MustCompile(`\s+`)

// excerpt is a short, single-line version of a string for a report.
func excerpt(s string, maxLen int) string {
	one := strings.TrimSpace(spaces.ReplaceAllString(s, " "))
	r := []rune(one)
	if len(r) > maxLen {
		return string(r[:maxLen-1]) + "…"
	}
	return one
}

var pagerTail = regexp.MustCompile(`(?::|\(END\)|--More--|lines \d+-\d+|byte \d+)$`)

// isPaging reports whether a run is sitting in a pager rather than hung.
//
// less and more take over the alternate screen and wait at a prompt, which from
// the outside looks exactly like a CLI that stopped and never came back. The
// difference matters: paging is conventional behaviour, hanging is a defect.
//
// The test is that the run used the alternate screen at all and was last seen
// at a pager's prompt. Counting entries against exits does not work: a pager
// that handles the terminating signal restores the screen on its way out, so a
// clean shutdown balances the two and a hung one does not, which is the
// opposite of what the count seems to promise.
func isPaging(p *run.Result) bool {
	if !p.OK() || !p.TimedOut {
		return false
	}
	if ansi.Parse(p.Output).Controls.AltScreenEnter == 0 {
		return false
	}
	tail := strings.TrimRight(visibleText(p.Raw), " \t\r\n")
	if len(tail) > 40 {
		tail = tail[len(tail)-40:]
	}
	return pagerTail.MatchString(tail)
}

// helpText is everything the CLI's help says it accepts.
func helpText(s *probe.Set) string {
	for _, id := range []string{"help", "helpPipe", "helpDumb"} {
		p := s.Get(id)
		if p.OK() {
			if t := visibleText(p.Raw); len(strings.TrimSpace(t)) > 20 {
				return t
			}
		}
	}
	return ""
}

// mentions returns which of these patterns the text contains.
func mentions(text string, patterns []string) []string {
	hay := strings.ToLower(text)
	var found []string
	for _, p := range patterns {
		if strings.Contains(hay, strings.ToLower(p)) {
			found = append(found, p)
		}
	}
	return found
}
