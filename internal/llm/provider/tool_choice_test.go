package provider

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/openai/openai-go"
	"google.golang.org/genai"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	toolsPkg "github.com/digiogithub/pando/internal/llm/tools"
)

// toolChoiceJSON marshals v and returns its "tool_choice" member decoded as
// a generic value (nil when absent).
func toolChoiceJSON(t *testing.T, v any, key string) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m[key]
}

func classifyTool() toolsPkg.BaseTool {
	return testTool{info: toolsPkg.ToolInfo{Name: "classify", Parameters: map[string]any{}}}
}

var toolChoiceCases = []struct {
	name string
	tc   *ToolChoice
}{
	{"unset", nil},
	{"auto", &ToolChoice{Mode: ToolChoiceAuto}},
	{"none", &ToolChoice{Mode: ToolChoiceNone}},
	{"required", &ToolChoice{Mode: ToolChoiceRequired}},
	{"function", &ToolChoice{Mode: ToolChoiceFunction, Name: "classify"}},
}

func TestOpenAIPreparedParamsToolChoice(t *testing.T) {
	want := map[string]any{
		"unset":    nil,
		"auto":     "auto",
		"none":     "none",
		"required": "required",
		"function": map[string]any{"type": "function", "function": map[string]any{"name": "classify"}},
	}
	for _, c := range toolChoiceCases {
		t.Run(c.name, func(t *testing.T) {
			o := &openaiClient{providerOptions: providerClientOptions{
				model: models.Model{APIModel: "gpt-4o"}, maxTokens: 100, toolChoice: c.tc,
			}}
			params := o.preparedParams(nil, o.convertTools([]toolsPkg.BaseTool{classifyTool()}))
			if got := toolChoiceJSON(t, params, "tool_choice"); !reflect.DeepEqual(got, want[c.name]) {
				t.Fatalf("tool_choice = %#v, want %#v", got, want[c.name])
			}
		})
	}
}

func TestOpenAIPreparedParamsOmitsToolChoiceWithoutTools(t *testing.T) {
	o := &openaiClient{providerOptions: providerClientOptions{
		model: models.Model{APIModel: "gpt-4o"}, maxTokens: 100,
		toolChoice: &ToolChoice{Mode: ToolChoiceRequired},
	}}
	params := o.preparedParams(nil, nil)
	if got := toolChoiceJSON(t, params, "tool_choice"); got != nil {
		t.Fatalf("tool_choice = %#v, want omitted when no tools are sent", got)
	}
}

func TestCopilotPreparedParamsToolChoice(t *testing.T) {
	c := &copilotClient{providerOptions: providerClientOptions{
		model: models.Model{APIModel: "gpt-4o"}, maxTokens: 100,
		toolChoice: &ToolChoice{Mode: ToolChoiceFunction, Name: "classify"},
	}}
	params := c.preparedParams(nil, []openai.ChatCompletionToolParam{{Function: openai.FunctionDefinitionParam{Name: "classify"}}})
	want := map[string]any{"type": "function", "function": map[string]any{"name": "classify"}}
	if got := toolChoiceJSON(t, params, "tool_choice"); !reflect.DeepEqual(got, want) {
		t.Fatalf("tool_choice = %#v, want %#v", got, want)
	}
}

func TestResponsesToolChoiceParam(t *testing.T) {
	want := map[string]any{
		"auto":     "auto",
		"none":     "none",
		"required": "required",
		"function": map[string]any{"type": "function", "name": "classify"},
	}
	for _, c := range toolChoiceCases {
		if c.tc == nil {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			b, err := json.Marshal(responsesToolChoiceParam(c.tc))
			if err != nil {
				t.Fatal(err)
			}
			var got any
			_ = json.Unmarshal(b, &got)
			if !reflect.DeepEqual(got, want[c.name]) {
				t.Fatalf("tool_choice = %s, want %#v", b, want[c.name])
			}
		})
	}
}

func TestAnthropicPreparedMessagesToolChoice(t *testing.T) {
	want := map[string]any{
		"unset":    nil,
		"auto":     map[string]any{"type": "auto"},
		"none":     map[string]any{"type": "none"},
		"required": map[string]any{"type": "any"},
		"function": map[string]any{"type": "tool", "name": "classify"},
	}
	for _, c := range toolChoiceCases {
		t.Run(c.name, func(t *testing.T) {
			a := &anthropicClient{providerOptions: providerClientOptions{
				model: models.Model{APIModel: "claude-3-7-sonnet-20250219"}, maxTokens: 1000, toolChoice: c.tc,
			}}
			params := a.preparedMessages(nil, a.convertTools([]toolsPkg.BaseTool{classifyTool()}))
			if got := toolChoiceJSON(t, params, "tool_choice"); !reflect.DeepEqual(got, want[c.name]) {
				t.Fatalf("tool_choice = %#v, want %#v", got, want[c.name])
			}
			beta := a.preparedMessagesBeta(nil, a.convertToolsBeta([]toolsPkg.BaseTool{classifyTool()}))
			if got := toolChoiceJSON(t, beta, "tool_choice"); !reflect.DeepEqual(got, want[c.name]) {
				t.Fatalf("beta tool_choice = %#v, want %#v", got, want[c.name])
			}
		})
	}
}

