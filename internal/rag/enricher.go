package rag

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/rag/events"
)

// ContextEnricher performs pre-prompt KB, events and code searches and formats
// the results as context to be prepended to the user's message.
type ContextEnricher struct {
	svc            *RemembrancesService
	kbResults      int
	codeResults    int
	codeProject    string
	eventsResults  int
	eventsSubject  string
	eventsLastDays int
}

// NewContextEnricher creates a ContextEnricher from the given RemembrancesService and config values.
// Returns nil when the service is nil.
func NewContextEnricher(svc *RemembrancesService, kbResults, codeResults int, codeProject string, eventsResults int, eventsSubject string, eventsLastDays int) *ContextEnricher {
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
	return &ContextEnricher{
		svc:            svc,
		kbResults:      kbResults,
		codeResults:    codeResults,
		codeProject:    codeProject,
		eventsResults:  eventsResults,
		eventsSubject:  eventsSubject,
		eventsLastDays: eventsLastDays,
	}
}

// EnrichContext searches the KB and code index using the user's query and returns
// a formatted context block ready to be prepended to the prompt.
// Returns an empty string when no relevant results are found.
func (e *ContextEnricher) EnrichContext(ctx context.Context, query string) string {
	if e == nil || e.svc == nil {
		return ""
	}

	var parts []string

	// KB search
	if e.svc.KB != nil {
		kbContext := e.searchKB(ctx, query)
		if kbContext != "" {
			parts = append(parts, kbContext)
		}
	}

	// Events search: previous session context for this project
	if e.svc.Events != nil {
		eventsContext := e.searchEvents(ctx, query)
		if eventsContext != "" {
			parts = append(parts, eventsContext)
		}
	}

	// Code search: only when a project ID is configured
	if e.svc.Code != nil && e.codeProject != "" {
		codeContext := e.searchCode(ctx, query)
		if codeContext != "" {
			parts = append(parts, codeContext)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<context source=\"remembrances\">\n")
	sb.WriteString("The following context was retrieved from the knowledge base, past session events, and code index to help answer your query.\n\n")
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
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Knowledge Base\n")
	for i, r := range results {
		filePath := r.Document.FilePath
		if filePath == "" {
			filePath = fmt.Sprintf("document-%d", r.Document.ID)
		}
		sb.WriteString(fmt.Sprintf("### [%d] %s (score: %.3f)\n", i+1, filePath, r.Score))
		chunk := strings.TrimSpace(r.ChunkContent)
		if chunk == "" {
			chunk = strings.TrimSpace(r.Document.Content)
			// Truncate very long documents to avoid context explosion
			if len(chunk) > 500 {
				chunk = chunk[:500] + "..."
			}
		}
		sb.WriteString(chunk)
		sb.WriteString("\n")
	}
	return sb.String()
}

func (e *ContextEnricher) searchEvents(ctx context.Context, query string) string {
	opts := events.SearchOptions{
		Query:          query,
		Subject:        e.eventsSubject,
		Limit:          e.eventsResults,
		LastDays:       e.eventsLastDays,
	}
	results, err := e.svc.Events.SearchEvents(ctx, opts)
	if err != nil {
		logging.Debug("context enricher: events search failed", "error", err)
		return ""
	}
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Past Session Events\n")
	for i, r := range results {
		ts := r.Event.EventAt.Format(time.RFC3339)
		subject := r.Event.Subject
		if subject == "" {
			subject = "general"
		}
		sb.WriteString(fmt.Sprintf("### [%d] [%s] subject:%s (score: %.3f)\n", i+1, ts, subject, r.Score))
		content := strings.TrimSpace(r.Event.Content)
		if len(content) > 600 {
			content = content[:600] + "..."
		}
		sb.WriteString(content)
		sb.WriteString("\n")
	}
	return sb.String()
}

func (e *ContextEnricher) searchCode(ctx context.Context, query string) string {
	results, err := e.svc.Code.HybridSearch(ctx, e.codeProject, query, e.codeResults, nil, nil)
	if err != nil {
		logging.Debug("context enricher: code search failed", "project", e.codeProject, "error", err)
		return ""
	}
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Code Index\n")
	for i, r := range results {
		if r.Symbol == nil {
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
		header += fmt.Sprintf(" (score: %.3f)", r.Score)
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
	}
	return sb.String()
}
