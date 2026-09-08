package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestTelemetryDefaults verifies the viper defaults registered in setDefaults
// without touching any file on disk.
func TestTelemetryDefaults(t *testing.T) {
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	configureViper()
	setDefaults(false)

	var loaded Config
	if err := viper.Unmarshal(&loaded); err != nil {
		t.Fatalf("unmarshal defaults: %v", err)
	}

	if loaded.Telemetry.Enabled {
		t.Fatal("telemetry.enabled default = true, want false")
	}
	if got := loaded.Telemetry.MinLevel; got != TelemetryLevelInfo {
		t.Fatalf("telemetry.minLevel default = %q, want %q", got, TelemetryLevelInfo)
	}
	if loaded.Telemetry.DebugID != "" {
		t.Fatalf("telemetry.debugId default = %q, want empty", loaded.Telemetry.DebugID)
	}
}

// TestUpdateTelemetryWritesGlobalFileOnly verifies that enabling telemetry
// always persists to the GLOBAL config file, even when a project-local
// .pando.toml is active for the current working directory (higher merge
// priority than the global file), and that the local file is left untouched.
func TestUpdateTelemetryWritesGlobalFileOnly(t *testing.T) {
	isolateGlobalConfig(t)
	homeDir := os.Getenv("HOME")

	projectDir := t.TempDir()
	localConfigPath := filepath.Join(projectDir, ".pando.toml")
	localBody := "[Mesnada]\nEnabled = true\n"
	if err := os.WriteFile(localConfigPath, []byte(localBody), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })
	if _, err := Load(projectDir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Telemetry.Enabled {
		t.Fatal("telemetry should start disabled")
	}

	// Sanity check: the ordinary (local-preferring) resolver must pick the
	// project file here, proving the scenario actually exercises the
	// global-vs-local distinction.
	if resolved, err := ResolveConfigFilePath(); err != nil || resolved != localConfigPath {
		t.Fatalf("ResolveConfigFilePath() = (%q, %v), want (%q, nil)", resolved, err, localConfigPath)
	}

	// Snapshot the local file right after Load, not right after writing
	// localBody: Load itself may rewrite it for unrelated reasons (e.g.
	// ensureEvaluatorDefaultModel persisting a first-seen default model), so
	// the baseline for "UpdateTelemetry must not touch the local file" is
	// whatever Load already left behind, not the original hand-written body.
	beforeUpdate, err := os.ReadFile(localConfigPath)
	if err != nil {
		t.Fatalf("read local config after Load: %v", err)
	}

	debugID, err := UpdateTelemetry(true)
	if err != nil {
		t.Fatalf("UpdateTelemetry(true): %v", err)
	}
	if len(debugID) != 16 {
		t.Fatalf("debugID = %q, want 16 digits", debugID)
	}
	if !cfg.Telemetry.Enabled || cfg.Telemetry.DebugID != debugID {
		t.Fatalf("in-memory telemetry = %+v, want enabled with debugID %q", cfg.Telemetry, debugID)
	}

	// The project-local file must be byte-identical to its post-Load state:
	// UpdateTelemetry must never touch it, only updateGlobalCfgFile's target.
	after, err := os.ReadFile(localConfigPath)
	if err != nil {
		t.Fatalf("read local config: %v", err)
	}
	if string(after) != string(beforeUpdate) {
		t.Fatalf("local project config changed by UpdateTelemetry:\n--- before ---\n%s--- after ---\n%s", beforeUpdate, after)
	}

	// The GLOBAL file (home dir default, since none existed before) must now
	// hold the change.
	globalConfigPath := filepath.Join(homeDir, ".pando.json")
	data, err := os.ReadFile(globalConfigPath)
	if err != nil {
		t.Fatalf("read global config %s: %v", globalConfigPath, err)
	}
	var onDisk Config
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal global config: %v", err)
	}
	if !onDisk.Telemetry.Enabled || onDisk.Telemetry.DebugID != debugID {
		t.Fatalf("global config on disk telemetry = %+v, want enabled with debugID %q", onDisk.Telemetry, debugID)
	}
}

