package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/llm/prompt"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/ponytail"
)

func superpowersCtx(sessionID string) context.Context {
	return context.WithValue(context.Background(), prompt.SessionIDKey, sessionID)
}

func TestSetAndGetSuperpowersMode(t *testing.T) {
	const sid = "sess-superpowers-1"
	t.Cleanup(func() { SetSuperpowersMode(sid, false) })

	if SuperpowersMode(sid) {
		t.Fatal("expected superpowers to be off by default")
	}

	SetSuperpowersMode(sid, true)
	if !SuperpowersMode(sid) {
		t.Fatal("expected superpowers to be enabled")
	}

	SetSuperpowersMode(sid, false)
	if SuperpowersMode(sid) {
		t.Fatal("expected superpowers to be cleared")
	}
}

func TestSuperpowersEnabledForContext(t *testing.T) {
	const sid = "sess-superpowers-2"
	t.Cleanup(func() { SetSuperpowersMode(sid, false) })

	SetSuperpowersMode(sid, true)
	if !superpowersEnabledForContext(superpowersCtx(sid)) {
		t.Fatal("expected superpowers enabled from ctx session id")
	}
	if superpowersEnabledForContext(context.Background()) {
		t.Fatal("expected superpowers off without a session id in ctx")
	}
}

func TestSessionPolicyActive(t *testing.T) {
	const sid = "sess-policy-active"
	t.Cleanup(func() {
		SetSuperpowersMode(sid, false)
		SetPonytailMode(sid, ponytail.ModeOff)
	})

	ctx := superpowersCtx(sid)
	if sessionPolicyActive(ctx) {
		t.Fatal("expected no active session policy by default")
	}

	SetSuperpowersMode(sid, true)
	if !sessionPolicyActive(ctx) {
		t.Fatal("expected superpowers to activate the session policy path")
	}

	SetSuperpowersMode(sid, false)
	SetPonytailMode(sid, ponytail.ModeFull)
	if !sessionPolicyActive(ctx) {
		t.Fatal("expected ponytail to keep activating the session policy path")
	}
}

func TestSessionPolicyInstructionsInjectsSuperpowers(t *testing.T) {
	const sid = "sess-policy-inject"
	t.Cleanup(func() { SetSuperpowersMode(sid, false) })

	ctx := superpowersCtx(sid)
	if got := sessionPolicyInstructions(ctx); len(got) != 0 {
		t.Fatalf("expected no instructions when disabled, got %d", len(got))
	}

	SetSuperpowersMode(sid, true)
	got := sessionPolicyInstructions(ctx)
	if len(got) != 1 {
		t.Fatalf("expected exactly one injected ruleset, got %d", len(got))
	}
	if !strings.Contains(got[0], "superpowers") {
		t.Errorf("expected the injected block to be named superpowers, got %q", got[0])
	}
	if !strings.Contains(got[0], "/superpowers-finish") {
		t.Error("expected the superpowers policy body to be injected")
	}
}

func TestSessionPolicyInstructionsOrdersSuperpowersBeforePonytail(t *testing.T) {
	const sid = "sess-policy-order"
	t.Cleanup(func() {
		SetSuperpowersMode(sid, false)
		SetPonytailMode(sid, ponytail.ModeOff)
	})

	SetSuperpowersMode(sid, true)
	SetPonytailMode(sid, ponytail.ModeUltra)

	got := sessionPolicyInstructions(superpowersCtx(sid))
	if len(got) != 2 {
		t.Fatalf("expected both rulesets to compose, got %d", len(got))
	}
	if !strings.Contains(got[0], "superpowers") {
		t.Errorf("expected superpowers first, got %q", got[0])
	}
	if !strings.Contains(got[1], "ponytail (ultra)") {
		t.Errorf("expected ponytail second, got %q", got[1])
	}
}

func TestSessionPolicyInstructionsDoNotLeakAcrossSessions(t *testing.T) {
	const enabled = "sess-policy-enabled"
	const other = "sess-policy-other"
	t.Cleanup(func() { SetSuperpowersMode(enabled, false) })

	SetSuperpowersMode(enabled, true)

	if got := sessionPolicyInstructions(superpowersCtx(other)); len(got) != 0 {
		t.Fatalf("expected no injection for a different session, got %d", len(got))
	}
	if got := sessionPolicyInstructions(context.Background()); len(got) != 0 {
		t.Fatalf("expected no injection without a session id, got %d", len(got))
	}
	if got := sessionPolicyInstructions(superpowersCtx(enabled)); len(got) != 1 {
		t.Fatalf("expected the enabled session to still be injected, got %d", len(got))
	}
}

// fakeFinishService is a Service stub that only implements Run, which is all
// RunSuperpowersFinish needs. It lets the success-only clearing rule be tested
// without a provider, a model or a database.
type fakeFinishService struct {
	Service

	runErr  error
	events  []AgentEvent
	prompts []string
}

