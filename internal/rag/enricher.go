package rag

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/rag/events"
)

// EnricherConfig holds all tunable parameters for ContextEnricher.
type EnricherConfig struct {
	KBResults      int
	CodeResults    int
	CodeProject    string
	EventsResults  int
	EventsSubject  string
	EventsLastDays int
	MinScore       float64

	// Char budgets per section (0 = no per-section limit).
	KBMaxChars     int
	CodeMaxChars   int
	EventsMaxChars int
	// TotalMaxChars caps the combined output across all sections (0 = no cap).
	TotalMaxChars int
}

// ContextEnricher performs pre-prompt KB, events and code searches in parallel and
// formats only relevant results (above minScore) as context to prepend to the user's message.
// A QueryPlanner derives per-source queries from the raw prompt before search.
type ContextEnricher struct {
	svc     *RemembrancesService
	cfg     EnricherConfig
	planner QueryPlanner
	// enabled allows runtime toggling without replacing the global enricher pointer.
	enabled atomic.Bool
	// relevance is the optional decision-model filter; nil = off. Swapped
	// atomically because hot reload replaces it while turns are running.
	relevance atomic.Pointer[relevanceHolder]
}

// relevanceHolder lets a RelevanceFilter interface value live in an atomic.Pointer.
type relevanceHolder struct{ f RelevanceFilter }

// NewContextEnricher creates a ContextEnricher from the given RemembrancesService and config values.
// Returns nil when the service is nil.
func NewContextEnricher(svc *RemembrancesService, cfg EnricherConfig) *ContextEnricher {
	if svc == nil {
		return nil
	}
	// Apply safe defaults.
	if cfg.KBResults <= 0 {
		cfg.KBResults = 2
	}
	if cfg.CodeResults <= 0 {
		cfg.CodeResults = 3
	}
	if cfg.EventsResults <= 0 {
		cfg.EventsResults = 2
	}
	if cfg.EventsLastDays <= 0 {
		cfg.EventsLastDays = 30
	}
	if cfg.MinScore <= 0 {
		cfg.MinScore = 0.55
	}
	e := &ContextEnricher{
		svc:     svc,
		cfg:     cfg,
		planner: &HeuristicPlanner{},
	}
	e.enabled.Store(true)
	return e
}

// SetPlanner replaces the query planner used to derive per-source queries.
// Pass nil to revert to the default HeuristicPlanner.
func (e *ContextEnricher) SetPlanner(p QueryPlanner) {
	if e == nil {
		return
	}
	if p == nil {
		p = &HeuristicPlanner{}
	}
	e.planner = p
}

// SetEnabled enables or disables enrichment at runtime without removing the enricher
// from the agent. When disabled, EnrichContext returns an empty string immediately.
func (e *ContextEnricher) SetEnabled(v bool) {
	if e == nil {
		return
	}
	e.enabled.Store(v)
}

// IsEnabled reports whether context enrichment is currently active.
func (e *ContextEnricher) IsEnabled() bool {
	if e == nil {
		return false
	}
	return e.enabled.Load()
}

// PlannerMode returns "llm" when an LLMPlanner is active, "heuristic" otherwise.
func (e *ContextEnricher) PlannerMode() string {
	if e == nil {
		return "heuristic"
	}
	if _, ok := e.planner.(*LLMPlanner); ok {
		return "llm"
	}
	return "heuristic"
}

// SetRelevanceFilter installs the decision-model relevance filter applied once
// over all retrieved candidates before they are formatted. Pass nil to turn it
// off. Safe to call concurrently with EnrichContext.
func (e *ContextEnricher) SetRelevanceFilter(f RelevanceFilter) {
	if e == nil {
		return
	}
	if f == nil {
		e.relevance.Store(nil)
		return
	}
	e.relevance.Store(&relevanceHolder{f: f})
}

func (e *ContextEnricher) relevanceFilter() RelevanceFilter {
	if h := e.relevance.Load(); h != nil {
		return h.f
	}
	return nil
}

// enrichHit is a retrieved item that passed MinScore: the candidate shown to
// the relevance filter plus the exact text it contributes to the block.
type enrichHit struct {
	cand  RelevanceCandidate
	entry string
}

// EnrichContext searches KB, events, and code index in parallel using queries derived
// from the raw user prompt via the QueryPlanner, filters results below minScore,
// and returns a formatted context block.
// Sections with no results above the threshold are omitted entirely.
// Returns an empty string when nothing relevant is found or enrichment is disabled.
func (e *ContextEnricher) EnrichContext(ctx context.Context, query string) string {
	out, _ := e.EnrichContextWithResult(ctx, query)
	return out
}

