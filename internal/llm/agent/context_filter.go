package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/extevents"
	"github.com/digiogithub/pando/internal/llm/modelrouter"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/rag"
)

// Context relevance filter (PANDO-EP-0018): the decision model may drop
// retrieved candidates (enrichment block, memory block) that are not useful for
// the prompt. The filter itself lives in internal/rag and is wired by the app;
// the agent only decides whether a turn is eligible, collects the results and
// reports them (notice, external event, telemetry records).

// FilteredContextEnricher is an optional extension of ContextEnricher for
// enrichers that report what the relevance filter did.
type FilteredContextEnricher interface {
	EnrichContextWithResult(ctx context.Context, query string) (string, rag.FilterResult)
}

// FilteredSessionContextEnricher is the session-aware counterpart of
// FilteredContextEnricher.
type FilteredSessionContextEnricher interface {
	EnrichContextForSessionWithResult(ctx context.Context, sessionID, query string) (string, rag.FilterResult)
}

// FilteredMemoryInjector is an optional extension of MemoryInjector that
// reports what the relevance filter did.
type FilteredMemoryInjector interface {
	BuildMemoryBlockWithResult(ctx context.Context, query string) (string, rag.FilterResult)
}

// ContextFilterInfo summarises the relevance filtering of one turn. It is the
// payload of the context filter notice (AgentEvent.ContextFilter). It never
// carries prompt or snippet text.
type ContextFilterInfo struct {
	Kept    int `json:"kept"`
	Dropped int `json:"dropped"`
	// BySource maps "code", "kb", "events" and "memory" to {kept, dropped};
	// only sources with at least one candidate are present.
	BySource       map[string]ContextFilterCounts `json:"bySource"`
	Threshold      float64                        `json:"threshold"`
	LatencyMs      int64                          `json:"latencyMs"`
	RouterProvider string                         `json:"routerProvider,omitempty"`
	RouterModel    string                         `json:"routerModel,omitempty"`
	// Reason is empty on full success, otherwise "partial:<class>".
	Reason string `json:"reason,omitempty"`
	// Notice is the human readable line (same text as the system message).
	Notice string `json:"notice"`
}

// ContextFilterCounts is the kept/dropped count of one source.
type ContextFilterCounts struct {
	Kept    int `json:"kept"`
	Dropped int `json:"dropped"`
}

// contextFilterSourceOrder is the stable order of the per-source detail.
var contextFilterSourceOrder = []string{rag.SourceCode, rag.SourceKB, rag.SourceEvents, rag.SourceMemory}

// filterCollector gathers the FilterResults produced during one turn.
type filterCollector struct {
	mu      sync.Mutex
	results []rag.FilterResult
}

func (c *filterCollector) add(r rag.FilterResult) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.results = append(c.results, r)
	c.mu.Unlock()
}

func (c *filterCollector) snapshot() []rag.FilterResult {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]rag.FilterResult(nil), c.results...)
}

type filterCollectorKey struct{}

func withFilterCollector(ctx context.Context, c *filterCollector) context.Context {
	return context.WithValue(ctx, filterCollectorKey{}, c)
}

func filterCollectorFrom(ctx context.Context) *filterCollector {
	if ctx == nil {
		return nil
	}
	c, _ := ctx.Value(filterCollectorKey{}).(*filterCollector)
	return c
}

// contextFilterEligible reports whether the relevance filter may run on this
// turn: the same gate as Auto model mode (coder agent, user initiated, not a
// clean-mode run) except that Auto mode itself need not be on, and child
// sessions (delegated subagents) are excluded. Enrichment itself is not
// affected: an ineligible turn only runs unfiltered.
func (a *agent) contextFilterEligible(ctx context.Context, sessionID, parentSessionID string) bool {
	return a.agentName == config.AgentCoder &&
		sessionID != "" &&
		parentSessionID == "" &&
		!isCleanModeContext(ctx) &&
		!isSystemInitiatedRun(ctx)
}

