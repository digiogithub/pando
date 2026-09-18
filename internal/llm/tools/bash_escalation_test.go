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
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/sandbox"
)

// sandboxMarkerEnv is set by markingWrapper in the environment of every
// command it wraps, so a test can tell whether a command ran confined.
const sandboxMarkerEnv = "PANDO_TEST_SANDBOXED"

// markingWrapper is an "enforced" fake backend that confines nothing but
// marks the wrapped command's environment and counts its calls.
type markingWrapper struct {
	mu    sync.Mutex
	calls int
}

func (m *markingWrapper) Capability() sandbox.Capability {
	return sandbox.Capability{Backend: "fake", Enforced: true, ProtectsNestedPaths: true, BlocksPorts: true}
}

func (m *markingWrapper) Wrap(cmd *exec.Cmd, p sandbox.Policy) error {
	if !p.Enabled() {
		return nil
	}
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	cmd.Env = append(cmd.Env, sandboxMarkerEnv+"=1")
	return nil
}

func (m *markingWrapper) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// setupEscalation is setupBashSandbox with the marking wrapper.
func setupEscalation(t *testing.T) (*config.Config, *markingWrapper) {
	t.Helper()
	cfg := setupBashSandbox(t, true)
	w := &markingWrapper{}
	restore := sandbox.SetDefaultForTests(w)
	t.Cleanup(restore)
	return cfg, w
}

func runBashParams(t *testing.T, perms permission.Service, params BashParams) (ToolResponse, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, SessionIDContextKey, bashSandboxSession)
	ctx = context.WithValue(ctx, MessageIDContextKey, "msg-1")
	input, _ := json.Marshal(params)
	return NewBashTool(perms).Run(ctx, ToolCall{ID: "call-1", Name: BashToolName, Input: string(input)})
}

func escalated(command string) BashParams {
	return BashParams{
		Command:            command,
		SandboxPermissions: SandboxPermissionsEscalated,
		Justification:      "needs the user's global npm prefix",
	}
}

// answeringPermissions is a real permission service (so auto-approve and
// session grants behave as in the app) with a subscriber that answers every
// published prompt with answer and records it.
type answeringPermissions struct {
	permission.Service
	mu        sync.Mutex
	published []permission.PermissionRequest
}

func newAnsweringPermissions(t *testing.T, answer func(svc permission.Service, req permission.PermissionRequest)) *answeringPermissions {
	t.Helper()
	a := &answeringPermissions{Service: permission.NewPermissionService()}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := a.Subscribe(ctx)
	go func() {
		for ev := range events {
			if ev.Type != pubsub.CreatedEvent {
				continue
			}
			a.mu.Lock()
			a.published = append(a.published, ev.Payload)
			a.mu.Unlock()
			answer(a.Service, ev.Payload)
		}
	}()
	return a
}

func (a *answeringPermissions) Published() []permission.PermissionRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]permission.PermissionRequest(nil), a.published...)
}

func TestBashSandboxDenialAddsHintAndMetadata(t *testing.T) {
	cases := map[string]struct {
		mutate  func(*config.Config)
		command string
		kind    string
		want    string
	}{
		"protected path": {
			command: `echo "touch: cannot touch '.pando/blocked': Read-only file system" >&2; false`,
			kind:    "fs",
			want:    "blocked a write to ",
		},
		"restricted network": {
			mutate:  func(c *config.Config) { c.Sandbox.Network = config.SandboxNetworkRestricted },
			command: `echo "curl: (6) Could not resolve host: example.com" >&2; (exit 6)`,
			kind:    "net",
			want:    "blocked network access",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, _ := setupEscalation(t)
			if tc.mutate != nil {
				tc.mutate(cfg)
			}
			perms := newRecordingPermissions(true)

			resp, err := runBashParams(t, perms, BashParams{Command: tc.command})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if !strings.Contains(resp.Content, "[sandbox]") || !strings.Contains(resp.Content, tc.want) ||
				!strings.Contains(resp.Content, `sandbox_permissions: "require_escalated"`) {
				t.Fatalf("output lacks the sandbox hint:\n%s", resp.Content)
			}
			md := bashMetadata(t, resp)
			if !md.SandboxDenied || md.SandboxDenialKind != tc.kind {
				t.Fatalf("metadata = %+v, want sandbox_denied kind %s", md, tc.kind)
			}
		})
	}
}

func TestBashNoHintForOrdinaryFailures(t *testing.T) {
	setupEscalation(t)
	perms := newRecordingPermissions(true)

	for _, command := range []string{
		"(exit 3)",
		`echo "git@github.com: Permission denied (publickey)." >&2; (exit 128)`,
	} {
		resp, err := runBashParams(t, perms, BashParams{Command: command})
		if err != nil {
			t.Fatalf("run %q: %v", command, err)
		}
		if strings.Contains(resp.Content, "[sandbox]") || bashMetadata(t, resp).SandboxDenied {
			t.Fatalf("%q: unexpected sandbox hint:\n%s", command, resp.Content)
		}
	}
}

