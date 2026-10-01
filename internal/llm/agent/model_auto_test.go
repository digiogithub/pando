package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/extevents"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/pkg/extension"
)

func TestAutoRoutesOncePerUserTurn(t *testing.T) {
	env := newAutoEnv(t)
	sid := env.session("auto-once", autoCoder)
	env.decide("implementation", 0.93)

	// A turn with three tool iterations: the router must still be hit once.
	env.provs[autoImpl].respond = func(call int, _ []message.Message) []provider.ProviderEvent {
		if call < 3 {
			return toolReply("call-"+string(rune('a'+call)), "echo")
		}
		return textReply("done")
	}

	events := env.run(sid, "implement the feature")

	if final := finalEvent(events); final.Type != AgentEventTypeResponse {
		t.Fatalf("final event = %+v", final)
	}
	if got := env.routerCalls(); got != 1 {
		t.Fatalf("router requests = %d, want exactly 1", got)
	}
	if got := env.tool.runs(); got != 3 {
		t.Fatalf("tool runs = %d, want 3", got)
	}
	if got := env.provs[autoImpl].callCount(); got != 4 {
		t.Fatalf("routed model calls = %d, want 4 (3 tool iterations + answer)", got)
	}
	if got := env.provs[autoCoder].callCount(); got != 0 {
		t.Fatalf("coder was called %d times, want 0", got)
	}
	for _, m := range env.msgs.assistantModels(sid)[1:] { // [0] is the seeded exchange
		if m != autoImpl {
			t.Fatalf("messages.model = %s, want %s", m, autoImpl)
		}
	}
	if m, ok := LastRoutedModel(sid); !ok || m != autoImpl {
		t.Fatalf("LastRoutedModel = %q %v", m, ok)
	}
	info, _ := LastRouting(sid)
	if info.RouteID != "implementation" || !info.Matched || info.Probability < 0.9 || info.FallbackUsed {
		t.Fatalf("LastRouting = %+v", info)
	}
	// The override is turn scoped and the global coder model untouched.
	if got := SessionModelOverrideID(sid); got != "" {
		t.Fatalf("override after the turn = %q, want cleared", got)
	}
	if got := config.Get().Agents[config.AgentCoder].Model; got != autoCoder {
		t.Fatalf("agents.coder.model = %s, must not change", got)
	}
	if !SessionAutoMode(sid) {
		t.Fatal("the Auto flag must survive the turn so the next prompt routes again")
	}

	// A second user turn routes again.
	env.provs[autoImpl].respond = nil
	env.run(sid, "and another thing")
	if got := env.routerCalls(); got != 2 {
		t.Fatalf("router requests after 2nd turn = %d, want 2", got)
	}
}

func TestAutoRouterDownUsesCoder(t *testing.T) {
	t.Run("closed port", func(t *testing.T) {
		env := newAutoEnv(t)
		sid := env.session("auto-down", autoCoder)
		env.srv.Close()

		events := env.run(sid, "do something")

		if final := finalEvent(events); final.Type != AgentEventTypeResponse {
			t.Fatalf("final event = %+v", final)
		}
		if env.provs[autoCoder].callCount() != 1 {
			t.Fatalf("coder calls = %d, want 1", env.provs[autoCoder].callCount())
		}
		warnings := autoNotices(events)
		if len(warnings) != 1 || !strings.Contains(warnings[0], "router unavailable") ||
			!strings.Contains(warnings[0], string(autoCoder)) {
			t.Fatalf("notices = %q", warnings)
		}
		info, _ := LastRouting(sid)
		if info.Kind != RoutingKindRouterUnavailable || info.Reason != "router_error" || info.Model != autoCoder {
			t.Fatalf("LastRouting = %+v", info)
		}
	})

	t.Run("timeout bound", func(t *testing.T) {
		env := newAutoEnv(t, withDecisionConfig(func(d *config.DecisionModelConfig) { d.TimeoutMs = 500 }))
		sid := env.session("auto-slow", autoCoder)
		env.srv.Delay(5 * time.Second)

		start := time.Now()
		events := env.run(sid, "do something")
		if final := finalEvent(events); final.Type != AgentEventTypeResponse {
			t.Fatalf("final event = %+v", final)
		}
		env.provs[autoCoder].mu.Lock()
		first := env.provs[autoCoder].starts[0]
		env.provs[autoCoder].mu.Unlock()
		if d := first.Sub(start); d > 900*time.Millisecond {
			t.Fatalf("coder request started after %v, want within ~700ms of the prompt", d)
		}
		info, _ := LastRouting(sid)
		if info.ErrorClass != "timeout" {
			t.Fatalf("error class = %q, want timeout", info.ErrorClass)
		}
	})

	t.Run("unauthorized names the class", func(t *testing.T) {
		env := newAutoEnv(t)
		sid := env.session("auto-unauth", autoCoder)
		env.decide("implementation", 0.9)
		env.run(sid, "warm the router budget cache")
		resetAutoWarnings()
		env.srv.FailNext(401)

		events := env.run(sid, "do something")
		warnings := autoNotices(events)
		if len(warnings) != 1 || !strings.Contains(warnings[0], "unauthorized") {
			t.Fatalf("notices = %q", warnings)
		}
		if env.provs[autoCoder].callCount() != 1 {
			t.Fatal("the turn must run on the coder model")
		}
	})
}