// EnrichContextWithResult is EnrichContext that also reports what the relevance
// filter did. The result is the zero FilterResult when no filter is installed
// or nothing was retrieved.
func (e *ContextEnricher) EnrichContextWithResult(ctx context.Context, query string) (string, FilterResult) {
	var res FilterResult
	if e == nil || e.svc == nil {
		return "", res
	}
	if !e.enabled.Load() {
		return "", res
	}

	plan, err := e.planner.Plan(ctx, query)
	if err != nil {
		logging.Debug("context enricher: planner failed, using raw query", "error", err)
		plan = &EnrichmentPlan{
			SemanticQuery: query,
			CodeQuery:     query,
			EventsQuery:   query,
		}
	}

	// Override result counts from plan only when plan specifies them.
	kbN := e.cfg.KBResults
	if plan.KBResults > 0 {
		kbN = plan.KBResults
	}
	codeN := e.cfg.CodeResults
	if plan.CodeResults > 0 {
		codeN = plan.CodeResults
	}
	eventsN := e.cfg.EventsResults
	if plan.EventsResults > 0 {
		eventsN = plan.EventsResults
	}

	var (
		kbHits, eventsHits, codeHits []enrichHit
		wg                           sync.WaitGroup
	)

	if e.svc.KB != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kbHits = e.searchKB(ctx, plan.SemanticQuery, kbN)
		}()
	}

	if e.svc.Events != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			eventsHits = e.searchEvents(ctx, plan.EventsQuery, eventsN)
		}()
	}

	if e.svc.Code != nil && e.cfg.CodeProject != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codeHits = e.searchCode(ctx, plan.CodeQuery, codeN)
		}()
	}

	wg.Wait()

	return e.assemble(ctx, query, codeHits, kbHits, eventsHits)
}

// assemble runs the relevance filter (when installed) once over every
// candidate, then formats the kept hits and applies the total char budget.
func (e *ContextEnricher) assemble(ctx context.Context, query string, codeHits, kbHits, eventsHits []enrichHit) (string, FilterResult) {
	var res FilterResult

	// Relevance filter: one pass over every candidate, in code -> KB -> events order.
	if f := e.relevanceFilter(); f != nil {
		all := make([]RelevanceCandidate, 0, len(codeHits)+len(kbHits)+len(eventsHits))
		for _, group := range [][]enrichHit{codeHits, kbHits, eventsHits} {
			for _, h := range group {
				all = append(all, h.cand)
			}
		}
		keep, r := applyRelevanceFilter(ctx, f, query, all)
		res = r
		idx := 0
		pick := func(group []enrichHit) []enrichHit {
			out := group[:0:0]
			for _, h := range group {
				if keep[idx] {
					out = append(out, h)
				}
				idx++
			}
			return out
		}
		codeHits, kbHits, eventsHits = pick(codeHits), pick(kbHits), pick(eventsHits)
	}

	// Assemble: code first (most precise), then KB, then events.
	sections := []string{
		e.formatSection("## Code Index\n", codeHits, e.cfg.CodeMaxChars),
		e.formatSection("## Knowledge Base\n", kbHits, e.cfg.KBMaxChars),
		e.formatSection("## Past Session Events\n", eventsHits, e.cfg.EventsMaxChars),
	}
	var parts []string
	for _, c := range sections {
		if c != "" {
			parts = append(parts, c)
		}
	}

	if len(parts) == 0 {
		return "", res
	}

	body := strings.Join(parts, "\n\n")

	// Apply total char budget.
	if e.cfg.TotalMaxChars > 0 && len(body) > e.cfg.TotalMaxChars {
		body = body[:e.cfg.TotalMaxChars] + "\n<!-- context truncated -->"
	}

	var sb strings.Builder
	sb.WriteString("<context source=\"remembrances\">\n")
	sb.WriteString(body)
	sb.WriteString("\n</context>")
	return sb.String(), res
}

// formatSection renders one section from its hits, applying the per-section
// char budget exactly as the unfiltered pipeline always did. Returns "" when
// there are no hits.
func (e *ContextEnricher) formatSection(header string, hits []enrichHit, maxChars int) string {
	if len(hits) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(header)
	for _, h := range hits {
		sb.WriteString(h.entry)
		if maxChars > 0 && sb.Len() >= maxChars {
			break
		}
	}
	out := sb.String()
	if maxChars > 0 && len(out) > maxChars {
		out = out[:maxChars] + "…"
	}
	return out
}

