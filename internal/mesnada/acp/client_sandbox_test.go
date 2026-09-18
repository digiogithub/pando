package acp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/sandbox"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// fakeWrapper is an "enforced" backend that records the commands it wraps
// and optionally rewrites them, mirroring internal/llm/tools/shell's test
// double.
type fakeWrapper struct {
	mu       sync.Mutex
	calls    int
	lastPath string
	lastArgs []string
	rewrite  func(cmd *exec.Cmd, p sandbox.Policy)
}

func (f *fakeWrapper) Capability() sandbox.Capability {
	return sandbox.Capability{Backend: "fake", Version: "1", Enforced: true}
}

func (f *fakeWrapper) Wrap(cmd *exec.Cmd, p sandbox.Policy) error {
	if !p.Enabled() {
		return nil
	}
	f.mu.Lock()
	f.calls++
	f.lastPath = cmd.Path
	f.lastArgs = append([]string(nil), cmd.Args...)
	f.mu.Unlock()
	if f.rewrite != nil {
		f.rewrite(cmd, p)
	}
	return nil
}

func (f *fakeWrapper) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// isolateSandboxConfig gives the test an isolated config (the default
// sandbox: workspace-write, ACP terminals covered) and undoes it on
// cleanup; per the repo pitfall, config-under-test never calls Load().
func isolateSandboxConfig(t *testing.T, ws string) {
	t.Helper()
	t.Setenv(config.SandboxEnvVar, "")
	t.Setenv("TMPDIR", "")
	prev := config.Get()
	config.SetForTests(&config.Config{WorkingDir: ws})
	t.Cleanup(func() { config.SetForTests(prev) })
}

func waitTerminalDone(t *testing.T, c *MesnadaACPClient, id string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.WaitForTerminalExit(ctx, acpsdk.WaitForTerminalExitRequest{TerminalId: id})
	if err != nil {
		t.Fatalf("WaitForTerminalExit: %v", err)
	}
	if resp.ExitCode == nil {
		t.Fatal("WaitForTerminalExit: nil exit code")
	}
	return *resp.ExitCode
}

func TestCreateTerminalIsWrappedAndEnvScrubbed(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("requires /bin/sh")
	}
	ws := t.TempDir()
	isolateSandboxConfig(t, ws)
	w := &fakeWrapper{}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	t.Setenv("FAKE_ACP_API_KEY", "super-secret")
	c := NewMesnadaACPClient("task-1", ws, nil, nil)

	resp, err := c.CreateTerminal(context.Background(), acpsdk.CreateTerminalRequest{
		Command: "/bin/sh",
		Args:    []string{"-c", `printf 'key=[%s]' "$FAKE_ACP_API_KEY"`},
	})
	if err != nil {
		t.Fatalf("CreateTerminal: %v", err)
	}
	if code := waitTerminalDone(t, c, resp.TerminalId); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if w.Calls() != 1 {
		t.Fatalf("calls = %d, want 1 (ACP terminals are covered by default)", w.Calls())
	}

	out, err := c.TerminalOutput(context.Background(), acpsdk.TerminalOutputRequest{TerminalId: resp.TerminalId})
	if err != nil {
		t.Fatalf("TerminalOutput: %v", err)
	}
	if strings.Contains(out.Output, "super-secret") {
		t.Fatalf("env not scrubbed: secret leaked into terminal output: %q", out.Output)
	}
	if got := strings.TrimSpace(out.Output); got != "key=[]" {
		t.Fatalf("output = %q, want the API key scrubbed", got)
	}
}

func TestCreateTerminalFailsClosedOnWrapError(t *testing.T) {
	ws := t.TempDir()
	isolateSandboxConfig(t, ws)
	restore := sandbox.SetDefaultForTests(&erroringWrapper{})
	defer restore()

	c := NewMesnadaACPClient("task-2", ws, nil, nil)
	if _, err := c.CreateTerminal(context.Background(), acpsdk.CreateTerminalRequest{Command: "/bin/sh", Args: []string{"-c", "true"}}); err == nil {
		t.Fatal("expected a wrap failure to fail closed")
	}
	c.terminalsMu.Lock()
	n := len(c.terminals)
	c.terminalsMu.Unlock()
	if n != 0 {
		t.Fatalf("terminals tracked = %d, want 0: no terminal must be created on a wrap failure", n)
	}
}