func TestAutoWarnOncePerSession(t *testing.T) {
	env := newAutoEnv(t)
	first := env.session("auto-warn-1", autoCoder)
	second := env.session("auto-warn-2", autoCoder)
	env.srv.Close()

	ev1 := env.run(first, "one")
	if n := autoNotices(ev1); len(n) != 1 {
		t.Fatalf("first prompt notices = %q, want one warning", n)
	}
	ev2 := env.run(first, "two")
	if n := autoNotices(ev2); len(n) != 0 {
		t.Fatalf("second prompt must be quiet, got %q", n)
	}
	if env.provs[autoCoder].callCount() != 2 {
		t.Fatalf("both turns must still run on the coder, calls = %d", env.provs[autoCoder].callCount())
	}
	// Another session has its own budget of warnings.
	if n := autoNotices(env.run(second, "three")); len(n) != 1 {
		t.Fatalf("other session notices = %q, want its own warning", n)
	}
	// The repeat is still visible to tooling.
	if info, _ := LastRouting(first); info.Kind != RoutingKindRouterUnavailable {
		t.Fatalf("LastRouting after a quiet failure = %+v", info)
	}
}

func TestAutoConsecutiveTurnsSwitchModel(t *testing.T) {
	env := newAutoEnv(t)
	sid := env.session("auto-switch", autoCoder)

	env.provs[autoImpl].respond = func(int, []message.Message) []provider.ProviderEvent {
		return reasoningReply("impl answer")
	}

	env.decide("implementation", 0.9)
	env.run(sid, "implement it")
	env.decide("planning", 0.9)
	env.run(sid, "now plan the next step")

	got := env.msgs.assistantModels(sid)
	want := []models.ModelID{autoCoder, autoImpl, autoPlan}
	if len(got) != len(want) {
		t.Fatalf("assistant models = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("assistant models = %v, want %v", got, want)
		}
	}

	// The plan model must not receive the reasoning of the models before it.
	history := env.provs[autoPlan].history(0)
	for _, msg := range history {
		for _, part := range msg.Parts {
			if _, isReasoning := part.(message.ReasoningContent); isReasoning {
				t.Fatalf("reasoning block leaked to the new model: %+v", msg)
			}
		}
	}
	// The stored history keeps the original reasoning.
	stored, _ := env.msgs.List(context.Background(), sid)
	foundReasoning := false
	for _, msg := range stored {
		for _, part := range msg.Parts {
			if _, ok := part.(message.ReasoningContent); ok {
				foundReasoning = true
			}
		}
	}
	if !foundReasoning {
		t.Fatal("sanitising for the new model must not rewrite stored messages")
	}
}

func TestAutoConcurrentSessions(t *testing.T) {
	env := newAutoEnv(t)
	sidA := env.session("auto-conc-a", autoCoder)
	sidB := env.session("auto-conc-b", autoCoder)

	gate := make(chan struct{})
	env.provs[autoImpl].gate = gate
	env.provs[autoImpl].started = make(chan struct{}, 1)

	env.decide("implementation", 0.9)
	chA, err := env.a.Run(context.Background(), sidA, "implement it")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-env.provs[autoImpl].started:
	case <-time.After(5 * time.Second):
		t.Fatal("session A never reached its routed model")
	}

	// A is now blocked mid-stream on the impl model; B routes elsewhere.
	env.decide("planning", 0.9)
	eventsB := env.run(sidB, "plan it")
	if final := finalEvent(eventsB); final.Type != AgentEventTypeResponse {
		t.Fatalf("B final = %+v", final)
	}
	if got := SessionModelOverrideID(sidA); got != autoImpl {
		t.Fatalf("A override while running = %q, want %s", got, autoImpl)
	}
	close(gate)
	eventsA := drainAutoEvents(t, chA)
	if final := finalEvent(eventsA); final.Type != AgentEventTypeResponse {
		t.Fatalf("A final = %+v", final)
	}

	if a := env.msgs.assistantModels(sidA); a[len(a)-1] != autoImpl {
		t.Fatalf("A answered on %v", a)
	}
	if b := env.msgs.assistantModels(sidB); b[len(b)-1] != autoPlan {
		t.Fatalf("B answered on %v", b)
	}
	if got := config.Get().Agents[config.AgentCoder].Model; got != autoCoder {
		t.Fatalf("agents.coder.model = %s, must not change", got)
	}
}

