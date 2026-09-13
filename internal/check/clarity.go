package check

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Nessaek/cli-a11y/internal/probe"
)

// Checks for comprehension: help you can find your way around, errors that say
// what to do next, exit codes that mean what they claim.
//
// This is the dimension people argue is "not really accessibility". It is the
// one that decides whether someone with a cognitive disability, or reading in a
// second language, or hearing the output one line at a time through speech, can
// use the tool at all.

func clarityChecks(s *probe.Set) []Finding {
	var out []Finding
	out = append(out, checkHelpExists(s))
	out = append(out, checkHelpExit(s)...)
	out = append(out, checkHelpStream(s)...)
	out = append(out, checkVersion(s)...)
	out = append(out, checkErrorExit(s)...)
	out = append(out, checkErrorStream(s)...)
	out = append(out, checkErrorActionable(s)...)
	out = append(out, checkHelpStructure(s)...)
	out = append(out, checkReadability(s)...)
	return out
}

var (
	usageRe     = regexp.MustCompile(`(?im)^\s*(usage|synopsis)\b`)
	optionsRe   = regexp.MustCompile(`(?im)^\s*(options|flags|arguments|commands|subcommands)\b`)
	dashLineRe  = regexp.MustCompile(`(?im)^\s*-{1,2}[a-z]`)
	examplesRe  = regexp.MustCompile(`(?im)^\s*(examples?)\b`)
	exampleCmd  = regexp.MustCompile(`(?m)^\s{2,}\$\s+\S`)
	versionLike = regexp.MustCompile(`(?i)\d+\.\d+|version`)
	// "Where to look next" takes many shapes: --help, a usage line, a spelling
	// suggestion, a documentation URL, or simply naming a help command. Matching
	// only the first few spellings punishes tools that do the right thing in
	// their own words.
	pointsOnRe = regexp.MustCompile(`(?i)--help\b|(?:^|\s)-h(?:\s|$)|usage:|did you mean|maybe you meant|\bhelp\b|https?://|for more (?:info|information|details)`)
)

func checkHelpExists(s *probe.Set) Finding {
	p := s.Get("help")
	if !p.OK() {
		p = s.Get("helpPipe")
	}
	if !p.OK() {
		return skip("C-HELP-EXISTS", Clarity, "Help is available", "The help invocation could not be run.")
	}
	m := s.Meta
	text := strings.TrimSpace(visibleText(p.Raw))

	// Paged help has not failed to return; it is waiting in less. Judge its
	// content by the piped run, which no pager touches.
	if p.TimedOut && isPaging(p) {
		if piped := s.Get("helpPipe"); piped.OK() {
			t := strings.TrimSpace(visibleText(piped.Raw))
			if len(t) > 20 {
				return pass("C-HELP-EXISTS", Clarity, "Help is available",
					fmt.Sprintf("%s printed %d lines, through a pager on a terminal. Paging itself is reported as V-PAGER.",
						m.HelpFlag, len(strings.Split(t, "\n"))))
			}
		}
	}

	if p.TimedOut {
		return fail("C-HELP-EXISTS", Clarity, m.HelpFlag+" does not return", Critical,
			fmt.Sprintf("Running %s did not terminate. Help is the one thing a user reaches for when stuck, and it is the first thing a screen reader user runs on an unfamiliar tool.", m.HelpFlag),
			nil, "Make the help flag short-circuit everything else and exit immediately.")
	}
	if len(text) < 20 {
		ev := []string{fmt.Sprintf("exit code %d", p.Code)}
		if text == "" {
			ev = append(ev, "no output at all")
		} else {
			ev = append(ev, "output: "+excerpt(text, 72))
		}
		return fail("C-HELP-EXISTS", Clarity, m.HelpFlag+" produces no help", Critical,
			fmt.Sprintf("Running %s produced %d characters of output. There is no way to discover what the tool does without leaving the terminal.", m.HelpFlag, len(text)),
			ev, "Print a usage summary, the available options and at least one example.")
	}
	return pass("C-HELP-EXISTS", Clarity, "Help is available",
		fmt.Sprintf("%s printed %d lines.", m.HelpFlag, len(strings.Split(text, "\n"))))
}

func checkHelpExit(s *probe.Set) []Finding {
	p := s.Get("helpPipe")
	if !p.OK() || p.TimedOut {
		return nil
	}
	if p.Code == 0 {
		return []Finding{pass("C-HELP-EXIT", Clarity, "Help exits successfully", s.Meta.HelpFlag+" exited 0.")}
	}
	return []Finding{fail("C-HELP-EXIT", Clarity, "Help exits with an error code", Minor,
		fmt.Sprintf("%s exited %d. Asking for help is not a failure, and a non-zero code here breaks shell chains and makes wrappers report a problem that did not happen.", s.Meta.HelpFlag, p.Code),
		nil, "Exit 0 when help was explicitly requested. Reserve non-zero for help printed because the invocation was wrong.")}
}

