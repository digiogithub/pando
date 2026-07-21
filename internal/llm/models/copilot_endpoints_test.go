package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCopilotModelUsesResponsesAPI(t *testing.T) {
	tests := []struct {
		name     string
		model    Model
		expected bool
	}{
		{
			// mai-code-1-flash only accepts /responses and its name matches no
			// gpt-version heuristic: this is the case that used to fail.
			name:     "responses-only model with a non-gpt name",
			model:    Model{APIModel: "mai-code-1-flash-picker", SupportedEndpoints: []string{"/responses"}},
			expected: true,
		},
		{
			name:     "responses-only gpt model",
			model:    Model{APIModel: "gpt-5.6-luna", SupportedEndpoints: []string{"/responses", "ws:/responses"}},
			expected: true,
		},
		{
			name:     "chat-completions-only model",
			model:    Model{APIModel: "kimi-k2.7-code", SupportedEndpoints: []string{"/chat/completions"}},
			expected: false,
		},
		{
			// Advertising both routes keeps the historical name-based decision.
			name:     "both endpoints, name says responses",
			model:    Model{APIModel: "gpt-5.4", SupportedEndpoints: []string{"/responses", "/chat/completions", "ws:/responses"}},
			expected: true,
		},
		{
			name:     "both endpoints, gpt-5-mini stays on chat completions",
			model:    Model{APIModel: "gpt-5-mini", SupportedEndpoints: []string{"/chat/completions", "/responses", "ws:/responses"}},
			expected: false,
		},
		{
			name:     "anthropic model over chat completions",
			model:    Model{APIModel: "claude-sonnet-5", SupportedEndpoints: []string{"/v1/messages", "/chat/completions"}},
			expected: false,
		},
		{
			name:     "no metadata falls back to the name heuristic",
			model:    Model{APIModel: "gpt-5.4"},
			expected: true,
		},
		{
			name:     "no metadata, non-responses name",
			model:    Model{APIModel: "gpt-4o"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CopilotModelUsesResponsesAPI(tt.model); got != tt.expected {
				t.Fatalf("CopilotModelUsesResponsesAPI(%q) = %v, want %v", tt.model.APIModel, got, tt.expected)
			}
		})
	}
}

func TestFetchCopilotModelsParsesEndpointsAndCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"mai-code-1-flash-picker","name":"MAI-Code-1-Flash","model_picker_enabled":true,
			 "supported_endpoints":["/responses"],
			 "capabilities":{"type":"chat","limits":{"max_context_window_tokens":256000,"max_output_tokens":128000},
			  "supports":{"reasoning_effort":["low","medium","high"],"tool_calls":true}}},
			{"id":"kimi-k2.7-code","name":"Kimi K2.7 Code","model_picker_enabled":true,
			 "supported_endpoints":["/chat/completions"],
			 "capabilities":{"type":"chat","limits":{"max_context_window_tokens":256000},
			  "supports":{"tool_calls":true,"vision":true}}},
			{"id":"text-embedding-3-small","name":"Embedding","model_picker_enabled":true,
			 "capabilities":{"type":"embeddings","limits":{}}}
		]}`))
	}))
	defer server.Close()

	oldURL := copilotModelsURL
	copilotModelsURL = server.URL
	t.Cleanup(func() { copilotModelsURL = oldURL })

	fetched, err := fetchCopilotModels(context.Background(), "test-token")
	if err != nil {
		t.Fatalf("fetchCopilotModels() error = %v", err)
	}
	if len(fetched) != 2 {
		t.Fatalf("len(fetched) = %d, want 2 (embeddings must be filtered out)", len(fetched))
	}

	mai := fetched[0]
	if want := []string{"/responses"}; len(mai.SupportedEndpoints) != 1 || mai.SupportedEndpoints[0] != want[0] {
		t.Fatalf("SupportedEndpoints = %v, want %v", mai.SupportedEndpoints, want)
	}
	if !mai.CanReason || !mai.SupportsReasoningEffort {
		t.Fatalf("CanReason = %v, SupportsReasoningEffort = %v, want both true", mai.CanReason, mai.SupportsReasoningEffort)
	}
	if mai.SupportsAttachments {
		t.Fatal("SupportsAttachments = true, want false (model reports no vision support)")
	}

	kimi := fetched[1]
	if kimi.CanReason || kimi.SupportsReasoningEffort {
		t.Fatalf("CanReason = %v, SupportsReasoningEffort = %v, want both false", kimi.CanReason, kimi.SupportsReasoningEffort)
	}
	if !kimi.SupportsAttachments {
		t.Fatal("SupportsAttachments = false, want true")
	}
}

func TestFetchOpenRouterModelsParsesCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"openai/gpt-5","name":"GPT-5","context_length":400000,
			 "top_provider":{"context_length":400000,"max_completion_tokens":128000},
			 "architecture":{"input_modalities":["text","image"]},
			 "supported_parameters":["tools","reasoning","include_reasoning"]},
			{"id":"meta/llama-3","name":"Llama 3","context_length":128000,
			 "architecture":{"input_modalities":["text"]},
			 "supported_parameters":["tools"]}
		]}`))
	}))
	defer server.Close()

	oldURL := openRouterModelsURL
	openRouterModelsURL = server.URL
	t.Cleanup(func() { openRouterModelsURL = oldURL })

	fetched, err := fetchOpenRouterModels(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("fetchOpenRouterModels() error = %v", err)
	}
	if len(fetched) != 2 {
		t.Fatalf("len(fetched) = %d, want 2", len(fetched))
	}

	gpt5 := fetched[0]
	if !gpt5.CanReason || !gpt5.SupportsReasoningEffort || !gpt5.SupportsAttachments {
		t.Fatalf("CanReason = %v, SupportsReasoningEffort = %v, SupportsAttachments = %v, want all true",
			gpt5.CanReason, gpt5.SupportsReasoningEffort, gpt5.SupportsAttachments)
	}
	if gpt5.MaxOutputTokens != 128_000 {
		t.Fatalf("MaxOutputTokens = %d, want %d", gpt5.MaxOutputTokens, 128_000)
	}

	llama := fetched[1]
	if llama.CanReason || llama.SupportsAttachments {
		t.Fatalf("CanReason = %v, SupportsAttachments = %v, want both false", llama.CanReason, llama.SupportsAttachments)
	}
}

func TestModelFromFetchedAccountModelCarriesCapabilities(t *testing.T) {
	params := AccountModelRefreshParams{
		ProviderType: ProviderCopilot,
		AccountID:    "acct",
	}
	fetched := FetchedModel{
		ID:                      "mai-code-1-flash-picker",
		Name:                    "MAI-Code-1-Flash",
		ContextWindow:           256_000,
		MaxOutputTokens:         128_000,
		SupportedEndpoints:      []string{"/responses"},
		CanReason:               true,
		SupportsReasoningEffort: true,
	}

	model := modelFromFetchedAccountModel(context.Background(), params, fetched)

	if !model.CanReason || !model.SupportsReasoningEffort {
		t.Fatalf("CanReason = %v, SupportsReasoningEffort = %v, want both true", model.CanReason, model.SupportsReasoningEffort)
	}
	if !CopilotModelUsesResponsesAPI(model) {
		t.Fatal("CopilotModelUsesResponsesAPI() = false, want true")
	}
}
