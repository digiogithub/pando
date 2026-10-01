package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
	"github.com/spf13/viper"
)

const autoTestKey = "sk-very-secret-key-7890"

// loadAutoModeConfig loads a real config in an isolated $HOME so the handlers
// can persist through config.UpdateModelAutoMode.
func loadAutoModeConfig(t *testing.T) *Server {
	t.Helper()
	config.IsolateForTests(t)
	viper.Reset()
	t.Cleanup(viper.Reset)
	if _, err := config.Load(t.TempDir(), false); err != nil {
		t.Fatalf("config.Load(): %v", err)
	}
	return &Server{}
}

// saveAutoWithRouter stores the shared decision router, then the auto mode block.
func saveAutoWithRouter(m config.ModelAutoModeConfig, r config.DecisionRouterConfig) error {
	if err := config.UpdateDecisionModel(config.DecisionModelConfig{Router: r}); err != nil {
		return err
	}
	return config.UpdateModelAutoMode(m)
}

func validAutoBody() map[string]any {
	return map[string]any{
		"enabled":   true,
		"threshold": 0.6,
		"router": map[string]any{
			"provider": "ollama",
			"model":    "tev1:0.8b",
		},
		"routes": []map[string]any{
			{"id": "code", "description": "coding tasks", "model": "claude-3.5-haiku"},
		},
	}
}

func doJSON(t *testing.T, h http.HandlerFunc, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestGetModelAutoModeMasksKey(t *testing.T) {
	s := loadAutoModeConfig(t)
	m := config.ModelAutoModeConfig{
		Enabled: true,
		Routes:  []config.ModelAutoRoute{{ID: "code", Description: "coding", Model: "claude-3.5-haiku"}},
	}
	r := config.DecisionRouterConfig{Provider: config.DecisionProviderTypeSafe, Model: "jev-latest", APIKey: autoTestKey}
	if err := saveAutoWithRouter(m, r); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, s.handleConfigModelAutoMode, http.MethodGet, "/api/v1/config/model-auto-mode", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), autoTestKey) {
		t.Fatalf("plain API key leaked: %s", rec.Body.String())
	}
	var resp ModelAutoModeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Router.APIKeySet || !strings.HasSuffix(resp.Router.APIKeyMasked, "7890") || resp.Router.APIKeyMasked == autoTestKey {
		t.Fatalf("router = %+v", resp.Router)
	}
	if len(resp.Routes) != 1 || resp.Routes[0].ID != "code" {
		t.Fatalf("routes = %+v", resp.Routes)
	}
}

func TestPutModelAutoModeKeepsKey(t *testing.T) {
	s := loadAutoModeConfig(t)
	body := validAutoBody()
	body["router"] = map[string]any{"provider": "typesafe", "model": "jev-latest", "apiKey": autoTestKey}
	if rec := doJSON(t, s.handleConfigModelAutoMode, http.MethodPut, "/x", body); rec.Code != http.StatusOK {
		t.Fatalf("first PUT: %d %s", rec.Code, rec.Body.String())
	}
	// Second PUT: empty key must keep the stored one; a masked echo as well.
	for _, echoed := range []string{"", "••••7890"} {
		body["router"] = map[string]any{"provider": "typesafe", "model": "jev-latest", "apiKey": echoed}
		body["threshold"] = 0.7
		rec := doJSON(t, s.handleConfigModelAutoMode, http.MethodPut, "/x", body)
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
	if config.Get().ModelAutoMode.Threshold != 0.7 {
		t.Fatal("other fields must still update")
	}
	// clearApiKey removes it.
	body["clearApiKey"] = true
	body["router"] = map[string]any{"provider": "typesafe", "model": "jev-latest"}
	t.Setenv(config.TypeSafeAPIKeyEnv, "")
	rec := doJSON(t, s.handleConfigModelAutoMode, http.MethodPut, "/x", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear PUT: %d %s", rec.Code, rec.Body.String())
	}
	var resp ModelAutoModeResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Router.APIKeySet || config.Get().DecisionModel.Router.EffectiveAPIKey() != "" {
		t.Fatalf("key not cleared: %+v", resp.Router)
	}
}

func TestPutModelAutoModeValidation(t *testing.T) {
	s := loadAutoModeConfig(t)
	body := validAutoBody()
	body["router"] = map[string]any{"provider": "custom", "model": ""}
	body["routes"] = []map[string]any{
		{"id": "none", "description": "", "model": "a"},
	}
	rec := doJSON(t, s.handleConfigModelAutoMode, http.MethodPut, "/x", body)
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
		if e.Message == "" {
			t.Fatalf("empty message: %+v", e)
		}
		got[e.Field] = true
	}
	for _, f := range []string{"modelAutoMode.router.baseURL", "modelAutoMode.router.model", "modelAutoMode.routes[0].id", "modelAutoMode.routes[0].description"} {
		if !got[f] {
			t.Fatalf("missing field error %q in %v", f, got)
		}
	}
	if config.Get().ModelAutoMode.Enabled {
		t.Fatal("invalid PUT must not change the config")
	}
}

