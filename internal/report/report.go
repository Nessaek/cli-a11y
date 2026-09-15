// Package report renders an audit.
//
// This tool would have no standing if its own output failed the rules it
// enforces, so: colour only ever reinforces a word that already says the same
// thing, NO_COLOR and TERM=dumb are honoured, nothing is dimmed, nothing is
// redrawn in place, and everything wraps to the real terminal width.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Nessaek/cli-a11y/internal/check"
	"github.com/Nessaek/cli-a11y/internal/probe"
)

// Dimension titles, in the order they are shown.
var dimensions = []struct{ key, title string }{
	{check.Vision, "Vision and screen readers"},
	{check.Motor, "Interaction and input"},
	{check.Clarity, "Comprehension"},
}

var severityOrder = []check.Severity{check.Critical, check.Serious, check.Moderate, check.Minor}

// Result is a finished audit.
type Result struct {
	Target   probe.Target
	Findings []check.Finding
	Meta     probe.Meta
	PTY      bool
}

// Options controls rendering.
type Options struct {
	Colour *bool // nil means decide from the environment
	Quiet  bool
	Width  int
}

// Score is the audit score out of 100.
func Score(fs []check.Finding) int {
	penalty := 0
	for _, f := range fs {
		if f.Failed() {
			penalty += f.Sev().Weight()
		}
	}
	return max(0, 100-penalty)
}

// Grade puts a score in words.
func Grade(n int) string {
	switch {
	case n >= 90:
		return "good"
	case n >= 70:
		return "workable, with gaps"
	case n >= 45:
		return "hard going"
	default:
		return "largely unusable"
	}
}

// Counts tallies findings by outcome.
type Counts struct {
	Critical, Serious, Moderate, Minor, Pass, Skip int
}

// Summarise counts the findings.
func Summarise(fs []check.Finding) Counts {
	var c Counts
	for _, f := range fs {
		switch {
		case f.Passed():
			c.Pass++
		case !f.Failed():
			c.Skip++
		default:
			switch f.Sev() {
			case check.Critical:
				c.Critical++
			case check.Serious:
				c.Serious++
			case check.Moderate:
				c.Moderate++
			case check.Minor:
				c.Minor++
			}
		}
	}
	return c
}

// ShouldFail reports whether anything reaches the threshold that fails a build.
func ShouldFail(fs []check.Finding, level check.Severity) bool {
	if level == check.None {
		return false
	}
	for _, f := range fs {
		if f.Failed() && f.Sev() <= level {
			return true
		}
	}
	return false
}

type styler struct{ on bool }

func (s styler) wrap(code string, v string) string {
	if !s.on {
		return v
	}
	return "\x1b[" + code + "m" + v + "\x1b[0m"
}

func (s styler) bold(v string) string  { return s.wrap("1", v) }
func (s styler) red(v string) string   { return s.wrap("91", v) }
func (s styler) amber(v string) string { return s.wrap("93", v) }
func (s styler) green(v string) string { return s.wrap("92", v) }
func (s styler) cyan(v string) string  { return s.wrap("96", v) }

// Deliberately no dim helper. Faint text is one of the things this tool reports
// as a fault; it has no business using it.

func colourEnabled(o Options) bool {
	if o.Colour != nil {
		return *o.Colour
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func wrapText(text string, width int, indent string) []string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		line := indent
		for _, word := range strings.Fields(para) {
			switch {
			case len(line) > len(indent) && len(line)+1+len(word) > width:
				out = append(out, line)
				line = indent + word
			case len(line) > len(indent):
				line += " " + word
			default:
				line = indent + word
			}
		}
		out = append(out, line)
	}
	return out
}

