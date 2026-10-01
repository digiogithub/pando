package acp

import (
	"bytes"
	"context"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/message"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

const testRoutingNotice = "Auto: code → claude-sonnet (p=0.93, 38 ms via ollama/tev1:0.8b)"

func TestReplayIncludesRoutingNotice(t *testing.T) {
	enableAutoModeForTest(t, true)
	ctx := context.Background()
	sessions := newMockSessionService()
	svc := &mockAgentService{currentModel: "coder-model"}
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.New(io.Discard, "", 0), svc, sessions, nil)

	var updates bytes.Buffer
	agent.conn = acpsdk.NewAgentSideConnection(NewSimpleACPAgent("1.0.0-test", log.New(io.Discard, "", 0)), &updates, bytes.NewReader(nil))

	sid := "pando-session-1"
	sessions.sessions[sid] = ACPSessionInfo{ID: sid, Title: "t"}
	sessions.messages = map[string][]message.Message{sid: {
		{ID: "u1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "hello"}}},
	}}

	// A live routed turn records the notice on the session (and persists it).
	acpSession := NewACPServerSession(acpsdk.SessionId(sid), "/tmp", agent.conn, sid)
	agent.sessions[acpsdk.SessionId(sid)] = acpSession
	events := make(chan AgentEvent, 2)
	events <- AgentEvent{Type: AgentEventTypeSystemMessage, SystemMessage: testRoutingNotice + "\n", Routing: true}
	close(events)
	if _, err := agent.processAgentEventStream(ctx, acpSession, events); err != nil {
		t.Fatalf("processAgentEventStream: %v", err)
	}
	live := decodeSessionUpdateRecords(t, updates.String())
	foundLive := false
	for _, r := range live {
		if r.Kind == "agent_message_chunk" && strings.Contains(r.Text, "Auto: code") {
			foundLive = true
		}
	}
	if !foundLive {
		t.Fatalf("routing notice not forwarded live: %+v", live)
	}

	// A fresh agent loads the session: the persisted notice is replayed.
	updates.Reset()
	agent2 := NewPandoACPAgent("1.0.0-test", "/tmp", log.New(io.Discard, "", 0), &mockAgentService{currentModel: "coder-model"}, sessions, nil)
	agent2.conn = acpsdk.NewAgentSideConnection(NewSimpleACPAgent("1.0.0-test", log.New(io.Discard, "", 0)), &updates, bytes.NewReader(nil))
	if _, err := agent2.LoadSession(ctx, acpsdk.LoadSessionRequest{SessionId: acpsdk.SessionId(sid), Cwd: "/tmp"}); err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	restored, err := agent2.getSession(acpsdk.SessionId(sid))
	if err != nil {
		t.Fatal(err)
	}
	agent2.streamSessionHistory(ctx, acpsdk.SessionId(sid), sid)

	var texts []string
	for _, r := range decodeSessionUpdateRecords(t, updates.String()) {
		if r.Kind == "agent_message_chunk" {
			texts = append(texts, r.Text)
		}
	}
	if len(texts) != 2 || texts[0] != "hello" || !strings.HasPrefix(texts[1], "Auto: code") {
		t.Fatalf("replayed agent chunks = %q (session notice %q)", texts, restored.RoutingNotice())
	}
}

const testContextFilterNotice = "Context filter: kept 4/9 (38 ms) — code 1/3, kb 2/4, events 1/2"

func TestContextFilterNoticeForwardedAsAgentMessage(t *testing.T) {
	ctx := context.Background()
	sessions := newMockSessionService()
	svc := &mockAgentService{currentModel: "coder-model"}
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.New(io.Discard, "", 0), svc, sessions, nil)
	var updates bytes.Buffer
	agent.conn = acpsdk.NewAgentSideConnection(NewSimpleACPAgent("1.0.0-test", log.New(io.Discard, "", 0)), &updates, bytes.NewReader(nil))

	sid := "pando-session-cf"
	sessions.sessions[sid] = ACPSessionInfo{ID: sid, Title: "t"}
	acpSession := NewACPServerSession(acpsdk.SessionId(sid), "/tmp", agent.conn, sid)
	agent.sessions[acpsdk.SessionId(sid)] = acpSession

	events := make(chan AgentEvent, 2)
	events <- AgentEvent{Type: AgentEventTypeSystemMessage, SystemMessage: testContextFilterNotice + "\n"}
	close(events)
	if _, err := agent.processAgentEventStream(ctx, acpSession, events); err != nil {
		t.Fatalf("processAgentEventStream: %v", err)
	}
	var texts []string
	for _, r := range decodeSessionUpdateRecords(t, updates.String()) {
		if r.Kind == "agent_message_chunk" {
			texts = append(texts, r.Text)
		}
	}
	if len(texts) != 1 || texts[0] != testContextFilterNotice {
		t.Fatalf("chunks = %q, want the notice verbatim", texts)
	}
	if got := acpSession.RoutingNotice(); got != "" {
		t.Fatalf("context filter notice must not be stored as a routing notice: %q", got)
	}
}
