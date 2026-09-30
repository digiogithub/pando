package agent

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/message"
)

func failingWith(err error) func(int, []message.Message) []provider.ProviderEvent {
	return func(int, []message.Message) []provider.ProviderEvent { return errorReply(err) }
}

func TestFailoverToNextCandidate(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		class string
	}{
		{"429 rate limit", errors.New("status code: 429 too many requests"), "rate_limit"},
		{"500 server", errors.New("status code: 500 internal server error"), "server"},
		{"network", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, "network"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newAutoEnv(t)
			sid := env.session("fo-next", autoCoder)
			env.decide("implementation", 0.93)
			env.provs[autoImpl].respond = failingWith(tc.err)

			events := env.run(sid, "implement it")

			if final := finalEvent(events); final.Type != AgentEventTypeResponse {
				t.Fatalf("final event = %+v", final)
			}
			if env.provs[autoFb1].callCount() != 1 || env.provs[autoFb2].callCount() != 0 {
				t.Fatalf("fb1=%d fb2=%d calls, want 1 and 0", env.provs[autoFb1].callCount(), env.provs[autoFb2].callCount())
			}
			// messages.model is the model that answered, and the failed partial
			// assistant message is gone.
			got := env.msgs.assistantModels(sid)
			if len(got) != 2 || got[1] != autoFb1 {
				t.Fatalf("assistant models = %v, want the answer on %s only", got, autoFb1)
			}
			want := "Auto: " + string(autoImpl) + " failed (" + tc.class + "), retrying on " + string(autoFb1)
			notices := autoNotices(events)
			if len(notices) != 2 || notices[1] != want {
				t.Fatalf("notices = %q, want second to be %q", notices, want)
			}
			info, _ := LastRouting(sid)
			if !info.FallbackUsed || info.Model != autoFb1 || info.Kind != RoutingKindFailover {
				t.Fatalf("LastRouting = %+v", info)
			}
			if m, _ := LastRoutedModel(sid); m != autoFb1 {
				t.Fatalf("LastRoutedModel = %s", m)
			}
			// Reduced retry budget on every candidate except the last one.
			if r := env.retriesFor(autoImpl); len(r) == 0 || r[0] != autoCandidateRetries {
				t.Fatalf("impl retry budget = %v, want %d", r, autoCandidateRetries)
			}
			if r := env.retriesFor(autoFb2); len(r) != 0 {
				t.Fatalf("fb2 was built although never needed: %v", r)
			}
			// No Model switched chatter: the Auto notice is the announcement.
			for _, n := range notices {
				if strings.Contains(n, "Model switched") {
					t.Fatalf("unexpected generic switch message %q", n)
				}
			}
			// A transient failure does not start a cooldown.
			if coolingDown(autoImpl) {
				t.Fatal("server/rate-limit/network errors must not start a cooldown")
			}
		})
	}

	t.Run("last candidate keeps the normal budget", func(t *testing.T) {
		env := newAutoEnv(t)
		sid := env.session("fo-budget", autoCoder)
		env.decide("implementation", 0.93)
		env.provs[autoImpl].respond = failingWith(errors.New("status code: 500"))
		env.provs[autoFb1].respond = failingWith(errors.New("status code: 500"))
		env.run(sid, "implement it")
		if r := env.retriesFor(autoFb2); len(r) != 1 || r[0] != 0 {
			t.Fatalf("last candidate retry budget = %v, want the default (0 = unchanged)", r)
		}
	})
}

