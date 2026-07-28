package agui

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/permission"
)

// newHITLRuntime builds a runtime with a real permission service, which is all
// the approval path touches.
func newHITLRuntime(t *testing.T, cfg Config) (*Runtime, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := newTestRuntime(cfg, "secret")
	r.perms = permission.NewPermissionService()
	r.baseCtx = ctx
	return r, cancel
}

func hitlConfig() Config {
	cfg := testConfig()
	cfg.HumanInTheLoop = true
	return cfg
}

// TestPermissionPromptRoundTrip is the P4 guarantee: an approval raised deep
// inside a tool becomes an AG-UI tool call, and the client's answer is what the
// tool sees.
func TestPermissionPromptRoundTrip(t *testing.T) {
	r, cancel := newHITLRuntime(t, hitlConfig())
	defer cancel()

	notify := r.pending.watch("s1")
	r.installPermissionPolicy("s1")

	verdict := make(chan bool, 1)
	go func() {
		verdict <- r.perms.Request(permission.CreatePermissionRequest{
			SessionID: "s1",
			ToolName:  "bash",
			Action:    "execute",
			Path:      "/repo",
			Params:    map[string]any{"command": "rm -rf build"},
		})
	}()

	var s suspension
	select {
	case s = <-notify:
	case <-time.After(2 * time.Second):
		t.Fatal("the permission request never suspended the run")
	}

	if !strings.HasPrefix(s.callID, "perm-") {
		t.Fatalf("unexpected call id %q", s.callID)
	}
	// A permission prompt has no counterpart in the agent's stream, so the
	// adapter must supply the whole tool call itself.
	if len(s.events) != 3 {
		t.Fatalf("expected START/ARGS/END, got %d events", len(s.events))
	}
	start, ok := s.events[0].(ToolCallStartEvent)
	if !ok || start.ToolCallName != permissionToolName || start.ToolCallID != s.callID {
		t.Fatalf("unexpected start event: %#v", s.events[0])
	}
	args, ok := s.events[1].(ToolCallArgsEvent)
	if !ok {
		t.Fatalf("unexpected args event: %#v", s.events[1])
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(args.Delta), &decoded); err != nil {
		t.Fatalf("arguments are not valid JSON: %v", err)
	}
	if decoded["toolName"] != "bash" || decoded["action"] != "execute" {
		t.Fatalf("the prompt lost its subject: %v", decoded)
	}
	if _, ok := s.events[2].(ToolCallEndEvent); !ok {
		t.Fatalf("unexpected end event: %#v", s.events[2])
	}

	if !r.pending.resolve("s1", s.callID, clientResult(`{"approved":true}`)) {
		t.Fatal("the approval should have found the waiting request")
	}
	select {
	case approved := <-verdict:
		if !approved {
			t.Fatal("the client approved but the tool was denied")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the permission request never unblocked")
	}
}

func TestPermissionAutoApproveSkipsTheClient(t *testing.T) {
	cfg := hitlConfig()
	cfg.AutoApprove = true
	r, cancel := newHITLRuntime(t, cfg)
	defer cancel()

	notify := r.pending.watch("s1")
	r.installPermissionPolicy("s1")

	if !r.perms.Request(permission.CreatePermissionRequest{SessionID: "s1", ToolName: "bash"}) {
		t.Fatal("auto-approve must approve")
	}
	select {
	case s := <-notify:
		t.Fatalf("auto-approve must not bother the client: %+v", s)
	default:
	}
}

// TestPermissionDeniedWithoutHumanInTheLoop preserves the pre-P4 behaviour: with
// nobody able to answer, the request is refused rather than left hanging.
func TestPermissionDeniedWithoutHumanInTheLoop(t *testing.T) {
	cfg := testConfig() // HumanInTheLoop stays false
	r, cancel := newHITLRuntime(t, cfg)
	defer cancel()

	r.installPermissionPolicy("s1")
	if r.perms.Request(permission.CreatePermissionRequest{SessionID: "s1", ToolName: "bash"}) {
		t.Fatal("a request nobody can answer must be denied")
	}
}

// TestPermissionFailsClosedWhenTheAdapterShutsDown covers the case where no
// answer will ever arrive.
func TestPermissionFailsClosedWhenTheAdapterShutsDown(t *testing.T) {
	r, cancel := newHITLRuntime(t, hitlConfig())
	r.installPermissionPolicy("s1")
	cancel()

	done := make(chan bool, 1)
	go func() {
		done <- r.perms.Request(permission.CreatePermissionRequest{SessionID: "s1", ToolName: "bash"})
	}()
	select {
	case approved := <-done:
		if approved {
			t.Fatal("a shutdown must never approve")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the request did not fail closed")
	}
}

func TestApprovalFromMessage(t *testing.T) {
	approvals := []string{`{"approved":true}`, `{"allow":true}`, "approve", "YES", `"allow"`, "true"}
	for _, content := range approvals {
		if !approvalFromMessage(clientResult(content)) {
			t.Fatalf("%q should approve", content)
		}
	}
	denials := []string{`{"approved":false}`, "deny", "", "maybe", "{}", `{"approved":null}`}
	for _, content := range denials {
		if approvalFromMessage(clientResult(content)) {
			t.Fatalf("%q must not approve", content)
		}
	}
	if approvalFromMessage(Message{Role: RoleTool, Error: "boom", Content: MessageContent{Text: "approve"}}) {
		t.Fatal("a client error must never approve")
	}
}

func TestAnswerFromMessage(t *testing.T) {
	structured := clientResult(`{"answers":[
		{"questionId":"q1","header":"Database","selected":["Postgres"]},
		{"questionId":"q2","header":"Cache","selected":[],"otherText":"none for now"}
	]}`)
	got := answerFromMessage(structured)
	if !strings.Contains(got, "Database: Postgres") || !strings.Contains(got, "none for now") {
		t.Fatalf("the selection was not rendered for the model: %q", got)
	}

	if got := answerFromMessage(clientResult(`{"cancelled":true}`)); got != questionCancelled {
		t.Fatalf("a cancelled answer must read as unanswered: %q", got)
	}
	if got := answerFromMessage(clientResult("")); got != questionCancelled {
		t.Fatalf("an empty answer must read as unanswered: %q", got)
	}
	// A client that answers in prose is passed straight through.
	if got := answerFromMessage(clientResult("use Postgres")); got != "use Postgres" {
		t.Fatalf("prose answer mangled: %q", got)
	}
	if got := answerFromMessage(Message{Error: "dialog closed"}); !strings.Contains(got, "dialog closed") {
		t.Fatalf("the failure should reach the model: %q", got)
	}
}

// TestQuestionToolKeepsInnerSchema matters because the model must see the exact
// same tool it sees on every other surface, and the client must recognise the
// well-known name.
func TestQuestionToolKeepsInnerSchema(t *testing.T) {
	inner := tools.NewAskUserQuestionTool(nil)
	wrapped := &hitlQuestionTool{inner: inner, pending: newPendingRegistry(), timeout: time.Minute}

	if wrapped.Info().Name != inner.Info().Name || wrapped.Info().Name != tools.AskUserQuestionToolName {
		t.Fatalf("the substituted tool changed its identity: %q", wrapped.Info().Name)
	}
	if len(wrapped.Info().Parameters) != len(inner.Info().Parameters) {
		t.Fatal("the substituted tool changed its schema")
	}
}

func TestQuestionToolResolvedByClient(t *testing.T) {
	pending := newPendingRegistry()
	notify := pending.watch("s1")
	tool := &hitlQuestionTool{
		inner:   tools.NewAskUserQuestionTool(nil),
		pending: pending,
		timeout: 2 * time.Second,
	}

	done := make(chan tools.ToolResponse, 1)
	go func() {
		resp, err := tool.Run(sessionContext("s1"), tools.ToolCall{ID: "call-q", Name: tools.AskUserQuestionToolName})
		if err != nil {
			t.Errorf("Run failed: %v", err)
		}
		done <- resp
	}()

	select {
	case s := <-notify:
		if s.callID != "call-q" {
			t.Fatalf("unexpected call id %q", s.callID)
		}
		// The agent already emitted this call; the handler must not invent it.
		if len(s.events) != 0 {
			t.Fatalf("a real tool call needs no synthetic events, got %d", len(s.events))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the question never suspended the run")
	}

	pending.resolve("s1", "call-q", clientResult(`{"answers":[{"header":"Database","selected":["Postgres"]}]}`))
	select {
	case resp := <-done:
		if !strings.Contains(resp.Content, "Postgres") {
			t.Fatalf("unexpected response: %+v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the question never unblocked")
	}
}

// TestStreamEmitsSyntheticPermissionCall checks the handler side: queued agent
// events first, then the synthesized prompt, then the interrupt.
func TestStreamEmitsSyntheticPermissionCall(t *testing.T) {
	r := newTestRuntime(hitlConfig(), "secret")
	events := make(chan agent.AgentEvent, 8)
	suspend := make(chan suspension, 1)
	run := newSuspendableRun(events, suspend)
	r.runs.put(run)

	// The tool that raised the prompt is already in the channel.
	events <- agent.AgentEvent{
		Type:     agent.AgentEventTypeToolCall,
		ToolCall: &message.ToolCall{ID: "call-bash", Name: "bash", Input: `{"command":"ls"}`, Finished: true},
	}
	suspend <- suspension{
		callID: "perm-1",
		events: []Event{
			NewToolCallStart("perm-1", permissionToolName, ""),
			NewToolCallArgs("perm-1", `{"toolName":"bash"}`),
			NewToolCallEnd("perm-1"),
		},
	}

	rec := httptest.NewRecorder()
	sse, _ := NewSSEWriter(rec)
	tr := newTranslator("t1", "r1")
	r.stream(context.Background(), sse, tr, run)
	run.unpark()

	body := rec.Body.String()
	if !strings.Contains(body, `"toolCallName":"bash"`) {
		t.Fatalf("the tool that raised the prompt was not flushed first: %s", body)
	}
	if !strings.Contains(body, `"toolCallName":"`+permissionToolName+`"`) {
		t.Fatalf("the permission prompt never reached the client: %s", body)
	}
	if !strings.Contains(body, `"outcome":"interrupt"`) {
		t.Fatalf("the run did not report an interrupt: %s", body)
	}

	types := frameTypes(body)
	prompt := indexOf(types, string(EventToolCallStart))
	finished := indexOf(types, string(EventRunFinished))
	if prompt < 0 || finished < 0 || prompt > finished {
		t.Fatalf("unexpected event order: %v", types)
	}
	// adoptToolCall must stop the translator from closing the prompt twice.
	ends := 0
	for _, typ := range types {
		if typ == string(EventToolCallEnd) {
			ends++
		}
	}
	if ends != 2 {
		t.Fatalf("expected exactly one END per call, got %d: %v", ends, types)
	}
	if _, ok := r.runs.get("t1"); !ok {
		t.Fatal("the run must stay registered until the client answers")
	}
}

// TestResumedRunDoesNotRecloseCarriedTools: the tool that was blocked on the
// approval reports its result in the *next* run, whose translator never saw it
// open.
func TestResumedRunDoesNotRecloseCarriedTools(t *testing.T) {
	run := &activeRun{threadID: "t1", cancel: func() {}}
	run.rememberEnded(map[string]bool{"call-bash": true})

	tr := newTranslator("t1", "r2").inheritEnded(run.endedCalls())
	got := tr.Translate(agent.AgentEvent{
		Type:       agent.AgentEventTypeToolResult,
		ToolResult: &message.ToolResult{ToolCallID: "call-bash", Content: "file.go"},
	})
	if len(got) != 1 {
		t.Fatalf("expected only the result, got %#v", got)
	}
	if _, ok := got[0].(ToolCallResultEvent); !ok {
		t.Fatalf("unexpected event: %#v", got[0])
	}
}
