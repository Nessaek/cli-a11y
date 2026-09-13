package run

import (
	"strings"
	"testing"
	"time"
)

func TestPtyIsATerminalAtTheRequestedSize(t *testing.T) {
	for _, cols := range []int{80, 40} {
		r := Run("sh", []string{"-c", "tty -s && stty size"},
			Options{TTY: true, Cols: cols, Rows: 24, Timeout: 4 * time.Second})
		if !r.OK() {
			t.Fatalf("cols=%d: %v", cols, r.Err)
		}
		got := strings.TrimSpace(r.Output)
		want := "24 " + itoa(cols)
		if got != want {
			t.Errorf("cols=%d: stty size = %q, want %q", cols, got, want)
		}
	}
}

func TestPipeIsNotATerminal(t *testing.T) {
	r := Run("sh", []string{"-c", "tty -s && echo terminal || echo pipe"},
		Options{Timeout: 4 * time.Second})
	if got := strings.TrimSpace(r.Output); got != "pipe" {
		t.Errorf("got %q, want %q", got, "pipe")
	}
}

func TestHeldStdinLetsTheTargetWait(t *testing.T) {
	r := Run("sh", []string{"-c", `printf 'Continue? [y/N] '; read answer`},
		Options{TTY: true, Stdin: StdinHold, Timeout: 1200 * time.Millisecond})
	if !r.TimedOut {
		t.Errorf("expected the run to time out waiting for input, got code %d", r.Code)
	}
	if !strings.Contains(r.Output, "Continue?") {
		t.Errorf("prompt not captured: %q", r.Output)
	}
}

// The Node implementation tore the process group down to escape its own
// stdin-holding sleep, which destroyed the exit code and needed a temp file to
// carry it back. A real pty has no such problem: a target that exits on its own
// is simply waited on.
func TestHeldStdinStillReportsExitCodePromptly(t *testing.T) {
	for _, want := range []int{0, 3} {
		r := Run("sh", []string{"-c", "echo done; exit " + itoa(want)},
			Options{TTY: true, Stdin: StdinHold, Timeout: 4 * time.Second})
		if r.TimedOut {
			t.Fatalf("exit %d: timed out, but the target exits immediately", want)
		}
		if r.Code != want {
			t.Errorf("exit code = %d, want %d", r.Code, want)
		}
		if r.Duration > time.Second {
			t.Errorf("took %v; a target that exits should not wait for the timeout", r.Duration)
		}
	}
}

func TestClosedStdinDeliversEOF(t *testing.T) {
	r := Run("sh", []string{"-c", `read x; echo "eof=$?"`},
		Options{TTY: true, Stdin: StdinClose, Timeout: 4 * time.Second})
	if r.TimedOut {
		t.Fatal("expected EOF to end the read, but it hung")
	}
	if !strings.Contains(r.Output, "eof=1") {
		t.Errorf("read did not see EOF: %q", r.Output)
	}
}

func TestEotEchoIsNotAttributedToTheTarget(t *testing.T) {
	r := Run("sh", []string{"-c", "printf hello"},
		Options{TTY: true, Stdin: StdinClose, Timeout: 4 * time.Second})
	if got := strings.TrimSpace(r.Output); got != "hello" {
		t.Errorf("got %q, want %q — the line discipline's ^D echo leaked through", got, "hello")
	}
}

func TestStreamsStayApartWhenPiped(t *testing.T) {
	r := Run("sh", []string{"-c", "echo out; echo err >&2; exit 2"}, Options{Timeout: 4 * time.Second})
	if !strings.Contains(r.Stdout, "out") || strings.Contains(r.Stdout, "err") {
		t.Errorf("stdout = %q", r.Stdout)
	}
	if !strings.Contains(r.Stderr, "err") {
		t.Errorf("stderr = %q", r.Stderr)
	}
	if r.Code != 2 {
		t.Errorf("code = %d, want 2", r.Code)
	}
}

func TestHostPreferencesCannotLeakIn(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "3")
	r := Run("sh", []string{"-c", `echo "[${NO_COLOR:-unset}/${FORCE_COLOR:-unset}]"`},
		Options{Timeout: 4 * time.Second})
	if got := strings.TrimSpace(r.Output); got != "[unset/unset]" {
		t.Errorf("got %q; the host's own colour preferences reached the probe", got)
	}
}

func TestMissingBinaryIsReportedAsAnError(t *testing.T) {
	r := Run("definitely-not-a-real-binary-xyz", nil, Options{Timeout: 2 * time.Second})
	if r.OK() {
		t.Error("expected an error for a binary that does not exist")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