// TestLoadHonorsOverlayLockedTelemetryEnabled verifies the code-review fix:
// an overlay provider that LOCKS telemetry.enabled (an Enterprise/host
// policy forcing diagnostics on) must have that value survive the
// global-only re-pin in Load — which otherwise unconditionally discards
// anything applyOverlayProviders merged into telemetry.*, the same way it
// discards a project-local [Telemetry] section.
func TestLoadHonorsOverlayLockedTelemetryEnabled(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)

	dir := t.TempDir()
	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		return Overlay{
			Source: "test",
			Values: map[string]any{"telemetry": map[string]any{"enabled": true}},
			Locked: []string{"telemetry.enabled"},
		}, nil
	}))

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !loaded.Telemetry.Enabled {
		t.Fatal("telemetry.enabled = false, want the overlay-locked value (true) to survive the global-only re-pin")
	}
	if !IsKeyLocked("telemetry.enabled") {
		t.Fatal("IsKeyLocked(telemetry.enabled) = false, want true")
	}
}

// TestLoadIgnoresUnlockedOverlayTelemetryEnabled is the flip side: an
// overlay that SETS telemetry.enabled without LOCKING it is still just a
// default a user could change, not an enforced policy — Load must keep
// pinning telemetry to the global-only snapshot in that case, same as
// before this fix.
func TestLoadIgnoresUnlockedOverlayTelemetryEnabled(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)

	dir := t.TempDir()
	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		return Overlay{
			Source: "test",
			Values: map[string]any{"telemetry": map[string]any{"enabled": true}},
			// No Locked entry for telemetry.enabled.
		}, nil
	}))

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Telemetry.Enabled {
		t.Fatal("telemetry.enabled = true, want the global-only snapshot (false) since the overlay did not lock it")
	}
}

// TestUpdateCfgFileNeverWritesTelemetryToLocalFile verifies the code-review
// fix: json:"telemetry,omitempty" is a no-op on a non-pointer struct field,
// so without an explicit write-time guard, EVERY write through updateCfgFile
// to an active project-local config (changing the theme, here) would stamp
// a "telemetry" block into that file. It must never appear there at all —
// not even empty — while the GLOBAL file (where telemetry was actually
// enabled) must be left with its state intact.
func TestUpdateCfgFileNeverWritesTelemetryToLocalFile(t *testing.T) {
	isolateGlobalConfig(t)
	homeDir := os.Getenv("HOME")

	projectDir := t.TempDir()
	localConfigPath := filepath.Join(projectDir, ".pando.toml")
	if err := os.WriteFile(localConfigPath, []byte("[Mesnada]\nEnabled = true\n"), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })
	if _, err := Load(projectDir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	debugID, err := UpdateTelemetry(true)
	if err != nil {
		t.Fatalf("UpdateTelemetry(true): %v", err)
	}

	// A write through the ordinary (local-preferring) path, to a project
	// with an active local file, must never touch telemetry at all.
	if err := UpdateTheme("pando-dark"); err != nil {
		t.Fatalf("UpdateTheme: %v", err)
	}

	localData, err := os.ReadFile(localConfigPath)
	if err != nil {
		t.Fatalf("read local config: %v", err)
	}
	if strings.Contains(strings.ToLower(string(localData)), "telemetry") {
		t.Fatalf("project-local config file must never contain a telemetry section, got:\n%s", localData)
	}

	// The GLOBAL file must still carry the real telemetry state, untouched
	// by the local-file write above.
	globalData, err := os.ReadFile(filepath.Join(homeDir, ".pando.json"))
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	var onDisk Config
	if err := json.Unmarshal(globalData, &onDisk); err != nil {
		t.Fatalf("unmarshal global config: %v", err)
	}
	if !onDisk.Telemetry.Enabled || onDisk.Telemetry.DebugID != debugID {
		t.Fatalf("global config telemetry = %+v, want enabled with debugID %q", onDisk.Telemetry, debugID)
	}
}