func TestFailoverCooldown(t *testing.T) {
	env := newAutoEnv(t)
	sid := env.session("fo-cooldown", autoCoder)
	env.decide("implementation", 0.93)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	autoClock = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }
	advance := func(d time.Duration) { clockMu.Lock(); now = now.Add(d); clockMu.Unlock() }

	env.provs[autoImpl].respond = failingWith(errors.New("status code: 401 unauthorized"))
	env.run(sid, "first")
	if env.provs[autoImpl].callCount() != 1 || env.provs[autoFb1].callCount() != 1 {
		t.Fatalf("turn 1: impl=%d fb1=%d", env.provs[autoImpl].callCount(), env.provs[autoFb1].callCount())
	}
	if !coolingDown(autoImpl) {
		t.Fatal("an auth failure must start a cooldown")
	}

	// Turn 2, different session, same route, inside the cooldown: starts at fb1.
	other := env.session("fo-cooldown-2", autoCoder)
	advance(4 * time.Minute)
	events := env.run(other, "second")
	if env.provs[autoImpl].callCount() != 1 {
		t.Fatalf("cooling-down candidate was tried again (%d calls)", env.provs[autoImpl].callCount())
	}
	if env.provs[autoFb1].callCount() != 2 {
		t.Fatalf("fb1 calls = %d, want 2", env.provs[autoFb1].callCount())
	}
	if n := autoNotices(events); len(n) != 1 || !strings.Contains(n[0], "→ "+string(autoFb1)) {
		t.Fatalf("notices = %q, the chain must start at fb1", n)
	}

	// After the cooldown it is tried again.
	advance(2 * time.Minute)
	env.provs[autoImpl].respond = nil
	env.run(env.session("fo-cooldown-3", autoCoder), "third")
	if env.provs[autoImpl].callCount() != 2 {
		t.Fatalf("impl calls after the cooldown = %d, want 2", env.provs[autoImpl].callCount())
	}
}

func TestFailoverAllFail(t *testing.T) {
	env := newAutoEnv(t)
	sid := env.session("fo-allfail", autoCoder)
	env.decide("implementation", 0.93)
	env.provs[autoImpl].respond = failingWith(errors.New("status code: 429 too many requests"))
	env.provs[autoFb1].respond = failingWith(errors.New("status code: 401 unauthorized"))
	env.provs[autoFb2].respond = failingWith(errors.New("status code: 503 service unavailable"))

	events := env.run(sid, "implement it")

	final := finalEvent(events)
	if final.Type != AgentEventTypeError || final.Error == nil {
		t.Fatalf("final event = %+v, want the error of the last candidate", final)
	}
	msg := final.Error.Error()
	if !strings.Contains(msg, "503") {
		t.Fatalf("error %q must carry the last candidate's error", msg)
	}
	wantChain := string(autoImpl) + " [rate_limit] → " + string(autoFb1) + " [auth] → " + string(autoFb2) + " [server]"
	if !strings.Contains(msg, wantChain) {
		t.Fatalf("error %q must summarise the chain %q", msg, wantChain)
	}
	if got := env.msgs.assistantModels(sid); len(got) != 1 {
		// Only the seeded message remains besides, at most, the last failed one.
		t.Logf("assistant messages after the failure: %v", got)
	}
	if n := len(autoNotices(events)); n != 3 { // routed + 2 failovers
		t.Fatalf("auto notices = %d, want 3: %q", n, autoNotices(events))
	}
}

func TestFailoverMidToolLoop(t *testing.T) {
	env := newAutoEnv(t)
	sid := env.session("fo-tool", autoCoder)
	env.decide("implementation", 0.93)
	env.provs[autoImpl].respond = func(call int, _ []message.Message) []provider.ProviderEvent {
		if call == 0 {
			return toolReply("call-1", "echo")
		}
		return errorReply(errors.New("status code: 500 boom"))
	}

	events := env.run(sid, "implement it")

	if final := finalEvent(events); final.Type != AgentEventTypeResponse {
		t.Fatalf("final event = %+v", final)
	}
	if got := env.tool.runs(); got != 1 {
		t.Fatalf("tool executed %d times, want exactly once", got)
	}
	if env.provs[autoImpl].callCount() != 2 || env.provs[autoFb1].callCount() != 1 {
		t.Fatalf("impl=%d fb1=%d", env.provs[autoImpl].callCount(), env.provs[autoFb1].callCount())
	}
	var sawResult bool
	for _, msg := range env.provs[autoFb1].history(0) {
		for _, tr := range msg.ToolResults() {
			if tr.ToolCallID == "call-1" && tr.Content == "echoed" {
				sawResult = true
			}
		}
	}
	if !sawResult {
		t.Fatal("the fallback model must receive the executed tool result in its history")
	}
}

