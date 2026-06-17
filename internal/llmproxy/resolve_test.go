package llmproxy

import (
	"encoding/json"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/message"
)

func TestSchemaToProxyTool(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{"type": "string"},
			"unit":     map[string]interface{}{"type": "string"},
		},
		"required": []interface{}{"location"},
	}

	tool := schemaToProxyTool("get_weather", "Get the weather", schema)
	info := tool.Info()

	if info.Name != "get_weather" {
		t.Fatalf("expected name get_weather, got %q", info.Name)
	}
	if len(info.Parameters) != 2 {
		t.Fatalf("expected 2 properties, got %d", len(info.Parameters))
	}
	if len(info.Required) != 1 || info.Required[0] != "location" {
		t.Fatalf("expected required [location], got %v", info.Required)
	}
}

func TestStripAccountPrefix(t *testing.T) {
	acc := config.ProviderAccount{ID: "work", Type: models.ProviderOpenAI}

	if got, ok := stripAccountPrefix("openai.gpt-4o", acc); !ok || got != "gpt-4o" {
		t.Errorf("provider prefix: got %q ok=%v", got, ok)
	}
	if got, ok := stripAccountPrefix("work.gpt-4o", acc); !ok || got != "gpt-4o" {
		t.Errorf("account prefix: got %q ok=%v", got, ok)
	}
	if got, ok := stripAccountPrefix("gpt-4o", acc); ok || got != "gpt-4o" {
		t.Errorf("no prefix: got %q ok=%v", got, ok)
	}
}

func TestSynthModel(t *testing.T) {
	acc := config.ProviderAccount{ID: "a1", Type: models.ProviderOpenAI}
	m := synthModel(acc, "some-new-model")
	if m.Provider != models.ProviderOpenAI {
		t.Errorf("provider = %v", m.Provider)
	}
	if m.APIModel != "some-new-model" {
		t.Errorf("apiModel = %q", m.APIModel)
	}
	if m.ID != "openai.some-new-model" {
		t.Errorf("id = %q", m.ID)
	}
	if m.DefaultMaxTokens <= 0 || m.ContextWindow <= 0 {
		t.Errorf("expected sane defaults, got maxTokens=%d ctx=%d", m.DefaultMaxTokens, m.ContextWindow)
	}
}

func TestToolInputJSON(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "{}"},
		{"   ", "{}"},
		{`{"a":1}`, `{"a":1}`},
		{"not json", "{}"},
	}
	for _, c := range cases {
		got := string(toolInputJSON(c.in))
		if got != c.want {
			t.Errorf("toolInputJSON(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAnthropicSystemString(t *testing.T) {
	if got := anthropicSystemString(json.RawMessage(`"hello"`)); got != "hello" {
		t.Errorf("plain string: got %q", got)
	}
	arr := json.RawMessage(`[{"type":"text","text":"a"},{"type":"text","text":"b"}]`)
	if got := anthropicSystemString(arr); got != "ab" {
		t.Errorf("array: got %q", got)
	}
	if got := anthropicSystemString(nil); got != "" {
		t.Errorf("nil: got %q", got)
	}
}

func TestAnthropicMessagesToInternal(t *testing.T) {
	msgs := []anthropicInputMessage{
		{Role: "user", Content: json.RawMessage(`"hi"`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"calling"},{"type":"tool_use","id":"t1","name":"foo","input":{"x":1}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"t1","content":"done"},{"type":"text","text":"thanks"}]`)},
	}

	internal := anthropicMessagesToInternal(msgs)

	// Expect: user(hi), assistant(text+toolcall), tool(result), user(thanks)
	if len(internal) != 4 {
		t.Fatalf("expected 4 internal messages, got %d", len(internal))
	}
	if internal[0].Role != message.User {
		t.Errorf("msg0 role = %v", internal[0].Role)
	}
	if internal[1].Role != message.Assistant {
		t.Errorf("msg1 role = %v", internal[1].Role)
	}
	// assistant must contain a tool call
	foundToolCall := false
	for _, p := range internal[1].Parts {
		if _, ok := p.(message.ToolCall); ok {
			foundToolCall = true
		}
	}
	if !foundToolCall {
		t.Errorf("assistant message missing tool call")
	}
	if internal[2].Role != message.Tool {
		t.Errorf("msg2 role = %v (want tool)", internal[2].Role)
	}
	if internal[3].Role != message.User {
		t.Errorf("msg3 role = %v (want user)", internal[3].Role)
	}
}

func TestOpenAIMessagesToInternalToolFlow(t *testing.T) {
	msgs := []openAIChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []openAIToolCall{
			{ID: "c1", Type: "function", Function: openAIFunctionCall{Name: "foo", Arguments: `{"a":1}`}},
		}},
		{Role: "tool", ToolCallID: "c1", Content: "result"},
	}

	internal := openAIMessagesToInternal(msgs)
	// system skipped -> user, assistant, tool
	if len(internal) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(internal))
	}
	if internal[2].Role != message.Tool {
		t.Errorf("expected tool role, got %v", internal[2].Role)
	}
}

func TestOpenAIFinishReason(t *testing.T) {
	if got := openAIFinishReason(message.FinishReasonToolUse, false); got != "tool_calls" {
		t.Errorf("tool_use => %q", got)
	}
	if got := openAIFinishReason(message.FinishReasonMaxTokens, false); got != "length" {
		t.Errorf("max_tokens => %q", got)
	}
	if got := openAIFinishReason(message.FinishReasonEndTurn, false); got != "stop" {
		t.Errorf("end_turn => %q", got)
	}
	if got := openAIFinishReason(message.FinishReasonUnknown, true); got != "tool_calls" {
		t.Errorf("unknown+tools => %q", got)
	}
}