// mergeFilterResults adds up the results of a turn. Applied is true when any
// of them judged candidates.
func mergeFilterResults(results []rag.FilterResult) rag.FilterResult {
	merged := rag.FilterResult{BySource: map[string][2]int{}}
	for _, r := range results {
		merged.Kept += r.Kept
		merged.Dropped += r.Dropped
		merged.Latency += r.Latency
		merged.Applied = merged.Applied || r.Applied
		for src, kd := range r.BySource {
			cur := merged.BySource[src]
			cur[0] += kd[0]
			cur[1] += kd[1]
			merged.BySource[src] = cur
		}
		if merged.Reason == "" && strings.HasPrefix(r.Reason, modelrouter.RelevanceReasonPartialPfx) {
			merged.Reason = r.Reason
		}
	}
	return merged
}

// contextFilterNotice renders the notice line:
//
//	Context filter: kept 4/9 (38 ms) — code 1/3, kb 2/4, events 1/2, memory 0/0
//
// Only sources with at least one candidate are listed.
func contextFilterNotice(merged rag.FilterResult) string {
	total := merged.Kept + merged.Dropped
	text := fmt.Sprintf("Context filter: kept %d/%d (%d ms)", merged.Kept, total, merged.Latency.Milliseconds())
	var parts []string
	for _, src := range contextFilterSourceOrder {
		kd, ok := merged.BySource[src]
		if !ok || kd[0]+kd[1] == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d", src, kd[0], kd[0]+kd[1]))
	}
	if len(parts) > 0 {
		text += " — " + strings.Join(parts, ", ")
	}
	return text
}

// reportContextFilter handles the results collected during the turn: debug
// log, telemetry records, fail-open warnings and, only when something was
// dropped, the notice and the ContextFiltered event.
func (a *agent) reportContextFilter(sessionID string, eventCh chan<- AgentEvent, results []rag.FilterResult) {
	if len(results) == 0 {
		return
	}
	cfg := config.Get()
	var threshold float64
	var routerProvider, routerModel string
	if cfg != nil {
		threshold = cfg.Remembrances.DecisionFilterThreshold()
		routerProvider = string(cfg.DecisionModel.Router.EffectiveProvider())
		routerModel = cfg.DecisionModel.Router.Model
	}

	for _, r := range results {
		a.handleContextFilterReason(sessionID, eventCh, r)
	}

	merged := mergeFilterResults(results)
	if merged.Kept+merged.Dropped == 0 && !merged.Applied {
		return
	}

	// Debug record per turn: counts, latency and reason only, never text.
	for _, src := range contextFilterSourceOrder {
		if kd, ok := merged.BySource[src]; ok && kd[0]+kd[1] > 0 {
			logging.Debug("context_filter: source", "session_id", sessionID, "source", src, "kept", kd[0], "dropped", kd[1])
		}
	}
	logging.Debug("context_filter: turn",
		"session_id", sessionID, "kept", merged.Kept, "dropped", merged.Dropped,
		"latency_ms", merged.Latency.Milliseconds(), "applied", merged.Applied, "reason", merged.Reason)

	if !merged.Applied || merged.Dropped == 0 {
		if merged.Applied {
			// Counter record: a successful run that dropped nothing.
			logging.Info("context_filter: decision", "session_id", sessionID,
				"context_filter.requests", 1, "context_filter.kept", merged.Kept, "context_filter.dropped", 0)
		}
		return
	}

	// Counter record for telemetry/logs: numbers and ids only.
	logging.Info("context_filter: decision", "session_id", sessionID,
		"context_filter.requests", 1,
		"context_filter.kept", merged.Kept,
		"context_filter.dropped", merged.Dropped,
		"router_provider", routerProvider,
		"router_model", routerModel,
		"latency_ms", merged.Latency.Milliseconds(),
		"reason", merged.Reason,
	)

	info := ContextFilterInfo{
		Kept:           merged.Kept,
		Dropped:        merged.Dropped,
		BySource:       map[string]ContextFilterCounts{},
		Threshold:      threshold,
		LatencyMs:      merged.Latency.Milliseconds(),
		RouterProvider: routerProvider,
		RouterModel:    routerModel,
		Reason:         merged.Reason,
		Notice:         contextFilterNotice(merged),
	}
	bySrc := map[string][2]int{}
	for src, kd := range merged.BySource {
		if kd[0]+kd[1] == 0 {
			continue
		}
		info.BySource[src] = ContextFilterCounts{Kept: kd[0], Dropped: kd[1]}
		bySrc[src] = kd
	}
	extevents.ContextFiltered(extevents.ContextFilteredInfo{
		SessionID: sessionID, Kept: info.Kept, Dropped: info.Dropped, BySource: bySrc,
		Threshold: info.Threshold, LatencyMs: info.LatencyMs,
		RouterProvider: routerProvider, RouterModel: routerModel, Reason: info.Reason,
	})
	a.emitContextFilterNotice(sessionID, eventCh, info)
}