func (f *fakeFinishService) Run(_ context.Context, _ string, content string, _ ...message.Attachment) (<-chan AgentEvent, error) {
	f.prompts = append(f.prompts, content)
	if f.runErr != nil {
		return nil, f.runErr
	}
	ch := make(chan AgentEvent, len(f.events)+1)
	for _, ev := range f.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func drain(t *testing.T, ch <-chan AgentEvent) {
	t.Helper()
	for range ch {
	}
}

func TestRunSuperpowersFinishRequiresActiveMode(t *testing.T) {
	svc := &fakeFinishService{}
	_, err := RunSuperpowersFinish(context.Background(), svc, "sess-finish-inactive")
	if !errors.Is(err, ErrSuperpowersNotActive) {
		t.Fatalf("expected ErrSuperpowersNotActive, got %v", err)
	}
	if len(svc.prompts) != 0 {
		t.Error("expected no agent turn for an inactive session")
	}
}

func TestRunSuperpowersFinishClearsModeOnSuccess(t *testing.T) {
	const sid = "sess-finish-ok"
	t.Cleanup(func() { SetSuperpowersMode(sid, false) })

	SetSuperpowersMode(sid, true)
	svc := &fakeFinishService{events: []AgentEvent{
		{Type: AgentEventTypeContentDelta, Delta: "verifying..."},
		{Type: AgentEventTypeResponse, Done: true},
	}}

	events, err := RunSuperpowersFinish(context.Background(), svc, sid)
	if err != nil {
		t.Fatalf("RunSuperpowersFinish failed: %v", err)
	}
	drain(t, events)

	if SuperpowersMode(sid) {
		t.Error("expected a successful closing turn to disable the mode")
	}
	if len(svc.prompts) != 1 || !strings.Contains(svc.prompts[0], "Superpowers") {
		t.Errorf("expected the finish prompt to be run, got %v", svc.prompts)
	}
}

func TestRunSuperpowersFinishRetainsModeOnFailure(t *testing.T) {
	const sid = "sess-finish-err"
	t.Cleanup(func() { SetSuperpowersMode(sid, false) })

	SetSuperpowersMode(sid, true)
	svc := &fakeFinishService{events: []AgentEvent{
		{Type: AgentEventTypeError, Error: errors.New("boom")},
	}}

	events, err := RunSuperpowersFinish(context.Background(), svc, sid)
	if err != nil {
		t.Fatalf("RunSuperpowersFinish failed: %v", err)
	}
	drain(t, events)

	if !SuperpowersMode(sid) {
		t.Error("expected a failed closing turn to keep the workflow active")
	}
}

func TestRunSuperpowersFinishRetainsModeOnCancellation(t *testing.T) {
	const sid = "sess-finish-cancel"
	t.Cleanup(func() { SetSuperpowersMode(sid, false) })

	SetSuperpowersMode(sid, true)
	// A cancelled run reports the cancellation as an error event and never emits a
	// terminal response, so the workflow must survive it.
	svc := &fakeFinishService{events: []AgentEvent{
		{Type: AgentEventTypeError, Error: ErrRequestCancelled},
	}}

	events, err := RunSuperpowersFinish(context.Background(), svc, sid)
	if err != nil {
		t.Fatalf("RunSuperpowersFinish failed: %v", err)
	}
	drain(t, events)

	if !SuperpowersMode(sid) {
		t.Error("expected a cancelled close to keep the workflow active")
	}
}

func TestRunSuperpowersFinishPropagatesRunError(t *testing.T) {
	const sid = "sess-finish-run-err"
	t.Cleanup(func() { SetSuperpowersMode(sid, false) })

	SetSuperpowersMode(sid, true)
	svc := &fakeFinishService{runErr: ErrSessionBusy}

	if _, err := RunSuperpowersFinish(context.Background(), svc, sid); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("expected ErrSessionBusy, got %v", err)
	}
	if !SuperpowersMode(sid) {
		t.Error("expected the mode to survive a run that never started")
	}
}

func TestSessionPolicyInstructionsConcurrentSessions(t *testing.T) {
	const enabled = "sess-policy-concurrent-on"
	const disabled = "sess-policy-concurrent-off"
	t.Cleanup(func() { SetSuperpowersMode(enabled, false) })

	SetSuperpowersMode(enabled, true)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 25 {
				if len(sessionPolicyInstructions(superpowersCtx(enabled))) != 1 {
					t.Error("enabled session lost its policy under concurrency")
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for range 25 {
				if len(sessionPolicyInstructions(superpowersCtx(disabled))) != 0 {
					t.Error("policy leaked into a disabled session under concurrency")
					return
				}
			}
		}()
	}
	wg.Wait()
}
