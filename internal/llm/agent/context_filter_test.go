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
	"github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/pkg/extension"
)

// fakeFilteredEnricher reports a scripted FilterResult unless the context says
// the turn is not eligible for filtering, in which case it reports none.
type fakeFilteredEnricher struct {
	mu       sync.Mutex
	res      rag.FilterResult
	calls    int
	disabled []bool
}

func (f *fakeFilteredEnricher) EnrichContext(ctx context.Context, q string) string {
	out, _ := f.EnrichContextWithResult(ctx, q)
	return out
}

func (f *fakeFilteredEnricher) EnrichContextWithResult(ctx context.Context, _ string) (string, rag.FilterResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	off := rag.RelevanceFilterDisabled(ctx)
	f.disabled = append(f.disabled, off)
	if off {
		return "<ctx>unfiltered</ctx>", rag.FilterResult{}
	}
	return "<ctx>filtered</ctx>", f.res
}

func installFakeEnricher(t *testing.T, res rag.FilterResult) *fakeFilteredEnricher {
	t.Helper()
	f := &fakeFilteredEnricher{res: res}
	prev := globalContextEnricher
	SetContextEnricher(f)
	t.Cleanup(func() { SetContextEnricher(prev) })
	return f
}

func contextFilterNotices(events []AgentEvent) []string {
	var out []string
	for _, n := range notices(events) {
		if strings.HasPrefix(n, "Context filter") {
			out = append(out, n)
		}
	}
	return out
}

func droppedResult() rag.FilterResult {
	return rag.FilterResult{
		Applied: true, Kept: 4, Dropped: 5, Latency: 38 * time.Millisecond,
		BySource: map[string][2]int{rag.SourceCode: {1, 2}, rag.SourceKB: {2, 3}, rag.SourceEvents: {1, 0}},
	}
}

func TestContextFilterNoticeOnlyWhenDropped(t *testing.T) {
	env := newAutoEnv(t, withAutoConfig(func(m *config.ModelAutoModeConfig) { m.Enabled = false }))
	enricher := installFakeEnricher(t, droppedResult())
	sid := env.session("cf-notice", autoCoder)
	env.decide("implementation", 0.9)

	events := env.run(sid, "fix the bug")
	got := contextFilterNotices(events)
	want := "Context filter: kept 4/9 (38 ms) — code 1/3, kb 2/5, events 1/1"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("notices = %q, want [%q]", got, want)
	}
	var payload *ContextFilterInfo
	for _, ev := range events {
		if ev.Type == AgentEventTypeSystemMessage && ev.ContextFilter != nil {
			payload = ev.ContextFilter
		}
	}
	if payload == nil || payload.Kept != 4 || payload.Dropped != 5 || payload.LatencyMs != 38 ||
		payload.BySource["code"] != (ContextFilterCounts{Kept: 1, Dropped: 2}) || payload.Notice != want {
		t.Fatalf("payload = %+v", payload)
	}
	if enricher.calls != 1 {
		t.Fatalf("enricher calls = %d", enricher.calls)
	}

	// Nothing dropped: no notice.
	enricher.res = rag.FilterResult{Applied: true, Kept: 3, BySource: map[string][2]int{rag.SourceKB: {3, 0}}}
	if got := contextFilterNotices(env.run(sid, "and again")); len(got) != 0 {
		t.Fatalf("unexpected notice: %q", got)
	}
	// Filter off (zero result): no notice.
	enricher.res = rag.FilterResult{}
	if got := contextFilterNotices(env.run(sid, "once more")); len(got) != 0 {
		t.Fatalf("unexpected notice: %q", got)
	}
}

