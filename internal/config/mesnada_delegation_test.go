package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// loadTempConfig loads a fresh config rooted at a temp dir containing the given
// config file body, and returns the config file path. It resets the package
// global config and viper, restoring them on cleanup.
func loadTempConfig(t *testing.T, body string) string {
	t.Helper()
	isolateGlobalConfig(t)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".pando.toml")
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })
	if _, err := Load(tmpDir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return configPath
}

// TestUpdateMesnadaDelegationPersist verifies the new delegation config is
// written to disk and reflected on the in-memory config.
func TestUpdateMesnadaDelegationPersist(t *testing.T) {
	configPath := loadTempConfig(t, "[Mesnada]\nEnabled = true\n")

	newCfg := MesnadaDelegationConfig{
		Enabled:             true,
		InjectIntoLiveLoop:  true,
		ResurrectIdleLoop:   true,
		SynthesizeFallback:  true,
		MaxResurrections:    7,
		MaxDepth:            5,
		MaxConcurrent:       9,
		ResurrectionTimeout: "15m",
	}
	if err := UpdateMesnadaDelegation(newCfg); err != nil {
		t.Fatalf("UpdateMesnadaDelegation: %v", err)
	}

	if cfg.Mesnada.Delegation != newCfg {
		t.Fatalf("in-memory delegation = %+v, want %+v", cfg.Mesnada.Delegation, newCfg)
	}

	// Reload from disk and confirm persistence.
	viper.Reset()
	cfg = nil
	if _, err := Load(filepath.Dir(configPath), false); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Mesnada.Delegation.MaxResurrections != 7 || !cfg.Mesnada.Delegation.Enabled || cfg.Mesnada.Delegation.ResurrectionTimeout != "15m" {
		t.Fatalf("persisted delegation not reloaded: %+v", cfg.Mesnada.Delegation)
	}
}

// TestUpdateMesnadaDelegationRollback verifies that when persistence fails the
// in-memory value is rolled back to its previous state.
func TestUpdateMesnadaDelegationRollback(t *testing.T) {
	configPath := loadTempConfig(t, "[Mesnada.Delegation]\nMaxDepth = 3\n")

	before := cfg.Mesnada.Delegation

	// Corrupt the on-disk config so updateCfgFile's parse step fails, forcing the
	// rollback path without writing anything.
	if err := os.WriteFile(configPath, []byte("this = = invalid toml ]["), 0o644); err != nil {
		t.Fatalf("corrupt config: %v", err)
	}

	err := UpdateMesnadaDelegation(MesnadaDelegationConfig{Enabled: true, MaxDepth: 99})
	if err == nil {
		t.Fatalf("expected error from corrupted config, got nil")
	}
	if cfg.Mesnada.Delegation != before {
		t.Fatalf("delegation not rolled back: got %+v, want %+v", cfg.Mesnada.Delegation, before)
	}
}

// TestMesnadaDelegationDefaults verifies the documented defaults are applied
// when the user does not set the fields (default-OFF, caps populated).
func TestMesnadaDelegationDefaults(t *testing.T) {
	loadTempConfig(t, "[Mesnada]\nEnabled = true\n")

	d := cfg.Mesnada.Delegation
	if d.Enabled {
		t.Errorf("delegation enabled by default, want false")
	}
	if d.MaxResurrections != 4 {
		t.Errorf("MaxResurrections = %d, want 4", d.MaxResurrections)
	}
	if d.MaxDepth != 3 {
		t.Errorf("MaxDepth = %d, want 3", d.MaxDepth)
	}
	if d.MaxConcurrent != 8 {
		t.Errorf("MaxConcurrent = %d, want 8", d.MaxConcurrent)
	}
	if d.ResurrectionTimeout != "10m" {
		t.Errorf("ResurrectionTimeout = %q, want 10m", d.ResurrectionTimeout)
	}
}

// TestMesnadaDelegationUserValuesWin verifies user-provided values are not
// clobbered by defaults.
func TestMesnadaDelegationUserValuesWin(t *testing.T) {
	loadTempConfig(t, "[Mesnada.Delegation]\nEnabled = true\nMaxDepth = 11\nResurrectionTimeout = \"30m\"\n")

	d := cfg.Mesnada.Delegation
	if !d.Enabled {
		t.Errorf("user Enabled=true lost")
	}
	if d.MaxDepth != 11 {
		t.Errorf("MaxDepth = %d, want user value 11", d.MaxDepth)
	}
	if d.ResurrectionTimeout != "30m" {
		t.Errorf("ResurrectionTimeout = %q, want user value 30m", d.ResurrectionTimeout)
	}
}

// TestMesnadaDelegationWarmDefaultsUnderShadowing guards the A1 fix: with an
// unrelated [Mesnada] sibling key present (which triggers viper's nested-default
// shadowing) the warm-routing defaults must still hold — AutoStartWarmInstance
// true and the caps populated — even though the user provided no
// [Mesnada.Delegation] table.
func TestMesnadaDelegationWarmDefaultsUnderShadowing(t *testing.T) {
	loadTempConfig(t, "[Mesnada]\nEnabled = true\n")

	d := cfg.Mesnada.Delegation
	if d.ReuseWarmInstances {
		t.Errorf("ReuseWarmInstances = true, want false (default OFF)")
	}
	if !d.AutoStartWarmInstance {
		t.Errorf("AutoStartWarmInstance = false, want true (default under shadowing)")
	}
	if d.MaxConcurrent != 8 {
		t.Errorf("MaxConcurrent = %d, want 8", d.MaxConcurrent)
	}
}

// TestMesnadaDelegationAutoStartFalseWins verifies a user-set
// AutoStartWarmInstance=false is preserved and not clobbered back to the true
// default by the normalize fallback.
func TestMesnadaDelegationAutoStartFalseWins(t *testing.T) {
	loadTempConfig(t, "[Mesnada.Delegation]\nReuseWarmInstances = true\nAutoStartWarmInstance = false\n")

	d := cfg.Mesnada.Delegation
	if !d.ReuseWarmInstances {
		t.Errorf("ReuseWarmInstances user value true lost")
	}
	if d.AutoStartWarmInstance {
		t.Errorf("AutoStartWarmInstance = true, want user value false")
	}
	// Caps the user did not list still fall back to documented defaults.
	if d.MaxConcurrent != 8 {
		t.Errorf("MaxConcurrent = %d, want default 8", d.MaxConcurrent)
	}
	if d.MaxDepth != 3 {
		t.Errorf("MaxDepth = %d, want default 3", d.MaxDepth)
	}
}

// TestParseEnvBool covers the permissive boolean parser used for env overrides.
func TestParseEnvBool(t *testing.T) {
	cases := map[string]bool{
		"1": true, "true": true, "YES": true, "on": true, " True ": true,
		"0": false, "false": false, "no": false, "off": false, "": false, "garbage": false,
	}
	for in, want := range cases {
		if got := parseEnvBool(in); got != want {
			t.Errorf("parseEnvBool(%q) = %v, want %v", in, got, want)
		}
	}
}
