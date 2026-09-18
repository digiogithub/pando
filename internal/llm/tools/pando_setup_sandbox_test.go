package tools

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// The sandbox bounds what the agent may do, so pando_setup must refuse every
// attempt to change it while still letting the agent read the status.
func TestPandoSetupRefusesSandboxWrites(t *testing.T) {
	t.Setenv(config.SandboxEnvVar, "")
	withTelemetrySetupConfig(t)
	tool := NewPandoSetupTool(nil, nil)

	refused := []struct{ command, args string }{
		{"sandbox", "off"},
		{"sandbox", "disable"},
		{"sandbox", "mode read-only"},
		{"sandbox", "status --mode off"},
		{"sandbox", "set sandbox.disabled=true"},
		{"run", "sandbox off"},
		{"run", "/sandbox"},
		{"model", "sandbox.mode=off"},
		{"telemetry", "--sandbox.disabled true"},
		{"lsp", "PANDO_SANDBOX=off"},
	}
	for _, tc := range refused {
		resp := runSetupTool(t, tool, sessionCtx("sess-1"), tc.command, tc.args)
		if !resp.IsError {
			t.Fatalf("%s %q: want a refusal, got: %s", tc.command, tc.args, resp.Content)
		}
		if !strings.Contains(resp.Content, "cannot be changed by the agent") {
			t.Fatalf("%s %q: refusal should explain the sandbox is read-only, got: %s", tc.command, tc.args, resp.Content)
		}
	}
	if config.Get().Sandbox.Disabled || config.Get().Sandbox.Mode != "" {
		t.Fatalf("sandbox config changed after refused writes: %+v", config.Get().Sandbox)
	}
}

func TestPandoSetupSandboxStatusIsReadable(t *testing.T) {
	t.Setenv(config.SandboxEnvVar, "")
	withTelemetrySetupConfig(t)
	tool := NewPandoSetupTool(nil, nil)

	for _, args := range []string{"", "status"} {
		resp := runSetupTool(t, tool, sessionCtx("sess-1"), "sandbox", args)
		if resp.IsError {
			t.Fatalf("sandbox %q errored: %s", args, resp.Content)
		}
		for _, want := range []string{"Sandbox (read-only)", "mode:     workspace-write", "backend:"} {
			if !strings.Contains(resp.Content, want) {
				t.Fatalf("sandbox %q output missing %q:\n%s", args, want, resp.Content)
			}
		}
	}

	// Reading the sandbox section through the read-only config command is fine.
	if resp := runSetupTool(t, tool, sessionCtx("sess-1"), "config", "sandbox"); resp.IsError && strings.Contains(resp.Content, "cannot be changed") {
		t.Fatalf("config sandbox must stay readable, got: %s", resp.Content)
	}
}
