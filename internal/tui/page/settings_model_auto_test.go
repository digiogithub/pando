package page

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
	"github.com/digiogithub/pando/internal/tui/components/settings"
)

func withModelAutoTUIConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".pando.toml"), []byte("Debug = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var modelID models.ModelID
	for id := range models.SupportedModels() {
		modelID = id
		break
	}
	prev := config.Get()
	cfg := &config.Config{
		WorkingDir: dir,
		Debug:      true,
		Agents:     map[config.AgentName]config.Agent{config.AgentCoder: {Model: modelID}},
		ModelAutoMode: config.ModelAutoModeConfig{
			Router: config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, Model: "tev1:0.8b"},
		},
	}
	config.SetForTests(cfg)
	t.Cleanup(func() { config.SetForTests(prev) })
	return cfg
}

func modelAutoFieldByKey(t *testing.T, cfg *config.Config, key string) (settings.Field, bool) {
	t.Helper()
	for _, f := range buildModelAutoModeSection(cfg).Fields {
		if f.Key == key {
			return f, true
		}
	}
	return settings.Field{}, false
}

func TestModelAutoModeSection(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)

	section := buildModelAutoModeSection(cfg)
	if section.Title != "Auto mode" {
		t.Errorf("title = %q", section.Title)
	}
	for _, key := range []string{
		"modelAutoMode.enabled", "modelAutoMode.defaultAuto", "modelAutoMode.provider",
		"modelAutoMode.baseURL", "modelAutoMode.apiKey", "modelAutoMode.model",
		"modelAutoMode.threshold", "modelAutoMode.timeoutMs", "modelAutoMode.historyPrompts",
		"action:model_auto_test", "action:model_auto_add_route",
	} {
		if _, ok := modelAutoFieldByKey(t, cfg, key); !ok {
			t.Errorf("missing field %q", key)
		}
	}
	if f, _ := modelAutoFieldByKey(t, cfg, "modelAutoMode.baseURL"); !f.Disabled {
		t.Error("Ollama base URL must be read-only")
	}

	// Enable, then add and edit a route.
	if err := saveModelAutoMode(settings.Field{Key: "modelAutoMode.enabled", Label: "Enabled", Value: "true"}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !config.Get().ModelAutoMode.Enabled {
		t.Fatal("enabled not persisted")
	}
	if err := addModelAutoRoute(); err != nil {
		t.Fatalf("add route: %v", err)
	}
	if err := saveModelAutoMode(settings.Field{Key: "modelAutoMode.routes.0.id", Label: "ID", Value: "code"}); err != nil {
		t.Fatalf("rename route: %v", err)
	}
	if got := config.Get().ModelAutoMode.Routes[0].ID; got != "code" {
		t.Errorf("route id = %q", got)
	}
	if _, ok := modelAutoFieldByKey(t, config.Get(), "modelAutoMode.routes.0.fallback2"); !ok {
		t.Error("route fields missing after add")
	}

	// Field errors are surfaced and nothing is persisted.
	err := saveModelAutoMode(settings.Field{Key: "modelAutoMode.routes.0.id", Label: "ID", Value: "none"})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("reserved id error = %v", err)
	}
	if got := config.Get().ModelAutoMode.Routes[0].ID; got != "code" {
		t.Errorf("invalid save leaked: id = %q", got)
	}
	if err := saveModelAutoMode(settings.Field{Key: "modelAutoMode.threshold", Label: "T", Value: "7"}); err == nil {
		t.Error("threshold 7 must be rejected")
	}

	// API key: stored, masked in the section, masked value keeps it.
	if err := saveModelAutoMode(settings.Field{Key: "modelAutoMode.apiKey", Value: "sk-live-9876"}); err != nil {
		t.Fatalf("set key: %v", err)
	}
	keyField, _ := modelAutoFieldByKey(t, config.Get(), "modelAutoMode.apiKey")
	if strings.Contains(keyField.Value, "sk-live-9876") || !strings.HasSuffix(keyField.Value, "9876") {
		t.Errorf("key field must be masked, got %q", keyField.Value)
	}
	if err := saveModelAutoMode(settings.Field{Key: "modelAutoMode.apiKey", Value: keyField.Value}); err != nil {
		t.Fatalf("masked resave: %v", err)
	}
	if config.Get().ModelAutoMode.Router.APIKey != "sk-live-9876" {
		t.Error("masked resave must keep the stored key")
	}

	// Provider switch resets the model.
	if err := saveModelAutoMode(settings.Field{Key: "modelAutoMode.provider", Value: "typesafe"}); err == nil {
		// Enabled + empty model is invalid, so the switch must be refused until a model is chosen.
		t.Error("switching provider while enabled without a model should fail validation")
	}

	// Delete the route.
	if err := deleteModelAutoRoute(0); err != nil {
		t.Fatalf("delete route: %v", err)
	}
	if n := len(config.Get().ModelAutoMode.Routes); n != 0 {
		t.Errorf("routes = %d after delete", n)
	}
}

