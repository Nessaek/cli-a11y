// Package ansi parses terminal escape sequences, resolves colours to RGB and
// computes WCAG contrast. Everything here works on the raw bytes a CLI actually
// wrote, because that is the only thing a screen reader or a low-vision user
// ever gets.
package ansi

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// RGB is an 8-bit-per-channel colour.
type RGB [3]int

// BASE16 is xterm's default palette for the 16 named colours. Terminals
// re-theme these, but a CLI that fails contrast against the defaults fails for
// most people.
var BASE16 = [16]RGB{
	{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0},
	{0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
	{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

var names = [16]string{
	"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
	"bright black", "bright red", "bright green", "bright yellow",
	"bright blue", "bright magenta", "bright cyan", "bright white",
}

// The two backgrounds worth testing against: a typical dark terminal and a
// typical light one. A colour that only works on one of them is a real problem,
// because the CLI cannot know which the user has.
var (
	DarkBG  = RGB{30, 30, 30}
	LightBG = RGB{255, 255, 255}
)

var cube = [6]int{0, 95, 135, 175, 215, 255}

// XTerm256 resolves a 256-colour index to RGB.
func XTerm256(n int) RGB {
	switch {
	case n < 16:
		return BASE16[n]
	case n < 232:
		i := n - 16
		return RGB{cube[(i/36)%6], cube[(i/6)%6], cube[i%6]}
	default:
		v := 8 + (n-232)*10
		return RGB{v, v, v}
	}
}

// Luminance is the WCAG relative luminance of a colour.
func Luminance(c RGB) float64 {
	f := func(v int) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c[0]) + 0.7152*f(c[1]) + 0.0722*f(c[2])
}

// Contrast is the WCAG contrast ratio between two colours.
func Contrast(a, b RGB) float64 {
	x, y := Luminance(a), Luminance(b)
	if y > x {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

// Hex renders a colour as #rrggbb.
func Hex(c RGB) string {
	clamp := func(v int) int { return max(0, min(255, v)) }
	return fmt.Sprintf("#%02x%02x%02x", clamp(c[0]), clamp(c[1]), clamp(c[2]))
}

// Describe names the nearest palette colour, for use in a report.
func Describe(c RGB) string {
	best, bestD := 0, math.MaxInt
	for i, p := range BASE16 {
		d := (p[0]-c[0])*(p[0]-c[0]) + (p[1]-c[1])*(p[1]-c[1]) + (p[2]-c[2])*(p[2]-c[2])
		if d < bestD {
			best, bestD = i, d
		}
	}
	if bestD == 0 {
		return names[best]
	}
	return names[best] + "-ish " + Hex(c)
}

// Style is the set of SGR attributes in force for a run of text.
type Style struct {
	FG, BG                            *RGB
	FGName, BGName                    string
	Bold, Dim, Italic, Underline, Rev bool
}

// Run is a stretch of text sharing one style.
type Run struct {
	Text        string
	Style       Style
	Line        int
	Probe       string
	ContextLine string
}

// Controls tallies the sequences that matter to assistive technology.
type Controls struct {
	CarriageReturns, EraseLine, EraseDisplay, CursorMove   int
	CursorHide, CursorShow, AltScreenEnter, AltScreenLeave int
	Bell, OSC, SGR                                         int
}

// Parsed is the result of walking an output stream.
type Parsed struct {
	Plain    string
	Runs     []Run
	Controls Controls
}

func (s *Style) apply(params []int) {
	if len(params) == 0 {
		params = []int{0}
	}
	for i := 0; i < len(params); i++ {
		n := params[i]
		switch {
		case n == 0:
			*s = Style{}
		case n == 1:
			s.Bold = true
		case n == 2:
			s.Dim = true
		case n == 3:
			s.Italic = true
		case n == 4:
			s.Underline = true
		case n == 7:
			s.Rev = true
		case n == 22:
			s.Bold, s.Dim = false, false
		case n == 23:
			s.Italic = false
		case n == 24:
			s.Underline = false
		case n == 27:
			s.Rev = false
		case n >= 30 && n <= 37:
			c := BASE16[n-30]
			s.FG, s.FGName = &c, strconv.Itoa(n)
		case n == 39:
			s.FG, s.FGName = nil, ""
		case n >= 40 && n <= 47:
			c := BASE16[n-40]
			s.BG, s.BGName = &c, strconv.Itoa(n)
		case n == 49:
			s.BG, s.BGName = nil, ""
		case n >= 90 && n <= 97:
			c := BASE16[n-90+8]
			s.FG, s.FGName = &c, strconv.Itoa(n)
		case n >= 100 && n <= 107:
			c := BASE16[n-100+8]
			s.BG, s.BGName = &c, strconv.Itoa(n)
		case n == 38 || n == 48:
			var c RGB
			var name string
			consumed := 0
			if i+1 < len(params) && params[i+1] == 5 && i+2 < len(params) {
				c = XTerm256(params[i+2])
				name = fmt.Sprintf("%d;5;%d", n, params[i+2])
				consumed = 2
			} else if i+1 < len(params) && params[i+1] == 2 && i+4 < len(params) {
				c = RGB{params[i+2], params[i+3], params[i+4]}
				name = fmt.Sprintf("%d;2;%d;%d;%d", n, params[i+2], params[i+3], params[i+4])
				consumed = 4
			} else {
				continue
			}
			if n == 38 {
				s.FG, s.FGName = &c, name
			} else {
				s.BG, s.BGName = &c, name
			}
			i += consumed
		}
	}
}

// EffectiveFG accounts for terminals rendering bold plus a basic colour as the
// bright variant, so the colour a CLI gets is often not the one it named.
func EffectiveFG(s Style) *RGB {
	if s.FG == nil {
		return nil
	}
	if s.Bold && len(s.FGName) == 2 && s.FGName[0] == '3' && s.FGName[1] >= '0' && s.FGName[1] <= '7' {
		n, _ := strconv.Atoi(s.FGName)
		c := BASE16[n-30+8]
		return &c
	}
	return s.FG
}

func isFinal(b byte) bool { return b >= '@' && b <= '~' }

// Parse walks a raw output stream, returning the visible text, the styled runs
// within it, and a tally of the control sequences that affect assistive tech.
func Parse(raw string) Parsed {
	var (
		style   Style
		out     Parsed
		plain   strings.Builder
		cur     *Run
		lineNum int
	)

	flush := func() {
		if cur != nil && cur.Text != "" {
			out.Runs = append(out.Runs, *cur)
		}
		cur = nil
	}

	for i := 0; i < len(raw); i++ {
		ch := raw[i]

		if ch == 0x1b {
			if i+1 < len(raw) && raw[i+1] == '[' {
				j := i + 2
				start := j
				for j < len(raw) && !isFinal(raw[j]) {
					j++
				}
				if j >= len(raw) {
					i = len(raw)
					break
				}
				body := raw[start:j]
				final := raw[j]
				private := body != "" && strings.ContainsRune("?<>=", rune(body[0]))
				nums := parseParams(strings.TrimLeft(body, "?<>="))

				switch {
				case final == 'm' && !private:
					flush()
					style.apply(nums)
					out.Controls.SGR++
				case final == 'K':
					out.Controls.EraseLine++
				case final == 'J':
					out.Controls.EraseDisplay++
				case strings.IndexByte("ABCDEFGHfd", final) >= 0:
					out.Controls.CursorMove++
				case private && (final == 'h' || final == 'l'):
					mode := 0
					if len(nums) > 0 {
						mode = nums[0]
					}
					if mode == 25 {
						if final == 'l' {
							out.Controls.CursorHide++
						} else {
							out.Controls.CursorShow++
						}
					}
					if mode == 1049 || mode == 47 || mode == 1047 {
						if final == 'h' {
							out.Controls.AltScreenEnter++
						} else {
							out.Controls.AltScreenLeave++
						}
					}
				}
				i = j
				continue
			}
			if i+1 < len(raw) && raw[i+1] == ']' {
				j := i + 2
				for j < len(raw) && raw[j] != 0x07 && !(raw[j] == 0x1b && j+1 < len(raw) && raw[j+1] == '\\') {
					j++
				}
				out.Controls.OSC++
				if j < len(raw) && raw[j] == 0x1b {
					j++
				}
				i = j
				continue
			}
			i++
			continue
		}

		if ch == '\r' {
			out.Controls.CarriageReturns++
			plain.WriteByte(ch)
			continue
		}
		if ch == 0x07 {
			out.Controls.Bell++
			continue
		}

		plain.WriteByte(ch)
		if ch == '\n' {
			flush()
			lineNum++
			continue
		}

		if cur == nil {
			s := style
			s.FG = EffectiveFG(style)
			cur = &Run{Style: s, Line: lineNum}
		}
		cur.Text += string(ch)
	}
	flush()

	out.Plain = plain.String()
	return out
}

func parseParams(body string) []int {
	if body == "" {
		return nil
	}
	parts := strings.Split(body, ";")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			nums = append(nums, 0)
			continue
		}
		if n, err := strconv.Atoi(p); err == nil {
			nums = append(nums, n)
		}
	}
	return nums
}

// Strip removes every escape sequence, leaving what a plain-text reader gets.
func Strip(raw string) string { return Parse(raw).Plain }

var sgrRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// IsColourParam reports whether an SGR parameter actually changes a colour.
// Bold, underline and italic are emphasis: they survive a monochrome terminal,
// they carry meaning without it, and NO_COLOR is not asking anyone to give them
// up. Faint and reverse do change what colour lands on the screen, so they
// count.
func IsColourParam(n int) bool {
	return (n >= 30 && n <= 49) || (n >= 90 && n <= 97) || (n >= 100 && n <= 107) ||
		n == 2 || n == 7
}

// HasColour reports whether a stream sets a colour, as opposed to merely
// emphasising text.
//
// The distinction matters more than it looks. A pager renders man-page style
// help with bold and underline, and those bytes arrive in a capture mixed in
// with the CLI's own; counting them as colour means accusing a perfectly
// well-behaved tool of ignoring NO_COLOR.
func HasColour(raw string) bool {
	for _, seq := range sgrRe.FindAllString(raw, -1) {
		for _, n := range parseParams(seq[2 : len(seq)-1]) {
			if IsColourParam(n) {
				return true
			}
		}
	}
	return false
}

// CountSGR counts styling sequences, for reporting.
func CountSGR(raw string) int { return len(sgrRe.FindAllString(raw, -1)) }
