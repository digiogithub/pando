package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/session"
)

const (
	// defaultEnrichmentLoopTimeout bounds a single enrichment loop run when
	// ContextEnrichmentAgentLoopTimeoutSeconds is unset. Lowered from the
	// original 60s: the main prompt blocks on this (see EnrichContextForSession),
	// and 25s is already a long silence for an ACP client to sit through even
	// with the start/heartbeat/done notices this package now emits.
	defaultEnrichmentLoopTimeout  = 25 * time.Second
	defaultEnrichmentLoopMaxChars = 6000
	// noRelevantContextMarker is what the enrichment agent emits when it found nothing.
	noRelevantContextMarker = "NO_RELEVANT_CONTEXT"
	enrichedContextOpenTag  = "<enriched_context>"
	enrichedContextCloseTag = "</enriched_context>"
	// ctxEnrichSessionIDPrefix identifies a session as an ephemeral enrichment-loop
	// run rather than a real chat session. Shared with the remembrances session
	// indexer (which must skip these) and the startup cleanup of leftovers from
	// before this package deleted its own sessions.
	ctxEnrichSessionIDPrefix = "ctxenrich-"
)

// ErrEnrichmentTimeout is wrapped into the error runLoop returns when the loop
// did not finish within its configured timeout, so EnrichContextForSession can
// tell a timeout apart from every other failure without string matching.
var ErrEnrichmentTimeout = errors.New("enrichment loop timed out")

// searchFallbackEnricher is the minimal surface agentLoopEnricher needs from the
// classic single-shot search enricher (*rag.ContextEnricher). Kept as a narrow
// interface rather than the concrete type so tests can substitute a fake
// instead of constructing a real one.
type searchFallbackEnricher interface {
	EnrichContext(ctx context.Context, query string) string
}

// agentLoopEnricher runs context enrichment as a dedicated agent loop on the
// context-enricher model, independent of the model the user selected for the main
// agent. The loop may call the memory, knowledge-base, events and code-index tools
// as many times as it needs; the main agent only ever sees the final context block.
//
// It implements agent.SessionContextEnricher so the run can be attached to the active
// chat session as a child session (visible and inspectable from the UI).
type agentLoopEnricher struct {
	sessions session.Service
	messages message.Service
	// fallback is the classic single-shot search enricher, used when the loop is
	// unavailable, times out or returns nothing (unless disabled by config).
	fallback searchFallbackEnricher

	timeout      time.Duration
	maxChars     int
	fallbackOff  bool
	hiddenInChat bool
	everyMessage bool
	silent       bool

	// enrich is built once (warm start at boot, or lazily on first use) and reused across
	// runs so the provider is not rebuilt on every prompt. A failed build is retried on the
	// next call: the model may simply not have been available yet at startup.
	agentMu  sync.Mutex
	enrich   agent.Service
	newAgent func() (agent.Service, error)
}

// newAgentLoopEnricher builds the agent-loop enricher. fallback may be nil.
func newAgentLoopEnricher(
	sessions session.Service,
	messages message.Service,
	remembrances *rag.RemembrancesService,
	lspProvider tools.LSPProvider,
	fallback *rag.ContextEnricher,
	cfg config.RemembrancesConfig,
) *agentLoopEnricher {
	timeout := defaultEnrichmentLoopTimeout
	if cfg.ContextEnrichmentAgentLoopTimeoutSeconds > 0 {
		timeout = time.Duration(cfg.ContextEnrichmentAgentLoopTimeoutSeconds) * time.Second
	}
	maxChars := defaultEnrichmentLoopMaxChars
	if cfg.ContextEnrichmentAgentLoopMaxChars > 0 {
		maxChars = cfg.ContextEnrichmentAgentLoopMaxChars
	}
	// Converted explicitly (rather than assigned straight into the interface
	// field) so a nil *rag.ContextEnricher becomes a true nil interface: a
	// typed-nil pointer boxed directly into an interface value is non-nil, which
	// would silently defeat every "e.fallback == nil" check below.
	var fb searchFallbackEnricher
	if fallback != nil {
		fb = fallback
	}
	return &agentLoopEnricher{
		sessions:     sessions,
		messages:     messages,
		fallback:     fb,
		timeout:      timeout,
		maxChars:     maxChars,
		fallbackOff:  cfg.ContextEnrichmentAgentLoopFallbackDisabled,
		hiddenInChat: cfg.ContextEnrichmentAgentLoopHiddenInChat,
		everyMessage: cfg.ContextEnrichmentAgentLoopEveryMessage,
		silent:       cfg.ContextEnrichmentAgentLoopSilent,
		newAgent: func() (agent.Service, error) {
			return agent.NewAgent(
				config.AgentContextEnricher,
				sessions,
				messages,
				agent.ContextEnricherAgentTools(remembrances, lspProvider),
				nil,
			)
		},
	}
}

