package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/message"
)

// newTestCopilotClient builds a copilotClient wired directly to an httptest
// server, bypassing newCopilotClient's credential-loading and models-API
// probing (both of which hit the network). sourceToken is left empty so
// refreshBearerToken's IsCopilotAPIToken check short-circuits and never tries
// to renew the bearer token against a real endpoint.
func newTestCopilotClient(t *testing.T, baseURL string, model models.Model, reasoningEffort string) *copilotClient {
	t.Helper()
	return &copilotClient{
		providerOptions: providerClientOptions{
			model:         model,
			maxTokens:     1024,
			systemMessage: "You are a helpful assistant.",
		},
		options: copilotOptions{
			bearerToken:     "test-bearer-token",
			reasoningEffort: reasoningEffort,
		},
		baseURL: baseURL,
	}
}

func reasoningCapableModel() models.Model {
	return models.Model{
		ID:                      "gpt-6-sol",
		APIModel:                "gpt-6-sol",
		Provider:                models.ProviderCopilot,
		CanReason:               true,
		SupportsReasoningEffort: true,
		DefaultMaxTokens:        4096,
	}
}

// --- convertMessagesToResponsesInput -------------------------------------

func TestConvertMessagesToResponsesInputKeepsAssistantTextAndFunctionCallInOrder(t *testing.T) {
	c := &copilotClient{providerOptions: providerClientOptions{model: reasoningCapableModel()}}

	msgs := []message.Message{
		{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: "Let me check that file for you."},
				message.ToolCall{ID: "call_1", Name: "read_file", Input: `{"path":"a.go"}`},
			},
		},
	}

	input := c.convertMessagesToResponsesInput(msgs)
	if len(input) != 2 {
		t.Fatalf("len(input) = %d, want 2 (text output_message + function_call)", len(input))
	}

	// The assistant's text must come first, as its own output_message item...
	if input[0].OfOutputMessage == nil {
		t.Fatalf("input[0] should be an output_message item, got %+v", input[0])
	}
	if len(input[0].OfOutputMessage.Content) != 1 || input[0].OfOutputMessage.Content[0].OfOutputText == nil {
		t.Fatalf("input[0] content = %+v, want a single output_text part", input[0].OfOutputMessage.Content)
	}
	if got, want := input[0].OfOutputMessage.Content[0].OfOutputText.Text, "Let me check that file for you."; got != want {
		t.Fatalf("assistant text = %q, want %q", got, want)
	}

	// ...followed by the function_call, not dropped.
	if input[1].OfFunctionCall == nil {
		t.Fatalf("input[1] should be a function_call item, got %+v", input[1])
	}
	if got, want := input[1].OfFunctionCall.Name, "read_file"; got != want {
		t.Fatalf("function_call.Name = %q, want %q", got, want)
	}
	if got, want := input[1].OfFunctionCall.CallID, "call_1"; got != want {
		t.Fatalf("function_call.CallID = %q, want %q", got, want)
	}
}

func TestConvertMessagesToResponsesInputMultipleToolCallsAfterText(t *testing.T) {
	c := &copilotClient{providerOptions: providerClientOptions{model: reasoningCapableModel()}}

	msgs := []message.Message{
		{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: "I'll read both files."},
				message.ToolCall{ID: "call_1", Name: "read_file", Input: `{"path":"a.go"}`},
				message.ToolCall{ID: "call_2", Name: "read_file", Input: `{"path":"b.go"}`},
			},
		},
	}

	input := c.convertMessagesToResponsesInput(msgs)
	if len(input) != 3 {
		t.Fatalf("len(input) = %d, want 3 (text + 2 function_calls)", len(input))
	}
	if input[0].OfOutputMessage == nil {
		t.Fatalf("input[0] should be the text output_message")
	}
	if input[1].OfFunctionCall == nil || input[1].OfFunctionCall.CallID != "call_1" {
		t.Fatalf("input[1] = %+v, want function_call call_1", input[1])
	}
	if input[2].OfFunctionCall == nil || input[2].OfFunctionCall.CallID != "call_2" {
		t.Fatalf("input[2] = %+v, want function_call call_2", input[2])
	}
}

// --- sendWithResponsesAPI: reasoning request params ------------------------

