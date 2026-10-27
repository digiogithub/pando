package acp

import (
	"context"
	"log"
	"testing"

	"github.com/digiogithub/pando/internal/mesnada/depth"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// A warm delegation declares its session's nesting level in session/prompt
// _meta; the agent records it per session so the spawn tools inside this
// long-lived instance count one level deeper.
func TestPromptRecordsDelegationDepthFromMeta(t *testing.T) {
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), &mockAgentService{}, newMockSessionService(), nil)
	ctx := context.Background()

	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp", McpServers: []acpsdk.McpServer{}})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	agent.sessionsMu.RLock()
	pandoSessionID := agent.sessions[resp.SessionId].PandoSessionID()
	agent.sessionsMu.RUnlock()
	t.Cleanup(func() { depth.ClearSession(pandoSessionID) })

	if _, err := agent.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: resp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("hello")},
		Meta:      depth.Meta(2),
	}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if got, ok := depth.Session(pandoSessionID); !ok || got != 2 {
		t.Fatalf("recorded depth = (%d,%v), want (2,true)", got, ok)
	}
}
