package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
)

// modelUpdateMockAgent records calls to Update so we can assert the API routes
// model selection through the running agent (which rebuilds its provider) and
// not just through config persistence.
type modelUpdateMockAgent struct {
	*steerMockAgent
	updateCalls []models.ModelID
}

func (m *modelUpdateMockAgent) Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error) {
	m.updateCalls = append(m.updateCalls, modelID)
	return models.Model{ID: modelID}, nil
}

// TestHandleSetActiveModelUpdatesRunningAgent guards against a regression where
// the web-UI/desktop only persisted the coder model to disk (via
// config.UpdateAgentModel) without rebuilding the live agent's provider. That
// left a freshly configured app reporting "no model configured" on the next
// message until a restart, even though the model appeared selected.
func TestHandleSetActiveModelUpdatesRunningAgent(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	mock := &modelUpdateMockAgent{steerMockAgent: newSteerMockAgent()}
	s := &Server{app: &app.App{CoderAgent: mock}}

	body := `{"model":"copilot.gpt-5.4"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/models/active", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	s.handleSetActiveModel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(mock.updateCalls) != 1 {
		t.Fatalf("CoderAgent.Update called %d times, want 1", len(mock.updateCalls))
	}
	if mock.updateCalls[0] != models.ModelID("copilot.gpt-5.4") {
		t.Fatalf("CoderAgent.Update called with %q, want copilot.gpt-5.4", mock.updateCalls[0])
	}
}

// TestSetCoderModelFallsBackToConfig verifies that when no live agent is present
// (e.g. a startup mode without a CoderAgent), the helper still persists the
// selection via config instead of panicking.
func TestSetCoderModelFallsBackToConfig(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	models.RegisterDynamicModel(models.Model{
		ID:       models.ModelID("copilot.gpt-5.4-fallback"),
		Provider: models.ProviderCopilot,
		APIModel: "gpt-5.4-fallback",
	})
	t.Cleanup(func() { models.DeleteSupportedModels(models.ModelID("copilot.gpt-5.4-fallback")) })

	s := &Server{app: &app.App{}} // CoderAgent is nil

	if err := s.setCoderModel(models.ModelID("copilot.gpt-5.4-fallback")); err != nil {
		t.Fatalf("setCoderModel returned error: %v", err)
	}
	if got := config.Get().Agents[config.AgentCoder].Model; got != models.ModelID("copilot.gpt-5.4-fallback") {
		t.Fatalf("persisted coder model = %q, want copilot.gpt-5.4-fallback", got)
	}
}

// TestHandleSetupModelsRejectsBadFastModelBeforeWriting guards the up-front
// validation: an unknown fast model must fail without touching the coder.
func TestHandleSetupModelsRejectsBadFastModelBeforeWriting(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	mock := &modelUpdateMockAgent{steerMockAgent: newSteerMockAgent()}
	s := &Server{app: &app.App{CoderAgent: mock}}

	body := `{"mainModel":"copilot.gpt-5.4","fastModel":"nope.does-not-exist"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/models", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	s.handleSetupModels(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(mock.updateCalls) != 0 {
		t.Fatalf("coder was updated %d times despite an invalid fast model", len(mock.updateCalls))
	}
}

// TestHandleSetActiveModelSessionScope guards the selector contract: a model
// picked for a session is an in-memory override of that session and never
// rewrites the coder model persisted in the configuration.
func TestHandleSetActiveModelSessionScope(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	for _, id := range []models.ModelID{"copilot.session-default", "copilot.session-pick"} {
		models.RegisterDynamicModel(models.Model{ID: id, Provider: models.ProviderCopilot, APIModel: string(id)})
		t.Cleanup(func() { models.DeleteSupportedModels(id) })
	}
	if err := config.UpdateAgentModel(config.AgentCoder, "copilot.session-default"); err != nil {
		t.Fatalf("seed coder model: %v", err)
	}

	const sessionID = "session-scope-test"
	t.Cleanup(func() { agent.SetSessionLLMOverrides(sessionID, agent.SessionLLMOverrides{}) })

	mock := &modelUpdateMockAgent{steerMockAgent: newSteerMockAgent()}
	s := &Server{app: &app.App{CoderAgent: mock}}

	body := `{"model":"copilot.session-pick","sessionId":"` + sessionID + `"}`
	rec := httptest.NewRecorder()
	s.handleSetActiveModel(rec, httptest.NewRequest(http.MethodPut, "/api/v1/models/active", bytes.NewBufferString(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if len(mock.updateCalls) != 0 {
		t.Fatalf("CoderAgent.Update called %d times, want 0", len(mock.updateCalls))
	}
	if got := config.Get().Agents[config.AgentCoder].Model; got != "copilot.session-default" {
		t.Fatalf("configured coder model = %q, want it unchanged", got)
	}
	if got := agent.SessionModelID(sessionID); got != "copilot.session-pick" {
		t.Fatalf("session model = %q, want copilot.session-pick", got)
	}
	if got := agent.SessionModelID("another-session"); got != "copilot.session-default" {
		t.Fatalf("other session model = %q, want the configured default", got)
	}

	// An unknown model is refused and leaves the session as it was.
	rec = httptest.NewRecorder()
	body = `{"model":"nope.does-not-exist","sessionId":"` + sessionID + `"}`
	s.handleSetActiveModel(rec, httptest.NewRequest(http.MethodPut, "/api/v1/models/active", bytes.NewBufferString(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown model: status = %d", rec.Code)
	}
	if got := agent.SessionModelID(sessionID); got != "copilot.session-pick" {
		t.Fatalf("session model after a refused switch = %q", got)
	}

	// The first prompt of a session carries the selection made before it existed.
	const fresh = "session-scope-fresh"
	t.Cleanup(func() { agent.SetSessionLLMOverrides(fresh, agent.SessionLLMOverrides{}) })
	if err := applyRequestModel(fresh, "copilot.session-pick"); err != nil {
		t.Fatalf("applyRequestModel: %v", err)
	}
	if got := agent.SessionModelID(fresh); got != "copilot.session-pick" {
		t.Fatalf("request model not applied to the session: %q", got)
	}
	if got := config.Get().Agents[config.AgentCoder].Model; got != "copilot.session-default" {
		t.Fatalf("configured coder model = %q after a request model", got)
	}
}
