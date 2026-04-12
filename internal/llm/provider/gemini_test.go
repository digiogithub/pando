package provider

import (
	"encoding/json"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/message"
	"google.golang.org/genai"
)

func TestGeminiBuildThinkingConfig(t *testing.T) {
	tests := []struct {
		name       string
		apiModel   string
		wantNil    bool
		wantBudget *int32
		wantLevel  genai.ThinkingLevel
	}{
		{
			name:      "gemini 3.1 pro uses high thinking level",
			apiModel:  "gemini-3.1-pro-preview-customtools",
			wantLevel: genai.ThinkingLevelHigh,
		},
		{
			name:      "gemini 3.1 pro base uses high thinking level",
			apiModel:  "gemini-3.1-pro-preview",
			wantLevel: genai.ThinkingLevelHigh,
		},
		{
			name:      "gemini 3 pro preview uses high thinking level",
			apiModel:  "gemini-3-pro-preview",
			wantLevel: genai.ThinkingLevelHigh,
		},
		{
			name:       "gemini 3 flash uses thinking budget",
			apiModel:   "gemini-3-flash-preview",
			wantBudget: int32Ptr(8192),
		},
		{
			name:       "gemini 3 flash with models prefix uses thinking budget",
			apiModel:   "models/gemini-3-flash-preview",
			wantBudget: int32Ptr(8192),
		},
		{
			name:       "gemini 3.1 flash lite uses thinking budget",
			apiModel:   "gemini-3.1-flash-lite-preview",
			wantBudget: int32Ptr(8192),
		},
		{
			name:       "gemini 2.5 pro uses thinking budget",
			apiModel:   "gemini-2.5-pro",
			wantBudget: int32Ptr(2000),
		},
		{
			name:       "gemini 2.5 flash uses thinking budget",
			apiModel:   "gemini-2.5-flash",
			wantBudget: int32Ptr(2000),
		},
		{
			name:    "gemini 2.5 flash-lite has no thinking config",
			apiModel: "gemini-2.5-flash-lite",
			wantNil: true,
		},
		{
			name:    "older gemini 2.0 model has no thinking config",
			apiModel: "gemini-2.0-flash",
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &geminiClient{
				providerOptions: providerClientOptions{
					model: models.Model{APIModel: tc.apiModel},
				},
			}

			got := client.buildThinkingConfig()
			if tc.wantNil {
				if got != nil {
					t.Fatalf("buildThinkingConfig() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("buildThinkingConfig() = nil, want non-nil")
			}
			if !got.IncludeThoughts {
				t.Fatal("buildThinkingConfig().IncludeThoughts = false, want true")
			}
			if tc.wantBudget != nil {
				if got.ThinkingBudget == nil {
					t.Fatal("buildThinkingConfig().ThinkingBudget = nil, want non-nil")
				}
				if *got.ThinkingBudget != *tc.wantBudget {
					t.Fatalf("buildThinkingConfig().ThinkingBudget = %d, want %d", *got.ThinkingBudget, *tc.wantBudget)
				}
				return
			}
			if got.ThinkingLevel != tc.wantLevel {
				t.Fatalf("buildThinkingConfig().ThinkingLevel = %v, want %v", got.ThinkingLevel, tc.wantLevel)
			}
		})
	}
}

func TestVisibleTextPartsFiltersThoughtContent(t *testing.T) {
	parts := []*genai.Part{
		{Text: "visible-1", Thought: false},
		{Text: "hidden-thought", Thought: true},
		{Text: "", Thought: false},
		{Text: "visible-2", Thought: false},
		nil,
	}

	got := visibleTextParts(parts)
	if len(got) != 2 {
		t.Fatalf("len(visibleTextParts()) = %d, want 2", len(got))
	}
	if got[0] != "visible-1" || got[1] != "visible-2" {
		t.Fatalf("visibleTextParts() = %v, want [visible-1 visible-2]", got)
	}

	joined := joinVisibleTextParts(parts)
	if joined != "visible-1visible-2" {
		t.Fatalf("joinVisibleTextParts() = %q, want %q", joined, "visible-1visible-2")
	}
}

func TestGeminiConvertMessagesPreservesToolCallMetadata(t *testing.T) {
	client := &geminiClient{}
	thoughtSignature := []byte("sig-123")
	messages := []message.Message{
		{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.ToolCall{
					ID:               "tool-call-id",
					Name:             "code_list_projects",
					Input:            `{"project_id":"pando"}`,
					Type:             "function",
					Finished:         true,
					ThoughtSignature: thoughtSignature,
				},
			},
		},
	}

	contents := client.convertMessages(messages)
	if len(contents) != 1 {
		t.Fatalf("len(convertMessages()) = %d, want 1", len(contents))
	}
	if len(contents[0].Parts) != 1 {
		t.Fatalf("len(convertMessages()[0].Parts) = %d, want 1", len(contents[0].Parts))
	}

	part := contents[0].Parts[0]
	if part.FunctionCall == nil {
		t.Fatal("convertMessages()[0].Parts[0].FunctionCall = nil, want non-nil")
	}
	if part.FunctionCall.ID != "tool-call-id" {
		t.Fatalf("FunctionCall.ID = %q, want %q", part.FunctionCall.ID, "tool-call-id")
	}
	if string(part.ThoughtSignature) != string(thoughtSignature) {
		t.Fatalf("ThoughtSignature = %q, want %q", string(part.ThoughtSignature), string(thoughtSignature))
	}
}

func TestGeminiToolCallsPreserveThoughtSignature(t *testing.T) {
	client := &geminiClient{}
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{
							FunctionCall: &genai.FunctionCall{
								ID:   "gemini-call-id",
								Name: "code_list_projects",
								Args: map[string]any{"project_id": "pando"},
							},
							ThoughtSignature: []byte("sig-456"),
						},
					},
				},
			},
		},
	}

	toolCalls := client.toolCalls(resp)
	if len(toolCalls) != 1 {
		t.Fatalf("len(toolCalls()) = %d, want 1", len(toolCalls))
	}
	if toolCalls[0].ID != "gemini-call-id" {
		t.Fatalf("ToolCall.ID = %q, want %q", toolCalls[0].ID, "gemini-call-id")
	}
	if string(toolCalls[0].ThoughtSignature) != "sig-456" {
		t.Fatalf("ToolCall.ThoughtSignature = %q, want %q", string(toolCalls[0].ThoughtSignature), "sig-456")
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(toolCalls[0].Input), &args); err != nil {
		t.Fatalf("json.Unmarshal(toolCalls[0].Input) error = %v", err)
	}
	if args["project_id"] != "pando" {
		t.Fatalf("toolCalls()[0].Input project_id = %v, want %q", args["project_id"], "pando")
	}
}
