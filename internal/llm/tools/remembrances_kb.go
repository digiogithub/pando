package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/rag/kb"
)

const timeLayoutRFC3339 = time.RFC3339

// KB tool names
const (
	kbAddDocumentToolName     = "kb_add_document"
	kbImportPathToolName      = "kb_import_path"
	kbSearchDocumentsToolName = "kb_search_documents"
	kbGetDocumentToolName     = "kb_get_document"
	kbDeleteDocumentToolName  = "kb_delete_document"
)

// KBAddDocumentTool adds a document to the knowledge base.
type KBAddDocumentTool struct {
	store *kb.KBStore
}

// KBImportPathTool imports/syncs markdown files from a filesystem path.
type KBImportPathTool struct {
	store *kb.KBStore
}

// KBSearchDocumentsTool searches documents in the knowledge base.
type KBSearchDocumentsTool struct {
	store *kb.KBStore
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

// NewKBImportPathTool creates a new KBImportPathTool.
func NewKBImportPathTool(store *kb.KBStore) BaseTool {
	return &KBImportPathTool{store: store}
}

// NewKBSearchDocumentsTool creates a new KBSearchDocumentsTool.
func NewKBSearchDocumentsTool(store *kb.KBStore) BaseTool {
	return &KBSearchDocumentsTool{store: store}
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
		Description: "Adds or updates a document in the knowledge base with automatic chunking and embedding. Use this to store important documentation, notes, plans, or any text content for future retrieval. Documents are automatically timestamped (created_at on first add, updated_at on every update). Tags are stored and can be used to filter search results.",
		Parameters: map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Unique path/identifier for the document (e.g. 'project/readme.md', 'notes/todo').",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The full text content to store.",
			},
			"tags": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional list of tags to associate with the document for filtering and categorization (e.g. [\"plan\", \"architecture\"]).",
			},
			"metadata": map[string]any{
				"type":        "object",
				"description": "Optional key-value metadata to associate with the document (e.g. {\"source\": \"user\"}).",
			},
		},
		Required: []string{"file_path", "content"},
	}
}

func (t *KBAddDocumentTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		FilePath string                 `json:"file_path"`
		Content  string                 `json:"content"`
		Tags     []string               `json:"tags"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.FilePath == "" {
		return NewTextErrorResponse("file_path is required"), nil
	}
	if req.Content == "" {
		return NewTextErrorResponse("content is required"), nil
	}

	// Strip any user-provided front matter from content — we manage it internally.
	bodyContent := kb.StripFrontMatter(req.Content)

	// Inject tags into metadata.
	req.Metadata = kb.InjectTagsIntoMetadata(req.Metadata, req.Tags)

	// Check if document already exists (update vs add)
	existing, err := t.store.GetDocument(ctx, req.FilePath)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb error: %v", err)), nil
	}

	if existing != nil {
		// Merge front matter: preserve created_at, update updated_at, merge tags.
		existingFM := kb.FrontMatter{
			CreatedAt: existing.CreatedAt,
			UpdatedAt: existing.UpdatedAt,
			Tags:      existing.Tags,
		}
		incomingFM := kb.FrontMatter{Tags: req.Tags}
		mergedFM := kb.MergeFrontMatter(existingFM, incomingFM)

		if err := t.store.UpdateDocument(ctx, req.FilePath, bodyContent, req.Metadata); err != nil {
			return NewTextErrorResponse(fmt.Sprintf("kb update error: %v", err)), nil
		}
		mirrorContent := kb.SerializeFrontMatter(mergedFM, bodyContent)
		if err := t.store.WriteDocumentToFilesystem(req.FilePath, mirrorContent); err != nil {
			return NewTextErrorResponse(fmt.Sprintf("kb filesystem mirror error: %v", err)), nil
		}
		return NewTextResponse(fmt.Sprintf("Document updated: %s (tags: %v)", req.FilePath, mergedFM.Tags)), nil
	}

	// New document: create front matter with current time.
	newFM := kb.NewFrontMatter(req.Tags)

	if err := t.store.AddDocument(ctx, req.FilePath, bodyContent, req.Metadata); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb add error: %v", err)), nil
	}
	mirrorContent := kb.SerializeFrontMatter(newFM, bodyContent)
	if err := t.store.WriteDocumentToFilesystem(req.FilePath, mirrorContent); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb filesystem mirror error: %v", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("Document added: %s (tags: %v)", req.FilePath, newFM.Tags)), nil
}

// ---- KBImportPathTool ----

func (t *KBImportPathTool) Info() ToolInfo {
	return ToolInfo{
		Name:        kbImportPathToolName,
		Description: "Imports and synchronizes all .md files from a directory (including subdirectories) into the knowledge base. Can optionally delete KB docs that no longer exist on disk.",
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative directory path to scan recursively for .md files.",
			},
			"delete_missing": map[string]any{
				"type":        "boolean",
				"description": "When true (default), remove KB documents previously imported from this path that no longer exist on disk.",
			},
		},
		Required: []string{"path"},
	}
}

func (t *KBImportPathTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		Path          string `json:"path"`
		DeleteMissing *bool  `json:"delete_missing"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.Path == "" {
		return NewTextErrorResponse("path is required"), nil
	}

	deleteMissing := true
	if req.DeleteMissing != nil {
		deleteMissing = *req.DeleteMissing
	}

	stats, err := t.store.SyncDirectoryWithStats(ctx, req.Path, deleteMissing)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb import error: %v", err)), nil
	}

	return NewStructuredResponse(map[string]any{
		"path":           req.Path,
		"delete_missing": deleteMissing,
		"scanned":        stats.Scanned,
		"added":          stats.Added,
		"updated":        stats.Updated,
		"unchanged":      stats.Unchanged,
		"deleted":        stats.Deleted,
	}), nil
}

