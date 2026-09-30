package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
)

func TestListModelsAutoFirst(t *testing.T) {
	s := loadAutoModeConfig(t)
	srv := systemonetest.NewOllama035(t)
	cfg := config.Get()
	cfg.Providers = map[models.ModelProvider]config.Provider{models.ProviderAnthropic: {APIKey: "k"}}

	list := func() (ids []string, first ModelInfo) {
		rec := httptest.NewRecorder()
		s.handleListModels(rec, httptest.NewRequest(http.MethodGet, "/api/v1/models", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Models []ModelInfo `json:"models"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		for _, m := range resp.Models {
			ids = append(ids, m.ID)
		}
		if len(resp.Models) > 0 {
			first = resp.Models[0]
		}
		return
	}

	ids, _ := list()
	for _, id := range ids {
		if id == config.AutoModelID {
			t.Fatal("auto listed while disabled")
		}
	}

	if err := config.UpdateModelAutoMode(config.ModelAutoModeConfig{
		Enabled: true,
		Router:  config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: srv.URL, Model: "tev1:0.8b"},
		Routes:  playgroundRoutes(),
	}); err != nil {
		t.Fatal(err)
	}
	ids, first := list()
	if len(ids) < 2 || ids[0] != "auto" {
		t.Fatalf("auto must be first of %d models, got %v", len(ids), ids)
	}
	if first.Name != "Auto" || first.Description == "" || first.RouterProvider != "ollama" || first.RouterModel != "tev1:0.8b" || !first.RouterHealthy {
		t.Fatalf("auto entry = %+v", first)
	}

	// Unhealthy router: still listed first, with problems.
	if err := config.UpdateModelAutoMode(config.ModelAutoModeConfig{
		Enabled: true,
		Router:  config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: srv.URL, Model: "missing:1b"},
		Routes:  playgroundRoutes(),
	}); err != nil {
		t.Fatal(err)
	}
	_, first = list()
	if first.ID != "auto" || first.RouterHealthy || len(first.RouterProblems) == 0 {
		t.Fatalf("unhealthy auto entry = %+v", first)
	}
}

func TestSetActiveModelLeavesAuto(t *testing.T) {
	s := loadAutoModeConfig(t)
	models.RegisterDynamicModel(models.Model{ID: "copilot.auto-test", Provider: models.ProviderCopilot, APIModel: "auto-test"})
	t.Cleanup(func() { models.DeleteSupportedModels("copilot.auto-test") })
	s.app = &app.App{}

	put := func(model string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]string{"model": model})
		rec := httptest.NewRecorder()
		s.handleSetActiveModel(rec, httptest.NewRequest(http.MethodPut, "/api/v1/models/active", bytes.NewReader(b)))
		return rec
	}

	// Disabled: "auto" is refused.
	if rec := put("auto"); rec.Code != http.StatusBadRequest {
		t.Fatalf("auto while disabled: %d", rec.Code)
	}

	if err := config.UpdateModelAutoMode(config.ModelAutoModeConfig{
		Enabled: true,
		Router:  config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, Model: "tev1:0.8b"},
		Routes:  playgroundRoutes(),
	}); err != nil {
		t.Fatal(err)
	}
	if rec := put("auto"); rec.Code != http.StatusOK {
		t.Fatalf("auto: %d %s", rec.Code, rec.Body.String())
	}
	if !config.Get().ModelAutoMode.AutoSelected() {
		t.Fatal("auto should be selected")
	}
	if rec := put("copilot.auto-test"); rec.Code != http.StatusOK {
		t.Fatalf("concrete: %d %s", rec.Code, rec.Body.String())
	}
	if config.Get().ModelAutoMode.AutoSelected() {
		t.Fatal("selecting a concrete model must leave auto")
	}
}
