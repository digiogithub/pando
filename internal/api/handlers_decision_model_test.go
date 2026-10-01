package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
)

func TestDecisionModelGetMasksKey(t *testing.T) {
	s := loadAutoModeConfig(t)
	if err := config.UpdateDecisionModel(config.DecisionModelConfig{
		Router:    config.DecisionRouterConfig{Provider: config.DecisionProviderTypeSafe, Model: "jev-latest", APIKey: autoTestKey},
		TimeoutMs: 1234,
	}); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, s.handleConfigDecisionModel, http.MethodGet, "/api/v1/config/decision-model", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), autoTestKey) {
		t.Fatalf("plain API key leaked: %s", rec.Body.String())
	}
	var resp DecisionModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Router.APIKeySet || !strings.HasSuffix(resp.Router.APIKeyMasked, "7890") || resp.TimeoutMs != 1234 ||
		resp.Router.Model != "jev-latest" || resp.Router.Provider != config.DecisionProviderTypeSafe {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Warnings == nil {
		t.Fatal("warnings must be an array, not null")
	}
}

func TestDecisionModelPutKeepsAndClearsKey(t *testing.T) {
	s := loadAutoModeConfig(t)
	t.Setenv(config.TypeSafeAPIKeyEnv, "")
	body := map[string]any{"router": map[string]any{"provider": "typesafe", "model": "jev-latest", "apiKey": autoTestKey}}
	if rec := doJSON(t, s.handleConfigDecisionModel, http.MethodPut, "/x", body); rec.Code != http.StatusOK {
		t.Fatalf("first PUT: %d %s", rec.Code, rec.Body.String())
	}
	for _, echoed := range []string{"", "••••7890"} {
		body["router"] = map[string]any{"provider": "typesafe", "model": "jev-latest", "apiKey": echoed}
		body["timeoutMs"] = 900
		rec := doJSON(t, s.handleConfigDecisionModel, http.MethodPut, "/x", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), autoTestKey) {
			t.Fatal("key leaked in PUT response")
		}
		if got := config.Get().DecisionModel.Router.EffectiveAPIKey(); got != autoTestKey {
			t.Fatalf("stored key = %q, want kept", got)
		}
	}
	if config.Get().DecisionModel.TimeoutMs != 900 {
		t.Fatal("other fields must still update")
	}

	// DELETE removes only the key.
	rec := doJSON(t, s.handleDecisionModelAPIKey, http.MethodDelete, "/api/v1/config/decision-model/api-key", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body.String())
	}
	var resp DecisionModelResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Router.APIKeySet || config.Get().DecisionModel.Router.EffectiveAPIKey() != "" || config.Get().DecisionModel.Router.Model != "jev-latest" {
		t.Fatalf("key not cleared or block damaged: %+v", resp)
	}

	// clearApiKey in a PUT does the same.
	body["router"] = map[string]any{"provider": "typesafe", "model": "jev-latest", "apiKey": autoTestKey}
	doJSON(t, s.handleConfigDecisionModel, http.MethodPut, "/x", body)
	body["router"] = map[string]any{"provider": "typesafe", "model": "jev-latest"}
	body["clearApiKey"] = true
	if rec := doJSON(t, s.handleConfigDecisionModel, http.MethodPut, "/x", body); rec.Code != http.StatusOK ||
		config.Get().DecisionModel.Router.EffectiveAPIKey() != "" {
		t.Fatalf("clearApiKey: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDecisionModelPutValidationErrors(t *testing.T) {
	s := loadAutoModeConfig(t)
	rec := doJSON(t, s.handleConfigDecisionModel, http.MethodPut, "/x", map[string]any{
		"router":    map[string]any{"provider": "custom", "model": "m"},
		"timeoutMs": -5,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Errors []struct{ Field, Message string } `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range resp.Errors {
		got[e.Field] = true
	}
	for _, f := range []string{"decisionModel.router.baseURL", "decisionModel.timeoutMs"} {
		if !got[f] {
			t.Fatalf("missing field error %q in %v", f, got)
		}
	}
	if config.Get().DecisionModel.Router.Model != "" {
		t.Fatal("invalid PUT must not change the config")
	}
}

func TestModelAutoModeRouterIsReadOnlyAliasOfDecisionModel(t *testing.T) {
	s := loadAutoModeConfig(t)
	if err := config.UpdateDecisionModel(config.DecisionModelConfig{
		Router: config.DecisionRouterConfig{Provider: config.DecisionProviderTypeSafe, Model: "jev-latest", APIKey: autoTestKey},
	}); err != nil {
		t.Fatal(err)
	}
	dm := doJSON(t, s.handleConfigDecisionModel, http.MethodGet, "/x", nil)
	am := doJSON(t, s.handleConfigModelAutoMode, http.MethodGet, "/x", nil)
	var d DecisionModelResponse
	var a ModelAutoModeResponse
	_ = json.Unmarshal(dm.Body.Bytes(), &d)
	_ = json.Unmarshal(am.Body.Bytes(), &a)
	if d.Router.Model == "" || !reflect.DeepEqual(d.Router, a.Router) {
		t.Fatalf("alias differs: decision=%+v modelAutoMode=%+v", d.Router, a.Router)
	}
}

func TestPutModelAutoModeRouterForwardsWithDeprecationWarning(t *testing.T) {
	s := loadAutoModeConfig(t)
	body := validAutoBody()
	rec := doJSON(t, s.handleConfigModelAutoMode, http.MethodPut, "/x", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	var resp ModelAutoModeResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	deprecated := false
	for _, w := range resp.Warnings {
		deprecated = deprecated || strings.Contains(w, "deprecated")
	}
	if !deprecated {
		t.Fatalf("missing deprecation warning: %v", resp.Warnings)
	}
	if config.Get().DecisionModel.Router.Model != "tev1:0.8b" {
		t.Fatalf("router not forwarded to decisionModel: %+v", config.Get().DecisionModel)
	}
	if config.Get().ModelAutoMode.LegacyRouter != nil {
		t.Fatal("legacy router must never be stored in modelAutoMode")
	}

	// Without a router the block saves without touching the shared model or warning.
	delete(body, "router")
	body["threshold"] = 0.8
	rec = doJSON(t, s.handleConfigModelAutoMode, http.MethodPut, "/x", body)
	resp = ModelAutoModeResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	for _, w := range resp.Warnings {
		if strings.Contains(w, "deprecated") {
			t.Fatalf("unexpected deprecation warning: %v", resp.Warnings)
		}
	}
	if rec.Code != http.StatusOK || config.Get().DecisionModel.Router.Model != "tev1:0.8b" || config.Get().ModelAutoMode.Threshold != 0.8 {
		t.Fatalf("PUT without router: %d %s", rec.Code, rec.Body.String())
	}
}

func TestLegacyRouterAliasServesSamePayload(t *testing.T) {
	s := loadAutoModeConfig(t)
	ollama := systemonetest.NewOllama035(t)
	draft := map[string]any{"router": map[string]any{"provider": "ollama", "baseURL": ollama.URL, "model": "tev1:0.8b"}}

	canonical := doJSON(t, s.handleDecisionRouterTest, http.MethodPost, "/api/v1/decision-model/router/test", draft)
	alias := doJSON(t, legacyRouterAlias(s.handleDecisionRouterTest), http.MethodPost, "/api/v1/model-auto-mode/router/test", draft)
	if canonical.Code != http.StatusOK || alias.Code != canonical.Code {
		t.Fatalf("canonical %d, alias %d", canonical.Code, alias.Code)
	}
	var c, a struct {
		OK bool `json:"ok"`
	}
	_ = json.Unmarshal(canonical.Body.Bytes(), &c)
	_ = json.Unmarshal(alias.Body.Bytes(), &a)
	if !c.OK || c != a {
		t.Fatalf("payloads differ: %s vs %s", canonical.Body.String(), alias.Body.String())
	}
	if alias.Header().Get("Deprecation") != "true" {
		t.Fatal("alias must carry a Deprecation header")
	}
}

func TestConfigServicesExposesAndValidatesDecisionFilter(t *testing.T) {
	s := loadAutoModeConfig(t)
	rec := doJSON(t, s.handleConfigServices, http.MethodGet, "/api/v1/config/services", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %d: %s", rec.Code, rec.Body.String())
	}
	var raw struct {
		Remembrances map[string]any `json:"remembrances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{
		"context_enrichment_decision_filter_enabled", "memory_context_decision_filter_enabled",
		"context_enrichment_decision_filter_threshold", "context_enrichment_decision_filter_max_candidates",
		"context_enrichment_decision_filter_max_candidate_chars", "context_enrichment_decision_filter_allow_hosted",
	} {
		if _, ok := raw.Remembrances[k]; !ok {
			t.Fatalf("settings payload misses %s: %v", k, raw.Remembrances)
		}
	}
	if raw.Remembrances["context_enrichment_decision_filter_threshold"] != 0.6 {
		t.Fatalf("threshold default = %v", raw.Remembrances["context_enrichment_decision_filter_threshold"])
	}

	var full map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &full)
	remb := full["remembrances"].(map[string]any)
	remb["context_enrichment_decision_filter_enabled"] = true
	remb["context_enrichment_decision_filter_threshold"] = 0.8
	if rec := doJSON(t, s.handleConfigServices, http.MethodPut, "/x", full); rec.Code != http.StatusOK {
		t.Fatalf("PUT %d: %s", rec.Code, rec.Body.String())
	}
	got := config.Get().Remembrances
	if !got.ContextEnrichmentDecisionFilterEnabled || got.ContextEnrichmentDecisionFilterThreshold != 0.8 {
		t.Fatalf("not persisted: %+v", got)
	}

	remb["context_enrichment_decision_filter_threshold"] = 1.7
	if rec := doJSON(t, s.handleConfigServices, http.MethodPut, "/x", full); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid threshold must be a 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if config.Get().Remembrances.ContextEnrichmentDecisionFilterThreshold != 0.8 {
		t.Fatal("rejected PUT changed the config")
	}
}