func TestAutoSubagentsUnaffected(t *testing.T) {
	env := newAutoEnv(t)
	sid := env.session("auto-sub", autoTask)
	env.decide("implementation", 0.95)

	// A task (sub)agent in a session that is globally in Auto mode.
	sub := &agent{
		Broker:            env.a.Broker,
		sessions:          env.a.sessions,
		messages:          env.msgs,
		agentName:         config.AgentTask,
		provider:          env.provs[autoTask],
		runStatusMessages: make(map[string][]string),
		steeringQueue:     make(map[string][]steeringMessage),
		resurrectCount:    make(map[string]int),
	}
	t.Cleanup(func() { waitForRunsOrFail(t, sub) })
	ch, err := sub.Run(context.Background(), sid, "delegated work")
	if err != nil {
		t.Fatal(err)
	}
	events := drainAutoEvents(t, ch)
	if final := finalEvent(events); final.Type != AgentEventTypeResponse {
		t.Fatalf("final = %+v", final)
	}
	if env.routerCalls() != 0 {
		t.Fatalf("router called %d times for a subagent", env.routerCalls())
	}
	if len(autoNotices(events)) != 0 {
		t.Fatalf("subagent emitted routing notices: %q", autoNotices(events))
	}
	if env.provs[autoTask].callCount() != 1 || env.provs[autoImpl].callCount() != 0 {
		t.Fatalf("subagent must stay on its own model")
	}

	// A resurrection (system initiated run) of an Auto session never re-routes.
	coderSession := env.session("auto-resume", autoCoder)
	if err := env.a.Resume(context.Background(), coderSession, "a delegated task finished"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	waitForRunsOrFail(t, env.a)
	if env.routerCalls() != 0 {
		t.Fatalf("router called %d times for a resurrection run", env.routerCalls())
	}
}

func TestAutoNoticeFormats(t *testing.T) {
	env := newAutoEnv(t)

	t.Run("matched", func(t *testing.T) {
		sid := env.session("fmt-matched", autoCoder)
		env.decide("implementation", 0.93)
		notice := autoNotices(env.run(sid, "implement"))
		want := "Auto: implementation → " + string(autoImpl) + " (p=0.93, "
		if len(notice) != 1 || !strings.HasPrefix(notice[0], want) ||
			!strings.HasSuffix(notice[0], " ms via ollama/tev1:0.8b)") {
			t.Fatalf("notice = %q, want prefix %q", notice, want)
		}
	})

	t.Run("no match", func(t *testing.T) {
		sid := env.session("fmt-nomatch", autoCoder)
		env.srv.SetDecision("planning", map[string]float64{"planning": 0.41, "implementation": 0.39, "none": 0.2})
		notice := autoNotices(env.run(sid, "hmm"))
		want := "Auto: no confident match (best planning p=0.41) → " + string(autoCoder)
		if len(notice) != 1 || notice[0] != want {
			t.Fatalf("notice = %q, want %q", notice, want)
		}
	})

	t.Run("none wins", func(t *testing.T) {
		sid := env.session("fmt-none", autoCoder)
		env.srv.SetDecision("none", map[string]float64{"none": 0.89, "implementation": 0.11})
		notice := autoNotices(env.run(sid, "ok continue"))
		want := "Auto: no confident match (best implementation p=0.11) → " + string(autoCoder)
		if len(notice) != 1 || notice[0] != want {
			t.Fatalf("notice = %q, want %q", notice, want)
		}
	})

	t.Run("router unavailable", func(t *testing.T) {
		sid := env.session("fmt-down", autoCoder)
		env.decide("implementation", 0.9)
		env.run(sid, "warm the router budget cache")
		resetAutoWarnings()
		env.srv.FailNext(401)
		notice := autoNotices(env.run(sid, "anything"))
		want := "Auto: router unavailable (unauthorized) → " + string(autoCoder)
		if len(notice) != 1 || notice[0] != want {
			t.Fatalf("notice = %q, want %q", notice, want)
		}
	})

	t.Run("structured payload rides the same event", func(t *testing.T) {
		sid := env.session("fmt-payload", autoCoder)
		env.decide("planning", 0.88)
		var routed *RoutingInfo
		for _, ev := range env.run(sid, "plan") {
			if ev.Type == AgentEventTypeSystemMessage && ev.Routing != nil {
				routed = ev.Routing
			}
		}
		if routed == nil || routed.Kind != RoutingKindRouted || routed.RouteID != "planning" ||
			routed.Model != autoPlan || !strings.HasPrefix(routed.Notice, "Auto: planning →") {
			t.Fatalf("routing payload = %+v", routed)
		}
	})
}

// captureLogs collects every slog record (message and attributes) until the
// returned restore function runs.
type logCapture struct {
	mu    sync.Mutex
	lines []string
}

func (c *logCapture) Enabled(context.Context, slog.Level) bool { return true }
func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler       { return c }
func (c *logCapture) WithGroup(string) slog.Handler            { return c }
func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		b.WriteString(" " + a.Key + "=" + a.Value.String())
		return true
	})
	c.mu.Lock()
	c.lines = append(c.lines, r.Level.String()+" "+b.String())
	c.mu.Unlock()
	return nil
}