func checkHelpStream(s *probe.Set) []Finding {
	p := s.Get("helpPipe")
	if !p.OK() || p.TimedOut {
		return nil
	}
	out := strings.TrimSpace(visibleText(p.Stdout))
	errText := strings.TrimSpace(visibleText(p.Stderr))
	if len(out) > 20 {
		return []Finding{pass("C-HELP-STREAM", Clarity, "Help goes to stdout",
			"Help can be piped, paged and saved.")}
	}
	if len(errText) > 20 {
		return []Finding{fail("C-HELP-STREAM", Clarity, "Help printed to stderr", Moderate,
			fmt.Sprintf("%s writes to stderr, so \"cmd --help | less\" and \"cmd --help > notes.txt\" both come back empty. Paging is how someone reading by speech gets through a long help text at their own pace.", s.Meta.HelpFlag),
			[]string{fmt.Sprintf("stdout: %d chars, stderr: %d chars", len(out), len(errText))},
			"Requested help goes to stdout. Only help printed in response to a usage error belongs on stderr.")}
	}
	return nil
}

func checkVersion(s *probe.Set) []Finding {
	p := s.Get("version")
	if !p.OK() {
		return nil
	}
	text := strings.TrimSpace(visibleText(p.Raw))
	if p.Code == 0 && versionLike.MatchString(text) && len(text) < 400 {
		return []Finding{pass("C-VERSION", Clarity, "Reports its version", excerpt(text, 60))}
	}
	ev := []string{fmt.Sprintf("exit code %d", p.Code)}
	if text == "" {
		ev = append(ev, "no output")
	} else {
		ev = append(ev, "output: "+excerpt(text, 72))
	}
	return []Finding{fail("C-VERSION", Clarity, s.Meta.VersionFlag+" does not report a version", Minor,
		"No recognisable version string. Someone who needs help — from a colleague, a forum, or a support desk they can reach — has to start by working out which version they have.",
		ev, "Print the version and exit 0.")}
}

func checkErrorExit(s *probe.Set) []Finding {
	p := s.Get("badFlag")
	if !p.OK() || p.TimedOut {
		return nil
	}
	if p.Code != 0 {
		return []Finding{pass("C-ERROR-EXIT", Clarity, "Bad input fails loudly",
			fmt.Sprintf("An unrecognised flag exited %d.", p.Code))}
	}
	return []Finding{fail("C-ERROR-EXIT", Clarity, "Bad input exits successfully", Serious,
		fmt.Sprintf("Passing %s exited 0. A typo is silently accepted, so the only signal that something went wrong is whatever the user can spot in the output — which is no signal at all if they cannot see it, or are reading it one line at a time.", s.Meta.BadFlag),
		nil, "Reject unknown options with a non-zero exit code.")}
}

func checkErrorStream(s *probe.Set) []Finding {
	p := s.Get("badFlag")
	if !p.OK() || p.TimedOut {
		return nil
	}
	errText := strings.TrimSpace(visibleText(p.Stderr))
	out := strings.TrimSpace(visibleText(p.Stdout))
	if errText != "" {
		return []Finding{pass("C-ERROR-STREAM", Clarity, "Errors go to stderr",
			"The error is visible even when stdout is redirected.")}
	}
	if out != "" {
		return []Finding{fail("C-ERROR-STREAM", Clarity, "Errors printed to stdout", Moderate,
			"The error message went to stdout. Redirect the output to a file — which is exactly what someone does before reading it in an editor with a screen reader — and the error vanishes into the file instead of reaching the terminal.",
			[]string{"stdout: " + excerpt(out, 60)},
			"Diagnostics go to stderr; only the tool's actual product goes to stdout.")}
	}
	return []Finding{fail("C-ERROR-STREAM", Clarity, "Bad input produces no message", Serious,
		"An unrecognised flag produced no output on either stream. The user gets nothing to act on.",
		nil, "Always say what was rejected and why.")}
}