func (e *ContextEnricher) searchKB(ctx context.Context, query string, n int) []enrichHit {
	results, err := e.svc.KB.SearchDocuments(ctx, query, n)
	if err != nil {
		logging.Debug("context enricher: kb search failed", "error", err)
		return nil
	}

	var hits []enrichHit
	for _, r := range results {
		if r.Score < e.cfg.MinScore {
			continue
		}
		filePath := r.Document.FilePath
		if filePath == "" {
			filePath = fmt.Sprintf("document-%d", r.Document.ID)
		}
		chunk := strings.TrimSpace(r.ChunkContent)
		if chunk == "" {
			chunk = strings.TrimSpace(r.Document.Content)
		}
		// Limit each entry to 200 chars — enough for orientation, not a full dump.
		if len(chunk) > 200 {
			chunk = chunk[:200] + "…"
		}
		chunk = strings.ReplaceAll(chunk, "\n", " ")
		// Compact format: path + score on one line, then a short excerpt.
		entry := fmt.Sprintf("- **%s** (%.2f)\n  %s\n", filePath, r.Score, chunk)
		hits = append(hits, enrichHit{
			cand:  RelevanceCandidate{Source: SourceKB, ID: filePath, Text: filePath + ": " + chunk, Score: r.Score},
			entry: entry,
		})
	}
	return hits
}

func (e *ContextEnricher) searchEvents(ctx context.Context, query string, n int) []enrichHit {
	opts := events.SearchOptions{
		Query:    query,
		Subject:  e.cfg.EventsSubject,
		Limit:    n,
		LastDays: e.cfg.EventsLastDays,
	}
	results, err := e.svc.Events.SearchEvents(ctx, opts)
	if err != nil {
		logging.Debug("context enricher: events search failed", "error", err)
		return nil
	}

	var hits []enrichHit
	for _, r := range results {
		if r.Score < e.cfg.MinScore {
			continue
		}
		ts := r.Event.EventAt.Format(time.DateOnly)
		subject := r.Event.Subject
		if subject == "" {
			subject = "general"
		}
		content := strings.TrimSpace(r.Event.Content)
		// Limit each event to 200 chars.
		if len(content) > 200 {
			content = content[:200] + "…"
		}
		content = strings.ReplaceAll(content, "\n", " ")
		entry := fmt.Sprintf("- [%s] **%s** (%.2f): %s\n", ts, subject, r.Score, content)
		hits = append(hits, enrichHit{
			cand:  RelevanceCandidate{Source: SourceEvents, ID: fmt.Sprintf("event-%d", r.Event.ID), Text: subject + ": " + content, Score: r.Score},
			entry: entry,
		})
	}
	return hits
}

func (e *ContextEnricher) searchCode(ctx context.Context, query string, n int) []enrichHit {
	results, err := e.svc.Code.HybridSearch(ctx, e.cfg.CodeProject, query, n, nil, nil)
	if err != nil {
		logging.Debug("context enricher: code search failed", "project", e.cfg.CodeProject, "error", err)
		return nil
	}

	var hits []enrichHit
	for _, r := range results {
		if r.Symbol == nil || r.Score < e.cfg.MinScore {
			continue
		}
		sym := r.Symbol
		loc := sym.FilePath
		if loc != "" && sym.StartLine > 0 {
			loc = fmt.Sprintf("%s:%d", loc, sym.StartLine)
		}
		// One-liner: type, name, location, score.
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("- **%s** `%s` — %s (%.2f)\n", sym.SymbolType, sym.Name, loc, r.Score))
		text := fmt.Sprintf("%s %s — %s", sym.SymbolType, sym.Name, loc)
		// Include docstring if present (trimmed to 120 chars).
		if sym.DocString != "" {
			doc := strings.TrimSpace(sym.DocString)
			if len(doc) > 120 {
				doc = doc[:120] + "…"
			}
			doc = strings.ReplaceAll(doc, "\n", " ")
			sb.WriteString("  ")
			sb.WriteString(doc)
			sb.WriteString("\n")
			text += ": " + doc
		}
		hits = append(hits, enrichHit{
			cand:  RelevanceCandidate{Source: SourceCode, ID: sym.Name + "@" + loc, Text: text, Score: r.Score},
			entry: sb.String(),
		})
	}
	return hits
}
