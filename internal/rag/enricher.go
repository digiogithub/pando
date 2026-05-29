package rag

import (
	"context"
	"fmt"
	"strings"
	"sync"
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
}

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
	return &ContextEnricher{
		svc:     svc,
		cfg:     cfg,
		planner: &HeuristicPlanner{},
	}
}

// EnrichContext searches KB, events, and code index in parallel using queries derived
// from the raw user prompt via the QueryPlanner, filters results below minScore,
// and returns a formatted context block.
// Sections with no results above the threshold are omitted entirely.
// Returns an empty string when nothing relevant is found.
func (e *ContextEnricher) EnrichContext(ctx context.Context, query string) string {
	if e == nil || e.svc == nil {
		return ""
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

	type result struct {
		content string
	}

	var (
		kbRes     result
		eventsRes result
		codeRes   result
		wg        sync.WaitGroup
	)

	if e.svc.KB != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kbRes.content = e.searchKB(ctx, plan.SemanticQuery, kbN)
		}()
	}

	if e.svc.Events != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			eventsRes.content = e.searchEvents(ctx, plan.EventsQuery, eventsN)
		}()
	}

	if e.svc.Code != nil && e.cfg.CodeProject != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codeRes.content = e.searchCode(ctx, plan.CodeQuery, codeN)
		}()
	}

	wg.Wait()

	// Assemble: code first (most precise), then KB, then events.
	sections := []string{codeRes.content, kbRes.content, eventsRes.content}
	var parts []string
	for _, c := range sections {
		if c != "" {
			parts = append(parts, c)
		}
	}

	if len(parts) == 0 {
		return ""
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
	return sb.String()
}

func (e *ContextEnricher) searchKB(ctx context.Context, query string, n int) string {
	results, err := e.svc.KB.SearchDocuments(ctx, query, n)
	if err != nil {
		logging.Debug("context enricher: kb search failed", "error", err)
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Knowledge Base\n")
	count := 0
	for _, r := range results {
		if r.Score < e.cfg.MinScore {
			continue
		}
		filePath := r.Document.FilePath
		if filePath == "" {
			filePath = fmt.Sprintf("document-%d", r.Document.ID)
		}
		// Compact format: path + score on one line, then a short excerpt.
		sb.WriteString(fmt.Sprintf("- **%s** (%.2f)\n", filePath, r.Score))
		chunk := strings.TrimSpace(r.ChunkContent)
		if chunk == "" {
			chunk = strings.TrimSpace(r.Document.Content)
		}
		// Limit each entry to 200 chars — enough for orientation, not a full dump.
		if len(chunk) > 200 {
			chunk = chunk[:200] + "…"
		}
		sb.WriteString("  ")
		sb.WriteString(strings.ReplaceAll(chunk, "\n", " "))
		sb.WriteString("\n")
		count++

		// Per-section char budget check.
		if e.cfg.KBMaxChars > 0 && sb.Len() >= e.cfg.KBMaxChars {
			break
		}
	}
	if count == 0 {
		return ""
	}
	out := sb.String()
	if e.cfg.KBMaxChars > 0 && len(out) > e.cfg.KBMaxChars {
		out = out[:e.cfg.KBMaxChars] + "…"
	}
	return out
}

func (e *ContextEnricher) searchEvents(ctx context.Context, query string, n int) string {
	opts := events.SearchOptions{
		Query:    query,
		Subject:  e.cfg.EventsSubject,
		Limit:    n,
		LastDays: e.cfg.EventsLastDays,
	}
	results, err := e.svc.Events.SearchEvents(ctx, opts)
	if err != nil {
		logging.Debug("context enricher: events search failed", "error", err)
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Past Session Events\n")
	count := 0
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
		sb.WriteString(fmt.Sprintf("- [%s] **%s** (%.2f): %s\n",
			ts, subject, r.Score, strings.ReplaceAll(content, "\n", " ")))
		count++

		if e.cfg.EventsMaxChars > 0 && sb.Len() >= e.cfg.EventsMaxChars {
			break
		}
	}
	if count == 0 {
		return ""
	}
	out := sb.String()
	if e.cfg.EventsMaxChars > 0 && len(out) > e.cfg.EventsMaxChars {
		out = out[:e.cfg.EventsMaxChars] + "…"
	}
	return out
}

func (e *ContextEnricher) searchCode(ctx context.Context, query string, n int) string {
	results, err := e.svc.Code.HybridSearch(ctx, e.cfg.CodeProject, query, n, nil, nil)
	if err != nil {
		logging.Debug("context enricher: code search failed", "project", e.cfg.CodeProject, "error", err)
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Code Index\n")
	count := 0
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
		sb.WriteString(fmt.Sprintf("- **%s** `%s` — %s (%.2f)\n", sym.SymbolType, sym.Name, loc, r.Score))
		// Include docstring if present (trimmed to 120 chars).
		if sym.DocString != "" {
			doc := strings.TrimSpace(sym.DocString)
			if len(doc) > 120 {
				doc = doc[:120] + "…"
			}
			sb.WriteString("  ")
			sb.WriteString(strings.ReplaceAll(doc, "\n", " "))
			sb.WriteString("\n")
		}
		count++

		if e.cfg.CodeMaxChars > 0 && sb.Len() >= e.cfg.CodeMaxChars {
			break
		}
	}
	if count == 0 {
		return ""
	}
	out := sb.String()
	if e.cfg.CodeMaxChars > 0 && len(out) > e.cfg.CodeMaxChars {
		out = out[:e.cfg.CodeMaxChars] + "…"
	}
	return out
}
