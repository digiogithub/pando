package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/sandbox"
	"github.com/spf13/viper"
)

// fakeSandboxWrapper reports a fixed capability and never touches a command.
type fakeSandboxWrapper struct{ c sandbox.Capability }

func (f fakeSandboxWrapper) Capability() sandbox.Capability       { return f.c }
func (f fakeSandboxWrapper) Wrap(*exec.Cmd, sandbox.Policy) error { return nil }

// withSandboxSettings loads a real config rooted at a throwaway project with
// an isolated $HOME: UpdateSandbox writes the GLOBAL config file, which would
// otherwise be the real user's. The backend is a fake enforced one so the
// response does not depend on the machine.
func withSandboxSettings(t *testing.T) *Server {
	t.Helper()
	t.Setenv(config.SandboxEnvVar, "")
	config.IsolateForTests(t)
	config.ClearOverlayProviders()
	viper.Reset()
	t.Cleanup(func() {
		config.ClearOverlayProviders()
		viper.Reset()
	})
	t.Cleanup(sandbox.SetDefaultForTests(fakeSandboxWrapper{c: sandbox.Capability{
		Backend: sandbox.BackendLandlock, Version: "5", Enforced: true,
	}}))

	if _, err := config.Load(t.TempDir(), false); err != nil {
		t.Fatalf("config.Load(): %v", err)
	}
	return &Server{}
}

func callSandbox(t *testing.T, s *Server, method, body string) (*httptest.ResponseRecorder, SandboxConfigResponse) {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, "/api/v1/config/sandbox", nil)
	} else {
		req = httptest.NewRequest(method, "/api/v1/config/sandbox", strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	s.handleConfigSandbox(rec, req)
	var resp SandboxConfigResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v (%s)", err, rec.Body.String())
		}
	}
	return rec, resp
}

func TestConfigSandboxGetShape(t *testing.T) {
	s := withSandboxSettings(t)

	rec, resp := callSandbox(t, s, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	for _, key := range []string{"config", "effective", "capability", "status", "policy", "locked", "platform"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("response is missing %q: %s", key, rec.Body.String())
		}
	}
	if resp.Capability.Backend != sandbox.BackendLandlock || !resp.Capability.Enforced {
		t.Fatalf("capability = %+v", resp.Capability)
	}
	if !resp.Status.Active || !resp.Status.Enabled || resp.Status.Mode != "workspace-write" {
		t.Fatalf("status = %+v, want an active workspace-write default", resp.Status)
	}
	// A Landlock-only backend (no bubblewrap) cannot protect .git/hooks
	// inside the workspace: the status says so and bash keeps prompting.
	if resp.Status.Label != "workspace-write (landlock+seccomp v5; partial: "+sandbox.GapProtectedPaths+")" {
		t.Fatalf("label = %q", resp.Status.Label)
	}
	if resp.Status.Full || len(resp.Status.Gaps) != 1 || resp.Policy.GuardedPorts == nil {
		t.Fatalf("status = %+v, policy ports = %v: want partial with one gap and a non-nil port list",
			resp.Status, resp.Policy.GuardedPorts)
	}
	if resp.Locked == nil || len(resp.Locked) != 0 {
		t.Fatalf("locked = %v, want an empty list", resp.Locked)
	}
	if len(resp.Policy.ProtectedPaths) == 0 || len(resp.Policy.WritableRoots) == 0 {
		t.Fatalf("policy = %+v, want resolved writable and protected paths", resp.Policy)
	}
}

func TestConfigSandboxPutPersistsAndTakesEffect(t *testing.T) {
	s := withSandboxSettings(t)

	rec, resp := callSandbox(t, s, http.MethodPut, `{"mode":"Strict","network":"restricted","denyPaths":[" ~/.ssh ",""],"autoAllowBashDisabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if resp.Config.Mode != "strict" || resp.Status.Mode != "strict" || resp.Policy.AutoAllowBash {
		t.Fatalf("response = %+v / %+v", resp.Config, resp.Status)
	}
	// The next command resolves the new policy without a restart.
	if got := sandbox.Current(); got.Mode != sandbox.ModeStrict {
		t.Fatalf("sandbox.Current().Mode = %q, want strict", got.Mode)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".pando.json"))
	if err != nil {
		t.Fatalf("global config not written: %v", err)
	}
	if !strings.Contains(string(data), `"mode": "strict"`) || !strings.Contains(string(data), "~/.ssh") {
		t.Fatalf("global config does not hold the sandbox section:\n%s", data)
	}

	// Disable: the policy turns off and the badge says so.
	rec, resp = callSandbox(t, s, http.MethodPut, `{"disabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if resp.Status.Enabled || resp.Status.Label != "off" || sandbox.Active() {
		t.Fatalf("status after disable = %+v", resp.Status)
	}
}

func TestConfigSandboxPutRejectsInvalid(t *testing.T) {
	s := withSandboxSettings(t)

	for _, body := range []string{`{"mode":"yolo"}`, `{"network":"sometimes"}`, `{"extendTo":["everything"]}`, `not json`} {
		rec, _ := callSandbox(t, s, http.MethodPut, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s: status = %d, want 400 (%s)", body, rec.Code, rec.Body.String())
		}
	}
	if config.Get().Sandbox.Mode != "" {
		t.Fatalf("an invalid PUT changed the config: %+v", config.Get().Sandbox)
	}
}

func TestConfigSandboxLockedFieldIsRefused(t *testing.T) {
	s := withSandboxSettings(t)
	config.RegisterOverlayProvider(config.OverlayProviderFunc(func(context.Context) (config.Overlay, error) {
		return config.Overlay{
			Values: map[string]any{"sandbox": map[string]any{"mode": "strict"}},
			Locked: []string{"sandbox.mode", "sandbox.disabled"},
		}, nil
	}))
	if err := config.ApplyOverlays(context.Background()); err != nil {
		t.Fatalf("ApplyOverlays: %v", err)
	}

	_, resp := callSandbox(t, s, http.MethodGet, "")
	if strings.Join(resp.Locked, ",") != "sandbox.disabled,sandbox.mode" {
		t.Fatalf("locked = %v", resp.Locked)
	}
	if resp.Config.Mode != "strict" || resp.Status.Source != "lock" {
		t.Fatalf("config/status = %+v / %+v, want the enforced strict from the lock", resp.Config, resp.Status)
	}

	for _, body := range []string{`{"mode":"off"}`, `{"mode":"strict","disabled":true}`} {
		rec, _ := callSandbox(t, s, http.MethodPut, body)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "config_key_locked") {
			t.Fatalf("PUT %s: status = %d body = %s, want 409 config_key_locked", body, rec.Code, rec.Body.String())
		}
	}
	if config.Get().Sandbox.Mode != "strict" || config.Get().Sandbox.Disabled {
		t.Fatalf("a refused PUT changed the config: %+v", config.Get().Sandbox)
	}

	// Echoing the locked value while changing an unlocked field is accepted.
	rec, resp := callSandbox(t, s, http.MethodPut, `{"mode":"strict","network":"restricted"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("echo PUT: status = %d (%s)", rec.Code, rec.Body.String())
	}
	if resp.Config.Network != "restricted" || resp.Config.Mode != "strict" {
		t.Fatalf("config after echo PUT = %+v", resp.Config)
	}
}

func TestConfigSandboxRejectsOtherMethods(t *testing.T) {
	s := withSandboxSettings(t)
	rec, _ := callSandbox(t, s, http.MethodDelete, "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
