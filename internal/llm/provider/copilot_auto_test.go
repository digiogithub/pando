package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/llm/models"
	toolsPkg "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
)

func userMessage(text string) message.Message {
	return message.Message{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}}
}

func sessionContext(sessionID string) context.Context {
	return context.WithValue(context.Background(), toolsPkg.SessionIDContextKey, sessionID)
}

func resetCopilotAutoRoutes(t *testing.T) {
	t.Helper()
	clear := func() {
		copilotAutoRoutesMu.Lock()
		copilotAutoRoutes = map[string]copilotAutoRoute{}
		copilotAutoRoutesMu.Unlock()
		copilotUtilityFailuresMu.Lock()
		copilotUtilityFailures = map[string]time.Time{}
		copilotUtilityFailuresMu.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

func autoModel() models.Model {
	return models.Model{ID: "copilot.auto", APIModel: models.CopilotAutoModelID, Provider: models.ProviderCopilot}
}

func TestCopilotRequestHeadersAgentLoop(t *testing.T) {
	c := newTestCopilotClient(t, "https://example.invalid", reasoningCapableModel(), "")

	headers := c.requestHeaders(sessionContext("session-1"), []message.Message{userMessage("hi")})
	want := map[string]string{
		"X-GitHub-Api-Version": auth.CopilotAPIVersion,
		"X-Interaction-Type":   CopilotInteractionAgent,
		"X-Interaction-Id":     "session-1",
		"x-initiator":          "user",
	}
	for key, value := range want {
		if headers[key] != value {
			t.Errorf("%s = %q, want %q", key, headers[key], value)
		}
	}
	if _, ok := headers["Copilot-Session-Token"]; ok {
		t.Error("Copilot-Session-Token must only be sent for an Auto pick")
	}
}

func TestCopilotRequestHeadersNonAgentInteractionIsAgentInitiated(t *testing.T) {
	c := newTestCopilotClient(t, "https://example.invalid", reasoningCapableModel(), "")
	c.options.interactionType = CopilotInteractionCompaction

	// The last message is the user's, yet a compaction is never user-initiated.
	headers := c.requestHeaders(context.Background(), []message.Message{userMessage("hi")})
	if headers["X-Interaction-Type"] != CopilotInteractionCompaction {
		t.Errorf("X-Interaction-Type = %q", headers["X-Interaction-Type"])
	}
	if headers["x-initiator"] != "agent" {
		t.Errorf("x-initiator = %q, want agent", headers["x-initiator"])
	}
	if _, ok := headers["X-Interaction-Id"]; ok {
		t.Error("X-Interaction-Id must be omitted without a session")
	}
}

// autoRouter serves POST /auto, counting calls and picking the given model.
func autoRouter(t *testing.T, calls *atomic.Int32, modelID string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auto" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		calls.Add(1)
		if got := r.Header.Get("X-GitHub-Api-Version"); got != auth.CopilotAPIVersion {
			t.Errorf("X-GitHub-Api-Version = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if payload["prompt"] != "fix the bug" {
			t.Errorf("prompt = %v, want the last user message", payload["prompt"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_token": "auto-token",
			"expires_at":    time.Now().Add(time.Hour).Unix(),
			"selected_model": map[string]any{
				"id":                  modelID,
				"name":                "Picked",
				"supported_endpoints": []string{"/responses"},
				"capabilities": map[string]any{
					"limits":   map[string]any{"max_context_window_tokens": 400000, "max_output_tokens": 512},
					"supports": map[string]any{"reasoning_effort": []string{"low", "high"}, "vision": true},
				},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCopilotForRequestRoutesAutoOncePerSession(t *testing.T) {
	resetCopilotAutoRoutes(t)
	var calls atomic.Int32
	server := autoRouter(t, &calls, "pando-test-unlisted-model")
	c := newTestCopilotClient(t, server.URL, autoModel(), "")
	msgs := []message.Message{userMessage("fix the bug")}

	routed, err := c.forRequest(sessionContext("s1"), msgs)
	if err != nil {
		t.Fatalf("forRequest: %v", err)
	}
	if routed == c {
		t.Fatal("an Auto request must be served by a per-request copy")
	}
	picked := routed.providerOptions.model
	if picked.APIModel != "pando-test-unlisted-model" {
		t.Fatalf("picked model = %q", picked.APIModel)
	}
	// A pick the catalog does not list is described by the router's answer.
	if !routed.isResponsesAPIModel() || !picked.SupportsReasoningEffort || !picked.SupportsAttachments {
		t.Fatalf("picked model metadata not taken from the router: %+v", picked)
	}
	if routed.providerOptions.maxTokens != 512 {
		t.Fatalf("maxTokens = %d, want it capped to the picked model's 512", routed.providerOptions.maxTokens)
	}
	if got := routed.requestHeaders(sessionContext("s1"), msgs)["Copilot-Session-Token"]; got != "auto-token" {
		t.Fatalf("Copilot-Session-Token = %q", got)
	}
	if c.providerOptions.model.APIModel != models.CopilotAutoModelID || c.autoSessionToken != "" {
		t.Fatal("the shared client must stay bound to Auto")
	}

	// Same session: the pick is reused. Another session routes on its own.
	if _, err := c.forRequest(sessionContext("s1"), msgs); err != nil {
		t.Fatalf("forRequest: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("router called %d times for one session, want 1", got)
	}
	if _, err := c.forRequest(sessionContext("s2"), msgs); err != nil {
		t.Fatalf("forRequest: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("router called %d times for two sessions, want 2", got)
	}
}

func TestCopilotAutoRoutesAgainAfterCompaction(t *testing.T) {
	resetCopilotAutoRoutes(t)
	var calls atomic.Int32
	server := autoRouter(t, &calls, "pando-test-unlisted-model")
	c := newTestCopilotClient(t, server.URL, autoModel(), "")
	msgs := []message.Message{userMessage("fix the bug")}
	ctx := sessionContext("s1")

	if _, err := c.forRequest(ctx, msgs); err != nil {
		t.Fatalf("forRequest: %v", err)
	}
	compactor := *c
	compactor.options.interactionType = CopilotInteractionCompaction
	routed, err := compactor.forRequest(ctx, msgs)
	if err != nil {
		t.Fatalf("forRequest (compaction): %v", err)
	}
	if routed.autoSessionToken != "auto-token" || calls.Load() != 1 {
		t.Fatalf("the compaction request must keep the current pick (calls=%d)", calls.Load())
	}
	if _, err := c.forRequest(ctx, msgs); err != nil {
		t.Fatalf("forRequest: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("router called %d times, want 2 (re-route after compaction)", got)
	}
}

func TestCopilotAutoRoutingFailureIsAnError(t *testing.T) {
	resetCopilotAutoRoutes(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()
	c := newTestCopilotClient(t, server.URL, autoModel(), "")

	if _, err := c.forRequest(sessionContext("s1"), []message.Message{userMessage("hi")}); err == nil {
		t.Fatal("expected an error when the router fails and there is no previous pick")
	}
}

func TestCopilotForRequestLeavesOtherModelsAlone(t *testing.T) {
	c := newTestCopilotClient(t, "https://example.invalid", reasoningCapableModel(), "")
	routed, err := c.forRequest(context.Background(), nil)
	if err != nil || routed != c {
		t.Fatalf("forRequest = %p, %v; want the receiver untouched", routed, err)
	}
}

func TestCopilotSendUsesUtilityModelForTitles(t *testing.T) {
	resetCopilotAutoRoutes(t)
	var gotModel, gotInteraction string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s, the utility model is served over chat completions", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &payload)
		gotModel = payload.Model
		gotInteraction = r.Header.Get("X-Interaction-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Fix login bug"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer server.Close()

	// The configured title model is a Responses API model; the title must not use it.
	c := newTestCopilotClient(t, server.URL, reasoningCapableModel(), "medium")
	c.providerOptions.model.SupportedEndpoints = []string{"/responses"}
	c.options.interactionType = CopilotInteractionBackground
	c.options.utilityModel = CopilotUtilityTitleModel

	resp, err := c.send(context.Background(), []message.Message{userMessage("help me fix login")}, nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if resp.Content != "Fix login bug" {
		t.Fatalf("content = %q", resp.Content)
	}
	if gotModel != CopilotUtilityTitleModel {
		t.Fatalf("model = %q, want %q", gotModel, CopilotUtilityTitleModel)
	}
	if gotInteraction != CopilotInteractionBackground {
		t.Fatalf("X-Interaction-Type = %q", gotInteraction)
	}
}

func TestCopilotUtilityModelFailureFallsBackAndIsRemembered(t *testing.T) {
	resetCopilotAutoRoutes(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":{"message":"model not supported"}}`, http.StatusBadRequest)
	}))
	defer server.Close()

	c := newTestCopilotClient(t, server.URL, reasoningCapableModel(), "")
	c.options.utilityModel = CopilotUtilityTitleModel
	msgs := []message.Message{userMessage("hi")}

	if _, ok := c.sendWithUtilityModel(context.Background(), msgs, nil); ok {
		t.Fatal("a rejected utility request must report false so the caller falls back")
	}
	if _, ok := c.sendWithUtilityModel(context.Background(), msgs, nil); ok {
		t.Fatal("expected false while the failure is remembered")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("utility model requested %d times, want 1 (failure remembered)", got)
	}
}