// SessionStartOnly reports whether the loop runs only for the first message of a session.
func (e *agentLoopEnricher) SessionStartOnly() bool { return !e.everyMessage }

// Announce reports whether the chat shows start/end notices while the loop runs.
func (e *agentLoopEnricher) Announce() bool { return !e.silent }

// Warmup builds the enrichment agent (and its provider) ahead of the first prompt so the
// first enriched message does not pay the provider construction cost. Safe to call from a
// goroutine at startup and safe to call more than once.
func (e *agentLoopEnricher) Warmup() {
	if e == nil {
		return
	}
	if _, err := e.ensureAgent(); err != nil {
		logging.Warn("context enrichment: warm start failed; will retry on first use", "error", err)
		return
	}
	logging.Debug("context enrichment: agent warm-started")
}

// ensureAgent returns the enrichment agent, building it on first use (or after a previous
// build failed).
func (e *agentLoopEnricher) ensureAgent() (agent.Service, error) {
	e.agentMu.Lock()
	defer e.agentMu.Unlock()
	if e.enrich != nil {
		return e.enrich, nil
	}
	built, err := e.newAgent()
	if err != nil {
		return nil, err
	}
	e.enrich = built
	return built, nil
}

// EnrichContext satisfies agent.ContextEnricher for callers with no session at hand.
func (e *agentLoopEnricher) EnrichContext(ctx context.Context, query string) string {
	return e.EnrichContextForSession(ctx, "", query).Block
}

// EnrichContextForSession runs the enrichment loop for the given chat session and
// reports the context block to append to the user prompt (empty when nothing
// helps) along with how the run went, so the caller can render a meaningful
// status notice instead of just "some text came back".
func (e *agentLoopEnricher) EnrichContextForSession(ctx context.Context, sessionID, query string) agent.EnrichmentOutcome {
	if e == nil || strings.TrimSpace(query) == "" {
		return agent.EnrichmentOutcome{}
	}

	start := time.Now()
	block, err := e.runLoop(ctx, sessionID, query)
	timedOut := errors.Is(err, ErrEnrichmentTimeout)
	if err != nil {
		logging.Warn("context enrichment agent loop failed", "session_id", sessionID, "error", err)
	}
	if block != "" {
		return agent.EnrichmentOutcome{
			Block: block, Source: agent.EnrichmentSourceAgentLoop,
			TimedOut: timedOut, Duration: time.Since(start),
		}
	}
	if e.fallbackOff || e.fallback == nil {
		return agent.EnrichmentOutcome{TimedOut: timedOut, Duration: time.Since(start)}
	}
	logging.Debug("context enrichment: falling back to search pipeline", "session_id", sessionID)
	fallbackBlock := e.fallback.EnrichContext(ctx, query)
	outcome := agent.EnrichmentOutcome{Block: fallbackBlock, TimedOut: timedOut, Duration: time.Since(start)}
	if fallbackBlock != "" {
		outcome.Source = agent.EnrichmentSourceSearchFallback
	}
	return outcome
}