func TestRouterTestEndpointDraft(t *testing.T) {
	s := loadAutoModeConfig(t)
	ollama := systemonetest.NewOllama035(t)

	// Nothing saved: the draft is probed.
	rec := doJSON(t, s.handleDecisionRouterTest, http.MethodPost, "/x", map[string]any{
		"router": map[string]any{"provider": "ollama", "baseURL": ollama.URL, "model": "tev1:0.8b"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK       bool     `json:"ok"`
		Problems []string `json:"problems"`
		Report   struct {
			Reachable  bool   `json:"reachable"`
			Version    string `json:"version"`
			IsDecision bool   `json:"isDecisionModel"`
		} `json:"report"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.OK || !resp.Report.Reachable || !resp.Report.IsDecision || resp.Report.Version == "" {
		t.Fatalf("resp = %s", rec.Body.String())
	}

	// A missing model reports a problem with the pull hint, draft still used.
	rec = doJSON(t, s.handleDecisionRouterTest, http.MethodPost, "/x", map[string]any{
		"router": map[string]any{"provider": "ollama", "baseURL": ollama.URL, "model": "nope:1b"},
	})
	resp.OK, resp.Problems = true, nil
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.OK || len(resp.Problems) == 0 {
		t.Fatalf("want a problem for a missing model: %s", rec.Body.String())
	}

	// Saved config is untouched by drafts.
	if config.Get().DecisionModel.Router.Model != "" {
		t.Fatal("draft test must not persist anything")
	}
}

func TestRouterEndpointsNeverEchoKey(t *testing.T) {
	s := loadAutoModeConfig(t)
	remote := systemonetest.NewRemote(t, systemonetest.WithAPIKey("the-real-server-key"), systemonetest.WithModelsJSON(systemonetest.TypeSafeModelsJSON))
	draft := map[string]any{"provider": "custom", "baseURL": remote.URL, "model": "jev-latest", "apiKey": autoTestKey}

	for name, h := range map[string]http.HandlerFunc{
		"test":   s.handleDecisionRouterTest,
		"models": s.handleDecisionRouterModels,
	} {
		rec := doJSON(t, h, http.MethodPost, "/x", map[string]any{"router": draft})
		if strings.Contains(rec.Body.String(), autoTestKey) {
			t.Fatalf("%s leaked the draft key: %s", name, rec.Body.String())
		}
	}

	// Saved key is reused only for the same target and never echoed.
	saved := config.ModelAutoModeConfig{Enabled: true}
	savedRouter := config.DecisionRouterConfig{Provider: config.DecisionProviderCustom, BaseURL: remote.URL, Model: "jev-latest", APIKey: "the-real-server-key"}
	if err := saveAutoWithRouter(saved, savedRouter); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, s.handleDecisionRouterTest, http.MethodPost, "/x", map[string]any{
		"router": map[string]any{"provider": "custom", "baseURL": remote.URL, "model": "jev-latest"},
	})
	if strings.Contains(rec.Body.String(), "the-real-server-key") {
		t.Fatalf("leaked stored key: %s", rec.Body.String())
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.OK {
		t.Fatalf("stored key should authorise the same target: %s", rec.Body.String())
	}
	// A different host must NOT receive the stored key.
	other := systemonetest.NewRemote(t, systemonetest.WithAPIKey("the-real-server-key"))
	rec = doJSON(t, s.handleDecisionRouterTest, http.MethodPost, "/x", map[string]any{
		"router": map[string]any{"provider": "custom", "baseURL": other.URL, "model": "jev-latest"},
	})
	for _, r := range other.Requests() {
		if strings.Contains(r.Header.Get("Authorization"), "the-real-server-key") {
			t.Fatal("stored key sent to an unsaved host")
		}
	}
	// GET config also masks it.
	rec = doJSON(t, s.handleConfigModelAutoMode, http.MethodGet, "/x", nil)
	if strings.Contains(rec.Body.String(), "the-real-server-key") {
		t.Fatal("GET leaked key")
	}
}

func TestRouterModelsEndpoint(t *testing.T) {
	s := loadAutoModeConfig(t)
	ollama := systemonetest.NewOllama035(t)
	req := httptest.NewRequest(http.MethodGet, "/x?provider=ollama&baseURL="+ollama.URL, nil)
	rec := httptest.NewRecorder()
	s.handleDecisionRouterModels(rec, req)
	var resp struct {
		Models []struct{ ID string } `json:"models"`
		Status string                `json:"status"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusOK || resp.Status != "filtered" || len(resp.Models) == 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func playgroundRoutes() []config.ModelAutoRoute {
	return []config.ModelAutoRoute{
		{ID: "code", Description: "coding tasks", Model: models.ModelID("claude-3.5-haiku")},
		{ID: "chat", Description: "chit chat", Model: models.ModelID("claude-3.5-haiku")},
	}
}

type playgroundResp struct {
	Decision struct {
		RouteID        string             `json:"routeId"`
		Matched        bool               `json:"matched"`
		Probability    float64            `json:"probability"`
		Probabilities  map[string]float64 `json:"probabilities"`
		Candidates     []string           `json:"candidates"`
		Reason         string             `json:"reason"`
		RouterProvider string             `json:"routerProvider"`
		RouterModel    string             `json:"routerModel"`
	} `json:"decision"`
	State string `json:"state"`
}

func TestPlaygroundDraft(t *testing.T) {
	s := loadAutoModeConfig(t)
	saved := systemonetest.NewOllama035(t)
	saved.SetDecision("chat", map[string]float64{"code": 0.05, "chat": 0.9, "none": 0.05})
	draftSrv := systemonetest.NewOllama035(t)
	draftSrv.SetDecision("code", map[string]float64{"code": 0.95, "chat": 0.03, "none": 0.02})

	if err := saveAutoWithRouter(config.ModelAutoModeConfig{
		Enabled: true,
		Routes:  playgroundRoutes(),
	}, config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: saved.URL, Model: "tev1:0.8b"}); err != nil {
		t.Fatal(err)
	}

	// Saved config.
	rec := doJSON(t, s.handleModelAutoPlayground, http.MethodPost, "/x", map[string]any{"prompt": "hello there"})
	var resp playgroundResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %v %s", rec.Code, err, rec.Body.String())
	}
	if resp.Decision.RouteID != "chat" || !resp.Decision.Matched || resp.State == "" || len(resp.Decision.Candidates) != 1 {
		t.Fatalf("saved decision = %+v", resp.Decision)
	}
	if resp.Decision.RouterModel != "tev1:0.8b" || resp.Decision.RouterProvider != "ollama" {
		t.Fatalf("router info: %+v", resp.Decision)
	}

	// Draft: different router server and routes, nothing saved.
	rec = doJSON(t, s.handleModelAutoPlayground, http.MethodPost, "/x", map[string]any{
		"prompt": "refactor this function",
		"config": map[string]any{
			"enabled": true, "threshold": 0.6,
			"router": map[string]any{"provider": "ollama", "baseURL": draftSrv.URL, "model": "tev1:0.8b"},
			"routes": []map[string]any{{"id": "code", "description": "coding tasks", "model": "claude-3.5-haiku"}},
		},
	})
	resp = playgroundResp{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusOK || resp.Decision.RouteID != "code" {
		t.Fatalf("draft decision: %d %s", rec.Code, rec.Body.String())
	}
	if draftSrv.Count("/v1/systemone") != 1 {
		t.Fatalf("draft router calls = %d", draftSrv.Count("/v1/systemone"))
	}
	if len(config.Get().ModelAutoMode.Routes) != 2 {
		t.Fatal("playground must not persist the draft")
	}

	// Invalid draft gives per-field errors.
	rec = doJSON(t, s.handleModelAutoPlayground, http.MethodPost, "/x", map[string]any{
		"prompt": "x",
		"config": map[string]any{"router": map[string]any{"provider": "bogus"}},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "modelAutoMode.router.provider") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, s.handleModelAutoPlayground, http.MethodPost, "/x", map[string]any{"prompt": " "}); rec.Code != http.StatusBadRequest {
		t.Fatalf("blank prompt: %d", rec.Code)
	}
}

func TestPlaygroundNoLLM(t *testing.T) {
	// Server has no app/agent at all: the playground must work with only the
	// decision provider and call nothing but /v1/systemone (plus budget lookups).
	s := loadAutoModeConfig(t)
	srv := systemonetest.NewOllama035(t)
	srv.SetDecision("code", map[string]float64{"code": 0.9, "chat": 0.05, "none": 0.05})
	if err := saveAutoWithRouter(config.ModelAutoModeConfig{
		Enabled: true,
		Routes:  playgroundRoutes(),
	}, config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: srv.URL, Model: "tev1:0.8b"}); err != nil {
		t.Fatal(err)
	}
	if s.app != nil {
		t.Fatal("test precondition: no app")
	}
	rec := doJSON(t, s.handleModelAutoPlayground, http.MethodPost, "/x", map[string]any{"prompt": "write a test"})
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if srv.Count("/v1/systemone") < 1 {
		t.Fatal("the decision provider was not called")
	}
	// A config-reload warm-up may add systemone calls; nothing else is allowed.
	allowed := map[string]bool{"/v1/systemone": true, "/api/version": true, "/api/tags": true, "/api/show": true}
	for _, r := range srv.Requests() {
		if !allowed[r.Path] {
			t.Fatalf("unexpected call %s (playground must not invoke a chat model)", r.Path)
		}
	}
}

func TestRouterModelsSuggestionsAndPull(t *testing.T) {
	s := loadAutoModeConfig(t)
	ollama := systemonetest.NewOllama035(t)
	req := httptest.NewRequest(http.MethodGet, "/x?provider=ollama&baseURL="+ollama.URL, nil)
	rec := httptest.NewRecorder()
	s.handleDecisionRouterModels(rec, req)
	var resp struct {
		Suggestions []string `json:"suggestions"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	joined := strings.Join(resp.Suggestions, ",")
	if strings.Contains(joined, "tev1:0.8b") || !strings.Contains(joined, "nimble") {
		t.Fatalf("suggestions = %v", resp.Suggestions)
	}

	router := map[string]any{"provider": "ollama", "baseURL": ollama.URL}

	// Non-suggested model: rejected, nothing reaches the server.
	rec = doJSON(t, s.handleDecisionRouterPull, http.MethodPost, "/x", map[string]any{"model": "llama3:70b", "router": router})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-suggested: %d %s", rec.Code, rec.Body.String())
	}
	// Non-ollama provider: rejected.
	rec = doJSON(t, s.handleDecisionRouterPull, http.MethodPost, "/x", map[string]any{
		"model": "nimble", "router": map[string]any{"provider": "custom", "baseURL": ollama.URL}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("custom provider: %d", rec.Code)
	}
	if ollama.Count("/api/pull") != 0 {
		t.Fatal("pull reached the server on rejected requests")
	}

	rec = doJSON(t, s.handleDecisionRouterPull, http.MethodPost, "/x", map[string]any{"model": "nimble", "router": router})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("pull: %d %s", rec.Code, rec.Body.String())
	}
	var job struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	deadline := time.Now().Add(5 * time.Second)
	for {
		jr := httptest.NewRequest(http.MethodGet, "/x", nil)
		jr.SetPathValue("id", job.ID)
		jrec := httptest.NewRecorder()
		s.handleDecisionRouterPullJob(jrec, jr)
		var st struct {
			State string `json:"state"`
		}
		_ = json.Unmarshal(jrec.Body.Bytes(), &st)
		if st.State == "done" {
			break
		}
		if st.State == "error" || time.Now().After(deadline) {
			t.Fatalf("job: %s", jrec.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
