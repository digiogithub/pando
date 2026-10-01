package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
)

func validAutoMode() ModelAutoModeConfig {
	return ModelAutoModeConfig{
		Enabled:   true,
		Threshold: 0.6,
		Routes: []ModelAutoRoute{
			{ID: "code", Description: "coding tasks", Model: models.Claude35Haiku},
		},
	}
}

func validDecision() DecisionModelConfig {
	return DecisionModelConfig{Router: DecisionRouterConfig{Provider: DecisionProviderOllama, Model: "tev1:0.8b"}}
}

// seedDecisionModel stores a valid shared decision model in the loaded config.
func seedDecisionModel(t *testing.T) {
	t.Helper()
	if err := UpdateDecisionModel(validDecision()); err != nil {
		t.Fatalf("UpdateDecisionModel: %v", err)
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
	if got.Enabled || !got.DefaultAuto || got.Threshold != 0.6 || got.LegacyRouter != nil {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	if Get().DecisionModel.Router.Provider != DecisionProviderOllama {
		t.Fatalf("unexpected decisionModel defaults: %+v", Get().DecisionModel)
	}
}

func TestModelAutoModeRequiresSharedDecisionModel(t *testing.T) {
	m := validAutoMode()
	errs, _ := ValidateModelAutoMode(m, DecisionModelConfig{})
	if !hasField(errs, "router.model") || !strings.Contains(errs[0].Message, "decisionModel.router.model is required") {
		t.Fatalf("enabled without shared model errs = %v", errs)
	}
	m.Enabled = false
	if errs, _ = ValidateModelAutoMode(m, DecisionModelConfig{}); hasField(errs, "router.model") {
		t.Fatalf("disabled must not require the model: %v", errs)
	}
}

func TestModelAutoModeRouteValidation(t *testing.T) {
	isolateGlobalConfig(t)
	route := func(id, model string, fb ...models.ModelID) ModelAutoRoute {
		return ModelAutoRoute{ID: id, Description: "d", Model: models.ModelID(model), Fallbacks: fb}
	}
	check := func(name string, m ModelAutoModeConfig, field string) {
		t.Helper()
		errs, _ := ValidateModelAutoMode(m, validDecision())
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
	errs, _ := ValidateModelAutoMode(m, validDecision())
	if !hasField(errs, "routes") || !strings.Contains(errs[0].Message, "25") {
		t.Fatalf("26 routes errs = %v", errs)
	}
	m.Routes[25].Disabled = true
	if errs, _ = ValidateModelAutoMode(m, validDecision()); hasField(errs, "routes") {
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
	errs, _ = ValidateModelAutoMode(m, validDecision())
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
	if errs, _ = ValidateModelAutoMode(m, validDecision()); hasField(errs, "routes[0].description") {
		t.Fatal("500 chars must be accepted")
	}
	m.Routes = []ModelAutoRoute{{ID: "a", Description: "d"}}
	check("missing model", m, "routes[0].model")

	// Unknown model is a warning only.
	m.Routes = []ModelAutoRoute{route("a", "no.such-model", "also.missing")}
	errs, warns := ValidateModelAutoMode(m, validDecision())
	if len(errs) != 0 || len(warns) != 2 || !strings.Contains(warns[0], "no.such-model") {
		t.Fatalf("unknown model: errs=%v warns=%v", errs, warns)
	}
}

func TestUpdateModelAutoModePersistReload(t *testing.T) {
	isolateGlobalConfig(t)
	dir := t.TempDir()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	seedDecisionModel(t)
	ch := make(chan ConfigChangeEvent, 8)
	Bus.Subscribe(ch)
	defer Bus.Unsubscribe(ch)

	m := validAutoMode()
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
	seedDecisionModel(t)
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
	seedDecisionModel(t)
	before := Get().ModelAutoMode

	overlayMu.Lock()
	lockedKeys = []string{"modelAutoMode.routes"}
	overlayMu.Unlock()

	m := validAutoMode()
	m.Threshold = 0.9
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
