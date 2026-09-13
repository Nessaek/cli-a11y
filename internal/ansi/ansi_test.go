package ansi

import (
	"math"
	"testing"
)

func TestStripRemovesEscapes(t *testing.T) {
	if got := Strip("\x1b[31mred\x1b[0m plain"); got != "red plain" {
		t.Errorf("got %q", got)
	}
}

func TestBoldPlusBasicColourResolvesToTheBrightVariant(t *testing.T) {
	// This is what terminals actually draw, so it is the colour that must be
	// measured for contrast.
	runs := Parse("\x1b[1;33mwarn\x1b[0m").Runs
	if len(runs) != 1 {
		t.Fatalf("got %d runs", len(runs))
	}
	if got := Hex(*runs[0].Style.FG); got != "#ffff00" {
		t.Errorf("effective foreground = %s, want #ffff00", got)
	}
}

func Test256AndTruecolour(t *testing.T) {
	runs := Parse("\x1b[38;5;196ma\x1b[0m\x1b[38;2;18;52;86mb\x1b[0m").Runs
	if len(runs) != 2 {
		t.Fatalf("got %d runs", len(runs))
	}
	if got := Hex(*runs[0].Style.FG); got != "#ff0000" {
		t.Errorf("256-colour = %s, want #ff0000", got)
	}
	if got := Hex(*runs[1].Style.FG); got != "#123456" {
		t.Errorf("truecolour = %s, want #123456", got)
	}
}

func TestControlsThatMatterToAScreenReader(t *testing.T) {
	c := Parse("\rspin\x1b[2K\x1b[?25l\x1b[?1049h\x1b[3A").Controls
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"carriage returns", c.CarriageReturns, 1},
		{"erase line", c.EraseLine, 1},
		{"cursor hide", c.CursorHide, 1},
		{"alt screen enter", c.AltScreenEnter, 1},
		{"cursor moves", c.CursorMove, 1},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

// A pager renders man-page help with bold and underline. Counting those as
// colour means accusing a well-behaved tool of ignoring NO_COLOR.
func TestHasColourIgnoresEmphasis(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"plain text", false},
		{"\x1b[0mreset only", false},
		{"\x1b[1mbold\x1b[m", false},
		{"\x1b[4munderline\x1b[m", false},
		{"\x1b[1m\x1b[4mboth\x1b[m", false},
		{"\x1b[31mred", true},
		{"\x1b[2mfaint", true},
		{"\x1b[7mreverse", true},
		{"\x1b[38;5;120mindexed", true},
		{"\x1b[41mbackground", true},
	}
	for _, c := range cases {
		if got := HasColour(c.in); got != c.want {
			t.Errorf("HasColour(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestContrastMatchesReferenceValues(t *testing.T) {
	if got := Contrast(RGB{255, 255, 255}, RGB{0, 0, 0}); math.Abs(got-21) > 0.01 {
		t.Errorf("white on black = %.2f, want 21", got)
	}
	if got := Contrast(RGB{0, 0, 0}, RGB{0, 0, 0}); math.Abs(got-1) > 0.001 {
		t.Errorf("black on black = %.4f, want 1", got)
	}
	// Contrast is symmetric.
	a, b := RGB{205, 0, 0}, DarkBG
	if math.Abs(Contrast(a, b)-Contrast(b, a)) > 1e-9 {
		t.Error("contrast is not symmetric")
	}
}

func TestRunsCarryTheirLineNumber(t *testing.T) {
	p := Parse("first\n\x1b[32msecond\x1b[0m\nthird")
	for _, r := range p.Runs {
		if r.Text == "second" && r.Line != 1 {
			t.Errorf("line = %d, want 1", r.Line)
		}
	}
}

func TestMalformedSequencesDoNotPanic(t *testing.T) {
	for _, in := range []string{"\x1b", "\x1b[", "\x1b[38;5", "\x1b]0;title", "\x1b[999m", "\x1b[38;2;1m"} {
		Parse(in) // must simply not panic
	}
}