func TestAutoTelemetryPrivacy(t *testing.T) {
	const marker = "SECRET-PROMPT-MARKER"
	const apiKey = "sk-router-secret-key-123"
	env := newAutoEnv(t, withDecisionConfig(func(d *config.DecisionModelConfig) {
		d.Router.APIKey = apiKey
	}), withAutoConfig(func(m *config.ModelAutoModeConfig) {
		m.HistoryPrompts = 2
	}))
	sid := env.session("auto-privacy", autoCoder)

	logs := &logCapture{}
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	var mu sync.Mutex
	var captured []extension.Event
	ctx, cancel := context.WithCancel(context.Background())
	extevents.SetSink(ctx, func(ev extension.Event) {
		mu.Lock()
		captured = append(captured, ev)
		mu.Unlock()
	})
	t.Cleanup(func() {
		cancel()
		extevents.SetSink(context.Background(), nil)
	})

	cost := 0.0002
	env.srv.SetCost(cost)
	env.decide("implementation", 0.93)
	events := env.run(sid, "please implement "+marker)
	env.run(sid, "and also "+marker+" again") // history prompts include the marker

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(captured)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	payloads, _ := json.Marshal(captured)
	count := len(captured)
	first := captured[0]
	mu.Unlock()
	if count < 2 {
		t.Fatalf("ModelRouted events = %d, want one per decision", count)
	}
	if first.Topic != extension.TopicModelRoute || first.Payload["model"] != string(autoImpl) ||
		first.Payload["routeId"] != "implementation" {
		t.Fatalf("event = %+v", first)
	}
	if _, ok := first.Payload["routerCostUsd"]; !ok {
		t.Fatalf("router cost missing from payload %v", first.Payload)
	}

	logs.mu.Lock()
	logText := strings.Join(logs.lines, "\n")
	logs.mu.Unlock()
	notice, _ := json.Marshal(events)
	status := strings.Join(env.a.LastRunSystemMessages(sid), "\n")
	var decisionLogged bool
	for _, line := range strings.Split(logText, "\n") {
		if strings.Contains(line, "model_auto: decision") {
			decisionLogged = true
			if !strings.HasPrefix(line, "INFO ") {
				t.Fatalf("decision must be logged at Info: %q", line)
			}
		}
	}
	if !decisionLogged {
		t.Fatalf("no model_auto Info line in logs:\n%s", logText)
	}

	for name, blob := range map[string]string{
		"extension events": string(payloads),
		"logs":             logText,
		"routing notices":  strings.Join(autoNotices(events), "\n") + status,
	} {
		if strings.Contains(blob, marker) {
			t.Fatalf("%s leak the prompt: %s", name, blob)
		}
		if strings.Contains(blob, apiKey) {
			t.Fatalf("%s leak the API key", name)
		}
	}
	_ = notice
}
