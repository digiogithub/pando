package acp

import (
	"bytes"
	"context"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

func TestAutoModelDescriptionReadsDecisionModel(t *testing.T) {
	enableAutoModeForTest(t, true)
	if got := autoModelDescription(); !strings.HasSuffix(got, "(router: ollama/tev1:0.8b)") {
		t.Fatalf("description = %q, want router: ollama/tev1:0.8b", got)
	}
	// Changing only the shared block changes the description.
	config.Get().DecisionModel.Router = config.DecisionRouterConfig{Provider: config.DecisionProviderCustom, Model: "jev-latest"}
	if got := autoModelDescription(); !strings.Contains(got, "router: custom/jev-latest") {
		t.Fatalf("description = %q", got)
	}
}

func TestDecisionModelCommandAdvertisedAndParsed(t *testing.T) {
	var found bool
	for _, cmd := range availableCommands() {
		if cmd.Name == "decision-model" {
			found = true
			if strings.TrimSpace(cmd.Description) == "" {
				t.Fatal("no description")
			}
		}
	}
	if !found {
		t.Fatal("/decision-model is not advertised")
	}
	c, ok := parseSlashCommand("/decision-model test")
	if !ok || c.Kind != slashCommandDecisionModel || c.Objective != "test" {
		t.Fatalf("parsed = %+v ok=%v", c, ok)
	}
}

func runDecisionModelCommand(t *testing.T, arg string) string {
	t.Helper()
	sessions := newMockSessionService()
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.New(io.Discard, "", 0), &mockAgentService{currentModel: "coder-model"}, sessions, nil)
	var updates bytes.Buffer
	agent.conn = acpsdk.NewAgentSideConnection(NewSimpleACPAgent("1.0.0-test", log.New(io.Discard, "", 0)), &updates, bytes.NewReader(nil))
	sid := "pando-session-dm"
	sessions.sessions[sid] = ACPSessionInfo{ID: sid, Title: "t"}
	acpSession := NewACPServerSession(acpsdk.SessionId(sid), "/tmp", agent.conn, sid)
	agent.sessions[acpsdk.SessionId(sid)] = acpSession

	reason, err := agent.processDecisionModelCommand(context.Background(), acpSession, arg)
	if err != nil || reason != acpsdk.StopReasonEndTurn {
		t.Fatalf("reason=%v err=%v", reason, err)
	}
	var out []string
	for _, r := range decodeSessionUpdateRecords(t, updates.String()) {
		if r.Kind == "agent_message_chunk" {
			out = append(out, r.Text)
		}
	}
	return strings.Join(out, "")
}

func TestDecisionModelCommandShowsStatusWithMaskedKey(t *testing.T) {
	srv := systemonetest.New(t)
	prev := config.Get()
	config.SetForTests(&config.Config{DecisionModel: config.DecisionModelConfig{Router: config.DecisionRouterConfig{
		Provider: config.DecisionProviderOllama, BaseURL: srv.BaseURL(), Model: "tev1:0.8b", APIKey: "sk-secret-1234567890",
	}}})
	t.Cleanup(func() { config.SetForTests(prev) })

	for _, arg := range []string{"", "test"} {
		out := runDecisionModelCommand(t, arg)
		for _, want := range []string{"## Decision model", "provider:  ollama", "model:     tev1:0.8b", "health:    healthy"} {
			if !strings.Contains(out, want) {
				t.Fatalf("arg %q: output missing %q:\n%s", arg, want, out)
			}
		}
		if strings.Contains(out, "sk-secret-1234567890") || !strings.Contains(out, "••••7890") {
			t.Fatalf("API key must be masked:\n%s", out)
		}
	}
	if out := runDecisionModelCommand(t, "bogus"); !strings.Contains(out, "Usage: /decision-model") {
		t.Fatalf("bad argument should print usage: %s", out)
	}
}
