package page

import (
	"strings"
	"testing"

	pandoapp "github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/tui/components/settings"
	"github.com/digiogithub/pando/internal/tui/util"
)

// withTelemetryTUIConfig isolates HOME/XDG so config.UpdateTelemetry and
// config.RegenerateTelemetryID (both GLOBAL-only writers) never touch the
// developer's real ~/.pando.toml, then installs cfg as the process-wide
// config the same way withCavemanTUIConfig does for the caveman settings.
func withTelemetryTUIConfig(t *testing.T, telemetryCfg config.TelemetryConfig) *config.Config {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	prev := config.Get()
	cfg := &config.Config{
		WorkingDir: t.TempDir(),
		Telemetry:  telemetryCfg,
	}
	config.SetForTests(cfg)
	t.Cleanup(func() { config.SetForTests(prev) })
	return cfg
}

func telemetryField(t *testing.T, cfg *config.Config, key string) settings.Field {
	t.Helper()
	section := buildGeneralSection(cfg)
	for _, field := range section.Fields {
		if field.Key == key {
			return field
		}
	}
	t.Fatalf("%q not found in the %q section", key, section.Title)
	return settings.Field{}
}

func TestBuildGeneralSectionTelemetryUnavailable(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")
	cfg := withTelemetryTUIConfig(t, config.TelemetryConfig{})

	enabled := telemetryField(t, cfg, "telemetry.enabled")
	if enabled.Type != settings.FieldToggle {
		t.Errorf("telemetry.enabled type = %v, want FieldToggle", enabled.Type)
	}
	if !enabled.Disabled {
		t.Error("telemetry.enabled must be disabled when the build has no token")
	}
	if !strings.Contains(enabled.Hint, "Not available") {
		t.Errorf("hint = %q, want it to say the setting is unavailable in this build", enabled.Hint)
	}

	debugID := telemetryField(t, cfg, "telemetry.debug_id")
	if !debugID.ReadOnly {
		t.Error("telemetry.debug_id must be read-only")
	}
	if debugID.Value != "—" {
		t.Errorf("debug id placeholder = %q, want %q", debugID.Value, "—")
	}

	minLevel := telemetryField(t, cfg, "telemetry.min_level")
	if !minLevel.Disabled {
		t.Error("telemetry.min_level must be disabled when telemetry is unavailable")
	}

	copyID := telemetryField(t, cfg, "action:telemetry_copy_id")
	if copyID.Type != settings.FieldAction {
		t.Errorf("action:telemetry_copy_id type = %v, want FieldAction", copyID.Type)
	}
	if !copyID.Disabled {
		t.Error("action:telemetry_copy_id must be disabled with no debug id yet")
	}

	regen := telemetryField(t, cfg, "action:telemetry_regenerate_id")
	if !regen.Disabled {
		t.Error("action:telemetry_regenerate_id must be disabled when telemetry is unavailable")
	}
}

func TestBuildGeneralSectionTelemetryAvailableAndEnabled(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test-token")
	cfg := withTelemetryTUIConfig(t, config.TelemetryConfig{
		Enabled:  true,
		DebugID:  "1234567890123456",
		MinLevel: config.TelemetryLevelWarn,
	})

	enabled := telemetryField(t, cfg, "telemetry.enabled")
	if enabled.Disabled {
		t.Error("telemetry.enabled must not be disabled when the build carries a token")
	}
	if enabled.Value != "true" {
		t.Errorf("telemetry.enabled value = %q, want %q", enabled.Value, "true")
	}

	debugID := telemetryField(t, cfg, "telemetry.debug_id")
	if debugID.Value != "1234-5678-9012-3456" {
		t.Errorf("debug id display = %q, want the grouped id", debugID.Value)
	}
	if len(debugID.Value) != 19 {
		t.Errorf("debug id display length = %d, want 19", len(debugID.Value))
	}

	minLevel := telemetryField(t, cfg, "telemetry.min_level")
	if minLevel.Disabled {
		t.Error("telemetry.min_level must not be disabled once telemetry is enabled")
	}
	if minLevel.Value != "warn" {
		t.Errorf("telemetry.min_level value = %q, want %q", minLevel.Value, "warn")
	}
	if want := []string{"debug", "info", "warn", "error"}; strings.Join(minLevel.Options, ",") != strings.Join(want, ",") {
		t.Errorf("telemetry.min_level options = %v, want %v", minLevel.Options, want)
	}

	copyID := telemetryField(t, cfg, "action:telemetry_copy_id")
	if copyID.Disabled {
		t.Error("action:telemetry_copy_id must be enabled once a debug id exists")
	}

	regen := telemetryField(t, cfg, "action:telemetry_regenerate_id")
	if regen.Disabled {
		t.Error("action:telemetry_regenerate_id must be enabled when telemetry is available")
	}
}

