package provider

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	toolsPkg "github.com/digiogithub/pando/internal/llm/tools"
)

type testTool struct {
	info toolsPkg.ToolInfo
}

func (t testTool) Info() toolsPkg.ToolInfo {
	return t.info
}

func (t testTool) Run(_ context.Context, _ toolsPkg.ToolCall) (toolsPkg.ToolResponse, error) {
	return toolsPkg.NewTextResponse("ok"), nil
}

func TestThinkingBudgetTokensReasoningEffort(t *testing.T) {
	tests := []struct {
		name            string
		mode            config.ThinkingMode
		reasoningEffort string
		maxTokens       int64
		want            int64
	}{
		{
			name:            "low effort maps to 20 percent",
			reasoningEffort: "low",
			maxTokens:       1000,
			want:            200,
		},
		{
			name:            "medium effort maps to 40 percent",
			reasoningEffort: "medium",
			maxTokens:       1000,
			want:            400,
		},
		{
			name:            "high effort maps to 80 percent",
			reasoningEffort: "high",
			maxTokens:       1000,
			want:            800,
		},
		{
			name:            "max effort maps to max tokens minus one",
			reasoningEffort: "max",
			maxTokens:       1000,
			want:            999,
		},
		{
			name:            "unknown effort falls back to thinking mode",
			mode:            config.ThinkingMedium,
			reasoningEffort: "unknown",
			maxTokens:       1000,
			want:            2048,
		},
		{
			name:            "low effort is clamped to max tokens minus one",
			reasoningEffort: "low",
			maxTokens:       2,
			want:            1,
		},
		{
			name:      "disabled mode with empty effort returns zero",
			maxTokens: 1000,
			want:      0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := thinkingBudgetTokens(tc.mode, tc.reasoningEffort, tc.maxTokens)
			if got != tc.want {
				t.Fatalf("thinkingBudgetTokens() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestAdaptiveThinkingModelDetection(t *testing.T) {
	for _, apiModel := range []string{
		"claude-sonnet-4-6",
		"claude-opus-4-6",
		"claude-sonnet-4.6",
		"claude-opus-4.6",
		"CLAUDE-SONNET-4-6",
	} {
		if !isAdaptiveThinkingModel(apiModel) {
			t.Fatalf("isAdaptiveThinkingModel(%q) = false, want true", apiModel)
		}
	}

	if isAdaptiveThinkingModel("claude-3-7-sonnet-20250219") {
		t.Fatal("isAdaptiveThinkingModel(claude-3-7-sonnet-20250219) = true, want false")
	}
}

func TestPreparedMessagesThinkingSelection(t *testing.T) {
	nonAdaptive := &anthropicClient{
		providerOptions: providerClientOptions{
			model:     models.Model{APIModel: "claude-3-7-sonnet-20250219"},
			maxTokens: 1000,
		},
		options: anthropicOptions{
			reasoningEffort: "medium",
		},
	}

	params := nonAdaptive.preparedMessages(nil, nil)
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal(preparedMessages non-adaptive): %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal(preparedMessages non-adaptive): %v", err)
	}
	if _, ok := body["thinking"]; !ok {
		t.Fatal("preparedMessages() for non-adaptive model missing thinking config")
	}
	if got, ok := body["temperature"].(float64); !ok || got != 1 {
		t.Fatalf("preparedMessages() non-adaptive temperature = %v, want 1", body["temperature"])
	}

	adaptive := &anthropicClient{
		providerOptions: providerClientOptions{
			model:     models.Model{APIModel: "claude-sonnet-4-6"},
			maxTokens: 1000,
		},
		options: anthropicOptions{
			reasoningEffort: "medium",
		},
	}

	adaptiveParams := adaptive.preparedMessages(nil, nil)
	adaptiveRaw, err := json.Marshal(adaptiveParams)
	if err != nil {
		t.Fatalf("json.Marshal(preparedMessages adaptive): %v", err)
	}

	var adaptiveBody map[string]any
	if err := json.Unmarshal(adaptiveRaw, &adaptiveBody); err != nil {
		t.Fatalf("json.Unmarshal(preparedMessages adaptive): %v", err)
	}
	if _, ok := adaptiveBody["thinking"]; ok {
		t.Fatal("preparedMessages() for adaptive model should not include static thinking block")
	}
	if got, ok := adaptiveBody["temperature"].(float64); !ok || got != 1 {
		t.Fatalf("preparedMessages() adaptive temperature = %v, want 1", adaptiveBody["temperature"])
	}
}

func TestThinkingRequestOptionsAdaptiveOnly(t *testing.T) {
	adaptive := &anthropicClient{
		providerOptions: providerClientOptions{
			model:     models.Model{APIModel: "claude-sonnet-4-6"},
			maxTokens: 1000,
		},
		options: anthropicOptions{
			reasoningEffort: "medium",
		},
	}
	if got := adaptive.thinkingRequestOptions(); len(got) == 0 {
		t.Fatal("thinkingRequestOptions() adaptive = empty, want non-empty")
	}

	nonAdaptive := &anthropicClient{
		providerOptions: providerClientOptions{
			model:     models.Model{APIModel: "claude-3-7-sonnet-20250219"},
			maxTokens: 1000,
		},
		options: anthropicOptions{
			reasoningEffort: "medium",
		},
	}
	if got := nonAdaptive.thinkingRequestOptions(); len(got) != 0 {
		t.Fatalf("thinkingRequestOptions() non-adaptive len = %d, want 0", len(got))
	}
}

func TestConvertToolsNoCacheWhenDisabled(t *testing.T) {
	client := &anthropicClient{
		options: anthropicOptions{
			disableCache: true,
		},
	}
	tools := []toolsPkg.BaseTool{
		testTool{info: toolsPkg.ToolInfo{
			Name:        "test_tool",
			Description: "A test tool",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		}},
	}
	result := client.convertTools(tools)
	if len(result) == 0 {
		t.Fatal("expected at least one tool")
	}
	// When disableCache is true, the last tool should NOT have CacheControl set.
	lastTool := result[len(result)-1]
	if lastTool.OfTool != nil && lastTool.OfTool.CacheControl.Type != "" {
		t.Error("expected no CacheControl when disableCache is true")
	}
}

func TestConvertToolsWithCacheEnabled(t *testing.T) {
	client := &anthropicClient{
		options: anthropicOptions{
			disableCache: false,
		},
	}
	tools := []toolsPkg.BaseTool{
		testTool{info: toolsPkg.ToolInfo{
			Name:        "test_tool",
			Description: "A test tool",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		}},
	}
	result := client.convertTools(tools)
	if len(result) == 0 {
		t.Fatal("expected at least one tool")
	}
	// When disableCache is false, the last tool SHOULD have CacheControl set to ephemeral.
	lastTool := result[len(result)-1]
	if lastTool.OfTool == nil || lastTool.OfTool.CacheControl.Type != "ephemeral" {
		t.Error("expected CacheControl=ephemeral on last tool when cache enabled")
	}
}

func TestConvertToolsPassesRequiredFields(t *testing.T) {
	client := &anthropicClient{
		options: anthropicOptions{
			disableCache: true,
		},
	}

	required := []string{"query", "limit"}
	converted := client.convertTools([]toolsPkg.BaseTool{
		testTool{
			info: toolsPkg.ToolInfo{
				Name:        "search",
				Description: "Search for documents",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
						"limit": map[string]any{"type": "integer"},
					},
				},
				Required: required,
			},
		},
	})

	if len(converted) != 1 {
		t.Fatalf("len(convertTools()) = %d, want 1", len(converted))
	}
	if converted[0].OfTool == nil {
		t.Fatal("convertTools()[0].OfTool = nil, want non-nil")
	}
	if !reflect.DeepEqual(converted[0].OfTool.InputSchema.Required, required) {
		t.Fatalf("convertTools()[0].OfTool.InputSchema.Required = %v, want %v", converted[0].OfTool.InputSchema.Required, required)
	}
}
