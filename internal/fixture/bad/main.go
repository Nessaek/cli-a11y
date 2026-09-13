// Command bad is a deliberately inaccessible CLI. Every fault here is one seen
// in real tools, and the test suite asserts that cli-a11y finds each of them.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

// Colour unconditionally: no isatty check, no NO_COLOR, no TERM=dumb.
func red(s string) string   { return "\x1b[31m" + s + "\x1b[0m" }
func green(s string) string { return "\x1b[32m" + s + "\x1b[0m" }
func faint(s string) string { return "\x1b[2m" + s + "\x1b[0m" }

func help() {
	// Wrong stream, wrong exit code, no usage line, no examples, hard-coded width.
	bar := strings.Repeat("─", 96)
	lines := []string{
		faint("deploytool — the deployment tool"),
		"",
		"  " + bar,
		"  │ " + faint("Configure the deployment by passing the appropriate configuration flags, noting that some") + " │",
		"  │ " + faint("of these are mutually exclusive and that the precedence rules are somewhat involved and") + "  │",
		"  │ " + faint("are described at length in the documentation which you should probably read first.") + "      │",
		"  " + bar,
		"",
		"  --target      the target",
		"  --strategy    the strategy",
		"  --config      the config",
		"",
	}
	fmt.Fprintln(os.Stderr, strings.Join(lines, "\n"))
	os.Exit(1)
}

func statusTable() {
	// Colour as the only signal: nothing but the colour says which passed.
	rule := "  " + strings.Repeat("━", 40)
	fmt.Println(rule)
	fmt.Println("  " + green("api-gateway") + "     2 replicas")
	fmt.Println("  " + red("worker-queue") + "    0 replicas")
	fmt.Println("  " + green("scheduler") + "       1 replica")
	fmt.Println(rule)
}

func bare() {
	fmt.Print("\x1b[?25l") // hide the cursor and never restore it
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧"}
	deadline := time.Now().Add(1200 * time.Millisecond)
	for i := 0; time.Now().Before(deadline); i++ {
		fmt.Printf("\r\x1b[2K%s %s", frames[i%len(frames)], faint("resolving manifests…"))
		time.Sleep(70 * time.Millisecond)
	}
	fmt.Print("\r\x1b[2K")
	statusTable()
	fmt.Println("\nUse arrow keys to choose an environment, then press <space>.")
	fmt.Println("Continuing automatically in 10 seconds.")
	fmt.Print("Are you sure you want to continue? [y/N] ")
	// Block on a real read, with no --yes to be found anywhere. An empty
	// select would be flagged as a deadlock by the runtime and exit.
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func main() {
	args := os.Args[1:]
	for _, a := range args {
		if a == "--help" || a == "-h" {
			help()
		}
	}
	for _, a := range args {
		if a == "--version" {
			os.Exit(0) // prints nothing
		}
	}
	if len(args) > 0 && strings.HasPrefix(args[0], "-") {
		// Unknown flag: wrong stream, useless message, success exit code.
		fmt.Println("Something went wrong.")
		os.Exit(0)
	}
	bare()
}
