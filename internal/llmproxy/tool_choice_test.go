package llmproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
)

func TestParseOpenAIToolChoice(t *testing.T) {
	cases := []struct {
		in      string
		want    *provider.ToolChoice
		wantErr bool
	}{
		{``, nil, false},
		{`null`, nil, false},
		{`"auto"`, &provider.ToolChoice{Mode: provider.ToolChoiceAuto}, false},
		{`"none"`, &provider.ToolChoice{Mode: provider.ToolChoiceNone}, false},
		{`"required"`, &provider.ToolChoice{Mode: provider.ToolChoiceRequired}, false},
		{`{"type":"function","function":{"name":"classify"}}`, &provider.ToolChoice{Mode: provider.ToolChoiceFunction, Name: "classify"}, false},
		{`{"type":"function","name":"classify"}`, &provider.ToolChoice{Mode: provider.ToolChoiceFunction, Name: "classify"}, false},
		{`"function"`, nil, true},
		{`"always"`, nil, true},
		{`{"type":"function","function":{}}`, nil, true},
		{`{"type":"custom"}`, nil, true},
		{`42`, nil, true},
	}
	for _, c := range cases {
		got, err := parseOpenAIToolChoice(json.RawMessage(c.in))
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if !c.wantErr && !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseAnthropicToolChoice(t *testing.T) {
	cases := map[string]*provider.ToolChoice{
		`{"type":"auto"}`:                   {Mode: provider.ToolChoiceAuto},
		`{"type":"none"}`:                   {Mode: provider.ToolChoiceNone},
		`{"type":"any"}`:                    {Mode: provider.ToolChoiceRequired},
		`{"type":"tool","name":"classify"}`: {Mode: provider.ToolChoiceFunction, Name: "classify"},
	}
	for in, want := range cases {
		got, err := parseAnthropicToolChoice(json.RawMessage(in))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v err %v, want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{`{"type":"tool"}`, `{"type":"x"}`, `"auto"`} {
		if _, err := parseAnthropicToolChoice(json.RawMessage(bad)); err == nil {
			t.Errorf("%s: want error", bad)
		}
	}
}

func TestPlanToolChoiceValidation(t *testing.T) {
	ts := openAIToolsToBaseTools([]openAIToolDef{{Type: "function", Function: openAIFunctionDef{Name: "classify"}}})
	if _, err := planToolChoice(&provider.ToolChoice{Mode: provider.ToolChoiceRequired}, nil, models.ProviderOpenAI, "m"); err == nil {
		t.Error("required without tools must be rejected")
	}
	if _, err := planToolChoice(&provider.ToolChoice{Mode: provider.ToolChoiceFunction, Name: "other"}, ts, models.ProviderOpenAI, "m"); err == nil {
		t.Error("a named function missing from tools must be rejected")
	}
	plan, err := planToolChoice(nil, ts, models.ProviderOpenAI, "m")
	if err != nil || plan.header != "" || len(plan.opts) != 0 || len(plan.tools) != 1 {
		t.Errorf("no tool choice must leave the request untouched: %+v %v", plan, err)
	}
	plan, err = planToolChoice(&provider.ToolChoice{Mode: provider.ToolChoiceNone}, ts, models.ProviderOllama, "m")
	if err != nil || plan.header != "emulated" || len(plan.tools) != 0 || len(plan.opts) != 0 {
		t.Errorf("ollama none must drop the tools: %+v %v", plan, err)
	}
}

// upstream is a fake OpenAI-compatible server that records the last request
// body and answers with a call to the "classify" tool.
type upstream struct {
	mu   sync.Mutex
	last map[string]any
}

func (u *upstream) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	u.mu.Lock()
	u.last = m
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","created":1,"model":"m",
"choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"",
"tool_calls":[{"id":"call_1","type":"function","function":{"name":"classify","arguments":"{\"level\":3}"}}]}}],
"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
}

func (u *upstream) request() map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.last
}

// proxyWithUpstream points a single provider account of the given type at a
// fake upstream and returns the proxy handler.
func proxyWithUpstream(t *testing.T, providerType models.ModelProvider) (http.Handler, *upstream) {
	t.Helper()
	up := &upstream{}
	srv := httptest.NewServer(http.HandlerFunc(up.handler))
	t.Cleanup(srv.Close)
	config.SetForTests(&config.Config{
		ProviderAccounts: []config.ProviderAccount{{
			ID: "up", Type: providerType, APIKey: "k", BaseURL: srv.URL + "/v1",
		}},
	})
	t.Cleanup(func() { config.SetForTests(nil) })
	mux := http.NewServeMux()
	(&LLMProxyServer{}).registerRoutes(mux)
	return mux, up
}

