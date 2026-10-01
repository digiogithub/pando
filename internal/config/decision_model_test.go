package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDecisionModelEffectiveTimeout(t *testing.T) {
	d := DecisionModelConfig{}
	if d.EffectiveTimeout() != 1500*time.Millisecond {
		t.Fatalf("ollama timeout = %v", d.EffectiveTimeout())
	}
	d.Router.Provider = DecisionProviderTypeSafe
	if d.EffectiveTimeout() != 3*time.Second {
		t.Fatalf("remote timeout = %v", d.EffectiveTimeout())
	}
	d.TimeoutMs = 700
	if d.EffectiveTimeout() != 700*time.Millisecond {
		t.Fatalf("explicit timeout = %v", d.EffectiveTimeout())
	}
}

func TestDecisionModelDefaultsReachLoadedConfig(t *testing.T) {
	isolateGlobalConfig(t)
	if _, err := Load(t.TempDir(), false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	d := Get().DecisionModel
	if d.Router.Provider != DecisionProviderOllama || d.Router.KeepAlive != "30m" || d.TimeoutMs != 0 {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	if Get().ModelAutoMode.LegacyRouter != nil {
		t.Fatal("a viper default must not make the legacy router look present")
	}
}

func TestValidateDecisionModel(t *testing.T) {
	isolateGlobalConfig(t)
	t.Setenv("TYPESAFE_API_KEY", "")

	if errs, _ := ValidateDecisionModel(validDecision()); len(errs) != 0 {
		t.Fatalf("valid config rejected: %v", errs)
	}
	if errs, _ := ValidateDecisionModel(DecisionModelConfig{}); len(errs) != 0 {
		t.Fatalf("empty model must be valid here: %v", errs)
	}

	d := validDecision()
	d.Router.Provider = "gemini"
	errs, _ := ValidateDecisionModel(d)
	if !hasField(errs, "router.provider") || !strings.Contains(errs[0].Message, "ollama, typesafe, custom") {
		t.Fatalf("unknown provider errs = %v", errs)
	}

	d = validDecision()
	d.Router.Provider = DecisionProviderCustom
	if errs, _ = ValidateDecisionModel(d); !hasField(errs, "router.baseURL") {
		t.Fatalf("custom without baseURL errs = %v", errs)
	}
	d.Router.BaseURL = "ftp://x"
	if errs, _ = ValidateDecisionModel(d); !hasField(errs, "router.baseURL") {
		t.Fatalf("non-http baseURL errs = %v", errs)
	}
	d.Router.BaseURL = "https://gw.example.com"
	if errs, _ = ValidateDecisionModel(d); len(errs) != 0 {
		t.Fatalf("valid custom rejected: %v", errs)
	}

	d = validDecision()
	d.Router.Model = "qwen3.5:cloud"
	errs, _ = ValidateDecisionModel(d)
	if !hasField(errs, "router.model") || !strings.Contains(errs[0].Message, "local") {
		t.Fatalf("cloud model errs = %v", errs)
	}

	d = validDecision()
	d.TimeoutMs = -1
	if errs, _ = ValidateDecisionModel(d); !hasField(errs, "timeoutMs") {
		t.Fatalf("negative timeout errs = %v", errs)
	}

	d = DecisionModelConfig{Router: DecisionRouterConfig{Provider: DecisionProviderTypeSafe, Model: "jev-latest"}}
	errs, warns := ValidateDecisionModel(d)
	if len(errs) != 0 || len(warns) == 0 {
		t.Fatalf("typesafe without key: errs=%v warns=%v (want warning only)", errs, warns)
	}
	t.Setenv("TYPESAFE_API_KEY", "abc")
	if _, warns = ValidateDecisionModel(d); len(warns) != 0 {
		t.Fatalf("env fallback must silence warning: %v", warns)
	}
	if got := d.Router.EffectiveAPIKey(); got != "abc" {
		t.Fatalf("env fallback key = %q", got)
	}
}

func TestNormalizeDecisionModel(t *testing.T) {
	d := normalizeDecisionModel(DecisionModelConfig{Router: DecisionRouterConfig{
		Provider: " Ollama ", Model: "  tev1:0.8b ", BaseURL: " http://x ", Headers: map[string]string{},
	}})
	if d.Router.Provider != DecisionProviderOllama || d.Router.Model != "tev1:0.8b" ||
		d.Router.BaseURL != "http://x" || d.Router.KeepAlive != "30m" || d.Router.Headers != nil {
		t.Fatalf("normalised = %+v", d.Router)
	}
	d = normalizeDecisionModel(DecisionModelConfig{Router: DecisionRouterConfig{Provider: DecisionProviderTypeSafe}})
	if d.Router.KeepAlive != "" {
		t.Fatalf("keepAlive default is ollama only: %+v", d.Router)
	}
}

func TestDecisionModelAPIKeyEncryptionKeepAndClear(t *testing.T) {
	isolateGlobalConfig(t)
	dir := t.TempDir()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ch := make(chan ConfigChangeEvent, 8)
	Bus.Subscribe(ch)
	defer Bus.Unsubscribe(ch)

	d := validDecision()
	d.Router.APIKey = "sk-test-123"
	if err := UpdateDecisionModel(d); err != nil {
		t.Fatalf("UpdateDecisionModel: %v", err)
	}
	path, err := ResolveConfigFilePath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-test-123") || !strings.Contains(string(raw), encryptedValuePrefix) {
		t.Fatalf("api key not encrypted on disk:\n%s", raw)
	}
	select {
	case ev := <-ch:
		if ev.Section != "decisionModel" {
			t.Fatalf("event section = %q", ev.Section)
		}
	case <-time.After(time.Second):
		t.Fatal("no ConfigChangeEvent published")
	}

	ResetForTests()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := Get().DecisionModel.Router.EffectiveAPIKey(); got != "sk-test-123" {
		t.Fatalf("reloaded key = %q", got)
	}

	// Empty incoming key keeps the stored one.
	d2 := validDecision()
	d2.TimeoutMs = 900
	if err := UpdateDecisionModel(d2); err != nil {
		t.Fatal(err)
	}
	if got := Get().DecisionModel.Router.APIKey; got != "sk-test-123" {
		t.Fatalf("key not kept: %q", got)
	}
	raw, _ = os.ReadFile(path)
	if !strings.Contains(string(raw), encryptedValuePrefix) {
		t.Fatal("stored key lost from disk")
	}

	if err := ClearDecisionModelAPIKey(); err != nil || Get().DecisionModel.Router.APIKey != "" {
		t.Fatalf("clear key: err=%v key=%q", err, Get().DecisionModel.Router.APIKey)
	}
	// The deprecated alias delegates.
	d3 := validDecision()
	d3.Router.APIKey = "again"
	if err := UpdateDecisionModel(d3); err != nil {
		t.Fatal(err)
	}
	if err := ClearModelAutoModeAPIKey(); err != nil || Get().DecisionModel.Router.APIKey != "" {
		t.Fatalf("alias clear: err=%v", err)
	}

	t.Setenv("MY_ROUTER_KEY", "from-env")
	if got := (DecisionRouterConfig{APIKey: "$MY_ROUTER_KEY"}).EffectiveAPIKey(); got != "from-env" {
		t.Fatalf("env key = %q", got)
	}
	if got := MaskAPIKey("sk-test-1234"); got != "••••1234" {
		t.Fatalf("mask = %q", got)
	}
	if MaskAPIKey("") != "" || MaskAPIKey("abc") != "••••" {
		t.Fatal("short/empty mask wrong")
	}
}

func TestUpdateDecisionModelRejectsInvalid(t *testing.T) {
	isolateGlobalConfig(t)
	if _, err := Load(t.TempDir(), false); err != nil {
		t.Fatal(err)
	}
	before := Get().DecisionModel
	bad := validDecision()
	bad.Router.Provider = DecisionProviderCustom
	if err := UpdateDecisionModel(bad); err == nil || !strings.Contains(err.Error(), "router.baseURL") {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(Get().DecisionModel, before) {
		t.Fatal("invalid update changed the config")
	}
}

func TestUpdateDecisionModelLocked(t *testing.T) {
	resetOverlayState(t)
	isolateGlobalConfig(t)
	if _, err := Load(t.TempDir(), false); err != nil {
		t.Fatal(err)
	}
	before := Get().DecisionModel
	overlayMu.Lock()
	lockedKeys = []string{"decisionModel"}
	overlayMu.Unlock()
	if err := UpdateDecisionModel(validDecision()); !errors.Is(err, ErrKeyLocked) {
		t.Fatalf("err = %v, want a lock error", err)
	}
	if !reflect.DeepEqual(Get().DecisionModel, before) {
		t.Fatal("config changed despite the lock")
	}
}

// writeHomeConfig writes the global config file the next Load will read.
func writeHomeConfig(t *testing.T, content string) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".pando.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const legacyRouterTOML = `
[ModelAutoMode]
Enabled = true
TimeoutMs = 2500

[ModelAutoMode.Router]
Provider = 'custom'
BaseURL = 'https://gw.example.com'
Model = 'jev-latest'
APIKey = 'sk-legacy'
`

func TestMigrateLegacyRouterPersists(t *testing.T) {
	isolateGlobalConfig(t)
	path := writeHomeConfig(t, legacyRouterTOML)
	if _, err := Load(t.TempDir(), false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	d := Get().DecisionModel
	if d.Router.Model != "jev-latest" || d.Router.Provider != DecisionProviderCustom ||
		d.Router.EffectiveAPIKey() != "sk-legacy" || d.TimeoutMs != 2500 {
		t.Fatalf("not migrated in memory: %+v", d)
	}
	if m := Get().ModelAutoMode; m.LegacyRouter != nil || m.LegacyTimeoutMs != 0 || !m.Enabled {
		t.Fatalf("legacy fields not cleared: %+v", m)
	}
	raw, _ := os.ReadFile(path)
	text := string(raw)
	if !strings.Contains(text, "[DecisionModel.Router]") || strings.Contains(text, "[ModelAutoMode.Router]") {
		t.Fatalf("file not migrated:\n%s", text)
	}
	if strings.Contains(text, "sk-legacy") || !strings.Contains(text, encryptedValuePrefix) {
		t.Fatalf("key must be persisted encrypted:\n%s", text)
	}

	// Idempotent: a second load changes nothing and rewrites nothing.
	ResetForTests()
	if _, err := Load(t.TempDir(), false); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := Get().DecisionModel; !reflect.DeepEqual(got, d) {
		t.Fatalf("second load changed decisionModel: %+v vs %+v", got, d)
	}
	raw2, _ := os.ReadFile(path)
	if string(raw2) != text {
		t.Fatal("idempotent load rewrote the file")
	}
}

func TestMigrateAlreadyMigratedUntouched(t *testing.T) {
	isolateGlobalConfig(t)
	content := "[DecisionModel]\nTimeoutMs = 100\n\n[DecisionModel.Router]\nProvider = 'ollama'\nModel = 'tev1:0.8b'\nKeepAlive = '30m'\n"
	path := writeHomeConfig(t, content)
	if _, err := Load(t.TempDir(), false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := Get().DecisionModel.Router.Model; got != "tev1:0.8b" {
		t.Fatalf("model = %q", got)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "[ModelAutoMode.Router]") || !strings.Contains(string(raw), "Model = 'tev1:0.8b'") {
		t.Fatalf("file content changed unexpectedly:\n%s", raw)
	}
}

func TestMigrateBothPresentDecisionModelWins(t *testing.T) {
	isolateGlobalConfig(t)
	writeHomeConfig(t, legacyRouterTOML+"\n[DecisionModel.Router]\nProvider = 'ollama'\nModel = 'tev1:0.8b'\n")
	if _, err := Load(t.TempDir(), false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	d := Get().DecisionModel
	if d.Router.Model != "tev1:0.8b" || d.Router.Provider != DecisionProviderOllama || d.TimeoutMs != 0 {
		t.Fatalf("decisionModel must win: %+v", d)
	}
	if Get().ModelAutoMode.LegacyRouter != nil {
		t.Fatal("legacy router must be dropped from memory")
	}
}

func TestMigrateLockedStaysInMemory(t *testing.T) {
	resetOverlayState(t)
	isolateGlobalConfig(t)
	if _, err := Load(t.TempDir(), false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Re-create the pre-migration state, then lock the destination.
	path := writeHomeConfig(t, legacyRouterTOML)
	cfg.DecisionModel = DecisionModelConfig{}
	cfg.ModelAutoMode.LegacyRouter = &DecisionRouterConfig{Provider: DecisionProviderCustom, BaseURL: "https://gw.example.com", Model: "jev-latest"}
	overlayMu.Lock()
	lockedKeys = []string{"decisionModel"}
	overlayMu.Unlock()

	migrateLegacyDecisionModel()

	if got := Get().DecisionModel.Router.Model; got != "jev-latest" {
		t.Fatalf("not migrated in memory: %q", got)
	}
	if Get().ModelAutoMode.LegacyRouter != nil {
		t.Fatal("legacy router must be cleared in memory")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != legacyRouterTOML {
		t.Fatalf("locked file must not be rewritten:\n%s", raw)
	}
}

func TestAnyDecisionConsumerEnabled(t *testing.T) {
	var nilCfg *Config
	if nilCfg.AnyDecisionConsumerEnabled() {
		t.Fatal("nil config has no consumer")
	}
	c := &Config{}
	if c.AnyDecisionConsumerEnabled() {
		t.Fatal("empty config has no consumer")
	}
	for name, set := range map[string]func(*Config){
		"auto mode":      func(c *Config) { c.ModelAutoMode.Enabled = true },
		"persona":        func(c *Config) { c.Agents = map[AgentName]Agent{AgentPersonaSelector: {UseDecisionModel: true}} },
		"context filter": func(c *Config) { c.Remembrances.ContextEnrichmentDecisionFilterEnabled = true },
		"memory filter":  func(c *Config) { c.Remembrances.MemoryContextDecisionFilterEnabled = true },
	} {
		c := &Config{}
		set(c)
		if !c.AnyDecisionConsumerEnabled() {
			t.Errorf("%s: want enabled", name)
		}
	}
}