// Render produces the terminal report.
func Render(r Result, o Options) string {
	c := styler{colourEnabled(o)}
	width := o.Width
	if width == 0 {
		width = 80
	}
	width = max(40, min(width, 100))

	counts := Summarise(r.Findings)
	total := Score(r.Findings)
	labels := map[check.Severity]string{
		check.Critical: c.red("CRITICAL"),
		check.Serious:  c.red("SERIOUS "),
		check.Moderate: c.amber("MODERATE"),
		check.Minor:    c.amber("MINOR   "),
	}

	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	lines := func(ss []string) {
		for _, s := range ss {
			line(s)
		}
	}

	target := strings.TrimSpace(r.Target.Cmd + " " + strings.Join(r.Target.Args, " "))
	line("")
	line(c.bold("Accessibility report for: " + target))
	line("")
	line(fmt.Sprintf("Score %s — %s", c.bold(fmt.Sprintf("%d/100", total)), Grade(total)))
	line(fmt.Sprintf("%d critical, %d serious, %d moderate, %d minor, %d passed, %d not applicable",
		counts.Critical, counts.Serious, counts.Moderate, counts.Minor, counts.Pass, counts.Skip))
	if !r.PTY {
		line("")
		lines(wrapText("Note: no pseudo-terminal could be opened, so every terminal probe was skipped. "+
			"Only piped behaviour was graded, which misses most colour and animation faults.", width, ""))
	}

	for _, d := range dimensions {
		var group []check.Finding
		for _, f := range r.Findings {
			if f.Dimension == d.key {
				group = append(group, f)
			}
		}
		if len(group) == 0 {
			continue
		}
		failed := 0
		for _, f := range group {
			if f.Failed() {
				failed++
			}
		}
		line("")
		line(c.bold(d.title) + fmt.Sprintf("  —  %d of %d checks failed, score %d/100", failed, len(group), Score(group)))
		line(strings.Repeat("-", min(width, 72)))

		var ordered []check.Finding
		for _, sev := range severityOrder {
			for _, f := range group {
				if f.Failed() && f.Sev() == sev {
					ordered = append(ordered, f)
				}
			}
		}
		if !o.Quiet {
			for _, f := range group {
				if !f.Failed() {
					ordered = append(ordered, f)
				}
			}
		}

		separated := false
		for _, f := range ordered {
			if f.Failed() {
				line("")
				line(fmt.Sprintf("%s  %s  [%s]", labels[f.Sev()], c.bold(f.Title), f.ID))
				lines(wrapText(f.Detail, width, "          "))
				for _, e := range f.Evidence {
					lines(wrapText("· "+e, width, "          "))
				}
				if f.Remedy != "" {
					lines(wrapText(c.cyan("Fix:")+" "+f.Remedy, width, "          "))
				}
				continue
			}
			// One blank line separates the failures from the roll-call of what
			// passed, so the two never run together when read aloud.
			if !separated {
				line("")
				separated = true
			}
			if f.Passed() {
				line(c.green("PASS") + "      " + f.Title)
				continue
			}
			wrapped := wrapText(f.Title+" — "+f.Detail, width-10, "")
			line("N/A       " + wrapped[0])
			for _, w := range wrapped[1:] {
				line("          " + w)
			}
		}
	}

	line("")
	return b.String()
}

type jsonReport struct {
	Target     map[string]any            `json:"target"`
	Score      int                       `json:"score"`
	Grade      string                    `json:"grade"`
	Counts     Counts                    `json:"counts"`
	Dimensions map[string]map[string]int `json:"dimensions"`
	Meta       map[string]any            `json:"meta"`
	Findings   []check.Finding           `json:"findings"`
}

// JSON renders the machine-readable report.
func JSON(r Result) (string, error) {
	dims := map[string]map[string]int{}
	for _, d := range dimensions {
		var group []check.Finding
		failed := 0
		for _, f := range r.Findings {
			if f.Dimension == d.key {
				group = append(group, f)
				if f.Failed() {
					failed++
				}
			}
		}
		dims[d.key] = map[string]int{"score": Score(group), "checks": len(group), "failed": failed}
	}
	out := jsonReport{
		Target:     map[string]any{"cmd": r.Target.Cmd, "args": r.Target.Args},
		Score:      Score(r.Findings),
		Grade:      Grade(Score(r.Findings)),
		Counts:     Summarise(r.Findings),
		Dimensions: dims,
		Meta: map[string]any{
			"pty":        r.PTY,
			"probes":     r.Meta.ProbeCount,
			"durationMs": r.Meta.Duration.Milliseconds(),
		},
		Findings: r.Findings,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	return string(b), err
}