// ---- KBSearchDocumentsTool ----

func (t *KBSearchDocumentsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        kbSearchDocumentsToolName,
		Description: "Searches the knowledge base for documents semantically similar to the query. Combines vector similarity and full-text search for best results. Use this to retrieve stored documentation, notes, or plans. Results include tags and timestamps, and can optionally be filtered by tags or sorted chronologically.",
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query in natural language.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 5, max: 20).",
			},
			"tags": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional list of tags to filter results. Uses fuzzy matching — documents matching any of the provided tags are included.",
			},
			"sort_by_date": map[string]any{
				"type":        "boolean",
				"description": "When true, sort results by updated_at descending (newest first) instead of relevance score.",
			},
		},
		Required: []string{"query"},
	}
}

func (t *KBSearchDocumentsTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		Query      string   `json:"query"`
		Limit      int      `json:"limit"`
		Tags       []string `json:"tags"`
		SortByDate bool     `json:"sort_by_date"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
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

	opts := kb.SearchOptions{
		Tags:       req.Tags,
		SortByDate: req.SortByDate,
	}
	results, err := t.store.SearchDocumentsWithOptions(ctx, req.Query, req.Limit, opts)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb search error: %v", err)), nil
	}

	if len(results) == 0 {
		return NewTextResponse("No documents found matching the query."), nil
	}

	type resultItem struct {
		FilePath     string                 `json:"file_path"`
		ChunkContent string                 `json:"chunk_content"`
		Score        float64                `json:"score"`
		Rank         int                    `json:"rank"`
		Tags         []string               `json:"tags,omitempty"`
		CreatedAt    string                 `json:"created_at"`
		UpdatedAt    string                 `json:"updated_at"`
		Metadata     map[string]interface{} `json:"metadata,omitempty"`
	}

	items := make([]resultItem, len(results))
	for i, r := range results {
		items[i] = resultItem{
			FilePath:     r.Document.FilePath,
			ChunkContent: r.ChunkContent,
			Score:        r.Score,
			Rank:         r.Rank,
			Tags:         r.Document.Tags,
			CreatedAt:    r.Document.CreatedAt.UTC().Format(timeLayoutRFC3339),
			UpdatedAt:    r.Document.UpdatedAt.UTC().Format(timeLayoutRFC3339),
			Metadata:     r.Document.Metadata,
		}
	}

	return NewStructuredResponse(map[string]any{
		"count":   len(items),
		"results": items,
	}), nil
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
	if err := DecodeToolInput(params.Input, &req); err != nil {
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

	return NewStructuredResponse(map[string]any{
		"file_path":  doc.FilePath,
		"content":    doc.Content,
		"tags":       doc.Tags,
		"metadata":   doc.Metadata,
		"created_at": doc.CreatedAt.UTC().Format(timeLayoutRFC3339),
		"updated_at": doc.UpdatedAt.UTC().Format(timeLayoutRFC3339),
	}), nil
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
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.FilePath == "" {
		return NewTextErrorResponse("file_path is required"), nil
	}

	if err := t.store.DeleteDocument(ctx, req.FilePath); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb delete error: %v", err)), nil
	}
	if err := t.store.DeleteDocumentFromFilesystem(req.FilePath); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb filesystem mirror error: %v", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("Document deleted: %s", req.FilePath)), nil
}
