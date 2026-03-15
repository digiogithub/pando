package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/rag/sessions"
)

// Session RAG tool names
const (
	sessionRAGSearchToolName = "session_rag_search"
	sessionRAGIndexToolName  = "session_rag_index"
	sessionRAGDeleteToolName = "session_rag_delete"
)

// ---- SessionRAGSearchTool ----

type sessionRAGSearchTool struct {
	store *sessions.SessionRAGStore
}

// NewSessionRAGSearchTool creates a tool that searches past conversation sessions semantically.
func NewSessionRAGSearchTool(store *sessions.SessionRAGStore) BaseTool {
	return &sessionRAGSearchTool{store: store}
}

func (t *sessionRAGSearchTool) Info() ToolInfo {
	return ToolInfo{
		Name:        sessionRAGSearchToolName,
		Description: "Search past conversation sessions semantically. Returns relevant conversation fragments from previous sessions that match the query.",
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Semantic search query to find relevant past conversations.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 5).",
			},
		},
		Required: []string{"query"},
	}
}

func (t *sessionRAGSearchTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.Query == "" {
		return NewTextErrorResponse("query is required"), nil
	}
	if req.Limit <= 0 {
		req.Limit = 5
	}

	results, err := t.store.SearchSessions(ctx, req.Query, req.Limit)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("session search error: %v", err)), nil
	}

	if len(results) == 0 {
		return NewTextResponse("No past sessions found matching the query."), nil
	}

	var sb strings.Builder
	for _, r := range results {
		turns := ""
		if r.TurnStart > 0 || r.TurnEnd > 0 {
			turns = fmt.Sprintf(", turns %d-%d", r.TurnStart, r.TurnEnd)
		}
		sb.WriteString(fmt.Sprintf("[Session: %q]%s:\n%s\n\n", r.Title, turns, r.Content))
	}

	return NewTextResponse(sb.String()), nil
}

// ---- SessionRAGIndexTool ----

type sessionRAGIndexTool struct {
	indexer *sessions.SessionIndexer
}

// NewSessionRAGIndexTool creates a tool that indexes a session for semantic search.
func NewSessionRAGIndexTool(indexer *sessions.SessionIndexer) BaseTool {
	return &sessionRAGIndexTool{indexer: indexer}
}

func (t *sessionRAGIndexTool) Info() ToolInfo {
	return ToolInfo{
		Name:        sessionRAGIndexToolName,
		Description: "Index the current or specified session for semantic search. Call this to make a session searchable via session_rag_search.",
		Parameters: map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "Session ID to index. If omitted, uses the current session from context.",
			},
		},
		Required: []string{},
	}
}

func (t *sessionRAGIndexTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID, _ = GetContextValues(ctx)
	}
	if sessionID == "" {
		return NewTextErrorResponse("session_id is required"), nil
	}

	if err := t.indexer.IndexSession(ctx, sessionID); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("index error: %v", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("Session indexed successfully: %s", sessionID)), nil
}

// ---- SessionRAGDeleteTool ----

type sessionRAGDeleteTool struct {
	store *sessions.SessionRAGStore
}

// NewSessionRAGDeleteTool creates a tool that removes a session from the RAG index.
func NewSessionRAGDeleteTool(store *sessions.SessionRAGStore) BaseTool {
	return &sessionRAGDeleteTool{store: store}
}

func (t *sessionRAGDeleteTool) Info() ToolInfo {
	return ToolInfo{
		Name:        sessionRAGDeleteToolName,
		Description: "Remove a session from the RAG index. The session conversation will no longer appear in session_rag_search results.",
		Parameters: map[string]any{
			"session_id": map[string]any{
				"type":        "string",
				"description": "The session ID to remove from the RAG index.",
			},
		},
		Required: []string{"session_id"},
	}
}

func (t *sessionRAGDeleteTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.SessionID == "" {
		return NewTextErrorResponse("session_id is required"), nil
	}

	if err := t.store.DeleteSession(ctx, req.SessionID); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("delete error: %v", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("Session removed from RAG index: %s", req.SessionID)), nil
}
