package check

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Nessaek/cli-a11y/internal/probe"
	"github.com/Nessaek/cli-a11y/internal/run"
)

// Checks for people who cannot produce fast, precise or timed keystrokes:
// switch access, eye tracking, voice control, tremor, RSI, or simply a screen
// reader that has to read a menu before a choice can be made.
//
// The recurring theme is the escape hatch. An interactive prompt is not itself
// a barrier; an interactive prompt with no way to answer it up front is.

func motorChecks(s *probe.Set) []Finding {
	return []Finding{
		checkInteractiveHang(s),
		checkPipePrompt(s),
		checkBatchFlags(s),
		checkArrowMenus(s),
		checkTimedPrompts(s),
		checkMachineOutput(s),
	}
}

// How a prompt looks when it is waiting for you.
var (
	promptTail  = regexp.MustCompile(`(\?|:|»|›|>)\s*$|\[[yYnN]/[yYnN]\]\s*$|\((?:y/n|yes/no)\)\s*[:?]?\s*$`)
	promptWords = regexp.MustCompile(`(?i)\b(enter|choose|select|confirm|continue|proceed|overwrite|password|username|press\s+(?:any\s+key|enter|return|[a-z]\b)|are you sure|would you like|do you want|type\s+(?:the|your|yes))\b`)
	arrowMenu   = regexp.MustCompile(`(?i)(use\s+arrow\s+keys|↑\s*/?\s*↓|↓\s*/?\s*↑|arrow keys to (?:move|navigate|select)|space\s+to\s+(?:select|toggle)|<space>|j/k\s+to|press\s+<?(?:tab|space)>?\s+to)`)
	timedRe     = regexp.MustCompile(`(?i)\b(?:in|within|after)\s+\d+\s*(?:second|sec|s\b|minute)|\btimed?\s*out\s+in\b|\bauto(?:matically)?\s+(?:continu|proceed|select|cancel)`)
	cancelRe    = regexp.MustCompile(`(?i)cancel|abort|continu|proceed`)
)

var batchFlags = []string{
	"--yes", "--assume-yes", "--non-interactive", "--noninteractive",
	"--no-input", "--no-interaction", "--batch", "--force", "--defaults", "--ci",
	"-y,", "-y ", "--confirm=", "--accept",
	// gcloud, apt and others spell "answer nothing and carry on" as --quiet.
	"--quiet", "-q,",
}

var machineFlags = []string{
	"--json", "--format", "--porcelain", "--output", "--quiet",
	"--silent", "-o,", "--plain", "--no-color", "--machine",
}

type promptSignal struct {
	looksLikePrompt bool
	paging          bool
	last            string
}

