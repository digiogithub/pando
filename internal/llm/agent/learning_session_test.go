package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/ponytail"
)

func TestSetAndGetLearningMode(t *testing.T) {
	const sid = "sess-learning-1"
	t.Cleanup(func() { SetLearningMode(sid, false) })

	if LearningMode(sid) {
		t.Fatal("expected learning to be off by default")
	}

	SetLearningMode(sid, true)
	if !LearningMode(sid) {
		t.Fatal("expected learning to be enabled")
	}

	SetLearningMode(sid, false)
	if LearningMode(sid) {
		t.Fatal("expected learning to be cleared")
	}
}

func TestLearningEnabledForContext(t *testing.T) {
	const sid = "sess-learning-2"
	t.Cleanup(func() { SetLearningMode(sid, false) })

	SetLearningMode(sid, true)
	if !learningEnabledForContext(superpowersCtx(sid)) {
		t.Fatal("expected learning enabled from ctx session id")
	}
	if learningEnabledForContext(context.Background()) {
		t.Fatal("expected learning off without a session id in ctx")
	}
}

func TestSessionPolicyActiveLearning(t *testing.T) {
	const sid = "sess-learning-policy-active"
	t.Cleanup(func() { SetLearningMode(sid, false) })

	ctx := superpowersCtx(sid)
	if sessionPolicyActive(ctx) {
		t.Fatal("expected no active session policy by default")
	}

	SetLearningMode(sid, true)
	if !sessionPolicyActive(ctx) {
		t.Fatal("expected learning to activate the session policy path")
	}
}

func TestSessionPolicyInstructionsInjectsLearning(t *testing.T) {
	const sid = "sess-learning-inject"
	t.Cleanup(func() { SetLearningMode(sid, false) })

	ctx := superpowersCtx(sid)
	if got := sessionPolicyInstructions(ctx); len(got) != 0 {
		t.Fatalf("expected no instructions when disabled, got %d", len(got))
	}

	SetLearningMode(sid, true)
	got := sessionPolicyInstructions(ctx)
	if len(got) != 1 {
		t.Fatalf("expected exactly one injected ruleset, got %d", len(got))
	}
	if !strings.Contains(got[0], "learning") {
		t.Errorf("expected the injected block to be named learning, got %q", got[0])
	}
	if !strings.Contains(got[0], "/learning-finish") {
		t.Error("expected the learning policy body to be injected")
	}
}

// TestSessionPolicyInstructionsOrdersSuperpowersLearningThenBrevity asserts the
// deliberate composition order: superpowers -> learning -> caveman/ponytail.
func TestSessionPolicyInstructionsOrdersSuperpowersLearningThenBrevity(t *testing.T) {
	const sid = "sess-learning-order"
	t.Cleanup(func() {
		SetSuperpowersMode(sid, false)
		SetLearningMode(sid, false)
		SetPonytailMode(sid, ponytail.ModeOff)
	})

	SetSuperpowersMode(sid, true)
	SetLearningMode(sid, true)
	SetPonytailMode(sid, ponytail.ModeUltra)

	got := sessionPolicyInstructions(superpowersCtx(sid))
	if len(got) != 3 {
		t.Fatalf("expected all three rulesets to compose, got %d", len(got))
	}
	if !strings.Contains(got[0], "superpowers") {
		t.Errorf("expected superpowers first, got %q", got[0])
	}
	if !strings.Contains(got[1], "learning") {
		t.Errorf("expected learning second, got %q", got[1])
	}
	if !strings.Contains(got[2], "ponytail (ultra)") {
		t.Errorf("expected ponytail last, got %q", got[2])
	}
}

func TestRunLearningFinishRequiresActiveMode(t *testing.T) {
	svc := &fakeFinishService{}
	_, err := RunLearningFinish(context.Background(), svc, "sess-learning-finish-inactive")
	if !errors.Is(err, ErrLearningNotActive) {
		t.Fatalf("expected ErrLearningNotActive, got %v", err)
	}
	if len(svc.prompts) != 0 {
		t.Error("expected no agent turn for an inactive session")
	}
}

func TestRunLearningFinishClearsModeOnSuccess(t *testing.T) {
	const sid = "sess-learning-finish-ok"
	t.Cleanup(func() { SetLearningMode(sid, false) })

	SetLearningMode(sid, true)
	svc := &fakeFinishService{events: []AgentEvent{
		{Type: AgentEventTypeContentDelta, Delta: "consolidating..."},
		{Type: AgentEventTypeResponse, Done: true},
	}}

	events, err := RunLearningFinish(context.Background(), svc, sid)
	if err != nil {
		t.Fatalf("RunLearningFinish failed: %v", err)
	}
	drain(t, events)

	if LearningMode(sid) {
		t.Error("expected a successful closing turn to disable the mode")
	}
	if len(svc.prompts) != 1 || !strings.Contains(svc.prompts[0], "Learning mode") {
		t.Errorf("expected the finish prompt to be run, got %v", svc.prompts)
	}
}

func TestRunLearningFinishRetainsModeOnCancellation(t *testing.T) {
	const sid = "sess-learning-finish-cancel"
	t.Cleanup(func() { SetLearningMode(sid, false) })

	SetLearningMode(sid, true)
	svc := &fakeFinishService{events: []AgentEvent{
		{Type: AgentEventTypeError, Error: ErrRequestCancelled},
	}}

	events, err := RunLearningFinish(context.Background(), svc, sid)
	if err != nil {
		t.Fatalf("RunLearningFinish failed: %v", err)
	}
	drain(t, events)

	if !LearningMode(sid) {
		t.Error("expected a cancelled close to keep learning active")
	}
}

func TestRunLearningFinishPropagatesRunError(t *testing.T) {
	const sid = "sess-learning-finish-run-err"
	t.Cleanup(func() { SetLearningMode(sid, false) })

	SetLearningMode(sid, true)
	svc := &fakeFinishService{runErr: ErrSessionBusy}

	if _, err := RunLearningFinish(context.Background(), svc, sid); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("expected ErrSessionBusy, got %v", err)
	}
	if !LearningMode(sid) {
		t.Error("expected the mode to survive a run that never started")
	}
}