// Anthropic rejects a forced tool choice together with extended thinking, so
// a forcing choice must switch thinking off for the request.
func TestAnthropicForcedToolChoiceDisablesThinking(t *testing.T) {
	newClient := func(tc *ToolChoice) *anthropicClient {
		return &anthropicClient{
			providerOptions: providerClientOptions{
				model: models.Model{APIModel: "claude-3-7-sonnet-20250219"}, maxTokens: 1000, toolChoice: tc,
			},
			options: anthropicOptions{thinkingMode: config.ThinkingMedium},
		}
	}
	thinking := newClient(&ToolChoice{Mode: ToolChoiceAuto})
	if thinking.thinkingBudget() == 0 {
		t.Fatal("auto tool choice must keep thinking on")
	}
	for _, tc := range []*ToolChoice{{Mode: ToolChoiceRequired}, {Mode: ToolChoiceFunction, Name: "classify"}} {
		a := newClient(tc)
		if got := a.thinkingBudget(); got != 0 {
			t.Fatalf("%s: thinking budget = %d, want 0", tc, got)
		}
		params := a.preparedMessages(nil, a.convertTools([]toolsPkg.BaseTool{classifyTool()}))
		if got := toolChoiceJSON(t, params, "thinking"); got != nil {
			t.Fatalf("%s: thinking = %#v, want omitted", tc, got)
		}
	}
}

func TestGeminiToolConfig(t *testing.T) {
	if geminiToolConfig(nil) != nil {
		t.Fatal("nil tool choice must not set a tool config")
	}
	cases := map[string]struct {
		mode  genai.FunctionCallingConfigMode
		names []string
	}{
		"auto":     {genai.FunctionCallingConfigModeAuto, nil},
		"none":     {genai.FunctionCallingConfigModeNone, nil},
		"required": {genai.FunctionCallingConfigModeAny, nil},
		"function": {genai.FunctionCallingConfigModeAny, []string{"classify"}},
	}
	for _, c := range toolChoiceCases {
		if c.tc == nil {
			continue
		}
		got := geminiToolConfig(c.tc).FunctionCallingConfig
		w := cases[c.name]
		if got.Mode != w.mode || !reflect.DeepEqual(got.AllowedFunctionNames, w.names) {
			t.Fatalf("%s: got mode=%s names=%v, want mode=%s names=%v", c.name, got.Mode, got.AllowedFunctionNames, w.mode, w.names)
		}
	}
}

func TestToolChoiceSupportFor(t *testing.T) {
	cases := map[models.ModelProvider]ToolChoiceSupport{
		models.ProviderOpenAI:           ToolChoiceNative,
		models.ProviderAnthropic:        ToolChoiceNative,
		models.ProviderBedrock:          ToolChoiceNative,
		models.ProviderGemini:           ToolChoiceNative,
		models.ProviderVertexAI:         ToolChoiceNative,
		models.ProviderCopilot:          ToolChoiceNative,
		models.ProviderAzure:            ToolChoiceNative,
		models.ProviderOllama:           ToolChoiceEmulated,
		models.ProviderOpenAICompatible: ToolChoiceForwarded,
		models.ProviderLlamaCpp:         ToolChoiceForwarded,
		models.ProviderLocal:            ToolChoiceForwarded,
	}
	for p, want := range cases {
		if got := ToolChoiceSupportFor(p); got != want {
			t.Errorf("%s: support = %s, want %s", p, got, want)
		}
	}
}

func TestEmulateToolChoice(t *testing.T) {
	ts := []string{"a", "classify", "b"}
	id := func(s string) string { return s }
	cases := []struct {
		tc    *ToolChoice
		want  []string
		exact bool
	}{
		{nil, ts, true},
		{&ToolChoice{Mode: ToolChoiceAuto}, ts, true},
		{&ToolChoice{Mode: ToolChoiceNone}, nil, true},
		{&ToolChoice{Mode: ToolChoiceRequired}, ts, false},
		{&ToolChoice{Mode: ToolChoiceFunction, Name: "classify"}, []string{"classify"}, false},
	}
	for _, c := range cases {
		got, exact := EmulateToolChoice(c.tc, ts, id)
		if !reflect.DeepEqual(got, c.want) || exact != c.exact {
			t.Errorf("%s: got %v exact=%v, want %v exact=%v", c.tc, got, exact, c.want, c.exact)
		}
	}
}