// TestUpdateCfgFileKeepsTelemetryWhenItResolvesToGlobalFile verifies the
// flip side of the guard above: when updateCfgFile's local-preferring
// resolver falls back to the GLOBAL file (no project-local config active,
// the common single-profile case), an ordinary write must NOT strip
// telemetry from it.
func TestUpdateCfgFileKeepsTelemetryWhenItResolvesToGlobalFile(t *testing.T) {
	isolateGlobalConfig(t)
	homeDir := os.Getenv("HOME")

	tmpDir := t.TempDir() // no project-local config file here
	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })
	if _, err := Load(tmpDir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	debugID, err := UpdateTelemetry(true)
	if err != nil {
		t.Fatalf("UpdateTelemetry(true): %v", err)
	}

	if err := UpdateTheme("pando-dark"); err != nil {
		t.Fatalf("UpdateTheme: %v", err)
	}

	globalData, err := os.ReadFile(filepath.Join(homeDir, ".pando.json"))
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	var onDisk Config
	if err := json.Unmarshal(globalData, &onDisk); err != nil {
		t.Fatalf("unmarshal global config: %v", err)
	}
	if !onDisk.Telemetry.Enabled || onDisk.Telemetry.DebugID != debugID {
		t.Fatalf("global config telemetry = %+v, want still enabled with debugID %q after an unrelated theme update", onDisk.Telemetry, debugID)
	}
}

// TestTelemetryDebugIDStableAcrossDisableEnable verifies the id is kept on
// disable and reused on the next enable, rather than regenerated.
func TestTelemetryDebugIDStableAcrossDisableEnable(t *testing.T) {
	isolateGlobalConfig(t)
	tmpDir := t.TempDir()
	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })
	if _, err := Load(tmpDir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	id1, err := UpdateTelemetry(true)
	if err != nil {
		t.Fatalf("UpdateTelemetry(true): %v", err)
	}
	if len(id1) != 16 {
		t.Fatalf("id1 = %q, want 16 digits", id1)
	}

	if _, err := UpdateTelemetry(false); err != nil {
		t.Fatalf("UpdateTelemetry(false): %v", err)
	}
	if cfg.Telemetry.DebugID != id1 {
		t.Fatalf("debug id changed after disable: got %q, want %q", cfg.Telemetry.DebugID, id1)
	}

	id2, err := UpdateTelemetry(true)
	if err != nil {
		t.Fatalf("UpdateTelemetry(true) again: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("re-enabling generated a new id: %q != %q", id2, id1)
	}
}

// TestRegenerateTelemetryIDChangesID verifies RegenerateTelemetryID produces
// and persists a different id without touching the Enabled flag.
func TestRegenerateTelemetryIDChangesID(t *testing.T) {
	isolateGlobalConfig(t)
	tmpDir := t.TempDir()
	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })
	if _, err := Load(tmpDir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	id1, err := UpdateTelemetry(true)
	if err != nil {
		t.Fatalf("UpdateTelemetry(true): %v", err)
	}

	id2, err := RegenerateTelemetryID()
	if err != nil {
		t.Fatalf("RegenerateTelemetryID: %v", err)
	}
	if id2 == id1 {
		t.Fatalf("RegenerateTelemetryID did not change the id: %q", id2)
	}
	if cfg.Telemetry.DebugID != id2 {
		t.Fatalf("in-memory debug id = %q, want %q", cfg.Telemetry.DebugID, id2)
	}
	if !cfg.Telemetry.Enabled {
		t.Fatal("RegenerateTelemetryID must not disable telemetry")
	}

	// Reload from disk (still isolated) and confirm persistence.
	viper.Reset()
	cfg = nil
	reloaded, err := Load(tmpDir, false)
	if err != nil {
		t.Fatalf("reload Load: %v", err)
	}
	if reloaded.Telemetry.DebugID != id2 {
		t.Fatalf("debug id on disk = %q, want %q", reloaded.Telemetry.DebugID, id2)
	}
}

