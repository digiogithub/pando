package bridge

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/mesnada/depth"
	"github.com/digiogithub/pando/internal/message"
)

// An IPC delegation carries the task's nesting level; the target records it for
// the ephemeral session so spawns made from it are counted one level deeper.
func TestAgentDelegationRunner_RecordsSessionDepth(t *testing.T) {
	const sessionID = "sess-depth"
	t.Cleanup(func() { depth.ClearSession(sessionID) })

	sess := &stubSessionCreator{nextID: sessionID}
	agSvc := &stubAgentRunner{result: agent.AgentEvent{
		Type:    agent.AgentEventTypeResponse,
		Message: message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "ok"}}},
	}}
	level := 2
	r := newAgentDelegationRunnerFromDeps(sess, agSvc)
	if _, err := r.RunDelegation(context.Background(), protocol.DelegationRunParams{Prompt: "go", Depth: &level}); err != nil {
		t.Fatalf("RunDelegation: %v", err)
	}
	if got, ok := depth.Session(sessionID); !ok || got != 2 {
		t.Fatalf("session depth = (%d,%v), want (2,true)", got, ok)
	}
}

// An older caller sends no depth: nothing is recorded and the target's own
// process level stays in effect.
func TestAgentDelegationRunner_NoDepthLeavesSessionUnset(t *testing.T) {
	const sessionID = "sess-nodepth"
	sess := &stubSessionCreator{nextID: sessionID}
	agSvc := &stubAgentRunner{result: agent.AgentEvent{
		Type:    agent.AgentEventTypeResponse,
		Message: message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "ok"}}},
	}}
	r := newAgentDelegationRunnerFromDeps(sess, agSvc)
	if _, err := r.RunDelegation(context.Background(), protocol.DelegationRunParams{Prompt: "go"}); err != nil {
		t.Fatalf("RunDelegation: %v", err)
	}
	if _, ok := depth.Session(sessionID); ok {
		t.Fatal("no depth should be recorded for a caller that sent none")
	}
}
