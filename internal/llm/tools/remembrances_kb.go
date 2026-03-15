package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/kb"
	"github.com/digiogithub/pando/internal/rag/sessions"
)

// KB tool names
const (
	kbAddDocumentToolName     = "kb_add_document"
	kbSearchDocumentsToolName = "kb_search_documents"
	kbGetDocumentToolName     = "kb_get_document"
	kbDeleteDocumentToolName  = "kb_delete_document"
)

// KBAddDocumentTool adds a document to the knowledge base.
type KBAddDocumentTool struct {
	store *kb.KBStore
}

// KBSearchDocumentsTool searches documents in the knowledge base and optionally sessions.
type KBSearchDocumentsTool struct {
	store    *kb.KBStore
	sessions *sessions.SessionRAGStore // may be nil
}

// KBGetDocumentTool retrieves a document from the knowledge base by path.
type KBGetDocumentTool struct {
	store *kb.KBStore
}

// KBDeleteDocumentTool removes a document from the knowledge base.
type KBDeleteDocumentTool struct {
	store *kb.KBStore
}

// NewKBAddDocumentTool creates a new KBAddDocumentTool.
func NewKBAddDocumentTool(store *kb.KBStore) BaseTool {
	return &KBAddDocumentTool{store: store}
}

// NewKBSearchDocumentsTool creates a new KBSearchDocumentsTool.
// sessStore may be nil; if nil only KB search is performed.
func NewKBSearchDocumentsTool(store *kb.KBStore, sessStore *sessions.SessionRAGStore) BaseTool {
	return &KBSearchDocumentsTool{store: store, sessions: sessStore}
}

// NewKBGetDocumentTool creates a new KBGetDocumentTool.
func NewKBGetDocumentTool(store *kb.KBStore) BaseTool {
	return &KBGetDocumentTool{store: store}
}

// NewKBDeleteDocumentTool creates a new KBDeleteDocumentTool.
func NewKBDeleteDocumentTool(store *kb.KBStore) BaseTool {
	return &KBDeleteDocumentTool{store: store}
}

// ---- KBAddDocumentTool ----

func (t *KBAddDocumentTool) Info() ToolInfo {
	return ToolInfo{
		Name:        kbAddDocumentToolName,
		Description: "Adds or updates a document in the knowledge base with automatic chunking and embedding. Use this to store important documentation, notes, plans, or any text content for future retrieval.",
		Parameters: map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Unique path/identifier for the document (e.g. 'project/readme.md', 'notes/todo').",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The full text content to store.",
			},
			"metadata": map[string]any{
				"type":        "object",
				"description": "Optional key-value metadata to associate with the document (e.g. {\"source\": \"user\", \"tags\": [\"important\"]}).",
			},
		},
		Required: []string{"file_path", "content"},
	}
}

func (t *KBAddDocumentTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		FilePath string                 `json:"file_path"`
		Content  string                 `json:"content"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.FilePath == "" {
		return NewTextErrorResponse("file_path is required"), nil
	}
	if req.Content == "" {
		return NewTextErrorResponse("content is required"), nil
	}

	// Check if document already exists (update vs add)
	existing, err := t.store.GetDocument(ctx, req.FilePath)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb error: %v", err)), nil
	}

	if existing != nil {
		if err := t.store.UpdateDocument(ctx, req.FilePath, req.Content, req.Metadata); err != nil {
			return NewTextErrorResponse(fmt.Sprintf("kb update error: %v", err)), nil
		}
		return NewTextResponse(fmt.Sprintf("Document updated: %s", req.FilePath)), nil
	}

	if err := t.store.AddDocument(ctx, req.FilePath, req.Content, req.Metadata); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb add error: %v", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("Document added: %s", req.FilePath)), nil
}

// ---- KBSearchDocumentsTool ----

func (t *KBSearchDocumentsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        kbSearchDocumentsToolName,
		Description: "Searches the knowledge base and/or past conversation sessions for content semantically similar to the query. Combines vector similarity and full-text search for best results.",
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query in natural language.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 5, max: 20).",
			},
			"source": map[string]any{
				"type":        "string",
				"description": "Source to search: 'kb' (documents only), 'sessions' (past conversations only), or 'all' (default, searches both).",
				"enum":        []string{"kb", "sessions", "all"},
			},
		},
		Required: []string{"query"},
	}
}

func (t *KBSearchDocumentsTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
		Source string `json:"source"`
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
	if req.Limit > 20 {
		req.Limit = 20
	}
	if req.Source == "" {
		req.Source = "all"
	}

	// Force KB-only when sessions store is not available.
	if t.sessions == nil && req.Source != "kb" {
		req.Source = "kb"
	}

	searcher := rag.NewUnifiedSearcher(t.store, t.sessions)
	results, err := searcher.Search(ctx, req.Query, req.Limit, req.Source)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("search error: %v", err)), nil
	}

	if len(results) == 0 {
		return NewTextResponse("No documents found matching the query."), nil
	}

	var sb strings.Builder
	for _, r := range results {
		switch r.Source {
		case "kb":
			sb.WriteString(fmt.Sprintf("[KB] %s:\n%s\n\n", r.FilePath, r.Content))
		case "session":
			turns := ""
			if r.TurnStart > 0 || r.TurnEnd > 0 {
				turns = fmt.Sprintf(", turns %d-%d", r.TurnStart, r.TurnEnd)
			}
			role := r.Role
			if role == "" {
				role = "mixed"
			}
			sb.WriteString(fmt.Sprintf("[Session: %q]%s, %s:\n%s\n\n", r.Title, turns, role, r.Content))
		}
	}

	return NewTextResponse(sb.String()), nil
}

// ---- KBGetDocumentTool ----

func (t *KBGetDocumentTool) Info() ToolInfo {
	return ToolInfo{
		Name:        kbGetDocumentToolName,
		Description: "Retrieves a specific document from the knowledge base by its file path.",
		Parameters: map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The document path/identifier to retrieve.",
			},
		},
		Required: []string{"file_path"},
	}
}

func (t *KBGetDocumentTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.FilePath == "" {
		return NewTextErrorResponse("file_path is required"), nil
	}

	doc, err := t.store.GetDocument(ctx, req.FilePath)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb error: %v", err)), nil
	}
	if doc == nil {
		return NewTextResponse(fmt.Sprintf("Document not found: %s", req.FilePath)), nil
	}

	out, err := json.MarshalIndent(map[string]any{
		"file_path":  doc.FilePath,
		"content":    doc.Content,
		"metadata":   doc.Metadata,
		"created_at": doc.CreatedAt,
		"updated_at": doc.UpdatedAt,
	}, "", "  ")
	if err != nil {
		return NewTextErrorResponse("failed to marshal document"), nil
	}
	return NewTextResponse(string(out)), nil
}

// ---- KBDeleteDocumentTool ----

func (t *KBDeleteDocumentTool) Info() ToolInfo {
	return ToolInfo{
		Name:        kbDeleteDocumentToolName,
		Description: "Removes a document and all its chunks from the knowledge base.",
		Parameters: map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The document path/identifier to delete.",
			},
		},
		Required: []string{"file_path"},
	}
}

func (t *KBDeleteDocumentTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.FilePath == "" {
		return NewTextErrorResponse("file_path is required"), nil
	}

	if err := t.store.DeleteDocument(ctx, req.FilePath); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb delete error: %v", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("Document deleted: %s", req.FilePath)), nil
}
