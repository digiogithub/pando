package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

func agentsFromResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]AgentConfigItem {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Agents []AgentConfigItem `json:"agents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	out := map[string]AgentConfigItem{}
	for _, a := range resp.Agents {
		out[a.Name] = a
	}
	return out
}

func TestConfigAgentsUseDecisionModelGetAndPut(t *testing.T) {
	resetProviderAccountsTestConfig(t)
	s := &Server{}

	rec := httptest.NewRecorder()
	s.handleConfigAgents(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config/agents", nil))
	agents := agentsFromResponse(t, rec)
	ps, ok := agents[string(config.AgentPersonaSelector)]
	if !ok || ps.UseDecisionModel == nil || *ps.UseDecisionModel {
		t.Fatalf("persona-selector GET = %+v, want useDecisionModel=false", ps)
	}
	if agents[string(config.AgentCoder)].UseDecisionModel != nil {
		t.Fatal("coder must not report useDecisionModel")
	}

	body := `{"agents":[{"name":"persona-selector","model":"","useDecisionModel":true},{"name":"title","model":"","useDecisionModel":true}]}`
	rec = httptest.NewRecorder()
	s.handleConfigAgents(rec, httptest.NewRequest(http.MethodPut, "/api/v1/config/agents", bytes.NewBufferString(body)))
	agents = agentsFromResponse(t, rec)
	if v := agents[string(config.AgentPersonaSelector)].UseDecisionModel; v == nil || !*v {
		t.Fatalf("persona-selector flag not set after PUT: %s", rec.Body.String())
	}
	if !config.PersonaSelectorUsesDecisionModel() {
		t.Fatal("config helper false after PUT")
	}
	if config.Get().Agents[config.AgentTitle].UseDecisionModel {
		t.Fatal("title agent must ignore useDecisionModel")
	}

	// A payload that omits the field keeps the stored value.
	body = `{"agents":[{"name":"persona-selector","model":""}]}`
	rec = httptest.NewRecorder()
	s.handleConfigAgents(rec, httptest.NewRequest(http.MethodPut, "/api/v1/config/agents", bytes.NewBufferString(body)))
	if rec.Code != http.StatusOK || !config.PersonaSelectorUsesDecisionModel() {
		t.Fatalf("omitted field must keep flag; code=%d", rec.Code)
	}
}