type erroringWrapper struct{}

func (erroringWrapper) Capability() sandbox.Capability {
	return sandbox.Capability{Backend: "fake", Enforced: true}
}
func (erroringWrapper) Wrap(*exec.Cmd, sandbox.Policy) error { return sandbox.ErrWrapFailed }

// TestCreateTerminalDeniesWriteOutsideWorkspaceUnderDefaultPolicy exercises
// the real, Resolve()-computed default policy (workspace-write, ACP
// terminals covered by DefaultExtendTo) against a fake backend that enforces
// it: writing inside the workspace succeeds, writing outside it is denied.
// A real OS backend (Landlock/Seatbelt) is exercised by internal/sandbox's
// own e2e tests; this one proves CreateTerminal threads the resolved policy
// through to the wrapper correctly.
func TestCreateTerminalDeniesWriteOutsideWorkspaceUnderDefaultPolicy(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("requires sh")
	}
	ws := t.TempDir()
	isolateSandboxConfig(t, ws)

	// A location outside the workspace, HOME's cache dirs and the temp
	// roots the default policy always allows (/tmp, /var/tmp, $TMPDIR):
	// the current package directory, definitely none of those.
	outside, err := os.MkdirTemp(".", ".pando-acp-e2e-outside-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(outside) })
	outsideAbs, err := filepath.Abs(outside)
	if err != nil {
		t.Fatal(err)
	}

	w := &fakeWrapper{
		rewrite: func(cmd *exec.Cmd, p sandbox.Policy) {
			target := writeTargetFromArgs(cmd.Args)
			if target == "" || pathInsideAnyRoot(target, p.WritableRoots) {
				return
			}
			// Simulate the real sandbox denying the write: the same
			// observable outcome as an OS-level EACCES/EPERM denial (the
			// command never gets to run).
			cmd.Path = "/bin/false"
			cmd.Args = []string{"false"}
		},
	}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	c := NewMesnadaACPClient("task-3", ws, nil, nil)

	inside := filepath.Join(ws, "ok.txt")
	resp, err := c.CreateTerminal(context.Background(), acpsdk.CreateTerminalRequest{
		Command: "/bin/sh", Args: []string{"-c", "echo hi > " + inside},
	})
	if err != nil {
		t.Fatalf("CreateTerminal (inside): %v", err)
	}
	if code := waitTerminalDone(t, c, resp.TerminalId); code != 0 {
		t.Fatalf("write inside the workspace: exit code = %d, want 0", code)
	}
	if _, err := os.Stat(inside); err != nil {
		t.Fatalf("write inside the workspace did not happen: %v", err)
	}

	target := filepath.Join(outsideAbs, "bad.txt")
	resp2, err := c.CreateTerminal(context.Background(), acpsdk.CreateTerminalRequest{
		Command: "/bin/sh", Args: []string{"-c", "echo hi > " + target},
	})
	if err != nil {
		t.Fatalf("CreateTerminal (outside): %v", err)
	}
	if code := waitTerminalDone(t, c, resp2.TerminalId); code == 0 {
		t.Fatal("write outside the workspace: expected a non-zero (denied) exit code")
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("write outside the workspace succeeded; the default policy should have denied it")
	}
}

// writeTargetFromArgs extracts the "> path" redirection target from a
// `sh -c "..."` invocation, good enough for this test's own fixed shape.
func writeTargetFromArgs(args []string) string {
	for i, a := range args {
		if a == "-c" && i+1 < len(args) {
			script := args[i+1]
			if idx := strings.LastIndex(script, "> "); idx >= 0 {
				return strings.TrimSpace(script[idx+2:])
			}
		}
	}
	return ""
}

func pathInsideAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}
