package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/spf13/viper"
)

// withTelemetrySettings loads a real config rooted at a throwaway project
// directory, with $HOME and $XDG_CONFIG_HOME pointed at an empty temp
// directory. Telemetry is a GLOBAL-only setting (internal/config/telemetry.go):
// enabling it or regenerating the debug id always persists through
// config.updateGlobalCfgFile, which falls back to the real user's $HOME when
// not isolated — so every telemetry-touching test MUST go through this
// helper rather than config.SetForTests (which never resolves a config file
// path at all, and would silently skip the persistence path this handler
// exercises). Mirrors internal/config.isolateGlobalConfig combined with the
// config.Load-based setup already used by loadTestConfig in
// handlers_container_test.go and proven to write to the isolated global file
// by internal/config.TestUpdateTelemetryWritesGlobalFileOnly.
func withTelemetrySettings(t *testing.T) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	dir := t.TempDir()
	_ = config.Reload()
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })

	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("config.Load(): %v", err)
	}

	return &Server{}
}

func TestGetSettingsExposesTelemetryFields(t *testing.T) {
	server := withTelemetrySettings(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := decodeSettings(t, rec)
	if resp.TelemetryEnabled {
		t.Error("telemetry_enabled should default to false")
	}
	if resp.TelemetryDebugID != "" {
		t.Errorf("telemetry_debug_id = %q, want empty before telemetry is ever enabled", resp.TelemetryDebugID)
	}
	if resp.TelemetryMinLevel != config.TelemetryLevelInfo {
		t.Errorf("telemetry_min_level = %q, want %q", resp.TelemetryMinLevel, config.TelemetryLevelInfo)
	}
	if resp.TelemetryAvailable {
		t.Error("telemetry_available should be false: no PANDO_TELEMETRY_TOKEN set")
	}
}

func TestPutSettingsEnableTelemetryGeneratesDebugID(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test")
	server := withTelemetrySettings(t)

	rec := putSettings(t, server, `{"telemetry_enabled": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := decodeSettings(t, rec)
	if !resp.TelemetryEnabled {
		t.Error("telemetry_enabled should be true after enabling")
	}
	if !resp.TelemetryAvailable {
		t.Error("telemetry_available should be true with PANDO_TELEMETRY_TOKEN set")
	}
	if got := len(resp.TelemetryDebugID); got != 19 { // "1234-5678-9012-3456"
		t.Fatalf("telemetry_debug_id = %q (len %d), want grouped 16-digit id (len 19)", resp.TelemetryDebugID, got)
	}

	if !config.Get().Telemetry.Enabled {
		t.Error("in-memory config should have telemetry enabled")
	}
	if len(config.Get().Telemetry.DebugID) != 16 {
		t.Errorf("stored debug id = %q, want 16 raw digits", config.Get().Telemetry.DebugID)
	}
}

func TestPutSettingsEnableTelemetryWithoutTokenRejected(t *testing.T) {
	server := withTelemetrySettings(t)

	rec := putSettings(t, server, `{"telemetry_enabled": true}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	if config.Get().Telemetry.Enabled {
		t.Error("telemetry must not be enabled when the build carries no ingest token")
	}
	if config.Get().Telemetry.DebugID != "" {
		t.Error("a debug id must not be generated on a rejected enable")
	}
}

func TestPutSettingsRegenerateTelemetryIDChangesID(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test")
	server := withTelemetrySettings(t)

	rec := putSettings(t, server, `{"telemetry_enabled": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	id1 := decodeSettings(t, rec).TelemetryDebugID

	rec = putSettings(t, server, `{"telemetry_regenerate_id": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeSettings(t, rec)
	if resp.TelemetryDebugID == id1 {
		t.Fatalf("regenerate did not change the debug id: still %q", id1)
	}
	if !resp.TelemetryEnabled {
		t.Error("regenerate must not disable telemetry")
	}
	if len(resp.TelemetryDebugID) != 19 {
		t.Fatalf("telemetry_debug_id = %q, want grouped 16-digit id", resp.TelemetryDebugID)
	}
}

// TestPutSettingsRegenerateWithoutExistingIDRejected verifies the review's
// API/TUI consistency fix: regenerate is refused (422) when no debug id
// exists yet — enabling telemetry is what generates the first one — the
// same rule internal/tui/page/settings.go's regenerateTelemetryID now uses.
func TestPutSettingsRegenerateWithoutExistingIDRejected(t *testing.T) {
	server := withTelemetrySettings(t)

	rec := putSettings(t, server, `{"telemetry_regenerate_id": true}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if config.Get().Telemetry.DebugID != "" {
		t.Error("a debug id was generated despite none existing before regenerate")
	}
}

// TestPutSettingsRegenerateAllowedWithoutTokenWhenIDExists verifies
// regenerate succeeds purely on "an id already exists", independent of
// telemetry.Available() — a build with no token can still have a leftover
// id from before enabling was possible.
func TestPutSettingsRegenerateAllowedWithoutTokenWhenIDExists(t *testing.T) {
	// Enable with a token first to obtain an id...
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test")
	server := withTelemetrySettings(t)
	rec := putSettings(t, server, `{"telemetry_enabled": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	id1 := decodeSettings(t, rec).TelemetryDebugID

	// ...then simulate a token-less rebuild for the regenerate call.
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")

	rec = putSettings(t, server, `{"telemetry_regenerate_id": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := decodeSettings(t, rec).TelemetryDebugID; got == id1 {
		t.Error("debug id was not regenerated")
	}
}

func TestPutSettingsUpdatesTelemetryMinLevel(t *testing.T) {
	server := withTelemetrySettings(t)

	rec := putSettings(t, server, `{"telemetry_min_level": "warn"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := decodeSettings(t, rec).TelemetryMinLevel; got != "warn" {
		t.Errorf("telemetry_min_level = %q, want %q", got, "warn")
	}

	rec = putSettings(t, server, `{"telemetry_min_level": "verbose"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d for an invalid level (body: %s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	// A rejected value must not change the persisted level.
	if got := config.Get().Telemetry.MinLevel; got != "warn" {
		t.Errorf("min level after rejected update = %q, want unchanged %q", got, "warn")
	}
}

// TestPutSettingsMinLevelNoopWhenUnchanged verifies the review fix: PUTting
// telemetry_min_level with the value already in effect must not perform
// another GLOBAL config file write (WebUI's saveSettings always PUTs the
// whole draft config, so every ordinary save would otherwise re-persist
// telemetry.minLevel even when the user only changed an unrelated field).
// config.UpdateTelemetryMinLevel publishes on config.Bus only on an actual
// persisted change, so "no Bus event" is used as the write-detection signal
// — the same mechanism TelemetryEnabled's existing no-op guard relies on.
func TestPutSettingsMinLevelNoopWhenUnchanged(t *testing.T) {
	server := withTelemetrySettings(t)

	// Establish a known level first.
	rec := putSettings(t, server, `{"telemetry_min_level": "warn"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	ch := make(chan config.ConfigChangeEvent, 4)
	config.Bus.Subscribe(ch)
	defer config.Bus.Unsubscribe(ch)

	// Re-saving the SAME level (including a case/whitespace variant, which
	// config.UpdateTelemetryMinLevel would normalize identically) must not
	// publish a change event.
	rec = putSettings(t, server, `{"telemetry_min_level": " Warn "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("noop status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	select {
	case ev := <-ch:
		t.Fatalf("unexpected config.Bus event for an unchanged telemetry_min_level: %+v", ev)
	case <-time.After(20 * time.Millisecond):
	}

	// A genuinely different value still persists and publishes normally.
	rec = putSettings(t, server, `{"telemetry_min_level": "error"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("change status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	select {
	case ev := <-ch:
		if ev.Section != "telemetry" {
			t.Errorf("event section = %q, want telemetry", ev.Section)
		}
	case <-time.After(time.Second):
		t.Fatal("expected a config.Bus event for an actual telemetry_min_level change, got none")
	}
	if got := config.Get().Telemetry.MinLevel; got != "error" {
		t.Errorf("min level = %q, want %q", got, "error")
	}
}
