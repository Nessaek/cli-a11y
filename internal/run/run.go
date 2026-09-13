// Package run executes the target CLI under controlled conditions.
//
// The interesting failures only appear on a real terminal: most CLIs suppress
// colour, spinners and prompts the moment stdout is a pipe, so piping alone
// would grade a program on output no terminal user ever sees.
//
// The Node version of this package fought script(1) for a pty — working around
// a tcgetattr that rejects socketpairs, a FIFO it also rejects, a sleep on the
// left of a pipe to hold stdin open, a process-group kill to end it and a temp
// file to carry the exit code back out. None of that survives here. A real pty
// is three lines, and its size is set directly rather than by running stty
// inside the session.
package run

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// DefaultTimeout is the per-probe limit.
const DefaultTimeout = 10 * time.Second

// StdinMode says what the target should find on its standard input.
type StdinMode int

const (
	// StdinClose delivers an immediate end-of-file.
	StdinClose StdinMode = iota
	// StdinHold keeps stdin open and silent, so a CLI that waits for input
	// really waits and the timeout is itself the finding.
	StdinHold
	// StdinData supplies literal input.
	StdinData
)

// Options configures one execution.
type Options struct {
	TTY     bool
	Env     map[string]string
	Cols    int
	Rows    int
	Timeout time.Duration
	Stdin   StdinMode
	Data    string
	Dir     string
}

// Result is everything a check can learn from one execution.
type Result struct {
	ID       string
	Cmd      string
	Args     []string
	TTY      bool
	Cols     int
	Env      map[string]string
	Stdout   string
	Stderr   string
	Raw      string
	Output   string
	Code     int
	TimedOut bool
	Err      string
	Skipped  string
	Duration time.Duration
}

// OK reports whether this probe produced a usable run.
func (r *Result) OK() bool { return r != nil && r.Skipped == "" && r.Err == "" }

// Variables that would let the host's own preferences quietly invalidate a
// probe.
var scrubbed = []string{
	"NO_COLOR", "FORCE_COLOR", "CLICOLOR", "CLICOLOR_FORCE",
	"TERM", "COLUMNS", "LINES", "CI", "COLORTERM",
}

func buildEnv(o Options) []string {
	drop := make(map[string]bool, len(scrubbed))
	for _, k := range scrubbed {
		drop[k] = true
	}
	var env []string
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && drop[kv[:i]] {
			continue
		}
		env = append(env, kv)
	}
	term := "dumb"
	if o.TTY {
		term = "xterm-256color"
	}
	base := map[string]string{
		"TERM":    term,
		"COLUMNS": strconv.Itoa(o.Cols),
		"LINES":   strconv.Itoa(o.Rows),
	}
	for k, v := range o.Env {
		base[k] = v
	}
	for k, v := range base {
		env = append(env, k+"="+v)
	}
	return env
}

// The line discipline echoes the EOF we send as caret notation and then rubs it
// out. That did not come from the CLI.
var eotEcho = regexp.MustCompile(`^(?:\x04|\^D)(?:\x08| )*`)

// Run executes the CLI once and captures what it wrote.
func Run(cmd string, args []string, o Options) Result {
	if o.Cols == 0 {
		o.Cols = 80
	}
	if o.Rows == 0 {
		o.Rows = 24
	}
	if o.Timeout == 0 {
		o.Timeout = DefaultTimeout
	}

	res := Result{Cmd: cmd, Args: args, TTY: o.TTY, Cols: o.Cols, Env: o.Env}
	started := time.Now()

	c := exec.Command(cmd, args...)
	c.Env = buildEnv(o)
	c.Dir = o.Dir

	var out, errBuf bytes.Buffer
	var wg sync.WaitGroup
	var ptmx *os.File

	if o.TTY {
		// A new session with the pty as controlling terminal, sized up front.
		var err error
		ptmx, err = pty.StartWithSize(c, &pty.Winsize{Rows: uint16(o.Rows), Cols: uint16(o.Cols)})
		if err != nil {
			res.Err = err.Error()
			res.Duration = time.Since(started)
			return res
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A closed pty reads as EOF on macOS and EIO on Linux. Both mean
			// the child is gone.
			_, _ = io.Copy(&out, ptmx)
		}()
		switch o.Stdin {
		case StdinClose:
			_, _ = ptmx.Write([]byte{0x04})
		case StdinData:
			_, _ = ptmx.WriteString(o.Data)
			_, _ = ptmx.Write([]byte{0x04})
		case StdinHold:
			// Write nothing and leave the pty open.
		}
	} else {
		c.Stdout = &out
		c.Stderr = &errBuf
		// Its own process group, so a timeout can take the whole tree.
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		switch o.Stdin {
		case StdinClose:
			c.Stdin = nil // exec connects /dev/null
		case StdinData:
			c.Stdin = strings.NewReader(o.Data)
		case StdinHold:
			// An open pipe nothing ever writes to.
			if w, err := c.StdinPipe(); err == nil {
				defer w.Close()
			}
		}
		if err := c.Start(); err != nil {
			res.Err = err.Error()
			res.Duration = time.Since(started)
			return res
		}
	}

	done := make(chan error, 1)
	go func() { done <- c.Wait() }()

	select {
	case err := <-done:
		res.Code = exitCode(err)
	case <-time.After(o.Timeout):
		res.TimedOut = true
		kill(c, syscall.SIGTERM)
		select {
		case err := <-done:
			res.Code = exitCode(err)
		case <-time.After(500 * time.Millisecond):
			kill(c, syscall.SIGKILL)
			<-done
			res.Code = -1
		}
	}

	if ptmx != nil {
		_ = ptmx.Close()
		wg.Wait()
	}

	raw := out.String()
	if o.TTY {
		raw = eotEcho.ReplaceAllString(raw, "")
		raw = strings.ReplaceAll(raw, "\x1b[?1034h", "")
		res.Stdout = raw
	} else {
		res.Stdout = raw
		res.Stderr = errBuf.String()
		// On a pty the two streams are one, exactly as a terminal presents
		// them; piped, they stay apart and stream discipline can be judged.
		raw += errBuf.String()
	}

	res.Raw = raw
	// Line endings normalised: a pty turns every \n into \r\n, and left in
	// place that would read as a screenful of cursor animation.
	res.Output = strings.ReplaceAll(raw, "\r\n", "\n")
	res.Duration = time.Since(started)
	return res
}

func kill(c *exec.Cmd, sig syscall.Signal) {
	if c.Process == nil {
		return
	}
	// Negative pid signals the whole group; fall back to the bare process.
	if err := syscall.Kill(-c.Process.Pid, sig); err != nil {
		_ = c.Process.Signal(sig)
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if ok := asExitError(err, &ee); ok {
		return ee.ExitCode()
	}
	return -1
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}
