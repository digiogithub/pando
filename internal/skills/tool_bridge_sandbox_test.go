package skills

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
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

// isolateSandboxConfig gives the test an isolated config with an otherwise
// default (workspace-write, skills covered) sandbox and undoes it on
// cleanup; per the repo pitfall, config-under-test never calls Load().
func isolateSandboxConfig(t *testing.T, ws string) {
	t.Helper()
	t.Setenv(config.SandboxEnvVar, "")
	prev := config.Get()
	config.SetForTests(&config.Config{WorkingDir: ws})
	t.Cleanup(func() { config.SetForTests(prev) })
}

// writeSkillScript writes an executable shell script skill and returns a
// CLIToolSkill pointing at it directly (bypassing DiscoverCLITools, which is
// not what is under test here).
func writeSkillScript(t *testing.T, body string) *CLIToolSkill {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &CLIToolSkill{name: "test-skill", execPath: path}
}

func TestCLIToolSkillRunIsWrappedAndEnvScrubbed(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("requires /bin/sh")
	}
	ws := t.TempDir()
	isolateSandboxConfig(t, ws)
	w := &fakeWrapper{}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	t.Setenv("FAKE_SKILL_API_KEY", "super-secret")
	skill := writeSkillScript(t, `echo "key=[$FAKE_SKILL_API_KEY]"`)

	resp, err := skill.Run(context.Background(), tools.ToolCall{Input: "{}"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.IsError {
		t.Fatalf("Run reported an error: %s", resp.Content)
	}
	if w.Calls() != 1 {
		t.Fatalf("calls = %d, want 1 (skills are covered by default)", w.Calls())
	}
	if got := strings.TrimSpace(resp.Content); got != "key=[]" {
		t.Fatalf("output = %q, want the API key scrubbed", got)
	}
}

func TestCLIToolSkillRunFailsClosedOnWrapError(t *testing.T) {
	ws := t.TempDir()
	isolateSandboxConfig(t, ws)
	w := &fakeWrapper{err: sandbox.ErrWrapFailed}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	marker := filepath.Join(t.TempDir(), "ran")
	skill := writeSkillScript(t, "touch "+marker)

	_, err := skill.Run(context.Background(), tools.ToolCall{Input: "{}"})
	if err == nil {
		t.Fatal("expected a wrap failure to fail closed with a Go error")
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("the skill executable ran despite the sandbox setup failure")
	}
}

func TestCLIToolSkillRunNotWrappedWhenSandboxOff(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("requires /bin/sh")
	}
	ws := t.TempDir()
	t.Setenv(config.SandboxEnvVar, "")
	prev := config.Get()
	config.SetForTests(&config.Config{WorkingDir: ws, Sandbox: config.SandboxConfig{Disabled: true}})
	t.Cleanup(func() { config.SetForTests(prev) })

	w := &fakeWrapper{}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	skill := writeSkillScript(t, "echo ok")
	resp, err := skill.Run(context.Background(), tools.ToolCall{Input: "{}"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.IsError {
		t.Fatalf("Run reported an error: %s", resp.Content)
	}
	if w.Calls() != 0 {
		t.Fatalf("calls = %d, want 0 (sandbox disabled)", w.Calls())
	}
}
