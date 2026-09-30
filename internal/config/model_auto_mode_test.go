package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/models"
)

func validAutoMode() ModelAutoModeConfig {
	return ModelAutoModeConfig{
		Enabled:   true,
		Router:    DecisionRouterConfig{Provider: DecisionProviderOllama, Model: "tev1:0.8b"},
		Threshold: 0.6,
		Routes: []ModelAutoRoute{
			{ID: "code", Description: "coding tasks", Model: models.Claude35Haiku},
		},
	}
}

func hasField(errs []FieldError, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}

func TestModelAutoModeProviderDefaults(t *testing.T) {
	isolateGlobalConfig(t)
	t.Setenv("OLLAMA_BASE_URL", "")
	t.Setenv("TYPESAFE_API_KEY", "")

	// Ollama provider base URL is reused (without /v1).
	cfg = &Config{Providers: map[models.ModelProvider]Provider{
		models.ProviderOllama: {BaseURL: "http://gpu-box:11434/v1"},
	}}
	r := DecisionRouterConfig{}
	if r.EffectiveProvider() != DecisionProviderOllama {
		t.Fatalf("default provider = %q", r.EffectiveProvider())
	}
	if got := r.EffectiveBaseURL(); got != "http://gpu-box:11434" {
		t.Fatalf("ollama base = %q", got)
	}
	cfg = nil
	if got := r.EffectiveBaseURL(); got != "http://localhost:11434" {
		t.Fatalf("auto-detected base = %q", got)
	}

	ts := DecisionRouterConfig{Provider: DecisionProviderTypeSafe}
	if got := ts.EffectiveBaseURL(); got != "https://api.typesafe.ai" {
		t.Fatalf("typesafe base = %q", got)
	}
	custom := DecisionRouterConfig{Provider: DecisionProviderCustom, BaseURL: "https://openrouter.ai/api/"}
	if got := custom.EffectiveBaseURL(); got != "https://openrouter.ai/api" {
		t.Fatalf("custom base = %q", got)
	}

	m := ModelAutoModeConfig{}
	if m.EffectiveTimeout() != 1500*time.Millisecond {
		t.Fatalf("ollama timeout = %v", m.EffectiveTimeout())
	}
	m.Router.Provider = DecisionProviderTypeSafe
	if m.EffectiveTimeout() != 3*time.Second {
		t.Fatalf("remote timeout = %v", m.EffectiveTimeout())
	}
	m.TimeoutMs = 700
	if m.EffectiveTimeout() != 700*time.Millisecond {
		t.Fatalf("explicit timeout = %v", m.EffectiveTimeout())
	}
	if (ModelAutoModeConfig{}).EffectiveThreshold() != 0.60 {
		t.Fatal("default threshold must be 0.60")
	}

	// Selected / AutoSelected.
	on, off := true, false
	sel := ModelAutoModeConfig{Enabled: true, DefaultAuto: true}
	if !sel.AutoSelected() {
		t.Fatal("nil Selected must follow DefaultAuto")
	}
	sel.Selected = &off
	if sel.AutoSelected() {
		t.Fatal("Selected=false must win")
	}
	sel.Selected, sel.Enabled = &on, false
	if sel.AutoSelected() {
		t.Fatal("disabled mode is never selected")
	}
	if len((ModelAutoModeConfig{Routes: []ModelAutoRoute{{ID: "a"}, {ID: "b", Disabled: true}}}).EnabledRoutes()) != 1 {
		t.Fatal("EnabledRoutes must skip disabled routes")
	}

	// Defaults reach a loaded config.
	dir := t.TempDir()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := Get().ModelAutoMode
	if got.Enabled || !got.DefaultAuto || got.Threshold != 0.6 || got.Router.Provider != DecisionProviderOllama {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestModelAutoModeProviderValidation(t *testing.T) {
	isolateGlobalConfig(t)
	t.Setenv("TYPESAFE_API_KEY", "")

	errs, _ := ValidateModelAutoMode(validAutoMode())
	if len(errs) != 0 {
		t.Fatalf("valid config rejected: %v", errs)
	}

	m := validAutoMode()
	m.Router.Provider = "gemini"
	errs, _ = ValidateModelAutoMode(m)
	if !hasField(errs, "router.provider") || !strings.Contains(errs[0].Message, "ollama, typesafe, custom") {
		t.Fatalf("unknown provider errs = %v", errs)
	}

	m = validAutoMode()
	m.Router.Provider = DecisionProviderCustom
	errs, _ = ValidateModelAutoMode(m)
	if !hasField(errs, "router.baseURL") {
		t.Fatalf("custom without baseURL errs = %v", errs)
	}
	m.Router.BaseURL = "ftp://x"
	if errs, _ = ValidateModelAutoMode(m); !hasField(errs, "router.baseURL") {
		t.Fatalf("non-http baseURL errs = %v", errs)
	}
	m.Router.BaseURL = "https://gw.example.com"
	if errs, _ = ValidateModelAutoMode(m); len(errs) != 0 {
		t.Fatalf("valid custom rejected: %v", errs)
	}

	m = validAutoMode()
	m.Router.Model = ""
	if errs, _ = ValidateModelAutoMode(m); !hasField(errs, "router.model") {
		t.Fatalf("enabled without model errs = %v", errs)
	}
	m.Enabled = false
	if errs, _ = ValidateModelAutoMode(m); hasField(errs, "router.model") {
		t.Fatalf("disabled must not require model: %v", errs)
	}

	m = validAutoMode()
	m.Router.Model = "qwen3.5:cloud"
	errs, _ = ValidateModelAutoMode(m)
	if !hasField(errs, "router.model") || !strings.Contains(errs[0].Message, "local") {
		t.Fatalf("cloud model errs = %v", errs)
	}

	m = validAutoMode()
	m.Router = DecisionRouterConfig{Provider: DecisionProviderTypeSafe, Model: "jev-latest"}
	errs, warns := ValidateModelAutoMode(m)
	if len(errs) != 0 || len(warns) == 0 {
		t.Fatalf("typesafe without key: errs=%v warns=%v (want warning only)", errs, warns)
	}
	t.Setenv("TYPESAFE_API_KEY", "abc")
	if _, warns = ValidateModelAutoMode(m); len(warns) != 0 {
		t.Fatalf("env fallback must silence warning: %v", warns)
	}
	if got := m.Router.EffectiveAPIKey(); got != "abc" {
		t.Fatalf("env fallback key = %q", got)
	}

	for _, th := range []float64{1.5, -0.1} {
		m = validAutoMode()
		m.Threshold = th
		if errs, _ = ValidateModelAutoMode(m); !hasField(errs, "threshold") {
			t.Fatalf("threshold %v accepted", th)
		}
	}
	m = validAutoMode()
	m.Threshold = 1
	if errs, _ = ValidateModelAutoMode(m); len(errs) != 0 {
		t.Fatalf("threshold 1 rejected: %v", errs)
	}
}

func TestModelAutoModeRouteValidation(t *testing.T) {
	isolateGlobalConfig(t)
	route := func(id, model string, fb ...models.ModelID) ModelAutoRoute {
		return ModelAutoRoute{ID: id, Description: "d", Model: models.ModelID(model), Fallbacks: fb}
	}
	check := func(name string, m ModelAutoModeConfig, field string) {
		t.Helper()
		errs, _ := ValidateModelAutoMode(m)
		if !hasField(errs, field) {
			t.Fatalf("%s: want error on %q, got %v", name, field, errs)
		}
	}
	base := validAutoMode()

	m := base
	m.Routes = nil
	for i := 0; i < 26; i++ {
		m.Routes = append(m.Routes, route("r"+string(rune('a'+i)), string(models.Claude35Haiku)))
	}
	errs, _ := ValidateModelAutoMode(m)
	if !hasField(errs, "routes") || !strings.Contains(errs[0].Message, "25") {
		t.Fatalf("26 routes errs = %v", errs)
	}
	m.Routes[25].Disabled = true
	if errs, _ = ValidateModelAutoMode(m); hasField(errs, "routes") {
		t.Fatalf("25 enabled routes must pass: %v", errs)
	}

	m = base
	m.Routes = []ModelAutoRoute{route("a", "x"), route("b", "x"), route("c", "x", "a", "b", "c")}
	check("three fallbacks", m, "routes[2].fallbacks")

	m.Routes = []ModelAutoRoute{route("a", "x", "x")}
	check("fallback equals primary", m, "routes[0].fallbacks")
	m.Routes = []ModelAutoRoute{route("a", "x", "y", "y")}
	check("duplicate fallback", m, "routes[0].fallbacks")

	m.Routes = []ModelAutoRoute{route("none", "x")}
	errs, _ = ValidateModelAutoMode(m)
	if !hasField(errs, "routes[0].id") || !strings.Contains(errs[0].Message, "reserved") {
		t.Fatalf("reserved id errs = %v", errs)
	}
	m.Routes = []ModelAutoRoute{route("  ", "x")}
	check("blank id", m, "routes[0].id")
	m.Routes = []ModelAutoRoute{route("a", "x"), route("A", "x")}
	check("duplicate id", m, "routes[1].id")
	m.Routes = []ModelAutoRoute{{ID: "a", Model: "x"}}
	check("missing description", m, "routes[0].description")
	m.Routes = []ModelAutoRoute{{ID: "a", Description: strings.Repeat("é", 501), Model: "x"}}
	check("long description", m, "routes[0].description")
	m.Routes = []ModelAutoRoute{{ID: "a", Description: strings.Repeat("é", 500), Model: "x"}}
	if errs, _ = ValidateModelAutoMode(m); hasField(errs, "routes[0].description") {
		t.Fatal("500 chars must be accepted")
	}
	m.Routes = []ModelAutoRoute{{ID: "a", Description: "d"}}
	check("missing model", m, "routes[0].model")

	// Unknown model is a warning only.
	m.Routes = []ModelAutoRoute{route("a", "no.such-model", "also.missing")}
	errs, warns := ValidateModelAutoMode(m)
	if len(errs) != 0 || len(warns) != 2 || !strings.Contains(warns[0], "no.such-model") {
		t.Fatalf("unknown model: errs=%v warns=%v", errs, warns)
	}
}

func TestModelAutoModeAPIKeyEncryption(t *testing.T) {
	isolateGlobalConfig(t)
	dir := t.TempDir()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := validAutoMode()
	m.Router.APIKey = "sk-test-123"
	if err := UpdateModelAutoMode(m); err != nil {
		t.Fatalf("UpdateModelAutoMode: %v", err)
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
	if got := Get().ModelAutoMode.Router.APIKey; got != "sk-test-123" {
		t.Fatalf("in-memory key = %q", got)
	}
	ResetForTests()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := Get().ModelAutoMode.Router.EffectiveAPIKey(); got != "sk-test-123" {
		t.Fatalf("reloaded key = %q", got)
	}

	// Empty incoming key keeps the stored one.
	m2 := validAutoMode()
	m2.Threshold = 0.7
	if err := UpdateModelAutoMode(m2); err != nil {
		t.Fatal(err)
	}
	if got := Get().ModelAutoMode.Router.APIKey; got != "sk-test-123" {
		t.Fatalf("key not kept: %q", got)
	}
	raw, _ = os.ReadFile(path)
	if !strings.Contains(string(raw), encryptedValuePrefix) {
		t.Fatal("stored key lost from disk")
	}
	if err := ClearModelAutoModeAPIKey(); err != nil || Get().ModelAutoMode.Router.APIKey != "" {
		t.Fatalf("clear key: err=%v key=%q", err, Get().ModelAutoMode.Router.APIKey)
	}

	// $ENV references and masking.
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

func TestUpdateModelAutoModePersistReload(t *testing.T) {
	isolateGlobalConfig(t)
	dir := t.TempDir()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ch := make(chan ConfigChangeEvent, 8)
	Bus.Subscribe(ch)
	defer Bus.Unsubscribe(ch)

	m := validAutoMode()
	m.Router.Headers = map[string]string{"x-team": "a"}
	m.Routes = []ModelAutoRoute{
		{ID: "code", Description: "coding", Model: models.Claude35Haiku, Fallbacks: []models.ModelID{"b", "c"}},
		{ID: "chat", Description: "chit chat", Model: "z", Disabled: true},
	}
	if err := UpdateModelAutoMode(m); err != nil {
		t.Fatalf("UpdateModelAutoMode: %v", err)
	}
	if err := SetModelAutoSelected(false); err != nil {
		t.Fatalf("SetModelAutoSelected: %v", err)
	}
	select {
	case ev := <-ch:
		if ev.Section != "modelAutoMode" {
			t.Fatalf("event = %+v", ev)
		}
	default:
		t.Fatal("no bus event published")
	}
	want := Get().ModelAutoMode
	if want.Selected == nil || *want.Selected || !want.Enabled || len(want.Routes) != 2 {
		t.Fatalf("in-memory block = %+v", want)
	}

	ResetForTests()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := Get().ModelAutoMode; !reflect.DeepEqual(got, want) {
		t.Fatalf("reloaded block differs:\n got %+v\nwant %+v", got, want)
	}

	// External edit is picked up by Reload().
	path, _ := ResolveConfigFilePath()
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "0.6", "0.7", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := Get().ModelAutoMode.Threshold; got != 0.7 {
		t.Fatalf("threshold after Reload = %v, want 0.7", got)
	}

	// Validation errors are refused and leave the config untouched.
	bad := validAutoMode()
	bad.Threshold = 2
	if err := UpdateModelAutoMode(bad); err == nil {
		t.Fatal("invalid block accepted")
	}
	if Get().ModelAutoMode.Threshold != 0.7 {
		t.Fatal("invalid update mutated config")
	}
}

func TestUpdateModelAutoModeRevertsOnWriteFailure(t *testing.T) {
	isolateGlobalConfig(t)
	dir := t.TempDir()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := UpdateModelAutoMode(validAutoMode()); err != nil {
		t.Fatal(err)
	}
	prev := Get().ModelAutoMode
	path, _ := ResolveConfigFilePath()
	if err := os.WriteFile(path, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	next := validAutoMode()
	next.Threshold = 0.9
	if err := UpdateModelAutoMode(next); err == nil {
		t.Fatal("expected write error")
	}
	if !reflect.DeepEqual(Get().ModelAutoMode, prev) {
		t.Fatalf("in-memory block not reverted: %+v", Get().ModelAutoMode)
	}
	_ = filepath.Base(path)
}

func TestUpdateModelAutoModeLocked(t *testing.T) {
	resetOverlayState(t)
	isolateGlobalConfig(t)
	dir := t.TempDir()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	before := Get().ModelAutoMode

	overlayMu.Lock()
	lockedKeys = []string{"modelAutoMode.router"}
	overlayMu.Unlock()

	m := validAutoMode()
	m.Router.Model = "other"
	err := UpdateModelAutoMode(m)
	if !errors.Is(err, ErrKeyLocked) {
		t.Fatalf("err = %v, want a lock error", err)
	}
	if !reflect.DeepEqual(Get().ModelAutoMode, before) {
		t.Fatal("config changed despite the lock")
	}
	if err := SetModelAutoSelected(true); err != nil {
		t.Fatalf("selected is outside the lock: %v", err)
	}
}