func TestContextFilterFailOpenWarnsOncePerClass(t *testing.T) {
	env := newAutoEnv(t, withAutoConfig(func(m *config.ModelAutoModeConfig) { m.Enabled = false }))
	enricher := installFakeEnricher(t, rag.FilterResult{Reason: "unreachable", BySource: map[string][2]int{}})
	sid := env.session("cf-failopen", autoCoder)

	first := contextFilterNotices(env.run(sid, "one"))
	want := "Context filter unavailable (unreachable): context injected unfiltered"
	if len(first) != 1 || first[0] != want {
		t.Fatalf("first = %q, want [%q]", first, want)
	}
	if again := contextFilterNotices(env.run(sid, "two")); len(again) != 0 {
		t.Fatalf("warning repeated: %q", again)
	}
	enricher.res = rag.FilterResult{Reason: "timeout"}
	if other := contextFilterNotices(env.run(sid, "three")); len(other) != 1 ||
		other[0] != "Context filter unavailable (timeout): context injected unfiltered" {
		t.Fatalf("new class must warn: %q", other)
	}

	enricher.res = rag.FilterResult{Reason: "no_router"}
	nr := contextFilterNotices(env.run(sid, "four"))
	if len(nr) != 1 || !strings.Contains(nr[0], "no decision model is configured") {
		t.Fatalf("no_router = %q", nr)
	}
	if again := contextFilterNotices(env.run(sid, "five")); len(again) != 0 {
		t.Fatalf("no_router repeated: %q", again)
	}

	// Hosted provider: debug only. Partial: applied + warning once.
	enricher.res = rag.FilterResult{Reason: "hosted_provider"}
	if got := contextFilterNotices(env.run(sid, "six")); len(got) != 0 {
		t.Fatalf("hosted must not notify: %q", got)
	}
	res := droppedResult()
	res.Reason = "partial:rate_limited"
	enricher.res = res
	got := contextFilterNotices(env.run(sid, "seven"))
	if len(got) != 2 || !strings.Contains(got[0], "partially unavailable (rate limited)") || !strings.HasPrefix(got[1], "Context filter: kept 4/9") {
		t.Fatalf("partial = %q", got)
	}
}

func TestContextFilterEligibility(t *testing.T) {
	env := newAutoEnv(t, withAutoConfig(func(m *config.ModelAutoModeConfig) { m.Enabled = false }))
	enricher := installFakeEnricher(t, droppedResult())
	sid := env.session("cf-elig", autoCoder)

	// Enrichment still runs for a non-coder agent, but unfiltered and silent.
	env.a.agentName = config.AgentTask
	if got := contextFilterNotices(env.run(sid, "subagent turn")); len(got) != 0 {
		t.Fatalf("subagent got a notice: %q", got)
	}
	if enricher.calls != 1 || !enricher.disabled[0] {
		t.Fatalf("subagent: calls=%d disabled=%v", enricher.calls, enricher.disabled)
	}

	// The context-enricher agent never enriches at all.
	env.a.agentName = config.AgentContextEnricher
	env.run(sid, "enricher turn")
	if enricher.calls != 1 {
		t.Fatalf("context-enricher agent must not enrich: calls=%d", enricher.calls)
	}

	// Coder, system initiated (resumed delegation): unfiltered.
	env.a.agentName = config.AgentCoder
	ctx := withSystemInitiatedRun(context.Background())
	ch, err := env.a.Run(ctx, sid, "resumed")
	if err != nil {
		t.Fatal(err)
	}
	if got := contextFilterNotices(drainAutoEvents(t, ch)); len(got) != 0 {
		t.Fatalf("resumed run got a notice: %q", got)
	}
	if enricher.calls != 2 || !enricher.disabled[1] {
		t.Fatalf("resumed: calls=%d disabled=%v", enricher.calls, enricher.disabled)
	}

	// Plain coder turn is filtered.
	if got := contextFilterNotices(env.run(sid, "user turn")); len(got) != 1 {
		t.Fatalf("coder turn notices = %q", got)
	}
	if enricher.disabled[2] {
		t.Fatalf("coder turn must not disable the filter")
	}
}

