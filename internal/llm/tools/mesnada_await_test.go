package tools

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// fakeAwaitOrch is a minimal awaitOrchestrator for testing the await tool's Run
// logic without a real orchestrator/store.
type fakeAwaitOrch struct {
	mu      sync.Mutex
	tasks   []*models.Task
	listErr error
	intents []*orchestrator.AwaitIntent
}

func (f *fakeAwaitOrch) ListByParentSession(sessionID string) ([]*models.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tasks, nil
}

func (f *fakeAwaitOrch) SetAwaitIntent(intent *orchestrator.AwaitIntent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.intents = append(f.intents, intent)
}

func (f *fakeAwaitOrch) lastIntent() *orchestrator.AwaitIntent {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.intents) == 0 {
		return nil
	}
	return f.intents[len(f.intents)-1]
}

func awaitTool(orch awaitOrchestrator) *MesnadaAwaitTool {
	return &MesnadaAwaitTool{orch: orch}
}

func sessCtx(sessionID string) context.Context {
	return context.WithValue(context.Background(), SessionIDContextKey, sessionID)
}

func runAwait(t *testing.T, tool *MesnadaAwaitTool, ctx context.Context, input string) ToolResponse {
	t.Helper()
	resp, err := tool.Run(ctx, ToolCall{Input: input})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	return resp
}

func mkTask(id string, status models.TaskStatus) *models.Task {
	return &models.Task{
		ID:              id,
		Engine:          models.EnginePando,
		Status:          status,
		ParentSessionID: "sess-1",
		Conclusion:      &models.Conclusion{Status: "success", Summary: "done " + id},
	}
}

func TestAwaitRequiresSession(t *testing.T) {
	tool := awaitTool(&fakeAwaitOrch{})
	resp := runAwait(t, tool, context.Background(), `{}`)
	if !resp.IsError {
		t.Fatal("expected error when no session in context")
	}
}

func TestAwaitEmptyTaskIDsResolvesOutstanding(t *testing.T) {
	orch := &fakeAwaitOrch{tasks: []*models.Task{
		mkTask("t1", models.TaskStatusRunning),
		mkTask("t2", models.TaskStatusRunning),
	}}
	tool := awaitTool(orch)

	resp := runAwait(t, tool, sessCtx("sess-1"), `{}`)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
	intent := orch.lastIntent()
	if intent == nil {
		t.Fatal("expected an await intent to be registered")
	}
	if len(intent.TaskIDs) != 2 {
		t.Fatalf("expected 2 outstanding tasks awaited, got %d", len(intent.TaskIDs))
	}
	if intent.Policy != orchestrator.AwaitPolicyAll {
		t.Fatalf("expected default policy all, got %s", intent.Policy)
	}
	if !strings.Contains(resp.Content, "END YOUR TURN") {
		t.Fatalf("expected end-your-turn instruction, got: %s", resp.Content)
	}
}

func TestAwaitAlreadySatisfiedReturnsConclusions(t *testing.T) {
	// All tasks already terminal => policy=all is already satisfied => no intent,
	// conclusions returned immediately.
	orch := &fakeAwaitOrch{tasks: []*models.Task{
		mkTask("t1", models.TaskStatusCompleted),
		mkTask("t2", models.TaskStatusFailed),
	}}
	tool := awaitTool(orch)

	resp := runAwait(t, tool, sessCtx("sess-1"), `{"task_ids":["t1","t2"]}`)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
	if orch.lastIntent() != nil {
		t.Fatal("expected NO intent registered when already satisfied")
	}
	if !strings.Contains(resp.Content, "already satisfied") {
		t.Fatalf("expected already-satisfied response, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "delegated-result") {
		t.Fatalf("expected conclusions embedded, got: %s", resp.Content)
	}
}

func TestAwaitNoOutstandingShortCircuits(t *testing.T) {
	// Empty task_ids and nothing outstanding => satisfied immediately, no intent.
	orch := &fakeAwaitOrch{tasks: []*models.Task{
		mkTask("t1", models.TaskStatusCompleted),
	}}
	tool := awaitTool(orch)

	resp := runAwait(t, tool, sessCtx("sess-1"), `{}`)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
	if orch.lastIntent() != nil {
		t.Fatal("expected no intent when nothing is outstanding")
	}
	if !strings.Contains(resp.Content, "No outstanding") {
		t.Fatalf("expected no-outstanding message, got: %s", resp.Content)
	}
}

func TestAwaitForeignTaskIDRejected(t *testing.T) {
	orch := &fakeAwaitOrch{tasks: []*models.Task{
		mkTask("t1", models.TaskStatusRunning),
	}}
	tool := awaitTool(orch)

	resp := runAwait(t, tool, sessCtx("sess-1"), `{"task_ids":["t1","foreign"]}`)
	if !resp.IsError {
		t.Fatal("expected error for a foreign (non-correlated) task id")
	}
	if !strings.Contains(resp.Content, "foreign") {
		t.Fatalf("expected the foreign id named in error, got: %s", resp.Content)
	}
}

func TestAwaitPolicyQuorumRequiresQuorum(t *testing.T) {
	orch := &fakeAwaitOrch{tasks: []*models.Task{
		mkTask("t1", models.TaskStatusRunning),
		mkTask("t2", models.TaskStatusRunning),
	}}
	tool := awaitTool(orch)

	// Missing quorum => error.
	resp := runAwait(t, tool, sessCtx("sess-1"), `{"policy":"quorum"}`)
	if !resp.IsError {
		t.Fatal("expected error when policy=quorum and quorum missing")
	}

	// With quorum => intent registered.
	resp = runAwait(t, tool, sessCtx("sess-1"), `{"policy":"quorum","quorum":1}`)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
	intent := orch.lastIntent()
	if intent == nil || intent.Policy != orchestrator.AwaitPolicyQuorum || intent.Quorum != 1 {
		t.Fatalf("expected quorum intent, got %#v", intent)
	}
}

func TestAwaitPolicyAnyPendingRegisters(t *testing.T) {
	// policy=any but none terminal yet => not satisfied => intent registered.
	orch := &fakeAwaitOrch{tasks: []*models.Task{
		mkTask("t1", models.TaskStatusRunning),
		mkTask("t2", models.TaskStatusRunning),
	}}
	tool := awaitTool(orch)

	resp := runAwait(t, tool, sessCtx("sess-1"), `{"policy":"any"}`)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
	if orch.lastIntent() == nil {
		t.Fatal("expected intent registered for unmet any policy")
	}
}

func TestAwaitInvalidPolicyRejected(t *testing.T) {
	orch := &fakeAwaitOrch{tasks: []*models.Task{mkTask("t1", models.TaskStatusRunning)}}
	tool := awaitTool(orch)
	resp := runAwait(t, tool, sessCtx("sess-1"), `{"policy":"bogus"}`)
	if !resp.IsError {
		t.Fatal("expected error for invalid policy")
	}
}
