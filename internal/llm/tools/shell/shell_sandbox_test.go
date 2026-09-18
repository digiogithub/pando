//go:build !windows

package shell

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/sandbox"
)

// fakeWrapper is an "enforced" backend that records the commands it wraps
// and optionally rewrites them (to simulate a launcher such as bwrap).
type fakeWrapper struct {
	mu      sync.Mutex
	calls   int
	err     error
	rewrite func(cmd *exec.Cmd)
}

func (f *fakeWrapper) Capability() sandbox.Capability {
	return sandbox.Capability{Backend: "fake", Version: "1", Enforced: true, ProtectsNestedPaths: true, BlocksPorts: true}
}

func (f *fakeWrapper) Wrap(cmd *exec.Cmd, p sandbox.Policy) error {
	if !p.Enabled() {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	if f.rewrite != nil {
		f.rewrite(cmd)
	}
	return nil
}

func (f *fakeWrapper) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// setupSandboxedShell installs an isolated config (workspace in a temp dir,
// a non-login bash) and the fake wrapper, and resets the global shell.
func setupSandboxedShell(t *testing.T, w sandbox.Wrapper) (*config.Config, string) {
	t.Helper()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("requires /bin/bash")
	}
	t.Setenv(config.SandboxEnvVar, "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FAKE_API_KEY", "super-secret")

	ws := t.TempDir()
	cfg := &config.Config{
		WorkingDir: ws,
		Shell:      config.ShellConfig{Path: "/bin/bash", Args: []string{"--noprofile", "--norc"}},
	}
	prev := config.Get()
	config.SetForTests(cfg)
	restore := sandbox.SetDefaultForTests(w)
	ResetForTests()
	t.Cleanup(func() {
		ResetForTests()
		restore()
		config.SetForTests(prev)
	})
	return cfg, ws
}

func mustExec(t *testing.T, sh *PersistentShell, command string) string {
	t.Helper()
	stdout, stderr, code, _, err := sh.Exec(context.Background(), command, 10_000)
	if err != nil || code != 0 {
		t.Fatalf("exec %q: code=%d err=%v stderr=%s", command, code, err, stderr)
	}
	return strings.TrimSpace(stdout)
}

func TestPersistentShellIsWrappedAndEnvScrubbed(t *testing.T) {
	w := &fakeWrapper{}
	_, ws := setupSandboxedShell(t, w)

	sh := GetPersistentShell(ws)
	if w.Calls() != 1 {
		t.Fatalf("wrapper calls = %d, want 1", w.Calls())
	}
	info := sh.Sandbox()
	if !info.Active() || !info.AutoAllowBash() {
		t.Fatalf("sandbox info = %+v, want active with auto-allow", info)
	}
	if info.PolicyHash != sandbox.CurrentPolicyHash() {
		t.Fatalf("recorded hash %q != current %q", info.PolicyHash, sandbox.CurrentPolicyHash())
	}

	out := mustExec(t, sh, `echo "key=[$FAKE_API_KEY] editor=[$GIT_EDITOR]"`)
	if out != "key=[] editor=[true]" {
		t.Fatalf("env inside sandboxed shell = %q, want the API key scrubbed and GIT_EDITOR kept", out)
	}

	if again := GetPersistentShell(ws); again != sh {
		t.Fatal("unchanged policy must reuse the running shell")
	}
	if w.Calls() != 1 {
		t.Fatalf("wrapper calls = %d after reuse, want 1", w.Calls())
	}
}

