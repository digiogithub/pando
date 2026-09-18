package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/sandbox"
)

// fakeWrapper is an "enforced" backend that records the commands it wraps,
// mirroring internal/llm/tools/shell's test double.
type fakeWrapper struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeWrapper) Capability() sandbox.Capability {
	return sandbox.Capability{Backend: "fake", Version: "1", Enforced: true}
}

func (f *fakeWrapper) Wrap(cmd *exec.Cmd, p sandbox.Policy) error {
	if !p.Enabled() {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func (f *fakeWrapper) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// isolateSandboxConfig gives the test an isolated Sandbox config and undoes
// it on cleanup; per the repo pitfall, config-under-test never calls Load().
func isolateSandboxConfig(t *testing.T, sc config.SandboxConfig) {
	t.Helper()
	t.Setenv(config.SandboxEnvVar, "")
	prev := config.Get()
	config.SetForTests(&config.Config{Sandbox: sc})
	t.Cleanup(func() { config.SetForTests(prev) })
}

func TestWrapSubagentCmdNotCoveredByDefault(t *testing.T) {
	// Sandbox.ExtendTo does not include "subagents" by default
	// (sandbox.DefaultExtendTo is acp-terminals + skills only): most
	// deployments must run these external CLIs unsandboxed.
	isolateSandboxConfig(t, config.SandboxConfig{})
	w := &fakeWrapper{}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	ws := t.TempDir()
	cmd := exec.Command("true")
	if _, _, err := wrapSubagentCmd(cmd, "claude", ws, subagentSandboxOpts{}); err != nil {
		t.Fatalf("wrapSubagentCmd: %v", err)
	}
	if w.Calls() != 0 {
		t.Fatalf("calls = %d, want 0 (subagents are opt-in)", w.Calls())
	}
}

func TestWrapSubagentCmdWrappedWhenExtended(t *testing.T) {
	isolateSandboxConfig(t, config.SandboxConfig{ExtendTo: []string{"subagents"}})
	w := &fakeWrapper{}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	t.Setenv("FAKE_SUBAGENT_API_KEY", "super-secret")
	ws := t.TempDir()
	logDir := t.TempDir()
	cmd := exec.Command("true")
	cmd.Env = append(cmd.Env, "FAKE_SUBAGENT_API_KEY=super-secret")

	policy, _, err := wrapSubagentCmd(cmd, "claude", ws, subagentSandboxOpts{ExtraRoots: []string{logDir}})
	if err != nil {
		t.Fatalf("wrapSubagentCmd: %v", err)
	}
	if w.Calls() != 1 {
		t.Fatalf("calls = %d, want 1 (Sandbox.ExtendTo includes subagents)", w.Calls())
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "FAKE_SUBAGENT_API_KEY=") {
			t.Fatalf("env not scrubbed: %q leaked into the wrapped command", kv)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available")
	}
	wantClaudeDir := filepath.Join(home, ".claude")
	if !containsPath(policy.WritableRoots, wantClaudeDir) {
		t.Fatalf("WritableRoots = %v, want it to include %q (claude's own config dir)", policy.WritableRoots, wantClaudeDir)
	}
	if !containsPath(policy.WritableRoots, logDir) {
		t.Fatalf("WritableRoots = %v, want it to include the spawner's own logDir %q", policy.WritableRoots, logDir)
	}
}

func TestWrapSubagentCmdKeepEnvSurvivesScrub(t *testing.T) {
	isolateSandboxConfig(t, config.SandboxConfig{ExtendTo: []string{"subagents"}})
	w := &fakeWrapper{}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	ws := t.TempDir()
	cmd := exec.Command("true")
	cmd.Env = []string{"ANTHROPIC_AUTH_TOKEN=ollama", "OTHER_SECRET_TOKEN=drop-me"}

	if _, _, err := wrapSubagentCmd(cmd, "claude", ws, subagentSandboxOpts{
		KeepEnv: []string{"ANTHROPIC_AUTH_TOKEN"},
	}); err != nil {
		t.Fatalf("wrapSubagentCmd: %v", err)
	}
	var sawKept, sawDropped bool
	for _, kv := range cmd.Env {
		if kv == "ANTHROPIC_AUTH_TOKEN=ollama" {
			sawKept = true
		}
		if strings.HasPrefix(kv, "OTHER_SECRET_TOKEN=") {
			sawDropped = true
		}
	}
	if !sawKept {
		t.Fatal("ANTHROPIC_AUTH_TOKEN was scrubbed despite being in KeepEnv")
	}
	if sawDropped {
		t.Fatal("OTHER_SECRET_TOKEN (not in KeepEnv) was not scrubbed")
	}
}

func TestWrapSubagentCmdAllowOwnControlPlane(t *testing.T) {
	isolateSandboxConfig(t, config.SandboxConfig{ExtendTo: []string{"subagents"}})
	w := &fakeWrapper{}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	ws := t.TempDir()
	cmd := exec.Command("true")
	policy, _, err := wrapSubagentCmd(cmd, "pando", ws, subagentSandboxOpts{AllowOwnControlPlane: true})
	if err != nil {
		t.Fatalf("wrapSubagentCmd: %v", err)
	}
	if containsPath(policy.ProtectedPaths, filepath.Join(ws, ".pando")) {
		t.Fatalf("ProtectedPaths = %v, want the nested project's own .pando un-protected", policy.ProtectedPaths)
	}
	if containsPath(policy.ProtectedPaths, filepath.Join(ws, ".pando.toml")) {
		t.Fatalf("ProtectedPaths = %v, want the nested project's own .pando.toml un-protected", policy.ProtectedPaths)
	}
	if !containsPath(policy.ProtectedPaths, filepath.Join(ws, ".git", "hooks")) {
		t.Fatalf("ProtectedPaths = %v, want .git/hooks still protected", policy.ProtectedPaths)
	}
}

func TestWrapSubagentCmdFailsClosedOnWrapError(t *testing.T) {
	isolateSandboxConfig(t, config.SandboxConfig{ExtendTo: []string{"subagents"}})
	restore := sandbox.SetDefaultForTests(&fakeWrapper{err: sandbox.ErrWrapFailed})
	defer restore()

	ws := t.TempDir()
	cmd := exec.Command("true")
	if _, _, err := wrapSubagentCmd(cmd, "claude", ws, subagentSandboxOpts{}); err == nil {
		t.Fatal("expected a wrap failure to be reported, not swallowed")
	}
}

func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}