func checkErrorActionable(s *probe.Set) []Finding {
	p := s.Get("badFlag")
	if !p.OK() || p.TimedOut {
		return nil
	}
	text := strings.TrimSpace(visibleText(p.Raw))
	if text == "" {
		return nil
	}
	m := s.Meta
	lines := strings.Split(text, "\n")
	namesInput := strings.Contains(text, m.BadFlag) || strings.Contains(text, strings.TrimPrefix(m.BadFlag, "--"))
	pointsOn := pointsOnRe.MatchString(text)

	var missing []string
	if !namesInput {
		missing = append(missing, "it does not repeat back the input it rejected")
	}
	if !pointsOn {
		missing = append(missing, "it does not say where to look next")
	}
	if len(lines) > 40 {
		missing = append(missing, fmt.Sprintf("it dumps %d lines of help on top of the message", len(lines)))
	}

	first := ""
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			first = l
			break
		}
	}
	if len(missing) == 0 {
		return []Finding{pass("C-ERROR-ACTIONABLE", Clarity, "Errors are actionable",
			"The message names the rejected input and points the user somewhere.", excerpt(first, 70))}
	}
	sev := Minor
	if len(missing) > 1 {
		sev = Moderate
	}
	return []Finding{fail("C-ERROR-ACTIONABLE", Clarity, "Error message is not actionable", sev,
		fmt.Sprintf("The message for an unrecognised flag is incomplete: %s. Read aloud, an error that does not name the offending input and does not say what to do next leaves nothing to act on.", strings.Join(missing, "; ")),
		[]string{"first line: " + excerpt(first, 70)},
		"Quote the input you rejected, suggest the nearest valid option, and name the one command that lists them all.")}
}

func checkHelpStructure(s *probe.Set) []Finding {
	help := helpText(s)
	if help == "" {
		return nil
	}
	hasUsage := usageRe.MatchString(help)
	hasOptions := optionsRe.MatchString(help) || dashLineRe.MatchString(help)
	hasExample := examplesRe.MatchString(help) || exampleCmd.MatchString(help)

	var out []Finding
	var missing []string
	if !hasUsage {
		missing = append(missing, "no usage line")
	}
	if !hasOptions {
		missing = append(missing, "no options section")
	}
	if len(missing) > 0 {
		out = append(out, fail("C-HELP-STRUCTURE", Clarity, "Help has no predictable structure", Moderate,
			fmt.Sprintf("Help text is missing landmarks users navigate by: %s. Reading by speech is linear, so a heading that can be searched for is the only way to skip to the relevant part.", strings.Join(missing, ", ")),
			nil, "Follow the conventional shape: a usage line, then a description, then options, then examples, each under a heading."))
	} else {
		out = append(out, pass("C-HELP-STRUCTURE", Clarity, "Help follows the conventional shape",
			"Usage line and options section both present."))
	}

	if hasExample {
		out = append(out, pass("C-HELP-EXAMPLES", Clarity, "Help includes examples",
			"At least one worked invocation is shown."))
	} else {
		out = append(out, fail("C-HELP-EXAMPLES", Clarity, "Help includes no examples", Minor,
			"No example invocation. An example is a template to copy; assembling one from a list of options is a working-memory task the rest of the help does not require.",
			nil, "Show two or three complete commands covering the common cases."))
	}
	return out
}

var sentenceSplit = regexp.MustCompile(`[.!?](?:\s|$)`)

func checkReadability(s *probe.Set) []Finding {
	help := helpText(s)
	if help == "" {
		return nil
	}
	lines := strings.Split(help, "\n")
	var long []string
	for _, l := range lines {
		if len(strings.TrimSpace(l)) > 100 {
			long = append(long, l)
		}
	}
	words := strings.Fields(help)
	var sentences []string
	for _, sent := range sentenceSplit.Split(help, -1) {
		if len(strings.Fields(sent)) > 3 {
			sentences = append(sentences, sent)
		}
	}
	avg := 0.0
	if len(sentences) > 0 {
		total := 0
		for _, sent := range sentences {
			total += len(strings.Fields(sent))
		}
		avg = float64(total) / float64(len(sentences))
	}

	var problems []string
	if len(long) > 3 {
		problems = append(problems, fmt.Sprintf("%d lines run past 100 characters", len(long)))
	}
	if avg > 25 {
		problems = append(problems, fmt.Sprintf("sentences average %.0f words", avg))
	}
	if len(lines) > 250 {
		problems = append(problems, fmt.Sprintf("%d lines of help arrive in one block", len(lines)))
	}

	if len(problems) == 0 {
		return []Finding{pass("C-READABILITY", Clarity, "Help is readable",
			fmt.Sprintf("%d lines, %d words, sentences averaging %.0f words.", len(lines), len(words), avg))}
	}
	var ev []string
	for _, l := range long {
		ev = append(ev, fmt.Sprintf("%d chars: %s", len(strings.TrimSpace(l)), excerpt(l, 60)))
	}
	return []Finding{fail("C-READABILITY", Clarity, "Help is hard to read", Minor,
		fmt.Sprintf("%s. Long lines and long sentences cost the most for readers with dyslexia or a cognitive disability, and for anyone hearing the text rather than scanning it.", strings.Join(problems, "; ")),
		trim(ev, 3),
		"Wrap to the terminal width, keep sentences short, and split a long help into subcommand help pages.")}
}