func (e *agentLoopEnricher) runLoop(ctx context.Context, sessionID, query string) (string, error) {
	enrichAgent, err := e.ensureAgent()
	if err != nil {
		return "", fmt.Errorf("enrichment agent unavailable: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel() // runs only after the drain below returns (the agent has finished).

	loopSession, cleanup, err := e.createSession(runCtx, sessionID)
	if err != nil {
		return "", err
	}
	defer cleanup() // LIFO: runs before cancel(), also only after the drain returns.

	done, err := enrichAgent.Run(runCtx, loopSession.ID, query)
	if err != nil {
		return "", fmt.Errorf("enrichment run failed: %w", err)
	}

	// Drain until the channel closes and use the terminal event as the result,
	// instead of reading a single event (the original bug: the agent streams
	// many events — status, deltas, tool calls, token usage — before the real
	// one, so the first receive is essentially never it). On timeout this also
	// cancels the run and keeps draining for a bounded grace period, so the
	// agent's own in-flight write finishes before cleanup()/cancel() run.
	final, timedOut := agent.CollectRunResult(runCtx, done, func() { enrichAgent.Cancel(loopSession.ID) })
	if timedOut {
		return "", fmt.Errorf("%w after %s", ErrEnrichmentTimeout, e.timeout)
	}
	if final.Error != nil {
		if errors.Is(final.Error, agent.ErrRequestCancelled) || errors.Is(final.Error, context.Canceled) {
			return "", fmt.Errorf("enrichment loop cancelled: %w", final.Error)
		}
		return "", fmt.Errorf("enrichment loop error: %w", final.Error)
	}
	if final.Type != agent.AgentEventTypeResponse || final.Message.Role != message.Assistant {
		return "", fmt.Errorf("enrichment loop ended without a response (last event %q)", final.Type)
	}

	e.chargeParent(ctx, sessionID, loopSession.ID)

	return normalizeEnrichedBlock(final.Message.Content().String(), e.maxChars), nil
}

// createSession creates the session the loop runs in. When there is a parent chat
// session (the default), it becomes a child session of it so the UI can show the
// live retrieval trace while the loop runs; otherwise (no parent, or hiddenInChat)
// it is a standalone session never shown in the UI. Either way it is ephemeral:
// the returned cleanup deletes it once the run finishes draining (see runLoop's
// defer order), unless cfg.Debug is on. Before this, the child-session branch
// never deleted its session at all, which is what let 28 "ctxenrich-*" sessions
// (and their duplicated remembrances index entries) pile up — see
// [[pando/fixes/context_enricher_agent_loop_first_event.md]].
func (e *agentLoopEnricher) createSession(ctx context.Context, parentSessionID string) (session.Session, func(), error) {
	if parentSessionID != "" && !e.hiddenInChat {
		s, err := e.sessions.CreateTaskSession(ctx, ctxEnrichSessionIDPrefix+uuid.NewString(), parentSessionID, "Context enrichment")
		if err != nil {
			return session.Session{}, func() {}, fmt.Errorf("failed to create enrichment session: %w", err)
		}
		return s, e.deleteSessionCleanup(s.ID), nil
	}

	s, err := e.sessions.Create(ctx, "Context enrichment")
	if err != nil {
		return session.Session{}, func() {}, fmt.Errorf("failed to create enrichment session: %w", err)
	}
	return s, e.deleteSessionCleanup(s.ID), nil
}

// deleteSessionCleanup returns the callback that removes an ephemeral enrichment
// session after the run finishes draining. It is skipped when cfg.Debug is on so a
// developer investigating a bad enrichment result can still open the run's child
// session from the UI, at the cost of the DB litter this package otherwise avoids.
func (e *agentLoopEnricher) deleteSessionCleanup(sessionID string) func() {
	return func() {
		if cfg := config.Get(); cfg != nil && cfg.Debug {
			return
		}
		// Detached context: the run context (and its timeout) is already done by
		// the time this fires — it runs from runLoop's defer, after the drain.
		if err := e.sessions.Delete(context.Background(), sessionID); err != nil {
			logging.Debug("context enrichment: failed to delete ephemeral session", "session_id", sessionID, "error", err)
		}
	}
}

// chargeParent adds the loop's cost to the chat session so the enrichment model shows
// up in the session cost the user sees.
func (e *agentLoopEnricher) chargeParent(ctx context.Context, parentSessionID, loopSessionID string) {
	if parentSessionID == "" {
		return
	}
	loopSession, err := e.sessions.Get(ctx, loopSessionID)
	if err != nil {
		return
	}
	parent, err := e.sessions.Get(ctx, parentSessionID)
	if err != nil {
		return
	}
	parent.Cost += loopSession.Cost
	if _, err := e.sessions.Save(ctx, parent); err != nil {
		logging.Debug("context enrichment: failed to charge parent session", "error", err)
	}
}

// normalizeEnrichedBlock extracts the <enriched_context> block from the agent's final
// message, drops the no-context marker and truncates to maxChars.
func normalizeEnrichedBlock(raw string, maxChars int) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}

	if start := strings.Index(text, enrichedContextOpenTag); start >= 0 {
		rest := text[start+len(enrichedContextOpenTag):]
		if end := strings.Index(rest, enrichedContextCloseTag); end >= 0 {
			rest = rest[:end]
		}
		text = strings.TrimSpace(rest)
	}

	if text == "" || strings.Contains(text, noRelevantContextMarker) {
		return ""
	}

	if maxChars > 0 && len(text) > maxChars {
		text = strings.TrimSpace(text[:maxChars]) + "\n… (truncated)"
	}

	return enrichedContextOpenTag + "\n" + text + "\n" + enrichedContextCloseTag
}
