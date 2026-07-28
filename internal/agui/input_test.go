package agui

import (
	"errors"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

func TestDecodeRunAgentInputRequiresIdentifiers(t *testing.T) {
	cases := map[string]string{
		"missing threadId": `{"runId":"r1"}`,
		"missing runId":    `{"threadId":"t1"}`,
		"roleless message": `{"threadId":"t1","runId":"r1","messages":[{"id":"m1","content":"hi"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeRunAgentInput(strings.NewReader(body), 0); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}

func TestDecodeRunAgentInputStringContent(t *testing.T) {
	body := `{
		"threadId":"t1","runId":"r1",
		"messages":[
			{"id":"m1","role":"user","content":"first"},
			{"id":"m2","role":"assistant","content":"answer"},
			{"id":"m3","role":"user","content":"second"}
		],
		"tools":[{"name":"showChart","description":"render a chart"}],
		"context":[{"description":"file","value":"main.go"}]
	}`
	in, err := DecodeRunAgentInput(strings.NewReader(body), 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	msg, ok := in.LastUserMessage()
	if !ok || msg.Content.String() != "second" {
		t.Fatalf("expected the trailing user message, got %q (ok=%v)", msg.Content.String(), ok)
	}
	if got := in.ContextBlock(); !strings.Contains(got, "file: main.go") {
		t.Fatalf("context block missing the entry: %q", got)
	}
	if len(in.Tools) != 1 || in.Tools[0].Name != "showChart" {
		t.Fatalf("frontend tools not decoded: %+v", in.Tools)
	}
}

func TestDecodeRunAgentInputMultimodalContent(t *testing.T) {
	body := `{
		"threadId":"t1","runId":"r1",
		"messages":[{"id":"m1","role":"user","content":[
			{"type":"text","text":"describe"},
			{"type":"image","url":"https://example.test/a.png"},
			{"type":"text","text":"this"}
		]}]
	}`
	in, err := DecodeRunAgentInput(strings.NewReader(body), 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	msg, _ := in.LastUserMessage()
	if got := msg.Content.String(); got != "describe\nthis" {
		t.Fatalf("text parts should be joined, got %q", got)
	}
	if !msg.Content.HasNonTextParts() {
		t.Fatal("expected the image part to be reported as non-text")
	}
}

func TestTrailingToolMessages(t *testing.T) {
	body := `{
		"threadId":"t1","runId":"r1",
		"messages":[
			{"id":"m1","role":"user","content":"go"},
			{"id":"m2","role":"assistant","content":""},
			{"id":"m3","role":"tool","toolCallId":"tc1","content":"done"}
		]
	}`
	in, err := DecodeRunAgentInput(strings.NewReader(body), 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := in.TrailingToolMessages()
	if len(got) != 1 || got[0].ToolCallID != "tc1" {
		t.Fatalf("expected the trailing tool result, got %+v", got)
	}
}

func TestDecodeRunAgentInputRespectsSizeLimit(t *testing.T) {
	body := `{"threadId":"t1","runId":"r1","messages":[{"id":"m1","role":"user","content":"hello"}]}`
	if _, err := DecodeRunAgentInput(strings.NewReader(body), 10); err == nil {
		t.Fatal("expected a truncated payload to fail")
	}
}

func TestConfigFromAppDefaults(t *testing.T) {
	got := ConfigFromApp(config.AGUIConfig{Enabled: true})
	if got.Path != defaultPath {
		t.Fatalf("path = %q, want %q", got.Path, defaultPath)
	}
	if got.AgentPoolSize != defaultPoolSize || got.AgentPoolTTL != defaultPoolTTL {
		t.Fatalf("pool defaults not applied: %+v", got)
	}
	if len(got.Agents) != 1 || got.Agents[0] != config.AgentCoder {
		t.Fatalf("expected the coder agent by default, got %v", got.Agents)
	}
}

func TestConfigFromAppDropsUnknownAgents(t *testing.T) {
	got := ConfigFromApp(config.AGUIConfig{
		Enabled:      true,
		Agents:       []string{"coder", "not-an-agent", "task"},
		AgentPoolTTL: "5m",
	})
	if len(got.Agents) != 2 {
		t.Fatalf("unknown agents must be dropped, got %v", got.Agents)
	}
	if !got.allowsAgent(config.AgentTask) || got.allowsAgent(config.AgentTitle) {
		t.Fatalf("allow-list is wrong: %v", got.Agents)
	}
	if got.AgentPoolTTL.Minutes() != 5 {
		t.Fatalf("TTL = %v, want 5m", got.AgentPoolTTL)
	}
}