func TestBashNoHintWhenSandboxNotEnforced(t *testing.T) {
	setupBashSandbox(t, false)
	perms := newRecordingPermissions(true)

	resp, err := runBashParams(t, perms, BashParams{Command: `echo "touch: cannot touch '.pando/x': Read-only file system" >&2; false`})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(resp.Content, "[sandbox]") || bashMetadata(t, resp).SandboxDenied {
		t.Fatalf("hint while the sandbox is not enforced:\n%s", resp.Content)
	}
}

func TestBashEscalationDeniedReturnsPermissionDenied(t *testing.T) {
	setupEscalation(t)
	perms := newRecordingPermissions(false)

	_, err := runBashParams(t, perms, escalated("touch /tmp/should-not-exist-escalation"))
	if !errors.Is(err, permission.ErrorPermissionDenied) {
		t.Fatalf("err = %v, want permission denied", err)
	}
	reqs := perms.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want exactly one", len(reqs))
	}
	r := reqs[0]
	if r.Action != permission.ActionExecuteUnsandboxed || !r.RequireExplicitApproval || !r.NeverAutoApprove {
		t.Fatalf("request = %+v, want an explicit execute_unsandboxed request", r)
	}
	if r.Justification == "" || !strings.Contains(r.Description, "Run outside sandbox") || !strings.Contains(r.Description, r.Justification) {
		t.Fatalf("description/justification missing: %+v", r)
	}
	if p, ok := r.Params.(BashPermissionsParams); !ok || !p.Unsandboxed || p.Justification == "" {
		t.Fatalf("params = %#v, want unsandboxed bash params with justification", r.Params)
	}
}

