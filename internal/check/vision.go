package check

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Nessaek/cli-a11y/internal/ansi"
	"github.com/Nessaek/cli-a11y/internal/probe"
	"github.com/Nessaek/cli-a11y/internal/run"
)

// Checks for people who use a screen reader, a magnifier, a high-contrast
// theme, or simply cannot pick a red "FAILED" out of a wall of white text.

func visionChecks(s *probe.Set) []Finding {
	var out []Finding
	out = append(out, checkPipeColour(s))
	out = append(out, checkNoColor(s))
	out = append(out, checkTermDumb(s))
	out = append(out, checkContrast(s))
	out = append(out, checkDim(s))
	out = append(out, checkColourOnly(s))
	out = append(out, checkAnimation(s))
	out = append(out, checkTerminalState(s))
	out = append(out, checkGlyphs(s))
	out = append(out, checkPager(s))
	out = append(out, checkReflow(s)...)
	out = append(out, checkWidth(s)...)
	return out
}

// semantic colours: red, green, yellow and their bright forms.
var semantic = map[int]bool{1: true, 2: true, 3: true, 9: true, 10: true, 11: true}

func nearestIndex(c ansi.RGB) int {
	best, bestD := 0, 1<<30
	for i, p := range ansi.BASE16 {
		d := (p[0]-c[0])*(p[0]-c[0]) + (p[1]-c[1])*(p[1]-c[1]) + (p[2]-c[2])*(p[2]-c[2])
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// Terminals draw SGR 2 (faint) by blending the colour halfway into the
// background. The named colour is not what lands on the screen.
func applyDim(fg, bg ansi.RGB) ansi.RGB {
	return ansi.RGB{
		(fg[0] + bg[0]) / 2,
		(fg[1] + bg[1]) / 2,
		(fg[2] + bg[2]) / 2,
	}
}

// Faint text that sets no colour of its own is still a contrast problem — the
// most common one — so the terminal's default foreground stands in for it.
func defaultFgFor(bg ansi.RGB) ansi.RGB {
	if ansi.Luminance(bg) > 0.5 {
		return ansi.RGB{0, 0, 0}
	}
	return ansi.RGB{204, 204, 204}
}

// A probe sitting in a pager is not showing the CLI's own output any more: less
// draws its own styling, and we killed the process mid-session. Nothing about
// colour can be concluded from it.
func usableForColour(p *run.Result) bool { return p.OK() && !isPaging(p) }

// needsTty gives the honest answer where no pty was available. Reporting a pass
// for a probe that never ran is the one thing an audit tool must never do.
func needsTty(s *probe.Set, id, title string) (Finding, bool) {
	for _, p := range s.Results {
		if p.OK() && p.TTY {
			return Finding{}, false
		}
	}
	return skip(id, Vision, title,
		"Not checked: no terminal probe was possible on this machine, so there was no terminal output to inspect."), true
}

// styledRuns is every styled run across the usable pty probes, carrying the
// probe it came from and the full plain-text line it sits on — a run only means
// something in the context of the line a reader would hear.
func styledRuns(s *probe.Set) []ansi.Run {
	var out []ansi.Run
	ids := sortedIDs(s)
	for _, id := range ids {
		p := s.Results[id]
		if !usableForColour(p) || !p.TTY {
			continue
		}
		parsed := ansi.Parse(p.Output)
		lines := strings.Split(parsed.Plain, "\n")
		for i := range lines {
			lines[i] = renderLine(lines[i])
		}
		for _, r := range parsed.Runs {
			if strings.TrimSpace(r.Text) == "" {
				continue
			}
			st := r.Style
			if st.FG == nil && st.BG == nil && !st.Dim && !st.Rev {
				continue
			}
			r.Probe = id
			if r.Line < len(lines) {
				r.ContextLine = lines[r.Line]
			} else {
				r.ContextLine = r.Text
			}
			out = append(out, r)
		}
	}
	return out
}

func sortedIDs(s *probe.Set) []string {
	ids := make([]string, 0, len(s.Results))
	for id := range s.Results {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func colouredFamilies(s *probe.Set) []probe.Family {
	var out []probe.Family
	for _, f := range s.Meta.Families {
		p := s.Get(f.TTY)
		if usableForColour(p) && ansi.HasColour(p.Raw) {
			out = append(out, f)
		}
	}
	return out
}

func checkPipeColour(s *probe.Set) Finding {
	var ev []string
	for _, f := range s.Meta.Families {
		p := s.Get(f.Pipe)
		if p.OK() && ansi.HasColour(p.Raw) {
			ev = append(ev, fmt.Sprintf("%s: %d styling sequences with stdout piped", p.ID, ansi.CountSGR(p.Raw)))
		}
	}
	if len(ev) == 0 {
		return pass("V-PIPE-COLOUR", Vision, "Colour suppressed when piped",
			"No escape sequences when stdout is not a terminal.")
	}
	return fail("V-PIPE-COLOUR", Vision, "Colour written to a pipe", Serious,
		"Escape sequences appear when stdout is not a terminal. Anything that redirects this output — a log file, a pager, a screen reader reading a saved transcript, a CI artefact — gets the raw control codes as literal text.",
		trim(ev, 3),
		"Gate colour on isatty(stdout), as almost every colour library does by default.")
}

// NO_COLOR and TERM=dumb are the same check against a different switch.
func checkColourSwitch(s *probe.Set, id, title, label string, sev Severity, pick func(probe.Family) string, detail, remedy string) Finding {
	coloured := colouredFamilies(s)
	if len(coloured) == 0 {
		return skip(id, Vision, title,
			"The CLI emits no colour on a terminal either, so there is nothing to turn off.")
	}
	var ev []string
	for _, f := range coloured {
		p := s.Get(pick(f))
		if usableForColour(p) && ansi.HasColour(p.Raw) {
			ev = append(ev, fmt.Sprintf("%s: still emitted %d styling sequences", p.ID, ansi.CountSGR(p.Raw)))
		}
	}
	if len(ev) == 0 {
		return pass(id, Vision, title, "Setting "+label+" removed all styling.")
	}
	return fail(id, Vision, label+" ignored", sev, detail, trim(ev, 3), remedy)
}

func checkNoColor(s *probe.Set) Finding {
	return checkColourSwitch(s, "V-NO-COLOR", "NO_COLOR honoured", "NO_COLOR", Serious,
		func(f probe.Family) string { return f.NoColor },
		"Colour is still emitted with NO_COLOR set. That variable is the one switch users of high-contrast themes and screen readers are told to set, and it is expected to work everywhere.",
		"Treat any non-empty NO_COLOR as \"disable all colour\", per no-color.org. Honour TERM=dumb the same way.")
}

func checkTermDumb(s *probe.Set) Finding {
	return checkColourSwitch(s, "V-TERM-DUMB", "TERM=dumb honoured", "TERM=dumb", Moderate,
		func(f probe.Family) string { return f.Dumb },
		"Styling is still emitted under TERM=dumb, which declares a terminal with no such capability. Emacs shell buffers and several screen-reader terminals set it.",
		"Check the terminfo capability, or at minimum special-case TERM=dumb alongside NO_COLOR.")
}

type contrastFailure struct {
	run        ansi.Run
	ratios     []ratio
	everywhere bool
	tier       Severity
}

type ratio struct {
	label string
	value float64
}

func checkContrast(s *probe.Set) Finding {
	if f, skipped := needsTty(s, "V-CONTRAST", "Colour contrast"); skipped {
		return f
	}
	runs := styledRuns(s)
	if len(runs) == 0 {
		return pass("V-CONTRAST", Vision, "Colour contrast",
			"No coloured output was produced, so there is nothing that can fail contrast.")
	}

	// One entry per distinct colour pairing, so a colour used on 400 lines is
	// reported once.
	seen := map[string]ansi.Run{}
	var order []string
	for _, r := range runs {
		// Either an explicit colour, or faint text taking the default one.
		if r.Style.FG == nil && !r.Style.Dim {
			continue
		}
		key := fmt.Sprintf("%s|%s|%v", r.Style.FGName, r.Style.BGName, r.Style.Dim)
		if _, ok := seen[key]; !ok {
			seen[key] = r
			order = append(order, key)
		}
	}

	var failures []contrastFailure
	for _, key := range order {
		r := seen[key]
		type bgOpt struct {
			label string
			rgb   ansi.RGB
		}
		var bgs []bgOpt
		if r.Style.BG != nil {
			bgs = []bgOpt{{"the background it sets itself", *r.Style.BG}}
		} else {
			bgs = []bgOpt{{"a dark terminal", ansi.DarkBG}, {"a light terminal", ansi.LightBG}}
		}
		var rs []ratio
		bad, best := 0, 0.0
		for _, b := range bgs {
			fg := defaultFgFor(b.rgb)
			if r.Style.FG != nil {
				fg = *r.Style.FG
			}
			if r.Style.Dim {
				fg = applyDim(fg, b.rgb)
			}
			v := ansi.Contrast(fg, b.rgb)
			rs = append(rs, ratio{b.label, v})
			if v < 4.5 {
				bad++
			}
			if v > best {
				best = v
			}
		}
		if bad == 0 {
			continue
		}
		// The friendlier of the two backgrounds decides how bad this is: a
		// colour that works on a dark terminal is merely a theme assumption,
		// one that works on neither cannot be read at all.
		tier := Minor
		if bad == len(rs) {
			tier = Moderate
			if best < 3 {
				tier = Serious
			}
		}
		failures = append(failures, contrastFailure{r, rs, bad == len(rs), tier})
	}

	if len(failures) == 0 {
		return pass("V-CONTRAST", Vision, "Colour contrast",
			fmt.Sprintf("All %d colour combinations reach WCAG AA (4.5:1).", len(seen)))
	}

	sort.SliceStable(failures, func(i, j int) bool { return failures[i].tier < failures[j].tier })
	worst := failures[0].tier

	var ev []string
	for _, f := range failures {
		var parts []string
		for _, r := range f.ratios {
			parts = append(parts, fmt.Sprintf("%.2f:1 on %s", r.value, r.label))
		}
		base := "the default foreground"
		if f.run.Style.FG != nil {
			base = ansi.Describe(*f.run.Style.FG)
		}
		if f.run.Style.Dim {
			base += " (faint)"
		}
		ev = append(ev, fmt.Sprintf("%s — %s — e.g. %q", base, strings.Join(parts, ", "), excerpt(f.run.Text, 40)))
	}

	unreadable, bothWays := 0, 0
	for _, f := range failures {
		if f.tier == Serious {
			unreadable++
		}
		if f.everywhere {
			bothWays++
		}
	}
	detail := fmt.Sprintf("%d colour combination(s) fall below WCAG AA (4.5:1). ", len(failures))
	switch {
	case unreadable > 0:
		detail += fmt.Sprintf("%d of them stay under 3:1 whichever background the user has, which is below the threshold for even large text.", unreadable)
	case bothWays > 0:
		detail += fmt.Sprintf("%d fall short on both a dark and a light terminal, though not severely.", bothWays)
	default:
		detail += "Each works on one of the two common backgrounds and not the other, and the CLI cannot know which the user has."
	}

	return fail("V-CONTRAST", Vision, "Colour contrast below WCAG AA", worst, detail, trim(ev, 8),
		"Prefer the bright variants of the ANSI colours, or pair colour with bold. Never rely on faint (SGR 2) for anything that must be read.")
}

func checkDim(s *probe.Set) Finding {
	if f, skipped := needsTty(s, "V-DIM", "Faint text"); skipped {
		return f
	}
	var dim []ansi.Run
	for _, r := range styledRuns(s) {
		if r.Style.Dim && len(strings.TrimSpace(r.Text)) > 2 {
			dim = append(dim, r)
		}
	}
	if len(dim) == 0 {
		return pass("V-DIM", Vision, "Faint text", "No faint (SGR 2) text was used.")
	}
	var ev []string
	for _, r := range dim {
		ev = append(ev, fmt.Sprintf("%s: %q", r.Probe, excerpt(r.Text, 50)))
	}
	return fail("V-DIM", Vision, "Faint text used for real content", Moderate,
		fmt.Sprintf("%d run(s) use faint styling, which most terminals render by blending the text halfway into the background. It is the single most common cause of unreadable CLI output for low-vision users.", len(dim)),
		trim(ev, 5),
		"Use faint only for decoration that repeats information available elsewhere. Never for hints, paths, defaults or timings the user has to read.")
}

var markerRe = regexp.MustCompile(`(?i)\b(error|err|warn|warning|fail|failed|failure|ok|pass|passed|success|succeeded|added|removed|deleted|new|skip|skipped|todo|note|info|deprecat|missing|invalid|yes|no)\b|[✓✔✗✘×√!]|^\s*[-+*]\s|\[[!?x+ -]\]|\bE\d{2,}\b`)

var (
	labelSymbols = regexp.MustCompile(`^[✓✔✗✘×√!+\-*·•]+$`)
	labelUpper   = regexp.MustCompile(`^[A-Z][A-Z0-9 _-]{1,11}$`)
	labelWord    = regexp.MustCompile(`(?i)^(ok|pass(ed)?|fail(ed|ure)?|warn(ing)?|err(or)?|done|skip(ped)?|new|added|removed|deleted|yes|no|todo|info|note|up|down)$`)
	bracketTrim  = regexp.MustCompile(`^[\[(<{]+|[\])>}]+$`)
)

// A coloured run that is itself a status token — [ok], DOWN, a cross — carries
// its meaning in the text. A coloured run that is a value, like a hostname,
// does not.
func isLabel(text string) bool {
	t := strings.TrimSpace(bracketTrim.ReplaceAllString(strings.TrimSpace(text), ""))
	if t == "" || len([]rune(t)) > 12 {
		return false
	}
	return labelSymbols.MatchString(t) || labelUpper.MatchString(t) || labelWord.MatchString(t)
}

func checkColourOnly(s *probe.Set) Finding {
	if f, skipped := needsTty(s, "V-COLOUR-ONLY", "Colour is not the only signal"); skipped {
		return f
	}
	var candidates []ansi.Run
	for _, r := range styledRuns(s) {
		if r.Style.FG != nil && semantic[nearestIndex(*r.Style.FG)] {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return pass("V-COLOUR-ONLY", Vision, "Colour is not the only signal",
			"No red, green or yellow text was used to convey status.")
	}

	var bad []ansi.Run
	seen := map[string]bool{}
	for _, r := range candidates {
		if markerRe.MatchString(r.ContextLine) || isLabel(r.Text) {
			continue
		}
		key := excerpt(r.Text, 40)
		if seen[key] {
			continue
		}
		seen[key] = true
		bad = append(bad, r)
	}
	if len(bad) == 0 {
		return pass("V-COLOUR-ONLY", Vision, "Colour is not the only signal",
			"Every coloured status string also carries a word or symbol that says the same thing.")
	}
	var ev []string
	for _, r := range bad {
		ev = append(ev, fmt.Sprintf("%s: %q", ansi.Describe(*r.Style.FG), excerpt(r.Text, 50)))
	}
	return fail("V-COLOUR-ONLY", Vision, "Colour used as the only signal", Serious,
		fmt.Sprintf("%d coloured string(s) carry meaning that disappears entirely when colour is stripped — which is what happens for the roughly 1 in 12 men with a colour vision deficiency, for anyone piping to a file, and for every screen reader.", len(bad)),
		trim(ev, 6),
		"Put the meaning in the text: a word (\"error\", \"added\"), a symbol with an ASCII fallback, or a prefix. Colour should reinforce, never carry.")
}

func checkAnimation(s *probe.Set) Finding {
	if f, skipped := needsTty(s, "V-ANIMATION", "In-place redrawing"); skipped {
		return f
	}
	var worstID string
	var worst ansi.Controls
	worstRate, worstSecs := 0.0, 0.0
	for _, id := range sortedIDs(s) {
		p := s.Results[id]
		if !p.OK() || !p.TTY {
			continue
		}
		c := ansi.Parse(p.Output).Controls
		repaints := c.CarriageReturns + c.EraseLine + c.EraseDisplay
		if repaints < 5 {
			continue
		}
		secs := max(0.25, p.Duration.Seconds())
		if rate := float64(repaints) / secs; rate > worstRate {
			worstRate, worstID, worst, worstSecs = rate, id, c, p.Duration.Seconds()
		}
	}
	if worstID == "" {
		return pass("V-ANIMATION", Vision, "No in-place redrawing",
			"Nothing rewrote a line in place, so output reads once and stays put.")
	}

	quiet := mentions(helpText(s), []string{"--quiet", "--no-progress", "--no-spinner", "--plain", "--silent", "--progress="})
	sev := Moderate
	if worstRate > 2 {
		sev = Serious
	}
	quietNote := "help mentions no flag that turns this off"
	remedy := "Fall back to one line per event when stdout is not a terminal, and add a --quiet or --no-progress flag. Honouring NO_COLOR here too is a reasonable convention."
	if len(quiet) > 0 {
		quietNote = "help does mention: " + strings.Join(quiet, ", ")
		remedy = "The escape hatch exists; make sure it is also taken automatically when stdout is not a terminal, and mention it near the progress output."
	}
	repaints := worst.CarriageReturns + worst.EraseLine + worst.EraseDisplay
	return fail("V-ANIMATION", Vision, "Output redrawn in place", sev,
		fmt.Sprintf("%d in-place repaints in %.1fs (%.1f/s) during %q. A screen reader re-announces a line every time it is rewritten, so a progress spinner becomes a continuous stream of speech that cannot be interrupted or read past.",
			repaints, worstSecs, worstRate, worstID),
		[]string{
			fmt.Sprintf("carriage returns: %d, erase-line: %d, erase-display: %d, cursor moves: %d",
				worst.CarriageReturns, worst.EraseLine, worst.EraseDisplay, worst.CursorMove),
			quietNote,
		}, remedy)
}

func checkTerminalState(s *probe.Set) Finding {
	if f, skipped := needsTty(s, "V-TERM-STATE", "Terminal state on exit"); skipped {
		return f
	}
	var broken []string
	for _, id := range sortedIDs(s) {
		p := s.Results[id]
		// We killed this one at the timeout, so it never reached its own
		// cleanup. Blaming it for the state we interrupted would be a false
		// accusation.
		if !p.OK() || !p.TTY || p.TimedOut {
			continue
		}
		c := ansi.Parse(p.Output).Controls
		if c.CursorHide > c.CursorShow {
			broken = append(broken, fmt.Sprintf("%s: cursor hidden %d× but restored %d×", id, c.CursorHide, c.CursorShow))
		}
		if c.AltScreenEnter > c.AltScreenLeave {
			broken = append(broken, fmt.Sprintf("%s: entered the alternate screen %d× but left it %d×", id, c.AltScreenEnter, c.AltScreenLeave))
		}
	}
	if len(broken) == 0 {
		return pass("V-TERM-STATE", Vision, "Terminal left in a usable state",
			"Cursor visibility and screen buffer were restored on exit.")
	}
	return fail("V-TERM-STATE", Vision, "Terminal left in a broken state", Serious,
		"The CLI exited without restoring what it changed. An invisible cursor or a stuck alternate screen persists into every later command, and the usual fix — typing \"reset\" blind — is exactly what a new or low-vision user cannot do.",
		trim(broken, 5),
		"Restore on every exit path, including SIGINT and unhandled errors, not just the happy one.")
}

type glyphClass struct {
	name string
	in   func(rune) bool
}

var glyphClasses = []glyphClass{
	{"box drawing", func(c rune) bool { return c >= 0x2500 && c <= 0x257f }},
	{"block elements", func(c rune) bool { return c >= 0x2580 && c <= 0x259f }},
	{"braille (spinner frames)", func(c rune) bool { return c >= 0x2800 && c <= 0x28ff }},
	{"emoji", func(c rune) bool { return (c >= 0x1f300 && c <= 0x1faff) || (c >= 0x2600 && c <= 0x27bf) }},
	{"arrows and symbols", func(c rune) bool {
		return (c >= 0x2190 && c <= 0x21ff) || (c >= 0x25a0 && c <= 0x25ff)
	}},
}

func checkGlyphs(s *probe.Set) Finding {
	if f, skipped := needsTty(s, "V-GLYPHS", "Decorative glyphs"); skipped {
		return f
	}
	counts := map[string]int{}
	samples := map[string]string{}
	for _, id := range sortedIDs(s) {
		p := s.Results[id]
		if !p.OK() || !p.TTY {
			continue
		}
		for _, c := range visibleText(p.Raw) {
			if c < 0x80 {
				continue
			}
			for _, g := range glyphClasses {
				if g.in(c) {
					counts[g.name]++
					if _, ok := samples[g.name]; !ok {
						samples[g.name] = string(c)
					}
				}
			}
		}
	}
	if len(counts) == 0 {
		return pass("V-GLYPHS", Vision, "Decorative glyphs",
			"Output is ASCII, which every terminal and screen reader handles.")
	}

	nonASCII := func(id string) int {
		p := s.Get(id)
		if !p.OK() {
			return 0
		}
		n := 0
		for _, c := range visibleText(p.Raw) {
			if c > 0x7f {
				n++
			}
		}
		return n
	}
	// An ASCII fallback under TERM=dumb is the mitigation that matters.
	dumbN, ttyN := nonASCII("helpDumb"), nonASCII("help")
	fallback := ttyN > 0 && dumbN < ttyN/2

	type kv struct {
		name string
		n    int
	}
	var rows []kv
	total := 0
	for k, v := range counts {
		rows = append(rows, kv{k, v})
		total += v
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].n > rows[j].n })
	var ev []string
	for _, r := range rows {
		ev = append(ev, fmt.Sprintf("%d× %s (%s)", r.n, r.name, samples[r.name]))
	}

	if fallback {
		return pass("V-GLYPHS", Vision, "Decorative glyphs have an ASCII fallback",
			fmt.Sprintf("Uses %d non-ASCII glyphs on a capable terminal but falls back under TERM=dumb.", total),
			trim(ev, 4)...)
	}
	sev := Minor
	if counts["box drawing"]+counts["block elements"] > 40 {
		sev = Moderate
	}
	return fail("V-GLYPHS", Vision, "Decorative glyphs with no ASCII fallback", sev,
		fmt.Sprintf("%d non-ASCII glyphs with no plainer alternative under TERM=dumb. Screen readers handle these inconsistently: box-drawing characters are often announced cell by cell, turning a table border into a paragraph of speech, and braille spinner frames are read as literal braille.", total),
		trim(ev, 5),
		"Offer an ASCII table or marker style and select it under TERM=dumb, NO_COLOR, or an explicit flag. Never put information only in a glyph.")
}

func checkPager(s *probe.Set) Finding {
	if f, skipped := needsTty(s, "V-PAGER", "Paged output"); skipped {
		return f
	}
	var paged []string
	for _, id := range sortedIDs(s) {
		if isPaging(s.Results[id]) {
			paged = append(paged, id)
		}
	}
	if len(paged) == 0 {
		return pass("V-PAGER", Vision, "Output is not forced through a pager",
			"Nothing took over the alternate screen and waited.")
	}
	var ev []string
	for _, id := range paged {
		ev = append(ev, id+": entered the alternate screen and waited at a pager prompt")
	}
	return fail("V-PAGER", Vision, "Output forced through a pager", Minor,
		fmt.Sprintf("%s handed off to a pager, which takes over the alternate screen and waits for keystrokes. Paging is conventional and the piped output is unaffected, but the alternate screen is the part of the terminal screen readers handle worst, and navigating it needs repeated precise keypresses.", strings.Join(paged, ", ")),
		ev,
		"Keep the paging, and document the way out — a --no-pager flag, or honouring PAGER=cat. Never page output short enough to fit the screen.")
}

func checkReflow(s *probe.Set) []Finding {
	type over struct {
		probe string
		n, w  int
		text  string
	}
	var wide []over
	examined := 0
	for _, f := range s.Meta.Families {
		p := s.Get(f.Narrow)
		if !p.OK() {
			continue
		}
		examined++
		for i, l := range visibleLines(p.Raw) {
			if w := displayWidth(l); w > s.Meta.NarrowCols {
				wide = append(wide, over{f.Narrow, i + 1, w, l})
			}
		}
	}
	if examined == 0 {
		if f, skipped := needsTty(s, "V-REFLOW", "Reflow to a narrow terminal"); skipped {
			return []Finding{f}
		}
		return nil
	}
	if len(wide) == 0 {
		return []Finding{pass("V-REFLOW", Vision, "Reflows to a narrow terminal",
			fmt.Sprintf("No line exceeded %d columns in a %d-column terminal.", s.Meta.NarrowCols, s.Meta.NarrowCols))}
	}
	worst := wide[0]
	for _, o := range wide {
		if o.w > worst.w {
			worst = o
		}
	}
	sev := Minor
	if len(wide) > 10 {
		sev = Moderate
	}
	var ev []string
	for _, o := range wide {
		ev = append(ev, fmt.Sprintf("%s line %d (%d cols): %s", o.probe, o.n, o.w, excerpt(o.text, 55)))
	}
	return []Finding{fail("V-REFLOW", Vision, "Output overflows a narrow terminal", sev,
		fmt.Sprintf("%d line(s) are wider than the %d-column terminal they were printed into, the widest at %d columns. A magnifier user at 4× has roughly this much width, and hard wrapping mid-word plus aligned columns that no longer align is what they get.",
			len(wide), s.Meta.NarrowCols, worst.w),
		trim(ev, 4),
		"Wrap on word boundaries to the real terminal width, and collapse multi-column layouts below about 60 columns.")}
}

func checkWidth(s *probe.Set) []Finding {
	maxW := func(p *run.Result) int {
		w := 0
		for _, l := range visibleLines(p.Raw) {
			if d := displayWidth(l); d > w {
				w = d
			}
		}
		return w
	}
	type row struct {
		name   string
		nw, ww int
	}
	var rows []row
	for _, f := range s.Meta.Families {
		n, w := s.Get(f.Narrow), s.Get(f.Wide)
		if !n.OK() || !w.OK() {
			continue
		}
		rows = append(rows, row{f.Name, maxW(n), maxW(w)})
	}
	if len(rows) == 0 {
		if f, skipped := needsTty(s, "V-WIDTH", "Terminal width awareness"); skipped {
			return []Finding{f}
		}
		return nil
	}
	fits := 0
	var frozen []row
	for _, r := range rows {
		if r.nw <= s.Meta.NarrowCols {
			fits++
		} else if r.nw == r.ww && r.ww > s.Meta.NarrowCols {
			frozen = append(frozen, r)
		}
	}
	if fits == len(rows) {
		return []Finding{pass("V-WIDTH", Vision, "Respects the terminal width",
			fmt.Sprintf("Output narrowed to fit a %d-column terminal.", s.Meta.NarrowCols))}
	}
	if len(frozen) > 0 {
		var ev []string
		for _, r := range frozen {
			ev = append(ev, fmt.Sprintf("%s: %d columns at both %d and %d columns of terminal",
				r.name, r.nw, s.Meta.NarrowCols, s.Meta.WideCols))
		}
		return []Finding{fail("V-WIDTH", Vision, "Terminal width ignored", Moderate,
			fmt.Sprintf("Output is the same width whether the terminal is %d or %d columns. The layout is hard-coded, so it can never fit a resized or magnified window.",
				s.Meta.NarrowCols, s.Meta.WideCols),
			trim(ev, 3),
			"Read the width from the terminal (ioctl, or $COLUMNS as a fallback) and lay out against it, clamping to a sane minimum.")}
	}
	return []Finding{pass("V-WIDTH", Vision, "Responds to the terminal width",
		"Output width changed with the terminal, though some lines still overflow.")}
}

func trim(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
