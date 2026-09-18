//go:build !windows

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools/shell"
	"github.com/digiogithub/pando/internal/sandbox"
)

// TestBashRealSandboxEndToEnd drives the bash tool through the persistent
// shell with the REAL platform backend (Landlock/bwrap on Linux, Seatbelt on
// macOS): writes inside the workspace succeed without a permission prompt,
// a write to a fake $HOME outside every writable root is blocked, and the
// output carries the [sandbox] hint for the model. It skips where the
// backend cannot enforce (old kernels, Windows, sandbox-exec missing).
func TestBashRealSandboxEndToEnd(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("requires /bin/bash")
	}
	if c := sandbox.Default().Capability(); !c.Enforced {
		t.Skipf("sandbox not enforced here: %s", c.Reason)
	}

	// The workspace is under the temp dir (itself writable in
	// workspace-write), so the "outside" home must live elsewhere: a scratch
	// dir next to this package, removed afterwards.
	home, err := os.MkdirTemp(".", ".sandbox-e2e-home-")
	if err != nil {
		t.Fatal(err)
	}
	home, _ = filepath.Abs(home)
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	t.Cleanup(func() { os.RemoveAll(home) })

	ws := t.TempDir()
	if real, err := filepath.EvalSymlinks(ws); err == nil {
		ws = real
	}
	// A pre-existing subdirectory: with the Landlock-only backend new
	// top-level entries in the workspace root are denied by design.
	if err := os.Mkdir(filepath.Join(ws, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv(config.SandboxEnvVar, "")
	t.Setenv("HOME", home)
	// Deterministic error texts; localized ones are covered by the
	// sandbox.Classify unit tests.
	t.Setenv("LC_ALL", "C")
	prev := config.Get()
	config.SetForTests(&config.Config{
		WorkingDir: ws,
		Shell:      config.ShellConfig{Path: "/bin/bash", Args: []string{"--noprofile", "--norc"}},
	})
	shell.ResetForTests()
	t.Cleanup(func() {
		shell.ResetForTests()
		config.SetForTests(prev)
	})

	perms := newRecordingPermissions(false)

	resp, err := runBash(t, perms, "touch src/inside.txt && echo ok")
	if err != nil {
		t.Fatalf("workspace write: %v", err)
	}
	if n := len(perms.Requests()); n != 0 {
		t.Fatalf("permission requests = %d, want 0 (auto-allowed by the enforced sandbox)", n)
	}
	if _, err := os.Stat(filepath.Join(ws, "src", "inside.txt")); err != nil {
		t.Fatalf("workspace write did not happen: %v\noutput: %s", err, resp.Content)
	}
	md := bashMetadata(t, resp)
	if !md.ApprovedBySandbox || md.SandboxMode != string(sandbox.ModeWorkspaceWrite) {
		t.Fatalf("metadata = %+v, want approved by the workspace-write sandbox", md)
	}

	resp, err = runBash(t, perms, `touch "$HOME/outside.txt"`)
	if err != nil {
		t.Fatalf("outside write: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "outside.txt")); statErr == nil {
		t.Fatalf("write outside the workspace succeeded under the sandbox\noutput: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "[sandbox]") {
		t.Fatalf("denied command output lacks the [sandbox] hint:\n%s", resp.Content)
	}
	t.Logf("denied output:\n%s", resp.Content)

	// Protected paths stay read-only inside the workspace.
	if err := os.WriteFile(filepath.Join(ws, ".pando.toml"), []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	shell.ResetForTests() // the policy now protects the new project config file
	if _, err := runBash(t, perms, "echo tampered > .pando.toml"); err != nil {
		t.Fatalf("protected write: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(ws, ".pando.toml")); string(data) != "orig" {
		t.Fatalf(".pando.toml was modified by a sandboxed command: %q", data)
	}
}
