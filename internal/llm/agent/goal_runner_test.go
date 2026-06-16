package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	llmmodels "github.com/digiogithub/pando/internal/llm/models"
	llmtools "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
)

type stubGoalRun struct {
	events []AgentEvent
	err    error
	onRun  func(ctx context.Context, sessionID string, prompt string)
}

type stubGoalService struct {
	runs    []stubGoalRun
	events  []AgentEvent
	err     error
	prompts []string
	callIdx int
}

func (s *stubGoalService) Run(ctx context.Context, sessionID string, prompt string, attachments ...message.Attachment) (<-chan AgentEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.prompts = append(s.prompts, prompt)

	run := stubGoalRun{events: s.events, err: s.err}
	if s.callIdx < len(s.runs) {
		run = s.runs[s.callIdx]
	}
	s.callIdx++
	if run.err != nil {
		return nil, run.err
	}
	if run.onRun != nil {
		run.onRun(ctx, sessionID, prompt)
	}

	ch := make(chan AgentEvent, len(run.events))
	go func() {
		defer close(ch)
		for _, event := range run.events {
			select {
			case <-ctx.Done():
				return
			case ch <- event:
			}
		}
	}()
	return ch, nil
}

func (s *stubGoalService) Cancel(sessionID string) {}

func (s *stubGoalService) LastRunSystemMessages(sessionID string) []string { return nil }

func (s *stubGoalService) Steer(sessionID string, content string, attachments ...message.Attachment) error {
	return ErrSessionNotBusy
}

func (s *stubGoalService) PendingSteering(sessionID string) int { return 0 }

func (s *stubGoalService) IsSessionBusy(sessionID string) bool { return false }

func (s *stubGoalService) IsBusy() bool { return false }

func (s *stubGoalService) Update(agentName config.AgentName, modelID llmmodels.ModelID) (llmmodels.Model, error) {
	return llmmodels.Model{}, nil
}

func (s *stubGoalService) Summarize(ctx context.Context, sessionID string) error { return nil }

func (s *stubGoalService) SetLuaManager(fm *luaengine.FilterManager) {}

func (s *stubGoalService) GetTools() []llmtools.BaseTool { return nil }

func (s *stubGoalService) Subscribe(ctx context.Context) <-chan pubsub.Event[AgentEvent] {
	ch := make(chan pubsub.Event[AgentEvent])
	close(ch)
	return ch
}

func (s *stubGoalService) Publish(eventType pubsub.EventType, event AgentEvent) {}

func (s *stubGoalService) Model() llmmodels.Model { return llmmodels.Model{} }

type stubGoalStore struct {
	created db.SessionGoal
	updated db.SessionGoal
}

func (s *stubGoalStore) CreateGoal(ctx context.Context, arg db.CreateGoalParams) (db.SessionGoal, error) {
	s.created = db.SessionGoal{
		ID:                 arg.ID,
		SessionID:          arg.SessionID,
		Objective:          arg.Objective,
		Status:             arg.Status,
		Iteration:          0,
		MaxIterations:      arg.MaxIterations,
		MaxDurationSeconds: arg.MaxDurationSeconds,
		StartedAt:          arg.StartedAt,
	}
	return s.created, nil
}

func (s *stubGoalStore) UpdateGoalStatus(ctx context.Context, arg db.UpdateGoalStatusParams) (db.SessionGoal, error) {
	s.updated = db.SessionGoal{
		ID:                 arg.ID,
		Status:             arg.Status,
		Iteration:          arg.Iteration,
		CompletedAt:        arg.CompletedAt,
		LastProgress:       arg.LastProgress,
		NextStep:           arg.NextStep,
		BlockedReason:      arg.BlockedReason,
		MaxIterations:      s.created.MaxIterations,
		MaxDurationSeconds: s.created.MaxDurationSeconds,
		SessionID:          s.created.SessionID,
		Objective:          s.created.Objective,
	}
	return s.updated, nil
}

func setSessionTodosForTest(t *testing.T, sessionID string, todos []llmtools.TodoItem) {
	t.Helper()
	ctx := context.WithValue(context.Background(), llmtools.SessionIDContextKey, sessionID)
	tool := llmtools.NewTodoWriteTool()
	if _, err := tool.Run(ctx, llmtools.ToolCall{Input: mustJSONTodos(t, todos)}); err != nil {
		t.Fatalf("TodoWrite.Run: %v", err)
	}
	t.Cleanup(func() {
		llmtools.UnregisterTodoCallbacks(sessionID)
	})
}