// TestProjectLocalConfigCannotEnableTelemetry verifies that a [Telemetry]
// section in a project-local config is entirely ignored: Load must pin
// cfg.Telemetry to the (empty/default) global value.
func TestProjectLocalConfigCannotEnableTelemetry(t *testing.T) {
	loadTempConfig(t, "[Telemetry]\nEnabled = true\nDebugID = \"1234567890123456\"\nMinLevel = \"debug\"\n")

	if cfg.Telemetry.Enabled {
		t.Fatal("a project-local [Telemetry] section must not enable telemetry")
	}
	if cfg.Telemetry.DebugID != "" {
		t.Fatalf("a project-local [Telemetry] section must not set a debug id, got %q", cfg.Telemetry.DebugID)
	}
	if cfg.Telemetry.MinLevel != TelemetryLevelInfo {
		t.Fatalf("a project-local [Telemetry] section must not set the level, got %q", cfg.Telemetry.MinLevel)
	}
}

// TestUpdateTelemetryMinLevelValidatesAndPersists exercises both the valid
// (case-insensitive) path and the validation-rejects-and-rolls-back path.
func TestUpdateTelemetryMinLevelValidatesAndPersists(t *testing.T) {
	isolateGlobalConfig(t)
	tmpDir := t.TempDir()
	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })
	if _, err := Load(tmpDir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := UpdateTelemetryMinLevel("WARN"); err != nil {
		t.Fatalf("UpdateTelemetryMinLevel(WARN): %v", err)
	}
	if cfg.Telemetry.MinLevel != TelemetryLevelWarn {
		t.Fatalf("MinLevel = %q, want %q", cfg.Telemetry.MinLevel, TelemetryLevelWarn)
	}

	if err := UpdateTelemetryMinLevel("verbose"); err == nil {
		t.Fatal("UpdateTelemetryMinLevel(verbose) should have failed validation")
	}
	if err := UpdateTelemetryMinLevel(""); err == nil {
		t.Fatal("UpdateTelemetryMinLevel(\"\") should have failed validation")
	}
	// A failed update must not change the in-memory value.
	if cfg.Telemetry.MinLevel != TelemetryLevelWarn {
		t.Fatalf("MinLevel after failed update = %q, want unchanged %q", cfg.Telemetry.MinLevel, TelemetryLevelWarn)
	}
}

// TestValidateTelemetryConfig is a direct unit test of the validation rules,
// independent of Load.
func TestValidateTelemetryConfig(t *testing.T) {
	cases := []struct {
		name    string
		cfg     TelemetryConfig
		wantErr bool
	}{
		{"valid empty id, info level", TelemetryConfig{MinLevel: TelemetryLevelInfo}, false},
		{"valid 16-digit id", TelemetryConfig{DebugID: "1234567890123456", MinLevel: TelemetryLevelDebug}, false},
		{"invalid id length", TelemetryConfig{DebugID: "12345", MinLevel: TelemetryLevelInfo}, true},
		{"invalid id chars", TelemetryConfig{DebugID: "abcd567890123456", MinLevel: TelemetryLevelInfo}, true},
		{"invalid level", TelemetryConfig{MinLevel: "verbose"}, true},
		{"empty level is tolerated (means not yet normalized)", TelemetryConfig{MinLevel: ""}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateTelemetryConfig(c.cfg)
			if (err != nil) != c.wantErr {
				t.Fatalf("validateTelemetryConfig(%+v) error = %v, wantErr %v", c.cfg, err, c.wantErr)
			}
		})
	}
}

// TestLoadNormalizesInvalidGlobalMinLevel verifies the code-review fix:
// Load must NEVER fail because of a malformed telemetry.* value in the
// GLOBAL config file — a bad minLevel is silently defaulted to "info"
// instead (previously, Validate() surfaced this as a hard Load error,
// which meant a corrupted telemetry section stopped Pando from starting).
func TestLoadNormalizesInvalidGlobalMinLevel(t *testing.T) {
	isolateGlobalConfig(t)
	homeDir := os.Getenv("HOME")
	globalCfg := `{"telemetry": {"minLevel": "verbose"}}`
	if err := os.WriteFile(filepath.Join(homeDir, ".pando.json"), []byte(globalCfg), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })

	tmpDir := t.TempDir()
	loaded, err := Load(tmpDir, false)
	if err != nil {
		t.Fatalf("Load() must succeed despite an invalid GLOBAL telemetry.minLevel, got: %v", err)
	}
	if loaded.Telemetry.MinLevel != TelemetryLevelInfo {
		t.Fatalf("MinLevel = %q, want normalized default %q", loaded.Telemetry.MinLevel, TelemetryLevelInfo)
	}
}

