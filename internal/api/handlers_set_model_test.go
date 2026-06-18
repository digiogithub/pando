package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
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
	t.Cleanup(func() { delete(models.SupportedModels, models.ModelID("copilot.gpt-5.4-fallback")) })

	s := &Server{app: &app.App{}} // CoderAgent is nil

	if err := s.setCoderModel(models.ModelID("copilot.gpt-5.4-fallback")); err != nil {
		t.Fatalf("setCoderModel returned error: %v", err)
	}
	if got := config.Get().Agents[config.AgentCoder].Model; got != models.ModelID("copilot.gpt-5.4-fallback") {
		t.Fatalf("persisted coder model = %q, want copilot.gpt-5.4-fallback", got)
	}
}
