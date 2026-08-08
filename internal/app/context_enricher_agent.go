package app

import (
	"context"
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
	defaultEnrichmentLoopTimeout  = 60 * time.Second
	defaultEnrichmentLoopMaxChars = 6000
	// noRelevantContextMarker is what the enrichment agent emits when it found nothing.
	noRelevantContextMarker = "NO_RELEVANT_CONTEXT"
	enrichedContextOpenTag  = "<enriched_context>"
	enrichedContextCloseTag = "</enriched_context>"
)

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
	fallback *rag.ContextEnricher

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
	return &agentLoopEnricher{
		sessions:     sessions,
		messages:     messages,
		fallback:     fallback,
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
	return e.EnrichContextForSession(ctx, "", query)
}

// EnrichContextForSession runs the enrichment loop for the given chat session and
// returns the context block to append to the user prompt (empty when nothing helps).
func (e *agentLoopEnricher) EnrichContextForSession(ctx context.Context, sessionID, query string) string {
	if e == nil || strings.TrimSpace(query) == "" {
		return ""
	}

	block, err := e.runLoop(ctx, sessionID, query)
	if err != nil {
		logging.Warn("context enrichment agent loop failed", "error", err)
	}
	if block != "" {
		return block
	}
	if e.fallbackOff || e.fallback == nil {
		return ""
	}
	logging.Debug("context enrichment: falling back to search pipeline")
	return e.fallback.EnrichContext(ctx, query)
}

func (e *agentLoopEnricher) runLoop(ctx context.Context, sessionID, query string) (string, error) {
	enrichAgent, err := e.ensureAgent()
	if err != nil {
		return "", fmt.Errorf("enrichment agent unavailable: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	loopSession, cleanup, err := e.createSession(runCtx, sessionID)
	if err != nil {
		return "", err
	}
	defer cleanup()

	done, err := enrichAgent.Run(runCtx, loopSession.ID, query)
	if err != nil {
		return "", fmt.Errorf("enrichment run failed: %w", err)
	}

	var result agent.AgentEvent
	select {
	case result = <-done:
	case <-runCtx.Done():
		enrichAgent.Cancel(loopSession.ID)
		return "", fmt.Errorf("enrichment loop timed out after %s", e.timeout)
	}
	if result.Error != nil {
		return "", fmt.Errorf("enrichment loop error: %w", result.Error)
	}
	if result.Message.Role != message.Assistant {
		return "", fmt.Errorf("enrichment loop produced no assistant message")
	}

	e.chargeParent(ctx, sessionID, loopSession.ID)

	return normalizeEnrichedBlock(result.Message.Content().String(), e.maxChars), nil
}

// createSession creates the session the loop runs in. When the loop is visible (default)
// and there is a parent chat session, it becomes a child session of it so the UI can show
// the whole retrieval trace; otherwise it is a standalone session deleted after the run.
func (e *agentLoopEnricher) createSession(ctx context.Context, parentSessionID string) (session.Session, func(), error) {
	noop := func() {}
	if parentSessionID != "" && !e.hiddenInChat {
		s, err := e.sessions.CreateTaskSession(ctx, "ctxenrich-"+uuid.NewString(), parentSessionID, "Context enrichment")
		if err != nil {
			return session.Session{}, noop, fmt.Errorf("failed to create enrichment session: %w", err)
		}
		return s, noop, nil
	}

	s, err := e.sessions.Create(ctx, "Context enrichment")
	if err != nil {
		return session.Session{}, noop, fmt.Errorf("failed to create enrichment session: %w", err)
	}
	cleanup := func() {
		// Detached context: the run context is already cancelled by the caller's defer.
		if err := e.sessions.Delete(context.Background(), s.ID); err != nil {
			logging.Debug("context enrichment: failed to delete hidden session", "error", err)
		}
	}
	return s, cleanup, nil
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