// TestLoadNormalizesInvalidGlobalDebugID verifies the same never-fail rule
// for a malformed debug id: too short/wrong characters is cleared (not
// rejected), and a dashed value (as if a user pasted the display format
// back into the file by hand) is normalized to its raw 16-digit form
// instead of being treated as invalid.
func TestLoadNormalizesInvalidGlobalDebugID(t *testing.T) {
	isolateGlobalConfig(t)
	homeDir := os.Getenv("HOME")
	globalCfg := `{"telemetry": {"debugId": "not-a-valid-id"}}`
	if err := os.WriteFile(filepath.Join(homeDir, ".pando.json"), []byte(globalCfg), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })

	tmpDir := t.TempDir()
	loaded, err := Load(tmpDir, false)
	if err != nil {
		t.Fatalf("Load() must succeed despite an invalid GLOBAL telemetry.debugId, got: %v", err)
	}
	if loaded.Telemetry.DebugID != "" {
		t.Fatalf("DebugID = %q, want cleared (not exactly 16 digits after stripping dashes/spaces)", loaded.Telemetry.DebugID)
	}
}

// TestLoadNormalizesDashedGlobalDebugID verifies a debug id stored in its
// dashed DISPLAY form (e.g. hand-edited back from what the UI shows) is
// repaired to the raw storage form rather than cleared.
func TestLoadNormalizesDashedGlobalDebugID(t *testing.T) {
	isolateGlobalConfig(t)
	homeDir := os.Getenv("HOME")
	globalCfg := `{"telemetry": {"debugId": "1234-5678-9012-3456"}}`
	if err := os.WriteFile(filepath.Join(homeDir, ".pando.json"), []byte(globalCfg), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })

	tmpDir := t.TempDir()
	loaded, err := Load(tmpDir, false)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if loaded.Telemetry.DebugID != "1234567890123456" {
		t.Fatalf("DebugID = %q, want dashes/spaces stripped to %q", loaded.Telemetry.DebugID, "1234567890123456")
	}
}

// TestLoadGeneratesInMemoryDebugIDWhenEnabledWithoutOne verifies that a
// config file with telemetry enabled but no (or an unrecoverably malformed)
// debug id gets a freshly generated one in memory, rather than shipping
// records with an empty debug_id or failing Load.
func TestLoadGeneratesInMemoryDebugIDWhenEnabledWithoutOne(t *testing.T) {
	isolateGlobalConfig(t)
	homeDir := os.Getenv("HOME")
	globalCfg := `{"telemetry": {"enabled": true}}`
	if err := os.WriteFile(filepath.Join(homeDir, ".pando.json"), []byte(globalCfg), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })

	tmpDir := t.TempDir()
	loaded, err := Load(tmpDir, false)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if !loaded.Telemetry.Enabled {
		t.Fatal("telemetry.enabled should stay true")
	}
	if len(loaded.Telemetry.DebugID) != 16 {
		t.Fatalf("DebugID = %q, want a freshly generated 16-digit id", loaded.Telemetry.DebugID)
	}
}

// TestLoadIgnoresInvalidLocalMinLevel verifies the flip side: a bad value in
// a project-local [Telemetry] section must never be able to break Load,
// because the global-only snapshot replaces it before Validate runs.
func TestLoadIgnoresInvalidLocalMinLevel(t *testing.T) {
	loadTempConfig(t, "[Telemetry]\nMinLevel = \"verbose\"\n")
	if cfg.Telemetry.MinLevel != TelemetryLevelInfo {
		t.Fatalf("MinLevel = %q, want default %q", cfg.Telemetry.MinLevel, TelemetryLevelInfo)
	}
}