func TestSendWithResponsesAPISetsReasoningWhenSupported(t *testing.T) {
	setProviderConfigForTests(t)

	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &capturedBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "resp_1", "object": "response", "created_at": 1, "model": "gpt-6-sol", "status": "completed",
			"output": [{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi"}]}],
			"usage": {"input_tokens":10,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":15}
		}`)
	}))
	defer server.Close()

	c := newTestCopilotClient(t, server.URL, reasoningCapableModel(), "high")

	msgs := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hello"}}}}
	if _, err := c.sendWithResponsesAPI(context.Background(), msgs, nil); err != nil {
		t.Fatalf("sendWithResponsesAPI() error = %v", err)
	}

	reasoning, ok := capturedBody["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("request body missing \"reasoning\" object, got %+v", capturedBody)
	}
	if got, want := reasoning["effort"], "high"; got != want {
		t.Fatalf("reasoning.effort = %v, want %v", got, want)
	}
	if got, want := reasoning["summary"], "auto"; got != want {
		t.Fatalf("reasoning.summary = %v, want %v; reasoning=%+v", got, want, reasoning)
	}
	if _, present := reasoning["generate_summary"]; present {
		t.Fatalf("deprecated reasoning.generate_summary must not be sent; reasoning=%+v", reasoning)
	}
}

func TestSendWithResponsesAPIOmitsReasoningWhenUnsupported(t *testing.T) {
	setProviderConfigForTests(t)

	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &capturedBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "resp_1", "object": "response", "created_at": 1, "model": "kimi-k2.7-code", "status": "completed",
			"output": [{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi"}]}],
			"usage": {"input_tokens":10,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":15}
		}`)
	}))
	defer server.Close()

	model := reasoningCapableModel()
	model.SupportsReasoningEffort = false
	c := newTestCopilotClient(t, server.URL, model, "high")

	msgs := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hello"}}}}
	if _, err := c.sendWithResponsesAPI(context.Background(), msgs, nil); err != nil {
		t.Fatalf("sendWithResponsesAPI() error = %v", err)
	}

	if _, present := capturedBody["reasoning"]; present {
		t.Fatalf("request body should not contain \"reasoning\" when unsupported, got %+v", capturedBody["reasoning"])
	}
}

// --- streamWithResponsesAPI: reasoning summary events ----------------------

func TestStreamWithResponsesAPIReasoningSummaryBeforeContent(t *testing.T) {
	setProviderConfigForTests(t)

	sse := strings.Join([]string{
		`data: {"type":"response.reasoning_summary_part.added","item_id":"rs_1","output_index":0}`,
		``,
		`data: {"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"delta":"Thinking step one"}`,
		``,
		`data: {"type":"response.reasoning_summary_part.added","item_id":"rs_1","output_index":0}`,
		``,
		`data: {"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"delta":"Thinking step two"}`,
		``,
		`data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":1,"delta":"Hello"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","created_at":1,"model":"gpt-6-sol","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello"}]}],"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":15}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sse)
	}))
	defer server.Close()

	c := newTestCopilotClient(t, server.URL, reasoningCapableModel(), "medium")

	msgs := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hello"}}}}
	events := c.streamWithResponsesAPI(context.Background(), msgs, nil)

	var thinking []string
	var sawContentDelta bool
	var sawThinkingBeforeContent bool
	var complete *ProviderResponse

	for event := range events {
		switch event.Type {
		case EventThinkingDelta:
			thinking = append(thinking, event.Thinking)
			if !sawContentDelta {
				sawThinkingBeforeContent = true
			}
		case EventContentDelta:
			sawContentDelta = true
		case EventComplete:
			complete = event.Response
		case EventError:
			t.Fatalf("unexpected EventError: %v", event.Error)
		}
	}

	if !sawThinkingBeforeContent {
		t.Fatalf("expected at least one EventThinkingDelta before EventContentDelta")
	}
	wantThinking := []string{"Thinking step one", "\n\n", "Thinking step two"}
	if len(thinking) != len(wantThinking) {
		t.Fatalf("thinking deltas = %q, want %q", thinking, wantThinking)
	}
	for i, want := range wantThinking {
		if thinking[i] != want {
			t.Fatalf("thinking[%d] = %q, want %q (full: %q)", i, thinking[i], want, thinking)
		}
	}
	if complete == nil {
		t.Fatalf("expected an EventComplete with a ProviderResponse")
	}
	if complete.Content != "Hello" {
		t.Fatalf("complete.Content = %q, want %q", complete.Content, "Hello")
	}
}

func TestStreamWithResponsesAPISingleReasoningPartHasNoSeparator(t *testing.T) {
	setProviderConfigForTests(t)

	sse := strings.Join([]string{
		`data: {"type":"response.reasoning_summary_part.added","item_id":"rs_1","output_index":0}`,
		``,
		`data: {"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"delta":"Only one part"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","created_at":1,"model":"gpt-6-sol","status":"completed","output":[],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":2}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sse)
	}))
	defer server.Close()

	c := newTestCopilotClient(t, server.URL, reasoningCapableModel(), "medium")
	msgs := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hello"}}}}

	var thinking []string
	for event := range c.streamWithResponsesAPI(context.Background(), msgs, nil) {
		if event.Type == EventThinkingDelta {
			thinking = append(thinking, event.Thinking)
		}
		if event.Type == EventError {
			t.Fatalf("unexpected EventError: %v", event.Error)
		}
	}

	want := []string{"Only one part"}
	if len(thinking) != len(want) || thinking[0] != want[0] {
		t.Fatalf("thinking = %q, want %q (no leading separator for the first part)", thinking, want)
	}
}
