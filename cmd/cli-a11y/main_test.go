package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// End-to-end tests of the binary itself: exit codes, JSON, and whether the tool
// survives its own rules.

var (
	bin      string
	badBin   string
	goodBin  string
	buildDir string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cli-a11y-e2e")
	if err != nil {
		panic(err)
	}
	buildDir = dir
	build := func(name, pkg string) string {
		out := filepath.Join(dir, name)
		if b, err := exec.Command("go", "build", "-o", out, pkg).CombinedOutput(); err != nil {
			panic(string(b))
		}
		return out
	}
	bin = build("cli-a11y", "github.com/Nessaek/cli-a11y/cmd/cli-a11y")
	badBin = build("bad", "github.com/Nessaek/cli-a11y/internal/fixture/bad")
	goodBin = build("good", "github.com/Nessaek/cli-a11y/internal/fixture/good")

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running cli-a11y: %v", err)
	}
	return out.String(), errBuf.String(), code
}

func TestExitsNonZeroOnFindings(t *testing.T) {
	_, _, code := run(t, "--no-color", "--timeout", "2s", "--", badBin)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestFailOnNoneAlwaysExitsZero(t *testing.T) {
	_, _, code := run(t, "--no-color", "--fail-on", "none", "--timeout", "2s", "--", badBin)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestGoodFixtureIsNotFlaggedAsSerious(t *testing.T) {
	out, _, code := run(t, "--no-color", "--timeout", "2s", "--", goodBin)
	if code != 0 {
		t.Errorf("exit code = %d, want 0.\n%s", code, out)
	}
}

func TestJSONIsValidAndComplete(t *testing.T) {
	out, _, _ := run(t, "--json", "--timeout", "2s", "--", goodBin)
	var parsed struct {
		Score    int `json:"score"`
		Findings []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("stdout was not JSON: %v\n%s", err, out)
	}
	if len(parsed.Findings) < 20 {
		t.Errorf("only %d findings", len(parsed.Findings))
	}
	for _, f := range parsed.Findings {
		if f.ID == "" || f.Status == "" {
			t.Errorf("finding with empty id or status: %+v", f)
		}
	}
}

func TestUnrunnableTargetExitsTwo(t *testing.T) {
	_, errOut, code := run(t, "--timeout", "2s", "--", "definitely-not-a-real-binary-xyz")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "could not run") {
		t.Errorf("stderr did not explain the failure: %q", errOut)
	}
}

func TestUnknownOptionExitsTwo(t *testing.T) {
	if _, _, code := run(t, "--nope", "--", "echo"); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestNoCommandExitsTwo(t *testing.T) {
	if _, _, code := run(t); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

// The report must obey the rules it enforces: no colour when it is switched
// off, and nothing that reads as styling.
func TestReportHonoursNoColor(t *testing.T) {
	out, _, _ := run(t, "--no-color", "--timeout", "2s", "--", goodBin)
	if strings.Contains(out, "\x1b[") {
		t.Error("report emitted escape sequences with --no-color")
	}
}

// The sharpest test there is: point the tool at itself.
func TestToolPassesItsOwnAudit(t *testing.T) {
	out, _, code := run(t, "--no-color", "--fail-on", "serious", "--timeout", "4s", "--", bin)
	if code != 0 {
		t.Errorf("cli-a11y failed its own checks (exit %d):\n%s", code, out)
	}
}