func TestPersistentShellRespawnsOnPolicyChangeKeepingCwd(t *testing.T) {
	w := &fakeWrapper{}
	cfg, ws := setupSandboxedShell(t, w)
	sub := filepath.Join(ws, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	first := GetPersistentShell(ws)
	mustExec(t, first, "cd "+sub)

	// Toggle the sandbox off: the next call gets a new, unwrapped shell in
	// the same directory, with the unscrubbed environment.
	cfg.Sandbox.Disabled = true
	second := GetPersistentShell(ws)
	if second == first {
		t.Fatal("policy change must re-spawn the shell")
	}
	if second.Sandbox().Active() {
		t.Fatal("shell spawned with the sandbox off reports active")
	}
	if w.Calls() != 1 {
		t.Fatalf("wrapper calls = %d, want 1 (disabled policy is not wrapped)", w.Calls())
	}
	if got := mustExec(t, second, "pwd"); got != sub {
		t.Fatalf("cwd after re-spawn = %q, want %q", got, sub)
	}
	if got := mustExec(t, second, `echo "$FAKE_API_KEY"`); got != "super-secret" {
		t.Fatalf("unsandboxed shell env FAKE_API_KEY = %q, want it inherited", got)
	}

	// The old shell was retired.
	select {
	case <-first.done:
	case <-time.After(5 * time.Second):
		t.Fatal("retired shell did not exit")
	}

	// Toggle back on: wrapped again.
	cfg.Sandbox.Disabled = false
	third := GetPersistentShell(ws)
	if third == second || !third.Sandbox().Active() || w.Calls() != 2 {
		t.Fatalf("re-enabling must re-spawn a wrapped shell (same=%v active=%v calls=%d)",
			third == second, third.Sandbox().Active(), w.Calls())
	}
	if got := mustExec(t, third, "pwd"); got != sub {
		t.Fatalf("cwd after second re-spawn = %q, want %q", got, sub)
	}
}

func TestPersistentShellWrapFailureDoesNotStartUnconfined(t *testing.T) {
	w := &fakeWrapper{err: sandbox.ErrWrapFailed}
	_, ws := setupSandboxedShell(t, w)

	sh := GetPersistentShell(ws)
	_, _, code, _, err := sh.Exec(context.Background(), "echo hi", 5_000)
	if err == nil || !errors.Is(err, sandbox.ErrWrapFailed) || code == 0 {
		t.Fatalf("exec on a shell whose wrap failed: code=%d err=%v, want ErrWrapFailed", code, err)
	}
	if sh.cmd != nil {
		t.Fatal("a shell whose wrap failed must not be started")
	}

	// Once the wrapper works, the next call retries the spawn.
	w.mu.Lock()
	w.err = nil
	w.mu.Unlock()
	if got := mustExec(t, GetPersistentShell(ws), "echo ok"); got != "ok" {
		t.Fatalf("retry after wrap failure = %q", got)
	}
}

// A launcher that forks (like bwrap) leaves an extra process between Pando
// and the shell: interrupting a command must still kill the command's
// processes, via the process group.
func TestPersistentShellInterruptKillsGroupBehindLauncher(t *testing.T) {
	w := &fakeWrapper{rewrite: func(cmd *exec.Cmd) {
		// /bin/sh -c '"$@"; :' forks the shell as its child instead of
		// exec'ing it, like a bwrap parent.
		argv := append([]string{"/bin/sh", "-c", `"$@"; :`, "launcher", cmd.Path}, cmd.Args[1:]...)
		cmd.Path = "/bin/sh"
		cmd.Args = argv
	}}
	_, ws := setupSandboxedShell(t, w)

	sh := GetPersistentShell(ws)
	if !sh.launcher {
		t.Fatal("a rewritten command must be recorded as launched")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	_, _, _, interrupted, _ := sh.Exec(context.Background(), "sleep 30 & echo $! > "+pidFile+"; wait", 500)
	if !interrupted {
		t.Fatal("command should have been interrupted by the timeout")
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("sleep (pid %d) survived the interrupt", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The shell went with its group; the next call re-spawns it.
	next := GetPersistentShell(ws)
	if next == sh {
		t.Fatal("killed shell must be replaced")
	}
	if got := mustExec(t, next, "echo alive"); got != "alive" {
		t.Fatalf("re-spawned shell output = %q", got)
	}
}