func TestPersistSettingEnableTelemetryRefusedWithoutToken(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")
	withTelemetryTUIConfig(t, config.TelemetryConfig{})

	if err := persistSetting(nil, settings.Field{Key: "telemetry.enabled", Value: "true"}); err == nil {
		t.Fatal("expected an error enabling telemetry with no token available")
	}
	if config.Get().Telemetry.Enabled {
		t.Error("telemetry must stay disabled on a refused enable")
	}
}

func TestPersistSettingEnableTelemetryGeneratesDebugID(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test-token")
	withTelemetryTUIConfig(t, config.TelemetryConfig{})

	if err := persistSetting(nil, settings.Field{Key: "telemetry.enabled", Value: "true"}); err != nil {
		t.Fatalf("persistSetting: %v", err)
	}

	cfg := config.Get()
	if !cfg.Telemetry.Enabled {
		t.Fatal("telemetry.enabled did not persist")
	}
	if len(cfg.Telemetry.DebugID) != 16 {
		t.Fatalf("debug id = %q, want 16 digits", cfg.Telemetry.DebugID)
	}

	// The rebuilt section (persistSetting alone, no config.Bus round trip) must
	// already show the new id grouped and 19 characters long — exactly what a
	// user would copy into a support issue.
	field := telemetryField(t, cfg, "telemetry.debug_id")
	if len(field.Value) != 19 {
		t.Errorf("debug id display = %q (%d chars), want 19", field.Value, len(field.Value))
	}
}

func TestPersistSettingTelemetryMinLevel(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test-token")
	withTelemetryTUIConfig(t, config.TelemetryConfig{Enabled: true, DebugID: "1234567890123456"})

	if err := persistSetting(nil, settings.Field{Key: "telemetry.min_level", Value: "error"}); err != nil {
		t.Fatalf("persistSetting: %v", err)
	}
	if got := config.Get().Telemetry.MinLevel; got != "error" {
		t.Errorf("telemetry min level = %q, want %q", got, "error")
	}
}

func TestPersistSettingTelemetryMinLevelRejectsUnknown(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test-token")
	withTelemetryTUIConfig(t, config.TelemetryConfig{Enabled: true, MinLevel: config.TelemetryLevelInfo})

	if err := persistSetting(nil, settings.Field{Key: "telemetry.min_level", Value: "shouty"}); err == nil {
		t.Fatal("expected an error for an unrecognized level")
	}
	if got := config.Get().Telemetry.MinLevel; got != config.TelemetryLevelInfo {
		t.Errorf("min level changed to %q on a rejected save, want %q", got, config.TelemetryLevelInfo)
	}
}

// swapClipboardHelper replaces util.CopyToClipboard with a fake for the
// duration of the test, restoring the original on cleanup. It is what makes
// the copy action testable without touching a real terminal or OS clipboard.
func swapClipboardHelper(t *testing.T, fake func(string) bool) *string {
	t.Helper()
	var captured string
	prev := util.CopyToClipboard
	util.CopyToClipboard = func(text string) bool {
		captured = text
		if fake != nil {
			return fake(text)
		}
		return true
	}
	t.Cleanup(func() { util.CopyToClipboard = prev })
	return &captured
}

func newTestSettingsPage() *settingsPage {
	return &settingsPage{app: &pandoapp.App{}, settings: settings.NewSettingsCmp()}
}

func TestCopyTelemetryDebugIDInvokesClipboardHelper(t *testing.T) {
	withTelemetryTUIConfig(t, config.TelemetryConfig{DebugID: "1234567890123456"})
	captured := swapClipboardHelper(t, nil)

	p := newTestSettingsPage()
	cmd := p.copyTelemetryDebugID()
	if cmd == nil {
		t.Fatal("expected a status command")
	}
	msg, ok := cmd().(util.InfoMsg)
	if !ok {
		t.Fatalf("expected util.InfoMsg, got %T", cmd())
	}
	if msg.Type != util.InfoTypeInfo {
		t.Errorf("info type = %v, want InfoTypeInfo", msg.Type)
	}
	if *captured != "1234-5678-9012-3456" {
		t.Errorf("clipboard helper got %q, want the grouped id", *captured)
	}
}

