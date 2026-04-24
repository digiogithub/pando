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

// ContextEnricher performs pre-prompt KB, events and code searches in parallel and
// formats only relevant results (above minScore) as context to prepend to the user's message.
type ContextEnricher struct {
	svc            *RemembrancesService
	kbResults      int
	codeResults    int
	codeProject    string
	eventsResults  int
	eventsSubject  string
	eventsLastDays int
	minScore       float64
}

// NewContextEnricher creates a ContextEnricher from the given RemembrancesService and config values.
// Returns nil when the service is nil.
func NewContextEnricher(svc *RemembrancesService, kbResults, codeResults int, codeProject string, eventsResults int, eventsSubject string, eventsLastDays int, minScore float64) *ContextEnricher {
	if svc == nil {
		return nil
	}
	if kbResults <= 0 {
		kbResults = 3
	}
	if codeResults <= 0 {
		codeResults = 5
	}
	if eventsResults <= 0 {
		eventsResults = 3
	}
	if eventsLastDays <= 0 {
		eventsLastDays = 30
	}
	if minScore <= 0 {
		minScore = 0.45
	}
	return &ContextEnricher{
		svc:            svc,
		kbResults:      kbResults,
		codeResults:    codeResults,
		codeProject:    codeProject,
		eventsResults:  eventsResults,
		eventsSubject:  eventsSubject,
		eventsLastDays: eventsLastDays,
		minScore:       minScore,
	}
}

// EnrichContext searches KB, events, and code index in parallel using the user's query,
// filters results below minScore, and returns a formatted context block.
// Sections with no results above the threshold are omitted entirely.
// Returns an empty string when nothing relevant is found.
func (e *ContextEnricher) EnrichContext(ctx context.Context, query string) string {
	if e == nil || e.svc == nil {
		return ""
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
			kbRes.content = e.searchKB(ctx, query)
		}()
	}

	if e.svc.Events != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			eventsRes.content = e.searchEvents(ctx, query)
		}()
	}

	if e.svc.Code != nil && e.codeProject != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codeRes.content = e.searchCode(ctx, query)
		}()
	}

	wg.Wait()

	var parts []string
	for _, c := range []string{kbRes.content, eventsRes.content, codeRes.content} {
		if c != "" {
			parts = append(parts, c)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<context source=\"remembrances\">\n")
	sb.WriteString(strings.Join(parts, "\n\n"))
	sb.WriteString("\n</context>")
	return sb.String()
}

func (e *ContextEnricher) searchKB(ctx context.Context, query string) string {
	results, err := e.svc.KB.SearchDocuments(ctx, query, e.kbResults)
	if err != nil {
		logging.Debug("context enricher: kb search failed", "error", err)
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Knowledge Base\n")
	count := 0
	for i, r := range results {
		if r.Score < e.minScore {
			continue
		}
		filePath := r.Document.FilePath
		if filePath == "" {
			filePath = fmt.Sprintf("document-%d", r.Document.ID)
		}
		sb.WriteString(fmt.Sprintf("### [%d] %s (score: %.2f)\n", i+1, filePath, r.Score))
		chunk := strings.TrimSpace(r.ChunkContent)
		if chunk == "" {
			chunk = strings.TrimSpace(r.Document.Content)
			if len(chunk) > 500 {
				chunk = chunk[:500] + "..."
			}
		}
		sb.WriteString(chunk)
		sb.WriteString("\n")
		count++
	}
	if count == 0 {
		return ""
	}
	return sb.String()
}

func (e *ContextEnricher) searchEvents(ctx context.Context, query string) string {
	opts := events.SearchOptions{
		Query:    query,
		Subject:  e.eventsSubject,
		Limit:    e.eventsResults,
		LastDays: e.eventsLastDays,
	}
	results, err := e.svc.Events.SearchEvents(ctx, opts)
	if err != nil {
		logging.Debug("context enricher: events search failed", "error", err)
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Past Session Events\n")
	count := 0
	for i, r := range results {
		if r.Score < e.minScore {
			continue
		}
		ts := r.Event.EventAt.Format(time.RFC3339)
		subject := r.Event.Subject
		if subject == "" {
			subject = "general"
		}
		sb.WriteString(fmt.Sprintf("### [%d] [%s] %s (score: %.2f)\n", i+1, ts, subject, r.Score))
		content := strings.TrimSpace(r.Event.Content)
		if len(content) > 600 {
			content = content[:600] + "..."
		}
		sb.WriteString(content)
		sb.WriteString("\n")
		count++
	}
	if count == 0 {
		return ""
	}
	return sb.String()
}

func (e *ContextEnricher) searchCode(ctx context.Context, query string) string {
	results, err := e.svc.Code.HybridSearch(ctx, e.codeProject, query, e.codeResults, nil, nil)
	if err != nil {
		logging.Debug("context enricher: code search failed", "project", e.codeProject, "error", err)
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Code Index\n")
	count := 0
	for i, r := range results {
		if r.Symbol == nil || r.Score < e.minScore {
			continue
		}
		sym := r.Symbol
		header := fmt.Sprintf("### [%d] %s `%s`", i+1, sym.SymbolType, sym.Name)
		if sym.FilePath != "" {
			if sym.StartLine > 0 {
				header += fmt.Sprintf(" — %s:%d", sym.FilePath, sym.StartLine)
			} else {
				header += fmt.Sprintf(" — %s", sym.FilePath)
			}
		}
		header += fmt.Sprintf(" (score: %.2f)", r.Score)
		sb.WriteString(header)
		sb.WriteString("\n")
		if sym.DocString != "" {
			sb.WriteString(strings.TrimSpace(sym.DocString))
			sb.WriteString("\n")
		}
		if sym.SourceCode != "" {
			body := strings.TrimSpace(sym.SourceCode)
			if len(body) > 400 {
				body = body[:400] + "..."
			}
			sb.WriteString("```\n")
			sb.WriteString(body)
			sb.WriteString("\n```\n")
		}
		count++
	}
	if count == 0 {
		return ""
	}
	return sb.String()
}