// signals reports whether a run looks like it stopped and waited for a human.
func signals(p *run.Result) promptSignal {
	if !p.OK() {
		return promptSignal{}
	}
	// A pager also stops and waits, and its prompt is a bare colon. That is not
	// the CLI asking the user a question.
	if isPaging(p) {
		return promptSignal{paging: true}
	}
	var lines []string
	for _, l := range visibleLines(p.Raw) {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	last := ""
	if len(lines) > 0 {
		last = lines[len(lines)-1]
	}
	all := strings.Join(lines, "\n")
	tail := promptTail.MatchString(last)
	words := promptWords.MatchString(all)
	return promptSignal{
		looksLikePrompt: (tail && words) || (p.TimedOut && (tail || words)),
		last:            last,
	}
}

func checkInteractiveHang(s *probe.Set) Finding {
	bare := s.Get("bare")
	if !bare.OK() {
		return skip("M-INTERACTIVE-HANG", Motor, "Waiting for input", "The bare invocation could not be run.")
	}
	sig := signals(bare)

	if !bare.TimedOut {
		return pass("M-INTERACTIVE-HANG", Motor, "Completes without waiting for input",
			fmt.Sprintf("A bare invocation finished on its own in %.1fs.", bare.Duration.Seconds()))
	}
	if sig.paging {
		return pass("M-INTERACTIVE-HANG", Motor, "Does not wait for input",
			"The bare invocation handed off to a pager rather than prompting. Reported separately as V-PAGER.")
	}
	if !sig.looksLikePrompt {
		return skip("M-INTERACTIVE-HANG", Motor, "Waiting for input",
			"A bare invocation did not finish within the timeout, but nothing in its output looks like a prompt. It is probably long-running or a filter rather than interactive; re-run with --cmd to exercise a command that terminates.")
	}

	escapes := mentions(helpText(s), batchFlags)
	title := "Prompts for input with no way to answer up front"
	sev := Serious
	detail := "The CLI stops and waits for a human, and the help advertises no flag that supplies the answers up front. Anyone driving this by voice control, switch access or a script is stuck at the prompt, and a screen reader user gets no announcement that input is expected at all."
	remedy := "Accept every prompted answer as a flag or environment variable, and default to non-interactive when stdin is not a terminal."
	note := "help mentions none of --yes, --non-interactive, --no-input, --batch, --force"
	if len(escapes) > 0 {
		title = "Prompts for input, but an escape hatch exists"
		sev = Minor
		detail = "The CLI stops and waits for a human, but the help does advertise a non-interactive mode."
		remedy = "Name the flag in the prompt itself, so someone who cannot answer it can discover the way out without leaving the terminal."
		note = "help mentions: " + strings.Join(escapes, ", ")
	}
	return fail("M-INTERACTIVE-HANG", Motor, title, sev, detail,
		[]string{fmt.Sprintf("last line before the timeout: %q", excerpt(sig.last, 60)), note}, remedy)
}

func checkPipePrompt(s *probe.Set) Finding {
	p := s.Get("barePipe")
	if !p.OK() {
		return skip("M-PIPE-PROMPT", Motor, "Prompting with stdin redirected", "The piped invocation could not be run.")
	}
	sig := signals(p)
	if p.TimedOut && sig.looksLikePrompt {
		return fail("M-PIPE-PROMPT", Motor, "Prompts even when stdin is not a terminal", Serious,
			"The CLI waits for interactive input with stdin redirected. Nothing can drive it — not a script, not CI, not an assistive tool that automates a workflow — and it simply hangs with no indication why.",
			[]string{fmt.Sprintf("last line before the timeout: %q", excerpt(sig.last, 60))},
			"Check isatty(stdin). When it is false, either take the default or fail with a message naming the flag that supplies the answer.")
	}
	return pass("M-PIPE-PROMPT", Motor, "Does not prompt when stdin is redirected",
		"Piped invocation did not block on a prompt.")
}

func checkBatchFlags(s *probe.Set) Finding {
	help := helpText(s)
	if help == "" {
		return skip("M-BATCH-FLAG", Motor, "Non-interactive mode", "No help text to search.")
	}
	if found := mentions(help, batchFlags); len(found) > 0 {
		return pass("M-BATCH-FLAG", Motor, "Offers a non-interactive mode",
			"Help advertises "+strings.Join(found, ", ")+".")
	}
	interactive := signals(s.Get("bare")).looksLikePrompt || signals(s.Get("barePipe")).looksLikePrompt
	if !interactive {
		return pass("M-BATCH-FLAG", Motor, "Non-interactive by nature",
			"No prompting was observed, so no batch flag is needed.")
	}
	return fail("M-BATCH-FLAG", Motor, "No documented non-interactive mode", Moderate,
		"Prompting was observed but the help advertises no flag to answer it in advance. The workaround people are left with is piping \"yes\", which is fragile and undiscoverable.",
		[]string{"searched help for: --yes, --assume-yes, --non-interactive, --no-input, --batch, --force, --quiet"},
		"Add a documented flag per prompt, plus one blanket --yes. Mention them in the help near the command that prompts.")
}

func checkArrowMenus(s *probe.Set) Finding {
	var hits []string
	for _, id := range sortedIDs(s) {
		p := s.Results[id]
		if !p.OK() {
			continue
		}
		text := visibleText(p.Raw)
		if !arrowMenu.MatchString(text) {
			continue
		}
		for _, l := range strings.Split(text, "\n") {
			if arrowMenu.MatchString(l) {
				hits = append(hits, fmt.Sprintf("%s: %s", id, excerpt(l, 60)))
				break
			}
		}
	}
	if len(hits) == 0 {
		return pass("M-ARROW-MENU", Motor, "No keystroke-driven menus",
			"No arrow-key or space-bar selection menus were seen.")
	}
	return fail("M-ARROW-MENU", Motor, "Selection menu driven by raw keystrokes", Moderate,
		"A menu navigated by arrow keys or space puts the CLI into raw mode. Screen readers get no announcement when the highlighted row changes, so the user is moving a cursor they cannot hear; voice and switch users have to issue one command per row.",
		trim(hits, 4),
		"Always offer the same choice as a flag argument, and accept a typed number or name as well as arrow keys. Print the full list first so it can be read before choosing.")
}

func checkTimedPrompts(s *probe.Set) Finding {
	var hits []string
	for _, id := range sortedIDs(s) {
		p := s.Results[id]
		if !p.OK() {
			continue
		}
		text := visibleText(p.Raw)
		if !timedRe.MatchString(text) {
			continue
		}
		for _, l := range strings.Split(text, "\n") {
			if !timedRe.MatchString(l) {
				continue
			}
			// Only interesting where something is being asked of the user.
			if promptWords.MatchString(text) || cancelRe.MatchString(l) {
				hits = append(hits, fmt.Sprintf("%s: %s", id, excerpt(l, 70)))
			}
			break
		}
	}
	if len(hits) == 0 {
		return pass("M-TIMED-PROMPT", Motor, "No time-limited prompts",
			"Nothing appears to act on a countdown.")
	}
	return fail("M-TIMED-PROMPT", Motor, "Prompt acts on a time limit", Serious,
		"Something proceeds or cancels on a timer. A screen reader may not have finished reading the question by then, and switch or voice input can take far longer than the allowance. WCAG treats an unextendable time limit as a failure for exactly this reason.",
		trim(hits, 4),
		"Wait indefinitely when stdin is a terminal, or make the timeout configurable and generous. Never let a countdown choose the destructive option.")
}

func checkMachineOutput(s *probe.Set) Finding {
	help := helpText(s)
	if help == "" {
		return skip("M-MACHINE-OUTPUT", Motor, "Machine-readable output", "No help text to search.")
	}
	if found := mentions(help, machineFlags); len(found) > 0 {
		return pass("M-MACHINE-OUTPUT", Motor, "Output can be consumed by other tools",
			"Help advertises "+strings.Join(found, ", ")+".")
	}
	return fail("M-MACHINE-OUTPUT", Motor, "No machine-readable output mode", Minor,
		"The help advertises no structured or quiet output. Scripting is how many disabled users avoid a difficult interface altogether — wrapping the tool once in something they can drive — and that route is closed without a stable output format.",
		[]string{"searched help for: --json, --format, --porcelain, --output, --quiet, --plain"},
		"Add --json, or at minimum a stable line-oriented --quiet mode that omits decoration.")
}
