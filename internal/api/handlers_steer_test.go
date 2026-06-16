package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
)

// steerMockAgent is a minimal agent.Service used to exercise the steer handler.
type steerMockAgent struct {
	*pubsub.Broker[agent.AgentEvent]
	steerErr   error
	steerCalls []string
	pending    int
}

func newSteerMockAgent() *steerMockAgent {
	return &steerMockAgent{Broker: pubsub.NewBroker[agent.AgentEvent]()}
}

func (m *steerMockAgent) Steer(sessionID string, content string, attachments ...message.Attachment) error {
	m.steerCalls = append(m.steerCalls, content)
	if m.steerErr != nil {
		return m.steerErr
	}
	m.pending++
	return nil
}
func (m *steerMockAgent) PendingSteering(sessionID string) int { return m.pending }

// Remaining agent.Service methods are no-ops for this test.
func (m *steerMockAgent) Model() models.Model { return models.Model{} }
func (m *steerMockAgent) Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan agent.AgentEvent, error) {
	return nil, nil
}
func (m *steerMockAgent) LastRunSystemMessages(sessionID string) []string { return nil }
func (m *steerMockAgent) Cancel(sessionID string)                         {}
func (m *steerMockAgent) IsSessionBusy(sessionID string) bool             { return true }
func (m *steerMockAgent) IsBusy() bool                                    { return true }
func (m *steerMockAgent) Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error) {
	return models.Model{}, nil
}
func (m *steerMockAgent) Summarize(ctx context.Context, sessionID string) error { return nil }
func (m *steerMockAgent) SetLuaManager(fm *luaengine.FilterManager)             {}
func (m *steerMockAgent) GetTools() []tools.BaseTool                            { return nil }

func newSteerTestServer(ag agent.Service) *Server {
	return &Server{app: &app.App{CoderAgent: ag}}
}

func doSteer(t *testing.T, s *Server, sessionID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sessionID+"/steer", bytes.NewBufferString(body))
	req.SetPathValue("id", sessionID)
	rec := httptest.NewRecorder()
	s.handleSteer(rec, req)
	return rec
}

func TestHandleSteerQueuesFeedback(t *testing.T) {
	ag := newSteerMockAgent()
	s := newSteerTestServer(ag)

	rec := doSteer(t, s, "sess-1", `{"prompt":"focus on the auth bug"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(ag.steerCalls) != 1 || ag.steerCalls[0] != "focus on the auth bug" {
		t.Fatalf("unexpected steer calls: %v", ag.steerCalls)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["sessionId"] != "sess-1" {
		t.Fatalf("sessionId = %v", resp["sessionId"])
	}
}

func TestHandleSteerEmptyPrompt(t *testing.T) {
	s := newSteerTestServer(newSteerMockAgent())
	rec := doSteer(t, s, "sess-1", `{"prompt":"   "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleSteerInvalidBody(t *testing.T) {
	s := newSteerTestServer(newSteerMockAgent())
	rec := doSteer(t, s, "sess-1", `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleSteerNotBusyConflict(t *testing.T) {
	ag := newSteerMockAgent()
	ag.steerErr = agent.ErrSessionNotBusy
	s := newSteerTestServer(ag)

	rec := doSteer(t, s, "sess-1", `{"prompt":"hello"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "not_busy" {
		t.Fatalf("error = %q, want not_busy", resp["error"])
	}
}