func mustJSONTodos(t *testing.T, todos []llmtools.TodoItem) string {
	t.Helper()
	params := llmtools.TodoWriteParams{Todos: todos}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(data)
}

func TestGoalRunnerRunCompletesGoal(t *testing.T) {
	store := &stubGoalStore{}
	service := &stubGoalService{
		events: []AgentEvent{{
			Type: AgentEventTypeResponse,
			Message: message.Message{Parts: []message.ContentPart{
				message.TextContent{Text: "GOAL COMPLETED: finished the foundation"},
			}},
			SessionID: "session-1",
			Done:      true,
		}},
	}

	runner := NewGoalRunner(service, store)
	runner.newGoalID = func() string { return "goal-1" }
	runner.now = func() time.Time { return time.Unix(100, 0) }

	events, err := runner.Run(context.Background(), "session-1", "Finish phase one", GoalOptions{MaxIterations: 2, MaxDuration: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var got []AgentEvent
	for event := range events {
		got = append(got, event)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 forwarded event, got %d", len(got))
	}
	if store.updated.Status != GoalStatusCompleted {
		t.Fatalf("expected completed status, got %q", store.updated.Status)
	}
	if store.updated.Iteration != 1 {
		t.Fatalf("expected one executed iteration, got %d", store.updated.Iteration)
	}
	if !store.updated.CompletedAt.Valid || store.updated.CompletedAt.Int64 != 100 {
		t.Fatalf("unexpected completion time: %+v", store.updated.CompletedAt)
	}
	if store.updated.LastProgress.String != "GOAL COMPLETED: finished the foundation" {
		t.Fatalf("unexpected last progress: %+v", store.updated.LastProgress)
	}
}

func TestGoalRunnerRunBlocksOnAgentError(t *testing.T) {
	store := &stubGoalStore{}
	runner := NewGoalRunner(&stubGoalService{err: errors.New("agent unavailable")}, store)
	runner.newGoalID = func() string { return "goal-2" }
	runner.now = func() time.Time { return time.Unix(200, 0) }

	events, err := runner.Run(context.Background(), "session-1", "Finish phase one", GoalOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var last AgentEvent
	for event := range events {
		last = event
	}
	if last.Type != AgentEventTypeError {
		t.Fatalf("expected error event, got %q", last.Type)
	}
	if store.updated.Status != GoalStatusBlocked {
		t.Fatalf("expected blocked status, got %q", store.updated.Status)
	}
	if store.updated.BlockedReason != (sql.NullString{String: "agent unavailable", Valid: true}) {
		t.Fatalf("unexpected blocked reason: %+v", store.updated.BlockedReason)
	}
}

func TestGoalRunnerRunMarksCancelledContext(t *testing.T) {
	store := &stubGoalStore{}
	runner := NewGoalRunner(&stubGoalService{}, store)
	runner.newGoalID = func() string { return "goal-3" }
	runner.now = func() time.Time { return time.Unix(300, 0) }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	events, err := runner.Run(ctx, "session-1", "Finish phase one", GoalOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for range events {
	}

	if store.updated.Status != GoalStatusCancelled {
		t.Fatalf("expected cancelled status, got %q", store.updated.Status)
	}
	if !store.updated.CompletedAt.Valid || store.updated.CompletedAt.Int64 != 300 {
		t.Fatalf("unexpected completion time: %+v", store.updated.CompletedAt)
	}
}

func TestGoalRunnerRunContinuesAcrossIterationsUntilCompleted(t *testing.T) {
	store := &stubGoalStore{}
	service := &stubGoalService{
		runs: []stubGoalRun{
			{
				onRun: func(ctx context.Context, sessionID string, prompt string) {
					setSessionTodosForTest(t, sessionID, []llmtools.TodoItem{
						{Content: "Implement evaluator", Status: "completed", Priority: "high"},
						{Content: "Add integration tests", Status: "in_progress", Priority: "high"},
					})
				},
				events: []AgentEvent{{
					Type: AgentEventTypeResponse,
					Message: message.Message{Parts: []message.ContentPart{
						message.TextContent{Text: "Implemented the evaluator. Next step: add integration tests."},
					}},
					SessionID: "session-1",
					Done:      true,
				}},
			},
			{
				onRun: func(ctx context.Context, sessionID string, prompt string) {
					setSessionTodosForTest(t, sessionID, []llmtools.TodoItem{
						{Content: "Implement evaluator", Status: "completed", Priority: "high"},
						{Content: "Add integration tests", Status: "completed", Priority: "high"},
					})
				},
				events: []AgentEvent{{
					Type: AgentEventTypeResponse,
					Message: message.Message{Parts: []message.ContentPart{
						message.TextContent{Text: "All tests pass and the goal is verified working."},
					}},
					SessionID: "session-1",
					Done:      true,
				}},
			},
		},
	}

	runner := NewGoalRunner(service, store)
	runner.newGoalID = func() string { return "goal-4" }
	runner.now = func() time.Time { return time.Unix(400, 0) }

	events, err := runner.Run(context.Background(), "session-1", "Finish phase five", GoalOptions{MaxIterations: 3, MaxDuration: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var responses int
	for event := range events {
		if event.Type == AgentEventTypeResponse {
			responses++
		}
	}

	if responses != 2 {
		t.Fatalf("expected 2 response events, got %d", responses)
	}
	if len(service.prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(service.prompts))
	}
	if !strings.Contains(service.prompts[1], "Implemented the evaluator.") {
		t.Fatalf("expected continuation prompt to include prior progress, got %q", service.prompts[1])
	}
	if store.updated.Status != GoalStatusCompleted {
		t.Fatalf("expected completed status, got %q", store.updated.Status)
	}
	if store.updated.Iteration != 2 {
		t.Fatalf("expected two executed iterations, got %d", store.updated.Iteration)
	}
}

func TestGoalRunnerRunMarksStalledAfterRepeatedNoProgress(t *testing.T) {
	store := &stubGoalStore{}
	service := &stubGoalService{
		runs: []stubGoalRun{
			{events: []AgentEvent{{Type: AgentEventTypeResponse, Message: message.Message{Parts: []message.ContentPart{message.TextContent{Text: "Still stuck on the same issue. Next step: retry the same command."}}}, SessionID: "session-1", Done: true}}},
			{events: []AgentEvent{{Type: AgentEventTypeResponse, Message: message.Message{Parts: []message.ContentPart{message.TextContent{Text: "Still stuck on the same issue. Next step: retry the same command."}}}, SessionID: "session-1", Done: true}}},
			{events: []AgentEvent{{Type: AgentEventTypeResponse, Message: message.Message{Parts: []message.ContentPart{message.TextContent{Text: "Still stuck on the same issue. Next step: retry the same command."}}}, SessionID: "session-1", Done: true}}},
		},
	}

	runner := NewGoalRunner(service, store)
	runner.newGoalID = func() string { return "goal-5" }
	runner.now = func() time.Time { return time.Unix(500, 0) }

	events, err := runner.Run(context.Background(), "session-1", "Finish phase five", GoalOptions{MaxIterations: 5, MaxDuration: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for range events {
	}

	if store.updated.Status != GoalStatusStalled {
		t.Fatalf("expected stalled status, got %q", store.updated.Status)
	}
	if store.updated.Iteration != 3 {
		t.Fatalf("expected three executed iterations before stalling, got %d", store.updated.Iteration)
	}
	if !store.updated.BlockedReason.Valid || !strings.Contains(store.updated.BlockedReason.String, "Still stuck on the same issue") {
		t.Fatalf("unexpected blocked reason: %+v", store.updated.BlockedReason)
	}
}

func TestGoalRunnerRunStopsAtMaxIterationsWithClearMessage(t *testing.T) {
	store := &stubGoalStore{}
	service := &stubGoalService{
		runs: []stubGoalRun{
			{events: []AgentEvent{{Type: AgentEventTypeResponse, Message: message.Message{Parts: []message.ContentPart{message.TextContent{Text: "Implemented the first chunk. Next step: continue."}}}, SessionID: "session-1", Done: true}}},
			{events: []AgentEvent{{Type: AgentEventTypeResponse, Message: message.Message{Parts: []message.ContentPart{message.TextContent{Text: "Implemented the second chunk. Next step: continue."}}}, SessionID: "session-1", Done: true}}},
		},
	}

	runner := NewGoalRunner(service, store)
	runner.newGoalID = func() string { return "goal-6" }
	runner.now = func() time.Time { return time.Unix(600, 0) }

	events, err := runner.Run(context.Background(), "session-1", "Finish phase six", GoalOptions{MaxIterations: 2, MaxDuration: 5 * time.Minute})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for range events {
	}

	if store.updated.Status != GoalStatusFailed {
		t.Fatalf("expected failed status, got %q", store.updated.Status)
	}
	if !store.updated.BlockedReason.Valid || store.updated.BlockedReason.String != "GOAL STOPPED: reached max iterations (2)" {
		t.Fatalf("unexpected blocked reason: %+v", store.updated.BlockedReason)
	}
	if !store.updated.LastProgress.Valid || !strings.Contains(store.updated.LastProgress.String, "GOAL STOPPED: reached max iterations (2)") {
		t.Fatalf("unexpected last progress: %+v", store.updated.LastProgress)
	}
}

func TestGoalRunnerRunTimesOutWithConfiguredDuration(t *testing.T) {
	store := &stubGoalStore{}
	service := &stubGoalService{
		runs: []stubGoalRun{{
			onRun: func(ctx context.Context, sessionID string, prompt string) {
				<-ctx.Done()
			},
		}},
	}

	runner := NewGoalRunner(service, store)
	start := time.Unix(700, 0)
	current := start
	runner.newGoalID = func() string { return "goal-7" }
	runner.now = func() time.Time { return current }

	events, err := runner.Run(context.Background(), "session-1", "Finish phase six", GoalOptions{MaxIterations: 2, MaxDuration: time.Second})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	current = start.Add(2 * time.Second)
	for range events {
	}

	if store.updated.Status != GoalStatusTimeout {
		t.Fatalf("expected timeout status, got %q", store.updated.Status)
	}
	if !store.updated.LastProgress.Valid || store.updated.LastProgress.String != "GOAL TIMEOUT: exceeded max duration (1s)" {
		t.Fatalf("unexpected last progress: %+v", store.updated.LastProgress)
	}
}

func TestGoalRunnerRunUsesConfiguredDefaults(t *testing.T) {
	config.SetForTests(&config.Config{
		Goal: config.GoalConfig{
			MaxIterations:   4,
			MaxDuration:     "90s",
			StallIterations: 2,
			AutoApprove:     true,
		},
	})
	t.Cleanup(func() {
		config.SetForTests(nil)
	})

	store := &stubGoalStore{}
	service := &stubGoalService{
		runs: []stubGoalRun{
			{events: []AgentEvent{{Type: AgentEventTypeResponse, Message: message.Message{Parts: []message.ContentPart{message.TextContent{Text: "Still stuck on the same issue. Next step: retry."}}}, SessionID: "session-1", Done: true}}},
			{events: []AgentEvent{{Type: AgentEventTypeResponse, Message: message.Message{Parts: []message.ContentPart{message.TextContent{Text: "Still stuck on the same issue. Next step: retry."}}}, SessionID: "session-1", Done: true}}},
		},
	}

	runner := NewGoalRunner(service, store)
	runner.newGoalID = func() string { return "goal-8" }
	runner.now = func() time.Time { return time.Unix(800, 0) }

	events, err := runner.Run(context.Background(), "session-1", "Finish phase six", GoalOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for range events {
	}

	if store.created.MaxIterations != 4 {
		t.Fatalf("expected configured max iterations, got %d", store.created.MaxIterations)
	}
	if store.created.MaxDurationSeconds != 90 {
		t.Fatalf("expected configured max duration seconds, got %d", store.created.MaxDurationSeconds)
	}
	if store.updated.Status != GoalStatusStalled {
		t.Fatalf("expected configured stall threshold to stop after two iterations, got %q", store.updated.Status)
	}
	if store.updated.Iteration != 2 {
		t.Fatalf("expected configured stall threshold after two iterations, got %d", store.updated.Iteration)
	}
}
