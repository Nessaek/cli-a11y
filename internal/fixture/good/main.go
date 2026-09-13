// Command good is the same tool as the bad fixture, built the other way round.
// The test suite asserts that cli-a11y passes each check it is designed to
// satisfy.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

var colourOK = isTerminal(os.Stdout) &&
	os.Getenv("NO_COLOR") == "" &&
	os.Getenv("TERM") != "dumb"

// Bright variants, which clear WCAG AA against a dark terminal. Nothing faint,
// and colour never carries meaning on its own.
func paint(code, s string) string {
	if !colourOK {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func red(s string) string   { return paint("91", s) }
func green(s string) string { return paint("92", s) }

func width() int {
	if v := os.Getenv("COLUMNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return max(30, n)
		}
	}
	return 80
}

func wrap(text, indent string) string {
	w := width()
	var out []string
	line := indent
	for _, word := range strings.Fields(text) {
		switch {
		case len(line) > len(indent) && len(line)+1+len(word) > w:
			out = append(out, line)
			line = indent + word
		case len(line) > len(indent):
			line += " " + word
		default:
			line = indent + word
		}
	}
	return strings.Join(append(out, line), "\n")
}

// Every line goes through the wrapper, including the option list, so the help
// fits whatever terminal it is printed into.
func option(flag, text string) string {
	const gutter = 20
	head := "  " + flag
	if len(head) < gutter {
		head += strings.Repeat(" ", gutter-len(head))
	}
	// wrap indents every line; drop the indent from the first so the flag sits
	// in front of it and continuation lines line up under the description.
	body := wrap(text, strings.Repeat(" ", gutter))
	return head + body[gutter:]
}

func help() {
	fmt.Println(strings.Join([]string{
		wrap("Usage: deploytool [options] [environment]", ""),
		"",
		wrap("Deploys the current project to an environment. With no environment, reports what is running where.", ""),
		"",
		"Options:",
		option("--target <name>", "Environment to deploy to."),
		option("--yes", "Answer every prompt with yes. Use this in CI."),
		option("--json", "Print machine-readable output instead of a table."),
		option("--quiet", "Suppress progress output."),
		option("--version", "Print the version."),
		option("--help", "Print this help."),
		"",
		"Examples:",
		wrap("$ deploytool --target staging", "  "),
		wrap("$ deploytool --target production --yes", "  "),
	}, "\n"))
	os.Exit(0)
}

type row struct {
	Status string `json:"status"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

func status(asJSON bool) {
	rows := []row{
		{"ok", "api-gateway", "2 replicas"},
		{"DOWN", "worker-queue", "0 replicas"},
		{"ok", "scheduler", "1 replica"},
	}
	if asJSON {
		b, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(b))
		return
	}
	// The word says it; the colour only reinforces it.
	for _, r := range rows {
		mark := green("[ok]  ")
		if r.Status != "ok" {
			mark = red("[DOWN]")
		}
		fmt.Println(wrap(fmt.Sprintf("%s %s — %s", mark, r.Name, r.Detail), ""))
	}
}

func main() {
	args := os.Args[1:]
	has := func(v string) bool {
		for _, a := range args {
			if a == v {
				return true
			}
		}
		return false
	}

	if has("--help") || has("-h") {
		help()
	}
	if has("--version") {
		fmt.Println("deploytool 2.4.1")
		os.Exit(0)
	}

	known := map[string]bool{
		"--target": true, "--yes": true, "--json": true,
		"--quiet": true, "--help": true, "--version": true,
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-") && !known[a] {
			fmt.Fprintf(os.Stderr, "deploytool: unrecognised option %q.\n", a)
			fmt.Fprintln(os.Stderr, `Did you mean --target? Run "deploytool --help" for the full list.`)
			os.Exit(2)
		}
	}
	status(has("--json"))
}
