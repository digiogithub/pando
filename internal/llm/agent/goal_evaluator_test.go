package agent

import (
	"context"
	"testing"

	llmtools "github.com/digiogithub/pando/internal/llm/tools"
)

func TestGoalEvaluatorExplicitCompletion(t *testing.T) {
	evaluator := NewHeuristicGoalEvaluator()
	goal := NewGoalState("goal-1", "session-1", "Finish phase five", 5, 600)

	result, err := evaluator.Evaluate(context.Background(), goal, "GOAL COMPLETED: phase five is done", nil)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.Complete {
		t.Fatal("expected evaluator to mark goal complete")
	}
	if result.Reason != "phase five is done" {
		t.Fatalf("unexpected completion reason %q", result.Reason)
	}
}

func TestGoalEvaluatorPatternCompletionRequiresNoRemainingWork(t *testing.T) {
	evaluator := NewHeuristicGoalEvaluator()
	goal := NewGoalState("goal-2", "session-1", "Finish phase five", 5, 600)

	result, err := evaluator.Evaluate(context.Background(), goal, "Successfully implemented the evaluator. Remaining tasks: add tests.", nil)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Complete {
		t.Fatal("did not expect completion while remaining work is present")
	}
}

func TestGoalEvaluatorTodoCompletionUsesUpdatedPlan(t *testing.T) {
	evaluator := NewHeuristicGoalEvaluator()
	goal := NewGoalState("goal-3", "session-1", "Finish phase five", 5, 600)
	goal.InitialTodoSignature = todoSignature([]llmtools.TodoItem{
		{Content: "Legacy item", Status: "completed", Priority: "low"},
	})

	todos := []llmtools.TodoItem{
		{Content: "Implement evaluator", Status: "completed", Priority: "high"},
		{Content: "Add tests", Status: "completed", Priority: "high"},
	}
	result, err := evaluator.Evaluate(context.Background(), goal, "Wrapped up the final verification.", todos)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.Complete {
		t.Fatal("expected all-completed updated todo plan to mark goal complete")
	}
}

func TestGoalEvaluatorBlockedDetection(t *testing.T) {
	evaluator := NewHeuristicGoalEvaluator()
	goal := NewGoalState("goal-4", "session-1", "Finish phase five", 5, 600)

	result, err := evaluator.Evaluate(context.Background(), goal, "GOAL BLOCKED: need repository credentials", nil)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.Blocked {
		t.Fatal("expected evaluator to mark goal blocked")
	}
	if result.Reason != "need repository credentials" {
		t.Fatalf("unexpected blocked reason %q", result.Reason)
	}
}

func TestGoalEvaluatorFlagsStallOnRepeatedNoProgress(t *testing.T) {
	evaluator := NewHeuristicGoalEvaluator()
	goal := NewGoalState("goal-5", "session-1", "Finish phase five", 5, 600)
	goal.ConsecutiveNoProgress = 2
	goal.LastEvaluationSignature = buildEvaluationSignature("retry the same command", "")

	result, err := evaluator.Evaluate(context.Background(), goal, "Still stuck on the same issue. Next step: retry the same command.", nil)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.ProgressMade {
		t.Fatal("expected no measurable progress")
	}
	if !result.Stalled {
		t.Fatal("expected evaluator to flag stall on repeated no-progress state")
	}
}
