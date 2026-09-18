//go:build !windows

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools/shell"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/sandbox"
)

const bashSandboxSession = "bash-sandbox-session"

// fakeBashWrapper is a sandbox backend for tests: it wraps nothing but can
// claim to be enforced. partial claims an enforced backend that cannot
// protect nested paths or block Pando's ports (sandbox.Guarantees gaps).
type fakeBashWrapper struct{ enforced, partial bool }

func (f fakeBashWrapper) Capability() sandbox.Capability {
	return sandbox.Capability{Backend: "fake", Enforced: f.enforced, ProtectsNestedPaths: !f.partial, BlocksPorts: !f.partial}
}

func (f fakeBashWrapper) Wrap(*exec.Cmd, sandbox.Policy) error { return nil }

// recordingPermissions answers every bash permission request with grant and
// records what was asked.
type recordingPermissions struct {
	permission.Service
	mu       sync.Mutex
	requests []permission.CreatePermissionRequest
}

func newRecordingPermissions(grant bool) *recordingPermissions {
	r := &recordingPermissions{Service: permission.NewPermissionService()}
	r.RegisterSessionHandler(bashSandboxSession, func(req permission.CreatePermissionRequest) bool {
		r.mu.Lock()
		r.requests = append(r.requests, req)
		r.mu.Unlock()
		return grant
	})
	return r
}

func (r *recordingPermissions) Requests() []permission.CreatePermissionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]permission.CreatePermissionRequest(nil), r.requests...)
}

func setupBashSandbox(t *testing.T, enforced bool) *config.Config {
	t.Helper()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("requires /bin/bash")
	}
	t.Setenv(config.SandboxEnvVar, "")
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		WorkingDir: t.TempDir(),
		Shell:      config.ShellConfig{Path: "/bin/bash", Args: []string{"--noprofile", "--norc"}},
	}
	prev := config.Get()
	config.SetForTests(cfg)
	restore := sandbox.SetDefaultForTests(fakeBashWrapper{enforced: enforced})
	shell.ResetForTests()
	t.Cleanup(func() {
		shell.ResetForTests()
		restore()
		config.SetForTests(prev)
	})
	return cfg
}

func runBash(t *testing.T, perms permission.Service, command string) (ToolResponse, error) {
	t.Helper()
	ctx := context.WithValue(context.Background(), SessionIDContextKey, bashSandboxSession)
	ctx = context.WithValue(ctx, MessageIDContextKey, "msg-1")
	input, _ := json.Marshal(BashParams{Command: command})
	return NewBashTool(perms).Run(ctx, ToolCall{ID: "call-1", Name: BashToolName, Input: string(input)})
}

func bashMetadata(t *testing.T, resp ToolResponse) BashResponseMetadata {
	t.Helper()
	var md BashResponseMetadata
	if err := json.Unmarshal([]byte(resp.Metadata), &md); err != nil {
		t.Fatalf("decode metadata %q: %v", resp.Metadata, err)
	}
	return md
}

func TestBashAutoAllowedWhenSandboxEnforced(t *testing.T) {
	cfg := setupBashSandbox(t, true)
	perms := newRecordingPermissions(false)

	resp, err := runBash(t, perms, "touch created.txt")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if n := len(perms.Requests()); n != 0 {
		t.Fatalf("permission requests = %d, want 0 (approved by the sandbox)", n)
	}
	if _, err := os.Stat(filepath.Join(cfg.WorkingDir, "created.txt")); err != nil {
		t.Fatalf("command did not run: %v", err)
	}
	md := bashMetadata(t, resp)
	if !md.ApprovedBySandbox || md.SandboxBackend != "fake" || md.SandboxMode != string(sandbox.ModeWorkspaceWrite) {
		t.Fatalf("metadata = %+v, want approved by the fake workspace-write sandbox", md)
	}
}

func TestBashDangerousCommandStillPromptsUnderSandbox(t *testing.T) {
	setupBashSandbox(t, true)
	perms := newRecordingPermissions(false)

	_, err := runBash(t, perms, "sudo true")
	if !errors.Is(err, permission.ErrorPermissionDenied) {
		t.Fatalf("err = %v, want permission denied", err)
	}
	reqs := perms.Requests()
	if len(reqs) != 1 || !reqs[0].RequireExplicitApproval {
		t.Fatalf("requests = %+v, want one explicit-approval request", reqs)
	}
}

func TestBashBannedCommandStillRejectedUnderSandbox(t *testing.T) {
	setupBashSandbox(t, true)
	perms := newRecordingPermissions(true)

	resp, err := runBash(t, perms, "curl https://example.com")
	if err != nil || !resp.IsError || !strings.Contains(resp.Content, "not allowed") {
		t.Fatalf("resp=%+v err=%v, want the banned-command error", resp, err)
	}
}

func TestBashPromptsWhenSandboxNotAutoAllowing(t *testing.T) {
	cases := map[string]struct {
		enforced bool
		partial  bool
		mutate   func(*config.Config)
	}{
		"backend not enforced": {enforced: false},
		"partial guarantees":   {enforced: true, partial: true},
		"sandbox disabled":     {enforced: true, mutate: func(c *config.Config) { c.Sandbox.Disabled = true }},
		"auto-allow disabled":  {enforced: true, mutate: func(c *config.Config) { c.Sandbox.AutoAllowBashDisabled = true }},
		"docker runtime":       {enforced: true, mutate: func(c *config.Config) { c.Container.Runtime = "docker" }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := setupBashSandbox(t, tc.enforced)
			if tc.mutate != nil {
				tc.mutate(cfg)
			}
			if tc.partial {
				t.Cleanup(sandbox.SetDefaultForTests(fakeBashWrapper{enforced: true, partial: true}))
				shell.ResetForTests()
			}
			perms := newRecordingPermissions(false)

			_, err := runBash(t, perms, "touch created.txt")
			if !errors.Is(err, permission.ErrorPermissionDenied) {
				t.Fatalf("err = %v, want permission denied (prompted)", err)
			}
			if n := len(perms.Requests()); n != 1 {
				t.Fatalf("permission requests = %d, want 1", n)
			}
		})
	}
}

func TestBashDescriptionMentionsSandboxOnlyWhenActive(t *testing.T) {
	cfg := setupBashSandbox(t, true)
	if desc := bashDescription(); !strings.Contains(desc, "Pando's sandbox (mode workspace-write)") {
		t.Fatalf("active sandbox not described:\n%s", desc[:600])
	}

	cfg.Sandbox.Disabled = true
	if desc := bashDescription(); strings.Contains(desc, "sandbox") {
		t.Fatal("description mentions the sandbox while it is off")
	}
}
