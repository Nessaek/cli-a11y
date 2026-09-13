// Package probe runs the target under every condition worth comparing.
//
// Probes are grouped into families — the same invocation under several
// conditions — because most of the interesting questions are comparisons.
// "Does it honour NO_COLOR?" only means something against the same command run
// without it, and colour usually lives in a tool's real output rather than in
// its help text, so every family is exercised, not just --help.
//
// Checks never spawn anything themselves. They read these results, so the
// target runs a fixed number of times no matter how many rules exist.
package probe

import (
	"fmt"
	"time"

	"github.com/Nessaek/cli-a11y/internal/run"
)

const (
	// NarrowCols is roughly the width a magnifier user has at 4x.
	NarrowCols = 40
	// WideCols is a comfortable terminal, for comparison.
	WideCols = 100
	// BadFlag is a flag nothing sane implements, used to provoke the error path.
	BadFlag = "--cli-a11y-nonexistent-flag"
)

// Family is one invocation under every condition, by probe ID.
type Family struct {
	Name                                   string
	Args                                   []string
	TTY, Pipe, NoColor, Dumb, Narrow, Wide string
}

// Meta describes how the probes were taken.
type Meta struct {
	BadFlag, HelpFlag, VersionFlag string
	NarrowCols, WideCols           int
	Families                       []Family
	ProbeCount                     int
	Duration                       time.Duration
}

// Set is every recorded run, plus how they were taken.
type Set struct {
	Results map[string]*run.Result
	Meta    Meta
}

// Get returns a probe by ID, or nil.
func (s *Set) Get(id string) *run.Result { return s.Results[id] }

// Target is the CLI under test.
type Target struct {
	Cmd  string
	Args []string
}

// Extra is an additional invocation the user asked to exercise.
type Extra struct {
	Label string
	Args  []string
}

// Options configures a collection.
type Options struct {
	Timeout     time.Duration
	Dir         string
	HelpFlag    string
	VersionFlag string
	Extra       []Extra
	OnProgress  func(id string, done, total int)
}

type plan struct {
	id   string
	args []string
	opts run.Options
}

// Collect executes the whole matrix.
func Collect(t Target, o Options) *Set {
	if o.Timeout == 0 {
		o.Timeout = run.DefaultTimeout
	}
	if o.HelpFlag == "" {
		o.HelpFlag = "--help"
	}
	if o.VersionFlag == "" {
		o.VersionFlag = "--version"
	}
	if o.OnProgress == nil {
		o.OnProgress = func(string, int, int) {}
	}

	with := func(more ...string) []string {
		return append(append([]string{}, t.Args...), more...)
	}

	// A bare run may never terminate by design, so it gets a shorter leash.
	bareTimeout := min(o.Timeout, 6*time.Second)

	var plans []plan
	var families []Family

	// family queues one invocation under every condition worth comparing.
	// Where the family is meant to catch a CLI that waits for input, only the
	// plain terminal and piped runs hold stdin open; the rest get an immediate
	// EOF so a genuinely interactive tool costs two timeouts rather than six.
	family := func(name string, args []string, base run.Options, holds bool) {
		f := Family{Name: name, Args: args}
		add := func(suffix string, mod func(*run.Options)) string {
			id := name + suffix
			opts := base
			opts.Timeout = base.Timeout
			if holds && (suffix == "" || suffix == "Pipe") {
				opts.Stdin = run.StdinHold
			} else {
				opts.Stdin = run.StdinClose
			}
			mod(&opts)
			plans = append(plans, plan{id, args, opts})
			return id
		}
		f.TTY = add("", func(o *run.Options) { o.TTY = true; o.Cols = 80 })
		f.Pipe = add("Pipe", func(o *run.Options) { o.TTY = false })
		f.NoColor = add("NoColor", func(o *run.Options) {
			o.TTY = true
			o.Env = map[string]string{"NO_COLOR": "1"}
		})
		f.Dumb = add("Dumb", func(o *run.Options) {
			o.TTY = true
			o.Env = map[string]string{"TERM": "dumb"}
		})
		f.Narrow = add("Narrow", func(o *run.Options) { o.TTY = true; o.Cols = NarrowCols })
		f.Wide = add("Wide", func(o *run.Options) { o.TTY = true; o.Cols = WideCols })
		families = append(families, f)
	}

	common := run.Options{Timeout: o.Timeout, Dir: o.Dir}
	bare := run.Options{Timeout: bareTimeout, Dir: o.Dir}

	family("help", with(o.HelpFlag), common, false)
	family("bare", with(), bare, true)
	for i, e := range o.Extra {
		family(fmt.Sprintf("extra%d", i), with(e.Args...), common, false)
	}

	// Single runs: nothing to compare them against.
	plans = append(plans,
		plan{"version", with(o.VersionFlag), run.Options{TTY: true, Timeout: o.Timeout, Dir: o.Dir}},
		plan{"badFlag", with(BadFlag), run.Options{Timeout: o.Timeout, Dir: o.Dir}},
	)

	started := time.Now()
	set := &Set{Results: make(map[string]*run.Result, len(plans))}
	for i, p := range plans {
		o.OnProgress(p.id, i+1, len(plans))
		r := run.Run(t.Cmd, p.args, p.opts)
		r.ID = p.id
		set.Results[p.id] = &r
	}

	set.Meta = Meta{
		BadFlag:     BadFlag,
		HelpFlag:    o.HelpFlag,
		VersionFlag: o.VersionFlag,
		NarrowCols:  NarrowCols,
		WideCols:    WideCols,
		Families:    families,
		ProbeCount:  len(plans),
		Duration:    time.Since(started),
	}
	return set
}