func TestModelAutoModeSectionRouteCap(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)
	for i := 0; i < config.ModelAutoModeMaxRoutes; i++ {
		if err := addModelAutoRoute(); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	if err := addModelAutoRoute(); err == nil {
		t.Error("26th route must be rejected")
	}
	f, _ := modelAutoFieldByKey(t, config.Get(), "action:model_auto_add_route")
	if !f.Disabled {
		t.Error("add action must be disabled at the cap")
	}
	_ = cfg
}

func TestModelAutoModeSectionParity(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)
	t.Cleanup(func() { setModelAutoDiscovery("", nil); setModelAutoRouterModels(nil) })

	// Presets only for Custom, and they fill the base URL and model.
	if _, ok := modelAutoFieldByKey(t, cfg, "modelAutoMode.preset"); ok {
		t.Fatal("preset field must be Custom-only")
	}
	if err := saveModelAutoMode(settings.Field{Key: "modelAutoMode.provider", Value: "custom"}); err != nil {
		t.Fatal(err)
	}
	preset, ok := modelAutoFieldByKey(t, config.Get(), "modelAutoMode.preset")
	if !ok || strings.Join(preset.Options, ",") != "(choose),OpenRouter,LiteLLM,Kev" {
		t.Fatalf("preset field = %+v", preset)
	}
	if err := saveModelAutoMode(settings.Field{Key: "modelAutoMode.preset", Value: "OpenRouter"}); err != nil {
		t.Fatal(err)
	}
	r := config.Get().ModelAutoMode.Router
	if r.BaseURL != "https://openrouter.ai/api" || r.Model != "typesafe/jev-1.13" {
		t.Fatalf("openrouter preset: %+v", r)
	}
	// Privacy notice names the remote host.
	prov, _ := modelAutoFieldByKey(t, config.Get(), "modelAutoMode.provider")
	if !strings.Contains(prov.Hint, "openrouter.ai") {
		t.Errorf("privacy hint = %q", prov.Hint)
	}
	if err := saveModelAutoMode(settings.Field{Key: "modelAutoMode.preset", Value: "Kev"}); err != nil {
		t.Fatal(err)
	}
	if got := config.Get().ModelAutoMode.Router.BaseURL; got != "http://localhost:8009" {
		t.Errorf("kev preset url = %q", got)
	}

	// Route reorder.
	if err := addModelAutoRoute(); err != nil {
		t.Fatal(err)
	}
	if err := addModelAutoRoute(); err != nil {
		t.Fatal(err)
	}
	first := config.Get().ModelAutoMode.Routes[0].ID
	if err := moveModelAutoRoute(0, 1); err != nil {
		t.Fatal(err)
	}
	if got := config.Get().ModelAutoMode.Routes[1].ID; got != first {
		t.Errorf("route not moved down: %q", got)
	}
	if err := moveModelAutoRoute(0, -1); err == nil {
		t.Error("moving the first route up must fail")
	}

	// Show-all toggle appears for a filtered listing; free text when unsupported.
	setModelAutoDiscovery("filtered", nil)
	if _, ok := modelAutoFieldByKey(t, config.Get(), "action:model_auto_toggle_show_all"); !ok {
		t.Error("show-all action missing for filtered listing")
	}
	setModelAutoDiscovery("unsupported", nil)
	if _, ok := modelAutoFieldByKey(t, config.Get(), "action:model_auto_toggle_show_all"); ok {
		t.Error("show-all must be hidden for unsupported listing")
	}
	model, _ := modelAutoFieldByKey(t, config.Get(), "modelAutoMode.model")
	if model.Type != settings.FieldText {
		t.Errorf("model field type = %v, want free text", model.Type)
	}
}

func TestModelAutoModePullAction(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)
	t.Cleanup(func() { setModelAutoDiscovery("", nil) })
	fake := systemonetest.NewOllama035(t)
	cfg.ModelAutoMode.Router.BaseURL = fake.URL

	setModelAutoDiscovery("filtered", []string{"nimble"})
	if _, ok := modelAutoFieldByKey(t, cfg, "action:model_auto_pull:nimble"); !ok {
		t.Fatal("pull action missing for suggestion")
	}

	// Non-suggested models never reach the server.
	_ = pullModelAutoModel("llama3:70b")()
	if fake.Count("/api/pull") != 0 {
		t.Fatal("non-suggested model was pulled")
	}

	var got *modelAutoPullMsg
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch m := c().(type) {
		case tea.BatchMsg:
			for _, sub := range m {
				run(sub)
			}
		case modelAutoPullMsg:
			got = &m
		}
	}
	run(pullModelAutoModel("nimble"))
	if got == nil || got.err != nil {
		t.Fatalf("pull result = %+v", got)
	}
	if fake.Count("/api/pull") != 1 {
		t.Fatalf("fake pulls = %d", fake.Count("/api/pull"))
	}
}