func TestContextFilterTelemetryAndEventPrivacy(t *testing.T) {
	const marker = "SECRET-PROMPT-MARKER"
	env := newAutoEnv(t, withAutoConfig(func(m *config.ModelAutoModeConfig) { m.Enabled = false }))
	installFakeEnricher(t, droppedResult())
	sid := env.session("cf-telemetry", autoCoder)

	logs := &logCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(prev) })

	var mu sync.Mutex
	var captured []extension.Event
	ctx, cancel := context.WithCancel(context.Background())
	extevents.SetSink(ctx, func(ev extension.Event) {
		mu.Lock()
		captured = append(captured, ev)
		mu.Unlock()
	})
	t.Cleanup(func() { cancel(); extevents.SetSink(context.Background(), nil) })

	env.run(sid, "do it "+marker)

	var ev *extension.Event
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline) && ev == nil; time.Sleep(10 * time.Millisecond) {
		mu.Lock()
		for i := range captured {
			if captured[i].Topic == extension.TopicContextFilter {
				ev = &captured[i]
			}
		}
		mu.Unlock()
	}
	if ev == nil || ev.Type != extension.EventFiltered || ev.Payload["kept"] != float64(4) || ev.Payload["dropped"] != float64(5) {
		t.Fatalf("event = %+v", ev)
	}
	raw, _ := json.Marshal(ev)
	if strings.Contains(string(raw), marker) || strings.Contains(string(raw), "filtered</ctx>") {
		t.Fatalf("event leaks text: %s", raw)
	}

	logs.mu.Lock()
	text := strings.Join(logs.lines, "\n")
	logs.mu.Unlock()
	var found bool
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "context_filter: decision") {
			found = true
			for _, k := range []string{"context_filter.requests=1", "context_filter.kept=4", "context_filter.dropped=5"} {
				if !strings.Contains(line, k) {
					t.Errorf("counter record lacks %s: %s", k, line)
				}
			}
			if !strings.HasPrefix(line, "INFO ") {
				t.Errorf("counter record must be Info: %s", line)
			}
		}
	}
	if !found {
		t.Fatalf("no counter record in logs:\n%s", text)
	}
	if strings.Contains(text, marker) {
		t.Errorf("logs leak the prompt")
	}
}

func TestContextFilterMemoryResultMerged(t *testing.T) {
	env := newAutoEnv(t, withAutoConfig(func(m *config.ModelAutoModeConfig) { m.Enabled = false }))
	inj := &filteredMemInjector{res: rag.FilterResult{
		Applied: true, Kept: 1, Dropped: 2, Latency: 10 * time.Millisecond,
		BySource: map[string][2]int{rag.SourceMemory: {1, 2}},
	}}
	prev := globalMemoryInjector
	SetMemoryInjector(inj)
	t.Cleanup(func() { SetMemoryInjector(prev) })
	invalidateSessionMemoryBlock("cf-mem")
	t.Cleanup(func() { invalidateSessionMemoryBlock("cf-mem") })

	build := func() []string {
		coll := &filterCollector{}
		ctx := withFilterCollector(sessionPromptCtx("cf-mem", "remember things"), coll)
		sessionMemoryBlock(ctx)
		// Enrichment result of the same turn is merged with the memory one.
		coll.add(rag.FilterResult{Applied: true, Kept: 2, Dropped: 0, BySource: map[string][2]int{rag.SourceKB: {2, 0}}})
		ch := make(chan AgentEvent, 8)
		env.a.reportContextFilter("cf-mem", ch, coll.snapshot())
		close(ch)
		var out []string
		for ev := range ch {
			out = append(out, strings.TrimSpace(ev.SystemMessage))
		}
		return out
	}
	got := build()
	if len(got) != 1 || got[0] != "Context filter: kept 3/5 (10 ms) — kb 2/2, memory 1/3" {
		t.Fatalf("notices = %q", got)
	}
	// The block is frozen per session: no second build, so only the enrichment
	// result remains and nothing was dropped.
	if got := build(); len(got) != 0 {
		t.Fatalf("frozen block re-reported: %q", got)
	}
}

// An ineligible turn marks the context: the memory block is built unfiltered
// and the collector is absent, so nothing is reported.
func TestContextFilterIneligibleMemoryContext(t *testing.T) {
	ctx := rag.WithoutRelevanceFilter(sessionPromptCtx("cf-mem-off", "q"))
	if !rag.RelevanceFilterDisabled(ctx) {
		t.Fatal("flag lost")
	}
	if filterCollectorFrom(ctx) != nil {
		t.Fatal("ineligible context must not carry a collector")
	}
}

type filteredMemInjector struct{ res rag.FilterResult }

func (f *filteredMemInjector) BuildMemoryBlock(ctx context.Context, q string) string {
	out, _ := f.BuildMemoryBlockWithResult(ctx, q)
	return out
}

func (f *filteredMemInjector) BuildMemoryBlockWithResult(context.Context, string) (string, rag.FilterResult) {
	return "<memories>m</memories>", f.res
}