func TestNoFailoverOnCancel(t *testing.T) {
	env := newAutoEnv(t)
	sid := env.session("fo-cancel", autoCoder)
	env.decide("implementation", 0.93)
	env.provs[autoImpl].respond = failingWith(context.Canceled)

	events := env.run(sid, "implement it")

	final := finalEvent(events)
	if final.Type != AgentEventTypeError || !errors.Is(final.Error, ErrRequestCancelled) {
		t.Fatalf("final event = %+v, want the cancellation", final)
	}
	if env.provs[autoFb1].callCount() != 0 {
		t.Fatal("a cancelled request must not fail over")
	}
	if coolingDown(autoImpl) {
		t.Fatal("a cancellation must not start a cooldown")
	}
}

func TestNoFailoverOnContextLength(t *testing.T) {
	env := newAutoEnv(t)
	sid := env.session("fo-ctx", autoCoder)
	env.decide("implementation", 0.93)
	env.provs[autoImpl].respond = failingWith(errors.New("status code: 400 maximum context length exceeded"))

	events := env.run(sid, "implement it")

	final := finalEvent(events)
	if final.Type != AgentEventTypeError || final.Error == nil ||
		!strings.Contains(final.Error.Error(), "maximum context length") {
		t.Fatalf("final event = %+v, want the context-length error", final)
	}
	if env.provs[autoFb1].callCount() != 0 {
		t.Fatal("a context-length error must not fail over: the next model gets the same prompt")
	}
	// The compaction path runs once and retries on the SAME model; the second
	// identical failure is surfaced instead of looping.
	if got := env.provs[autoImpl].callCount(); got < 1 || got > 2 {
		t.Fatalf("impl calls = %d, want 1 (compaction impossible) or 2 (one retry after compaction)", got)
	}
}

func TestNoFailoverOutsideAutoTurn(t *testing.T) {
	env := newAutoEnv(t)
	sid := env.session("fo-manual", autoCoder)
	env.decide("implementation", 0.93)
	// Manual model choice: Auto is off for the session.
	SetSessionModelOverride(sid, autoImpl)
	env.provs[autoImpl].respond = failingWith(errors.New("status code: 500 boom"))

	events := env.run(sid, "implement it")

	if final := finalEvent(events); final.Type != AgentEventTypeError {
		t.Fatalf("final event = %+v, want the plain error", final)
	}
	if env.routerCalls() != 0 || env.provs[autoFb1].callCount() != 0 {
		t.Fatalf("manual selection must not route or fail over (router=%d fb1=%d)",
			env.routerCalls(), env.provs[autoFb1].callCount())
	}
	if SessionAutoMode(sid) {
		t.Fatal("selecting a concrete model must clear the Auto flag")
	}
}

func TestSessionAutoModeFlag(t *testing.T) {
	env := newAutoEnv(t)
	sid := env.session("flag", autoCoder)

	if !SessionAutoMode(sid) {
		t.Fatal("DefaultAuto should make an untouched session Auto")
	}
	SetSessionAutoMode(sid, false)
	if SessionAutoMode(sid) {
		t.Fatal("explicit false must win over the global default")
	}
	SetSessionAutoMode(sid, true)
	if !SessionAutoMode(sid) {
		t.Fatal("explicit true")
	}
	SetSessionModelOverride(sid, models.ModelID(autoImpl))
	if SessionAutoMode(sid) {
		t.Fatal("a concrete model selection must clear Auto")
	}
	SetSessionAutoMode(sid, true)
	if SessionModelOverrideID(sid) != "" {
		t.Fatal("turning Auto on must drop the manual override")
	}
	// Disabled globally: always false.
	env2 := newAutoEnv(t, withAutoConfig(func(m *config.ModelAutoModeConfig) { m.Enabled = false }))
	sid2 := env2.session("flag-off", autoCoder)
	SetSessionAutoMode(sid2, true)
	if SessionAutoMode(sid2) {
		t.Fatal("Auto must be off when modelAutoMode.enabled is false")
	}
}