func agentFieldByKey(cfg *config.Config, key string) (settings.Field, bool) {
	for _, f := range buildAgentsSection(cfg).Fields {
		if f.Key == key {
			return f, true
		}
	}
	return settings.Field{}, false
}

func TestPersonaSelectorDecisionModelFields(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)
	key := "agents.persona-selector.useDecisionModel"

	if _, ok := agentFieldByKey(cfg, key); !ok {
		t.Fatal("persona-selector must expose the decision model toggle")
	}
	if _, ok := agentFieldByKey(cfg, "agents.coder.useDecisionModel"); ok {
		t.Error("toggle must exist only for persona-selector")
	}
	if _, ok := agentFieldByKey(cfg, "agents.persona-selector.routerInfo"); ok {
		t.Error("router info must be hidden while the toggle is off")
	}

	if err := saveAgent(settings.Field{Key: key, Value: "true"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if !config.Get().Agents[config.AgentPersonaSelector].UseDecisionModel {
		t.Fatal("flag not persisted")
	}
	if err := saveAgent(settings.Field{Key: "agents.coder.useDecisionModel", Value: "true"}); err == nil {
		t.Error("other agents must be rejected")
	}

	cfg = config.Get()
	f, ok := agentFieldByKey(cfg, "agents.persona-selector.routerInfo")
	if !ok || !strings.Contains(f.Value, "tev1:0.8b") {
		t.Fatalf("router info = %+v ok=%v", f, ok)
	}
	if _, ok := agentFieldByKey(cfg, "agents.persona-selector.routerPrivacy"); ok {
		t.Error("no privacy note for local ollama")
	}
	setPersonaRouterHealth("", "")
	if f, ok := agentFieldByKey(cfg, "agents.persona-selector.routerHealth"); !ok || f.Value != "checking..." {
		t.Fatalf("router health before the probe = %+v ok=%v", f, ok)
	}
	setPersonaRouterHealth(personaRouterHealthKey(cfg.ModelAutoMode), "Healthy")
	if f, _ := agentFieldByKey(cfg, "agents.persona-selector.routerHealth"); f.Value != "Healthy" {
		t.Fatalf("router health = %q, want Healthy", f.Value)
	}
	t.Cleanup(func() { setPersonaRouterHealth("", "") })
	if _, ok := agentFieldByKey(cfg, "agents.persona-selector.model"); !ok {
		t.Error("model field missing")
	}

	cfg.ModelAutoMode.Router.Provider = config.DecisionProviderTypeSafe
	if _, ok := agentFieldByKey(cfg, "agents.persona-selector.routerPrivacy"); !ok {
		t.Error("hosted router must show the privacy note")
	}
	cfg.ModelAutoMode.Router.Model = ""
	if f, ok := agentFieldByKey(cfg, "agents.persona-selector.routerWarning"); !ok || !strings.Contains(f.Value, "No router model") {
		t.Errorf("warning missing: %+v", f)
	}
}

func TestCheckPersonaRouterHealthCmd(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)

	// Option off or no router model: no probe at all.
	if checkPersonaRouterHealth() != nil {
		t.Fatal("no probe expected while the toggle is off")
	}
	cfg.Agents[config.AgentPersonaSelector] = config.Agent{UseDecisionModel: true}
	cfg.ModelAutoMode.Router.Model = ""
	if checkPersonaRouterHealth() != nil {
		t.Fatal("no probe expected without a router model")
	}

	// An unreachable router reports Unhealthy without blocking the caller.
	cfg.ModelAutoMode.Router.Model = "tev1:0.8b"
	cfg.ModelAutoMode.Router.BaseURL = "http://127.0.0.1:1"
	cmd := checkPersonaRouterHealth()
	if cmd == nil {
		t.Fatal("probe expected")
	}
	msg, ok := cmd().(personaRouterHealthMsg)
	if !ok || !strings.HasPrefix(msg.status, "Unhealthy") || msg.key != personaRouterHealthKey(cfg.ModelAutoMode) {
		t.Fatalf("msg = %+v ok=%v", msg, ok)
	}
}