func TestBashEscalationApprovedRunsUnsandboxedInShellCwd(t *testing.T) {
	cfg, wrapper := setupEscalation(t)
	if err := os.Mkdir(filepath.Join(cfg.WorkingDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	perms := newRecordingPermissions(true)

	// A sandboxed command: auto-allowed, runs in the marked shell, and moves
	// the persistent shell into sub/.
	resp, err := runBashParams(t, perms, BashParams{Command: "cd sub && echo marker=$" + sandboxMarkerEnv})
	if err != nil || !strings.Contains(resp.Content, "marker=1") {
		t.Fatalf("sandboxed run: resp=%q err=%v, want the marker", resp.Content, err)
	}
	callsBefore := wrapper.Calls()

	resp, err = runBashParams(t, perms, escalated("echo marker=$"+sandboxMarkerEnv+"; pwd"))
	if err != nil {
		t.Fatalf("escalated run: %v", err)
	}
	if strings.Contains(resp.Content, "marker=1") {
		t.Fatalf("escalated command ran sandboxed:\n%s", resp.Content)
	}
	wantDir, _ := filepath.EvalSymlinks(filepath.Join(cfg.WorkingDir, "sub"))
	if !strings.Contains(resp.Content, wantDir) && !strings.Contains(resp.Content, filepath.Join(cfg.WorkingDir, "sub")) {
		t.Fatalf("escalated command did not run in the shell's cwd %s:\n%s", wantDir, resp.Content)
	}
	if wrapper.Calls() != callsBefore {
		t.Fatalf("wrapper called %d more times for the escalated run", wrapper.Calls()-callsBefore)
	}
	md := bashMetadata(t, resp)
	if !md.Unsandboxed || md.SandboxBackend != "" {
		t.Fatalf("metadata = %+v, want unsandboxed", md)
	}
	reqs := perms.Requests()
	if len(reqs) != 1 || reqs[0].Action != permission.ActionExecuteUnsandboxed {
		t.Fatalf("requests = %+v, want exactly one execute_unsandboxed request", reqs)
	}
}

func TestBashEscalationValidation(t *testing.T) {
	setupEscalation(t)
	perms := newRecordingPermissions(true)

	resp, err := runBashParams(t, perms, BashParams{Command: "true", SandboxPermissions: SandboxPermissionsEscalated})
	if err != nil || !resp.IsError || !strings.Contains(resp.Content, "justification is required") {
		t.Fatalf("missing justification: resp=%+v err=%v", resp, err)
	}
	resp, err = runBashParams(t, perms, BashParams{Command: "true", SandboxPermissions: "bogus"})
	if err != nil || !resp.IsError || !strings.Contains(resp.Content, "invalid sandbox_permissions") {
		t.Fatalf("invalid value: resp=%+v err=%v", resp, err)
	}
	if n := len(perms.Requests()); n != 0 {
		t.Fatalf("requests = %d, want 0", n)
	}
}

func TestBashEscalationIsNoOpWhenSandboxInactive(t *testing.T) {
	setupBashSandbox(t, false)
	perms := newRecordingPermissions(true)

	resp, err := runBashParams(t, perms, BashParams{Command: "touch created.txt && echo hi", SandboxPermissions: SandboxPermissionsEscalated})
	if err != nil || !strings.Contains(resp.Content, "hi") {
		t.Fatalf("resp=%q err=%v", resp.Content, err)
	}
	reqs := perms.Requests()
	if len(reqs) != 1 || reqs[0].Action != "execute" {
		t.Fatalf("requests = %+v, want the regular execute prompt", reqs)
	}
	if bashMetadata(t, resp).Unsandboxed {
		t.Fatal("inactive sandbox reported an unsandboxed escalation")
	}
}

func TestBashEscalationNotAutoApproved(t *testing.T) {
	setupEscalation(t)
	perms := newAnsweringPermissions(t, func(svc permission.Service, req permission.PermissionRequest) {
		svc.Deny(req)
	})
	// Every automatic grant there is: auto mode, global auto-approve (yolo,
	// goal/autopilot use these) and an unscoped session grant for bash.
	perms.AutoApproveSession(bashSandboxSession)
	perms.SetGlobalAutoApprove(true)
	perms.GrantPersistant(permission.PermissionRequest{
		SessionID: bashSandboxSession, ToolName: BashToolName, Action: permission.ActionExecuteUnsandboxed,
		Path: filepath.Dir(config.WorkingDirectory()),
	})

	_, err := runBashParams(t, perms, escalated("echo escalated"))
	if !errors.Is(err, permission.ErrorPermissionDenied) {
		t.Fatalf("err = %v, want permission denied (prompted and denied)", err)
	}
	pub := perms.Published()
	if len(pub) != 1 || pub[0].Action != permission.ActionExecuteUnsandboxed || !pub[0].RequireExplicitApproval || pub[0].Justification == "" {
		t.Fatalf("published = %+v, want one explicit execute_unsandboxed prompt", pub)
	}
}

func TestBashEscalationAutoApprovedWhenPolicyAllows(t *testing.T) {
	cfg, _ := setupEscalation(t)
	cfg.Sandbox.AllowAutoEscalation = true
	perms := newAnsweringPermissions(t, func(svc permission.Service, req permission.PermissionRequest) {
		svc.Deny(req)
	})
	perms.AutoApproveSession(bashSandboxSession)

	resp, err := runBashParams(t, perms, escalated("echo marker=$"+sandboxMarkerEnv))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if n := len(perms.Published()); n != 0 {
		t.Fatalf("published = %d prompts, want 0 (auto-escalation allowed)", n)
	}
	if strings.Contains(resp.Content, "marker=1") || !bashMetadata(t, resp).Unsandboxed {
		t.Fatalf("escalated command did not run unsandboxed:\n%s", resp.Content)
	}

	// Dangerous commands keep the floor even with auto-escalation.
	_, err = runBashParams(t, perms, escalated("sudo true"))
	if !errors.Is(err, permission.ErrorPermissionDenied) || len(perms.Published()) != 1 {
		t.Fatalf("dangerous escalation: err=%v published=%d, want one prompt (denied)", err, len(perms.Published()))
	}
}

func TestBashEscalationSessionGrantScopedByCommandPrefix(t *testing.T) {
	setupEscalation(t)
	perms := newAnsweringPermissions(t, func(svc permission.Service, req permission.PermissionRequest) {
		svc.GrantPersistant(req)
	})

	for i, command := range []string{"echo one two", "echo one three", "echo other", "echo one two; echo x"} {
		if _, err := runBashParams(t, perms, escalated(command)); err != nil {
			t.Fatalf("run %d %q: %v", i, command, err)
		}
	}
	pub := perms.Published()
	var got []string
	for _, p := range pub {
		got = append(got, p.GrantKey)
	}
	// "echo one three" reuses the "echo one" grant; the others prompt.
	want := []string{"prefix:echo one", "prefix:echo other", "exact:echo one two; echo x"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("prompted grant keys = %q, want %q", got, want)
	}
}

func TestEscalationGrantKey(t *testing.T) {
	cases := map[string]string{
		"npm install -g foo":     "prefix:npm install",
		"npm":                    "prefix:npm",
		"  go test ./...  ":      "prefix:go test",
		"ls -la /root":           "exact:ls -la /root",
		"npm install && rm -rf":  "exact:npm install && rm -rf",
		"FOO=bar make":           "exact:FOO=bar make",
		"echo $HOME":             "exact:echo $HOME",
		"cat 'a b'":              "exact:cat 'a b'",
		"cp *.go /tmp":           "exact:cp *.go /tmp",
		"git push origin main":   "prefix:git push",
		"python3 script.py arg1": "prefix:python3 script.py",
	}
	for in, want := range cases {
		if got := escalationGrantKey(in); got != want {
			t.Errorf("escalationGrantKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBashSchemaHasEscalationParams(t *testing.T) {
	setupBashSandbox(t, false)
	info := NewBashTool(newRecordingPermissions(true)).Info()
	sp, ok := info.Parameters["sandbox_permissions"].(map[string]any)
	if !ok || sp["type"] != "string" {
		t.Fatalf("sandbox_permissions schema = %#v", info.Parameters["sandbox_permissions"])
	}
	if _, ok := info.Parameters["justification"].(map[string]any); !ok {
		t.Fatal("justification param missing")
	}
	for _, r := range info.Required {
		if r == "sandbox_permissions" || r == "justification" {
			t.Fatalf("%s must be optional", r)
		}
	}
}
