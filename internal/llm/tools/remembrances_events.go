package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/rag/events"
)

// Events tool names
const (
	saveEventToolName    = "save_event"
	searchEventsToolName = "search_events"
)

// SaveEventTool stores a temporal event with semantic search.
type SaveEventTool struct {
	store *events.EventStore
}

// SearchEventsTool searches events with hybrid text and time filters.
type SearchEventsTool struct {
	store *events.EventStore
}

// NewSaveEventTool creates a new SaveEventTool.
func NewSaveEventTool(store *events.EventStore) BaseTool {
	return &SaveEventTool{store: store}
}

// NewSearchEventsTool creates a new SearchEventsTool.
func NewSearchEventsTool(store *events.EventStore) BaseTool {
	return &SearchEventsTool{store: store}
}

// ---- SaveEventTool ----

func (t *SaveEventTool) Info() ToolInfo {
	return ToolInfo{
		Name:        saveEventToolName,
		Description: "Stores a temporal event with semantic search capability. Use this to record important events, decisions, observations, or progress notes that need to be recalled later.",
		Parameters: map[string]any{
			"subject": map[string]any{
				"type":        "string",
				"description": "Category or subject for the event (e.g. 'user', 'project', 'decision', 'error'). Used for filtering.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The event content to store and make searchable.",
			},
			"metadata": map[string]any{
				"type":        "object",
				"description": "Optional key-value metadata (e.g. {\"session_id\": \"abc\", \"importance\": \"high\"}).",
			},
		},
		Required: []string{"subject", "content"},
	}
}

func (t *SaveEventTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		Subject  string                 `json:"subject"`
		Content  string                 `json:"content"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.Subject == "" {
		return NewTextErrorResponse("subject is required"), nil
	}
	if req.Content == "" {
		return NewTextErrorResponse("content is required"), nil
	}

	id, err := t.store.SaveEvent(ctx, req.Subject, req.Content, req.Metadata)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("save event error: %v", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("Event saved with ID: %d", id)), nil
}

// ---- SearchEventsTool ----

func (t *SearchEventsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        searchEventsToolName,
		Description: "Searches stored events using hybrid semantic search with optional time and subject filters. Use this to recall past events, decisions, or observations.",
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Optional semantic search query. If omitted, events are listed by recency.",
			},
			"subject": map[string]any{
				"type":        "string",
				"description": "Optional subject filter to restrict results (e.g. 'user', 'project').",
			},
			"last_hours": map[string]any{
				"type":        "integer",
				"description": "Return events from the last N hours.",
			},
			"last_days": map[string]any{
				"type":        "integer",
				"description": "Return events from the last N days.",
			},
			"from_date": map[string]any{
				"type":        "string",
				"description": "Return events on or after this ISO 8601 date (e.g. '2026-03-01T00:00:00Z').",
			},
			"to_date": map[string]any{
				"type":        "string",
				"description": "Return events on or before this ISO 8601 date.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 10, max: 50).",
			},
		},
	}
}

func (t *SearchEventsTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		Query     string `json:"query"`
		Subject   string `json:"subject"`
		LastHours int    `json:"last_hours"`
		LastDays  int    `json:"last_days"`
		FromDate  string `json:"from_date"`
		ToDate    string `json:"to_date"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}
	if req.Limit > 50 {
		req.Limit = 50
	}

	opts := events.SearchOptions{
		Query:     req.Query,
		Subject:   req.Subject,
		LastHours: req.LastHours,
		LastDays:  req.LastDays,
		Limit:     req.Limit,
	}

	if req.FromDate != "" {
		t, err := time.Parse(time.RFC3339, req.FromDate)
		if err != nil {
			return NewTextErrorResponse(fmt.Sprintf("invalid from_date: %v", err)), nil
		}
		opts.FromDate = &t
	}
	if req.ToDate != "" {
		t, err := time.Parse(time.RFC3339, req.ToDate)
		if err != nil {
			return NewTextErrorResponse(fmt.Sprintf("invalid to_date: %v", err)), nil
		}
		opts.ToDate = &t
	}

	results, err := t.store.SearchEvents(ctx, opts)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("search events error: %v", err)), nil
	}

	if len(results) == 0 {
		return NewTextResponse("No events found matching the query."), nil
	}

	type eventItem struct {
		ID       int64                  `json:"id"`
		Subject  string                 `json:"subject"`
		Content  string                 `json:"content"`
		Score    float64                `json:"score"`
		Rank     int                    `json:"rank"`
		EventAt  time.Time              `json:"event_at"`
		Metadata map[string]interface{} `json:"metadata,omitempty"`
	}

	items := make([]eventItem, len(results))
	for i, r := range results {
		items[i] = eventItem{
			ID:       r.Event.ID,
			Subject:  r.Event.Subject,
			Content:  r.Event.Content,
			Score:    r.Score,
			Rank:     r.Rank,
			EventAt:  r.Event.EventAt,
			Metadata: r.Event.Metadata,
		}
	}

	out, err := json.MarshalIndent(map[string]any{
		"count":   len(items),
		"results": items,
	}, "", "  ")
	if err != nil {
		return NewTextErrorResponse("failed to marshal results"), nil
	}
	return NewTextResponse(string(out)), nil
}