// handleContextFilterReason logs and warns about a filter that did not (fully)
// run. Warnings are once per session and error class; the first one is the
// notice, later ones are Debug only.
func (a *agent) handleContextFilterReason(sessionID string, eventCh chan<- AgentEvent, r rag.FilterResult) {
	reason := r.Reason
	if reason == "" || reason == modelrouter.RelevanceReasonNoCands {
		return
	}
	switch {
	case reason == modelrouter.RelevanceReasonHosted:
		logging.Debug("context_filter: skipped, decision provider is hosted and local-only is on", "session_id", sessionID)
		return
	case reason == modelrouter.RelevanceReasonNoRouter:
		if !warnAutoOnce(sessionID, "ctxfilter:no_router") {
			logging.Debug("context_filter: no decision model configured (already warned)", "session_id", sessionID)
			return
		}
		logging.Warn("context_filter: no decision model configured", "session_id", sessionID)
		a.emitSystemNotice(sessionID, eventCh, "Context filter: no decision model is configured, context injected unfiltered")
		return
	}

	class := strings.TrimPrefix(reason, modelrouter.RelevanceReasonPartialPfx)
	text := fmt.Sprintf("Context filter unavailable (%s): context injected unfiltered", routerErrorWording(class))
	if strings.HasPrefix(reason, modelrouter.RelevanceReasonPartialPfx) {
		text = fmt.Sprintf("Context filter partially unavailable (%s): some context injected unfiltered", routerErrorWording(class))
	}
	// Counter record: error class only.
	logging.Info("context_filter: failopen", "session_id", sessionID, "context_filter.failopen", 1, "class", class)
	if !warnAutoOnce(sessionID, "ctxfilter:"+class) {
		logging.Debug("context_filter: fail-open (already warned)", "session_id", sessionID, "class", class)
		return
	}
	logging.Warn("context_filter: fail-open", "session_id", sessionID, "class", class)
	a.emitSystemNotice(sessionID, eventCh, text)
}

// emitContextFilterNotice publishes the notice as a system message carrying the
// structured ContextFilterInfo in AgentEvent.ContextFilter, and records it as a
// run status message, like emitRoutingNotice.
func (a *agent) emitContextFilterNotice(sessionID string, eventCh chan<- AgentEvent, info ContextFilterInfo) {
	a.addRunStatusMessage(sessionID, info.Notice)
	payload := info
	ev := AgentEvent{
		Type:          AgentEventTypeSystemMessage,
		SessionID:     sessionID,
		SystemMessage: info.Notice + "\n",
		ContextFilter: &payload,
	}
	a.publishEvent(ev)
	if eventCh != nil {
		select {
		case eventCh <- ev:
		default:
		}
	}
}

var _ FilteredContextEnricher = (*rag.ContextEnricher)(nil)
