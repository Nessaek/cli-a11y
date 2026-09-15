// Command cli-a11y runs a command-line tool under a range of terminal
// conditions and grades how usable the result is for someone on a screen
// reader, a magnifier, a high-contrast theme, voice control or switch access.
package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/Nessaek/cli-a11y/internal/check"
	"github.com/Nessaek/cli-a11y/internal/probe"
	"github.com/Nessaek/cli-a11y/internal/report"
)

// Version is the tool's own version. Release builds can set it with
// -ldflags "-X main.Version=..."; otherwise it comes from the module version
// recorded by go install.
var Version = ""

func version() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

const usage = `Usage: cli-a11y [options] -- <command> [args...]
       cli-a11y [options] <command> [args...]

Runs <command> under a range of terminal conditions and grades how usable the
result is for people using a screen reader, a magnifier, a high-contrast theme,
voice control, switch access, or plain unfamiliarity.

Options:
  --cmd "<args>"        An extra invocation to exercise, on top of --help and
                        --version. Repeatable. Use it to reach the real work:
                        progress bars and prompts rarely appear in help text.
  --fail-on <level>     Exit non-zero at this severity or worse.
                        critical | serious | moderate | minor | none
                        (default: serious)
  --json                Emit the full report as JSON instead of text.
  --quiet               Show only the failures, not the checks that passed.
  --timeout <duration>  Per-probe timeout, e.g. 10s or 500ms (default: 10s).
  --dir <path>          Working directory for the target.
  --help-flag <flag>    Flag that prints help (default: --help).
  --version-flag <flag> Flag that prints the version (default: --version).
  --no-color            Never colour this report.
  -h, --help            Show this help.
  -V, --version         Show the version of cli-a11y itself.

Examples:
  $ cli-a11y -- git status
  $ cli-a11y --cmd "install --dry-run" --fail-on moderate -- npm
  $ cli-a11y --json -- ./my-tool > report.json

Exit codes:
  0  nothing at or above the --fail-on severity
  1  findings at or above that severity
  2  cli-a11y could not run the target at all
`

type options struct {
	extra       []probe.Extra
	failOn      check.Severity
	json        bool
	quiet       bool
	timeout     time.Duration
	dir         string
	helpFlag    string
	versionFlag string
	colour      *bool
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "cli-a11y: "+format+"\n", args...)
	os.Exit(2)
}

func parseArgs(argv []string) (options, []string) {
	o := options{
		failOn: check.Serious, timeout: 10 * time.Second,
		helpFlag: "--help", versionFlag: "--version",
	}
	var rest []string
	sawSeparator := false

	need := func(i *int, flag string) string {
		*i++
		if *i >= len(argv) {
			die("%s needs a value", flag)
		}
		return argv[*i]
	}

	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if sawSeparator {
			rest = append(rest, a)
			continue
		}
		if a == "--" {
			sawSeparator = true
			continue
		}
		// Once the target command has started, everything belongs to it.
		if len(rest) > 0 {
			rest = append(rest, a)
			continue
		}

		switch a {
		case "-h", "--help":
			fmt.Print(usage)
			os.Exit(0)
		case "-V", "--version":
			fmt.Println(version())
			os.Exit(0)
		case "--json":
			o.json = true
		case "--quiet":
			o.quiet = true
		case "--no-color":
			no := false
			o.colour = &no
		case "--color":
			yes := true
			o.colour = &yes
		case "--cmd":
			raw := need(&i, "--cmd")
			o.extra = append(o.extra, probe.Extra{Label: raw, Args: strings.Fields(raw)})
		case "--fail-on":
			v := need(&i, "--fail-on")
			if v == "none" {
				o.failOn = check.None
				break
			}
			sev, ok := check.ParseSeverity(v)
			if !ok {
				die("--fail-on must be one of critical, serious, moderate, minor, none, got %q", v)
			}
			o.failOn = sev
		case "--timeout":
			v := need(&i, "--timeout")
			d, err := time.ParseDuration(v)
			if err != nil {
				// Bare numbers are read as milliseconds, as the Node version took them.
				if ms, convErr := strconv.Atoi(v); convErr == nil {
					d = time.Duration(ms) * time.Millisecond
				} else {
					die("--timeout must be a duration such as 10s or 500ms, got %q", v)
				}
			}
			if d < 500*time.Millisecond {
				die("--timeout must be at least 500ms")
			}
			o.timeout = d
		case "--dir":
			o.dir = need(&i, "--dir")
		case "--help-flag":
			o.helpFlag = need(&i, "--help-flag")
		case "--version-flag":
			o.versionFlag = need(&i, "--version-flag")
		default:
			if strings.HasPrefix(a, "-") && len(a) > 1 {
				die("unknown option %s\nTry: cli-a11y --help", a)
			}
			rest = append(rest, a)
		}
	}
	return o, rest
}

func main() {
	o, rest := parseArgs(os.Args[1:])
	if len(rest) == 0 {
		fmt.Fprint(os.Stderr, "cli-a11y: no command given.\n\n"+usage)
		os.Exit(2)
	}

	target := probe.Target{Cmd: rest[0], Args: rest[1:]}
	stderrIsTTY := isCharDevice(os.Stderr)

	set := probe.Collect(target, probe.Options{
		Timeout:     o.timeout,
		Dir:         o.dir,
		HelpFlag:    o.helpFlag,
		VersionFlag: o.versionFlag,
		Extra:       o.extra,
		OnProgress: func(id string, done, total int) {
			// One line per probe, never redrawn in place: this tool does not
			// get to fail its own animation check.
			if stderrIsTTY && !o.json {
				fmt.Fprintf(os.Stderr, "  probe %d/%d: %s\n", done, total, id)
			}
		},
	})

	// The pty probes go through a pseudo-terminal that reports its own errors,
	// but only the direct spawn of the piped probe surfaces a missing binary.
	// Only the piped probe can say the binary is missing. A terminal probe can
	// also fail because no pty could be opened, and that must degrade to a
	// partial report, not stop the audit.
	for _, id := range []string{"helpPipe"} {
		if p := set.Get(id); p != nil && p.Err != "" {
			fmt.Fprintf(os.Stderr, "cli-a11y: could not run %q: %s\n", target.Cmd, p.Err)
			fmt.Fprintln(os.Stderr, "Check the name, or pass the full path to the executable.")
			os.Exit(2)
		}
	}

	findings := check.All(set)
	pty := false
	for _, p := range set.Results {
		if p.TTY && p.OK() {
			pty = true
			break
		}
	}
	res := report.Result{Target: target, Findings: findings, Meta: set.Meta, PTY: pty}

	if o.json {
		out, err := report.JSON(res)
		if err != nil {
			die("could not render JSON: %v", err)
		}
		fmt.Println(out)
	} else {
		fmt.Print(report.Render(res, report.Options{
			Colour: o.colour,
			Quiet:  o.quiet,
			Width:  terminalWidth(),
		}))
	}

	if report.ShouldFail(findings, o.failOn) {
		os.Exit(1)
	}
}

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func terminalWidth() int {
	if v := os.Getenv("COLUMNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 80
}