const chatWithTools = `{"model":"up.test-model","messages":[{"role":"user","content":"classify this"}],
"tools":[{"type":"function","function":{"name":"classify","parameters":{"type":"object","properties":{"level":{"type":"integer"}}}}},
         {"type":"function","function":{"name":"summarise","parameters":{"type":"object","properties":{}}}}],
"tool_choice":%s}`

func postChat(t *testing.T, h http.Handler, toolChoice string) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.Replace(chatWithTools, "%s", toolChoice, 1)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func toolNames(req map[string]any) []string {
	var names []string
	ts, _ := req["tools"].([]any)
	for _, t := range ts {
		fn, _ := t.(map[string]any)["function"].(map[string]any)
		names = append(names, fn["name"].(string))
	}
	return names
}

func TestChatCompletionsForwardsNamedToolChoice(t *testing.T) {
	h, up := proxyWithUpstream(t, models.ProviderOpenAICompatible)
	rec := postChat(t, h, `{"type":"function","function":{"name":"classify"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	want := map[string]any{"type": "function", "function": map[string]any{"name": "classify"}}
	if got := up.request()["tool_choice"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("upstream tool_choice = %#v, want %#v", got, want)
	}
	if got := rec.Header().Get(toolChoiceHeader); got != "forwarded" {
		t.Fatalf("%s = %q, want forwarded", toolChoiceHeader, got)
	}
	var resp openAIChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if calls := resp.Choices[0].Message.ToolCalls; len(calls) != 1 || calls[0].Function.Name != "classify" {
		t.Fatalf("tool calls = %+v, want one call to classify", calls)
	}
}

func TestChatCompletionsForwardsRequiredToolChoice(t *testing.T) {
	h, up := proxyWithUpstream(t, models.ProviderOpenAICompatible)
	if rec := postChat(t, h, `"required"`); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := up.request()["tool_choice"]; got != "required" {
		t.Fatalf("upstream tool_choice = %#v, want required", got)
	}
}

func TestChatCompletionsOmitsToolChoiceWhenAbsent(t *testing.T) {
	h, up := proxyWithUpstream(t, models.ProviderOpenAICompatible)
	if rec := postChat(t, h, `null`); rec.Code != http.StatusOK || rec.Header().Get(toolChoiceHeader) != "" {
		t.Fatalf("status %d header %q", rec.Code, rec.Header().Get(toolChoiceHeader))
	}
	if _, ok := up.request()["tool_choice"]; ok {
		t.Fatal("tool_choice must not be sent when the client did not set it")
	}
}

func TestChatCompletionsRejectsUnknownNamedTool(t *testing.T) {
	h, up := proxyWithUpstream(t, models.ProviderOpenAICompatible)
	rec := postChat(t, h, `{"type":"function","function":{"name":"nope"}}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_tool_choice") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if up.request() != nil {
		t.Fatal("an invalid request must not reach the upstream")
	}
}

// Ollama ignores tool_choice, so the proxy narrows the tools to the named
// one, does not send the field, and tells the client it degraded.
func TestChatCompletionsDegradesNamedToolChoiceOnOllama(t *testing.T) {
	h, up := proxyWithUpstream(t, models.ProviderOllama)
	rec := postChat(t, h, `{"type":"function","function":{"name":"classify"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get(toolChoiceHeader); got != "degraded" {
		t.Fatalf("%s = %q, want degraded", toolChoiceHeader, got)
	}
	req := up.request()
	if _, ok := req["tool_choice"]; ok {
		t.Fatal("tool_choice must not be sent to Ollama")
	}
	if got := toolNames(req); !reflect.DeepEqual(got, []string{"classify"}) {
		t.Fatalf("upstream tools = %v, want [classify]", got)
	}
}

func TestAnthropicMessagesMapsToolChoice(t *testing.T) {
	h, up := proxyWithUpstream(t, models.ProviderOpenAICompatible)
	body := `{"model":"up.test-model","max_tokens":100,"messages":[{"role":"user","content":"classify"}],
"tools":[{"name":"classify","input_schema":{"type":"object","properties":{}}}],
"tool_choice":{"type":"tool","name":"classify"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	want := map[string]any{"type": "function", "function": map[string]any{"name": "classify"}}
	if got := up.request()["tool_choice"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("upstream tool_choice = %#v, want %#v", got, want)
	}
}
