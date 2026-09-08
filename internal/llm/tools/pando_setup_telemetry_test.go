package tools

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// withTelemetrySetupConfig loads a real config rooted at a throwaway project
// directory, with $HOME and $XDG_CONFIG_HOME pointed at an empty temp
// directory. Telemetry is a GLOBAL-only setting (internal/config/telemetry.go):
// enabling it, disabling it, or regenerating the debug id always persists
// through config.updateGlobalCfgFile, which falls back to the real user's
// $HOME when not isolated — so any test that calls the "telemetry" command's
// enable/disable/regenerate/level actions MUST go through this helper rather
// than config.SetForTests (which never resolves a config file path at all).
//
// config.Load is a process-wide singleton loader: once the package-level
// config is non-nil it just returns the cached value and ignores its
// arguments, so config.ResetForTests() must run first or this call would
// silently reuse whatever an earlier test in this binary loaded (including a
// real, non-isolated $HOME on a machine where telemetry was ever enabled for
// real). Mirrors internal/api's withTelemetrySettings / internal/tui/page's
// withTelemetryTUIConfig helpers used for the same feature's other surfaces.
func withTelemetrySetupConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	dir := t.TempDir()
	config.ResetForTests()
	t.Cleanup(config.ResetForTests)

	cfg, err := config.Load(dir, false)
	if err != nil {
		t.Fatalf("config.Load(): %v", err)
	}
	return cfg
}

func TestPandoSetupTelemetryStatusUnavailableByDefault(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")
	withTelemetrySetupConfig(t)

	resp := runSetupTool(t, NewPandoSetupTool(nil, nil), sessionCtx("sess-1"), "telemetry", "")
	if resp.IsError {
		t.Fatalf("telemetry status errored: %s", resp.Content)
	}
	for _, want := range []string{"available: no", "enabled:   no", "min level: info"} {
		if !strings.Contains(resp.Content, want) {
			t.Fatalf("status output missing %q:\n%s", want, resp.Content)
		}
	}
	if !strings.Contains(resp.Content, "none yet") {
		t.Fatalf("status output should say no debug id exists yet:\n%s", resp.Content)
	}
}

