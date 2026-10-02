package page

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
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
		DecisionModel: config.DecisionModelConfig{
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
		"modelAutoMode.enabled", "modelAutoMode.defaultAuto",
		"modelAutoMode.threshold", "modelAutoMode.historyPrompts",
		"modelAutoMode.info.routerInfo", "modelAutoMode.info.routerHealth", "modelAutoMode.info.routerHint",
		"action:model_auto_add_route",
	} {
		if _, ok := modelAutoFieldByKey(t, cfg, key); !ok {
			t.Errorf("missing field %q", key)
		}
	}
	// The router is owned by the Decision model section: no editable router fields here.
	for _, key := range []string{
		"modelAutoMode.provider", "modelAutoMode.baseURL", "modelAutoMode.apiKey",
		"modelAutoMode.model", "modelAutoMode.timeoutMs", "action:model_auto_test",
		"action:model_auto_discover", "action:model_auto_clear_key",
	} {
		if _, ok := modelAutoFieldByKey(t, cfg, key); ok {
			t.Errorf("router field %q must not live in the Auto mode section", key)
		}
	}
	if f, _ := modelAutoFieldByKey(t, cfg, "modelAutoMode.info.routerInfo"); f.Value != "ollama/tev1:0.8b" || !f.Disabled {
		t.Errorf("decision model info row = %+v", f)
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

func TestModelAutoModeRouteCardIDsAndErrorsUseCardContext(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)
	cfg.ModelAutoMode.Routes = []config.ModelAutoRoute{{ID: "route-a", Description: "First route", Model: cfg.Agents[config.AgentCoder].Model}}

	f, ok := modelAutoFieldByKey(t, cfg, "modelAutoMode.routes.0.enabled")
	if !ok || f.CardID != "route-a" {
		t.Fatalf("route field card id = %+v ok=%v, want route-a", f, ok)
	}

	err := saveModelAutoMode(settings.Field{Key: "modelAutoMode.routes.0.enabled", Label: "Enabled", Card: "Route Alpha", Value: "wat"})
	if err == nil || !strings.Contains(err.Error(), "Route Alpha › Enabled") {
		t.Fatalf("card-aware validation error = %v", err)
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

func TestFieldDisplayLabelUsesCardContext(t *testing.T) {
	field := settings.Field{Label: "Enabled", Card: "Route Alpha"}
	if got := fieldDisplayLabel(field); got != "Route Alpha › Enabled" {
		t.Fatalf("fieldDisplayLabel() = %q, want %q", got, "Route Alpha › Enabled")
	}
	if got := fieldDisplayLabel(settings.Field{Label: "Theme"}); got != "Theme" {
		t.Fatalf("fieldDisplayLabel() without card = %q, want Theme", got)
	}
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
	setPersonaRouterHealth(personaRouterHealthKey(cfg.DecisionModel), "Healthy")
	if f, _ := agentFieldByKey(cfg, "agents.persona-selector.routerHealth"); f.Value != "Healthy" {
		t.Fatalf("router health = %q, want Healthy", f.Value)
	}
	t.Cleanup(func() { setPersonaRouterHealth("", "") })
	if _, ok := agentFieldByKey(cfg, "agents.persona-selector.model"); !ok {
		t.Error("model field missing")
	}

	cfg.DecisionModel.Router.Provider = config.DecisionProviderTypeSafe
	if _, ok := agentFieldByKey(cfg, "agents.persona-selector.routerPrivacy"); !ok {
		t.Error("hosted router must show the privacy note")
	}
	cfg.DecisionModel.Router.Model = ""
	if f, ok := agentFieldByKey(cfg, "agents.persona-selector.routerWarning"); !ok || !strings.Contains(f.Value, "Not configured") {
		t.Errorf("warning missing: %+v", f)
	}
}

func TestCheckPersonaRouterHealthCmd(t *testing.T) {
	cfg := withModelAutoTUIConfig(t)

	// No decision model: no probe at all.
	cfg.DecisionModel.Router.Model = ""
	if checkPersonaRouterHealth() != nil {
		t.Fatal("no probe expected without a router model")
	}

	// An unreachable router reports Unhealthy without blocking the caller.
	cfg.DecisionModel.Router.Model = "tev1:0.8b"
	cfg.DecisionModel.Router.BaseURL = "http://127.0.0.1:1"
	cmd := checkPersonaRouterHealth()
	if cmd == nil {
		t.Fatal("probe expected")
	}
	msg, ok := cmd().(personaRouterHealthMsg)
	if !ok || !strings.HasPrefix(msg.status, "Unhealthy") || msg.key != personaRouterHealthKey(cfg.DecisionModel) {
		t.Fatalf("msg = %+v ok=%v", msg, ok)
	}
}