func TestCopyTelemetryDebugIDErrorsWithNoID(t *testing.T) {
	withTelemetryTUIConfig(t, config.TelemetryConfig{})
	captured := swapClipboardHelper(t, nil)

	p := newTestSettingsPage()
	cmd := p.copyTelemetryDebugID()
	if cmd == nil {
		t.Fatal("expected an error command")
	}
	msg, ok := cmd().(util.InfoMsg)
	if !ok || msg.Type != util.InfoTypeError {
		t.Fatalf("expected an InfoTypeError message, got %#v", cmd())
	}
	if *captured != "" {
		t.Error("clipboard helper must not run when there is no debug id yet")
	}
}

func TestRegenerateTelemetryIDChangesIDAndRebuildsSection(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test-token")
	withTelemetryTUIConfig(t, config.TelemetryConfig{Enabled: true, DebugID: "1111111111111111"})

	p := newTestSettingsPage()
	p.settings.SetSections(buildSections(p.app))
	p.settings.SetSize(80, 40)

	cmd := p.regenerateTelemetryID()
	if cmd == nil {
		t.Fatal("expected a status command")
	}
	msg, ok := cmd().(util.InfoMsg)
	if !ok || msg.Type != util.InfoTypeInfo {
		t.Fatalf("expected an InfoTypeInfo message, got %#v", cmd())
	}

	newID := config.Get().Telemetry.DebugID
	if newID == "1111111111111111" {
		t.Error("debug id was not regenerated")
	}
	if len(newID) != 16 {
		t.Fatalf("new debug id = %q, want 16 digits", newID)
	}

	// buildGeneralSection must reflect the new id right away: this is the
	// synchronous, config.Bus-independent rebuild path required by Phase 5.
	field := telemetryField(t, config.Get(), "telemetry.debug_id")
	if !strings.HasPrefix(field.Value, newID[:4]) {
		t.Errorf("rebuilt debug id field = %q, does not reflect the new id %q", field.Value, newID)
	}
}

// TestRegenerateTelemetryIDAllowedWithoutTokenWhenIDExists verifies the
// code-review consistency fix: regenerate is gated on "a debug id already
// exists", not telemetry.Available() — a build with no token can still have
// a leftover id from before (e.g. it was built with a token, then rebuilt
// without one), and regenerating that leftover id is harmless. This is also
// the rule handlers_settings.go's handlePutSettings uses now, so both
// surfaces agree (previously the TUI alone refused here).
func TestRegenerateTelemetryIDAllowedWithoutTokenWhenIDExists(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")
	withTelemetryTUIConfig(t, config.TelemetryConfig{DebugID: "1111111111111111"})

	p := newTestSettingsPage()
	cmd := p.regenerateTelemetryID()
	msg, ok := cmd().(util.InfoMsg)
	if !ok || msg.Type != util.InfoTypeInfo {
		t.Fatalf("expected an InfoTypeInfo message (regenerate allowed when an id already exists), got %#v", cmd())
	}
	if config.Get().Telemetry.DebugID == "1111111111111111" {
		t.Error("debug id was not regenerated")
	}
}

// TestRegenerateTelemetryIDRefusedWithoutExistingID verifies the flip side:
// with no debug id at all yet (never enabled), regenerate is refused —
// regardless of whether the build has a token — since "regenerate" only
// makes sense once there is something to regenerate; enabling telemetry is
// what generates the first id.
func TestRegenerateTelemetryIDRefusedWithoutExistingID(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test-token")
	withTelemetryTUIConfig(t, config.TelemetryConfig{})

	p := newTestSettingsPage()
	cmd := p.regenerateTelemetryID()
	msg, ok := cmd().(util.InfoMsg)
	if !ok || msg.Type != util.InfoTypeError {
		t.Fatalf("expected an error message, got %#v", cmd())
	}
	if config.Get().Telemetry.DebugID != "" {
		t.Error("a debug id was generated despite none existing before regenerate")
	}
}