func TestPandoSetupTelemetryEnableRefusedWithoutToken(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")
	withTelemetrySetupConfig(t)

	resp := runSetupTool(t, NewPandoSetupTool(nil, nil), sessionCtx("sess-1"), "telemetry", "enable")
	if !resp.IsError {
		t.Fatalf("enable without a token should error, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "not available") {
		t.Fatalf("error should explain telemetry is unavailable in this build:\n%s", resp.Content)
	}
	if config.Get().Telemetry.Enabled {
		t.Fatal("Enabled must stay false when the enable is refused")
	}
}

func TestPandoSetupTelemetryEnableGeneratesDebugID(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test")
	withTelemetrySetupConfig(t)

	resp := runSetupTool(t, NewPandoSetupTool(nil, nil), sessionCtx("sess-1"), "telemetry", "enable")
	if resp.IsError {
		t.Fatalf("enable errored: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "enabled") {
		t.Fatalf("enable output should confirm the change:\n%s", resp.Content)
	}

	id := config.Get().Telemetry.DebugID
	if len(id) != 16 {
		t.Fatalf("DebugID = %q, want 16 digits", id)
	}
	grouped := config.TelemetryDebugIDDisplay()
	if !strings.Contains(resp.Content, grouped) {
		t.Fatalf("enable output missing the grouped debug id %q:\n%s", grouped, resp.Content)
	}
	if !config.Get().Telemetry.Enabled {
		t.Fatal("Enabled should be true after a successful enable")
	}
}

func TestPandoSetupTelemetryDisableKeepsDebugID(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test")
	withTelemetrySetupConfig(t)
	tool := NewPandoSetupTool(nil, nil)

	if resp := runSetupTool(t, tool, sessionCtx("sess-1"), "telemetry", "enable"); resp.IsError {
		t.Fatalf("enable errored: %s", resp.Content)
	}
	id := config.Get().Telemetry.DebugID

	resp := runSetupTool(t, tool, sessionCtx("sess-1"), "telemetry", "disable")
	if resp.IsError {
		t.Fatalf("disable errored: %s", resp.Content)
	}
	if config.Get().Telemetry.Enabled {
		t.Fatal("Enabled should be false after disable")
	}
	if config.Get().Telemetry.DebugID != id {
		t.Fatalf("disable must keep the debug id: got %q, want %q", config.Get().Telemetry.DebugID, id)
	}

	// Re-enabling reuses the same id rather than minting a new one.
	if resp := runSetupTool(t, tool, sessionCtx("sess-1"), "telemetry", "enable"); resp.IsError {
		t.Fatalf("re-enable errored: %s", resp.Content)
	}
	if config.Get().Telemetry.DebugID != id {
		t.Fatalf("re-enable changed the debug id: got %q, want %q", config.Get().Telemetry.DebugID, id)
	}
}

func TestPandoSetupTelemetryRegenerateChangesDebugID(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test")
	withTelemetrySetupConfig(t)
	tool := NewPandoSetupTool(nil, nil)

	if resp := runSetupTool(t, tool, sessionCtx("sess-1"), "telemetry", "enable"); resp.IsError {
		t.Fatalf("enable errored: %s", resp.Content)
	}
	original := config.Get().Telemetry.DebugID

	resp := runSetupTool(t, tool, sessionCtx("sess-1"), "telemetry", "regenerate")
	if resp.IsError {
		t.Fatalf("regenerate errored: %s", resp.Content)
	}
	newID := config.Get().Telemetry.DebugID
	if len(newID) != 16 {
		t.Fatalf("regenerated DebugID = %q, want 16 digits", newID)
	}
	if newID == original {
		t.Fatal("regenerate should replace the debug id, not keep it")
	}
	if !strings.Contains(resp.Content, config.TelemetryDebugIDDisplay()) {
		t.Fatalf("regenerate output missing the new grouped debug id:\n%s", resp.Content)
	}
}

func TestPandoSetupTelemetryRegenerateRefusedWithoutToken(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")
	withTelemetrySetupConfig(t)

	resp := runSetupTool(t, NewPandoSetupTool(nil, nil), sessionCtx("sess-1"), "telemetry", "regenerate")
	if !resp.IsError {
		t.Fatalf("regenerate without a token should error, got: %s", resp.Content)
	}
}

func TestPandoSetupTelemetryLevel(t *testing.T) {
	withTelemetrySetupConfig(t)
	tool := NewPandoSetupTool(nil, nil)

	resp := runSetupTool(t, tool, sessionCtx("sess-1"), "telemetry", "level warn")
	if resp.IsError {
		t.Fatalf("level warn errored: %s", resp.Content)
	}
	if config.Get().Telemetry.MinLevel != "warn" {
		t.Fatalf("MinLevel = %q, want warn", config.Get().Telemetry.MinLevel)
	}

	bad := runSetupTool(t, tool, sessionCtx("sess-1"), "telemetry", "level verbose")
	if !bad.IsError {
		t.Fatalf("an unknown level should error, got: %s", bad.Content)
	}
	if config.Get().Telemetry.MinLevel != "warn" {
		t.Fatalf("a rejected level must not change the persisted one, got %q", config.Get().Telemetry.MinLevel)
	}
}

func TestPandoSetupTelemetryUnknownAction(t *testing.T) {
	withTelemetrySetupConfig(t)

	resp := runSetupTool(t, NewPandoSetupTool(nil, nil), sessionCtx("sess-1"), "telemetry", "frobnicate")
	if !resp.IsError {
		t.Fatalf("an unknown telemetry action should error, got: %s", resp.Content)
	}
}

func TestPandoSetupTelemetryHelp(t *testing.T) {
	resp := runSetupTool(t, NewPandoSetupTool(nil, nil), sessionCtx("sess-1"), "telemetry", "--help")
	if resp.IsError {
		t.Fatalf("telemetry --help errored: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Usage: telemetry") {
		t.Fatalf("help output missing usage:\n%s", resp.Content)
	}
}
