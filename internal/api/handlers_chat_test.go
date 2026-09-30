package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/llm/agent"
)

func dispatchOne(t *testing.T, ev agent.AgentEvent) string {
	t.Helper()
	rec := httptest.NewRecorder()
	s := &Server{}
	var mu sync.Mutex
	s.dispatchSSEEvent(rec, rec, &mu, map[string]bool{}, map[string]string{}, "", ev)
	return rec.Body.String()
}

func TestSSEForwardsSystemMessage(t *testing.T) {
	// Routing notice carries the structured payload.
	body := dispatchOne(t, agent.AgentEvent{
		Type:          agent.AgentEventTypeSystemMessage,
		SessionID:     "s1",
		SystemMessage: "Auto: code → m (p=0.93)\n",
		Routing:       &agent.RoutingInfo{RouteID: "code", Model: "m", Matched: true, Probability: 0.93, Kind: "routed"},
	})
	if !strings.HasPrefix(body, "event: system_message\n") {
		t.Fatalf("unexpected frame: %q", body)
	}
	data := strings.TrimSpace(strings.SplitN(strings.SplitN(body, "data: ", 2)[1], "\n", 2)[0])
	var got struct {
		Text    string             `json:"text"`
		Routing *agent.RoutingInfo `json:"routing"`
	}
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		t.Fatal(err)
	}
	if got.Routing == nil || got.Routing.RouteID != "code" || got.Routing.Kind != "routed" || !strings.Contains(got.Text, "Auto:") {
		t.Fatalf("bad payload: %+v", got)
	}

	// Generic message: text only, no routing key.
	body = dispatchOne(t, agent.AgentEvent{Type: agent.AgentEventTypeSystemMessage, SystemMessage: "hello"})
	if !strings.Contains(body, "event: system_message") || strings.Contains(body, `"routing"`) || !strings.Contains(body, `"text":"hello"`) {
		t.Fatalf("bad generic frame: %q", body)
	}

	// Empty message without routing is dropped.
	if body = dispatchOne(t, agent.AgentEvent{Type: agent.AgentEventTypeSystemMessage}); body != "" {
		t.Fatalf("expected no frame, got %q", body)
	}
}
