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
	kbMarkOutdatedToolName    = "kb_mark_outdated"
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

// KBMarkOutdatedTool flags a knowledge-base document as superseded/outdated.
type KBMarkOutdatedTool struct {
	store *kb.KBStore
}

// NewKBMarkOutdatedTool creates a new KBMarkOutdatedTool.
func NewKBMarkOutdatedTool(store *kb.KBStore) BaseTool {
	return &KBMarkOutdatedTool{store: store}
}

// ---- KBAddDocumentTool ----

func (t *KBAddDocumentTool) Info() ToolInfo {
	return ToolInfo{
		Name: kbAddDocumentToolName,
		Description: "Adds or updates a document in the knowledge base with automatic chunking and embedding. Use this to store important documentation, notes, plans, or any text content for future retrieval. Documents are automatically timestamped (created_at on first add, updated_at on every update). Tags are stored and can be used to filter search results. When tag 'memory' and key are provided, performs a key-based upsert via the memory subsystem.\n\n" +
			kbWikiLinkSyntax + " Link the document to the plans, features and fixes it builds on: the links are indexed as a navigable graph (see kb_related_documents), so writing them is what keeps the knowledge base connected instead of a pile of loose files.",
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
			"key": map[string]any{
				"type":        "string",
				"description": "Optional memory upsert key. When provided along with tag 'memory', performs key-based upsert via the memory subsystem.",
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
		Key      string                 `json:"key"`
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

	// Route through memory subsystem when tag "memory" and key are both set.
	isMemory := false
	for _, tag := range req.Tags {
		if tag == "memory" {
			isMemory = true
			break
		}
	}

	if isMemory && req.Key != "" {
		scope := ""
		if s, ok := req.Metadata["scope"].(string); ok {
			scope = s
		}
		importance := 0.0
		if imp, ok := req.Metadata["importance"].(float64); ok {
			importance = imp
		}
		created, err := t.store.UpsertMemory(ctx, kb.MemoryUpsertOptions{
			FilePath:   req.FilePath,
			Key:        req.Key,
			Scope:      scope,
			Content:    bodyContent,
			Tags:       req.Tags,
			Metadata:   req.Metadata,
			Importance: importance,
			Source:     "kb_add_document",
		})
		if err != nil {
			return NewTextErrorResponse(fmt.Sprintf("kb memory upsert error: %v", err)), nil
		}
		action := "updated"
		if created {
			action = "added"
		}
		return NewTextResponse(fmt.Sprintf("Memory %s: %s (key: %s)%s",
			action, req.FilePath, req.Key, linkFeedback(ctx, t.store, req.FilePath))), nil
	}

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
		return NewTextResponse(fmt.Sprintf("Document updated: %s (tags: %v)%s",
			req.FilePath, mergedFM.Tags, linkFeedback(ctx, t.store, req.FilePath))), nil
	}

	// New document: create front matter with current time.
	newFM := kb.NewFrontMatter(req.Tags, nil)

	if err := t.store.AddDocument(ctx, req.FilePath, bodyContent, req.Metadata); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb add error: %v", err)), nil
	}
	mirrorContent := kb.SerializeFrontMatter(newFM, bodyContent)
	if err := t.store.WriteDocumentToFilesystem(req.FilePath, mirrorContent); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb filesystem mirror error: %v", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("Document added: %s (tags: %v)%s",
		req.FilePath, newFM.Tags, linkFeedback(ctx, t.store, req.FilePath))), nil
}

// ---- KBImportPathTool ----

func (t *KBImportPathTool) Info() ToolInfo {
	return ToolInfo{
		Name:        kbImportPathToolName,
		Description: "Imports and synchronizes all .md files from a directory (including subdirectories) into the knowledge base. Can optionally delete KB docs that no longer exist on disk. " +
			"Any [[wiki links]] written in the imported documents are indexed into the document graph and counted as 'links_indexed'.",
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

	out := map[string]any{
		"path":           req.Path,
		"delete_missing": deleteMissing,
		"scanned":        stats.Scanned,
		"added":          stats.Added,
		"updated":        stats.Updated,
		"unchanged":      stats.Unchanged,
		"deleted":        stats.Deleted,
	}
	// Reported only when the imported documents actually use the syntax, so an
	// import into a knowledge base without wiki links reads as it always did.
	if stats.LinksIndexed > 0 {
		out["links_indexed"] = stats.LinksIndexed
	}

	return NewStructuredResponse(out), nil
}

// ---- KBSearchDocumentsTool ----

func (t *KBSearchDocumentsTool) Info() ToolInfo {
	return ToolInfo{
		Name: kbSearchDocumentsToolName,
		Description: "Searches the knowledge base for documents semantically similar to the query. Combines vector similarity and full-text search for best results. Use this to retrieve stored documentation, notes, or plans. Results include tags and timestamps, and can optionally be filtered by tags or sorted chronologically. " +
			"Results that take part in the wiki graph also report how many [[wiki links]] they declare and receive, and the neighbours of the best match are listed under 'related_to_top_result': follow them with kb_get_document or kb_related_documents instead of running another search.",
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
			"exclude_outdated": map[string]any{
				"type":        "boolean",
				"description": "When true (default), exclude memory documents marked as outdated.",
			},
			"scope": map[string]any{
				"type":        "string",
				"description": "Optional memory scope prefix to restrict results (e.g. 'user/', 'project/'). Empty means all scopes.",
			},
		},
		Required: []string{"query"},
	}
}

func (t *KBSearchDocumentsTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		Query           string   `json:"query"`
		Limit           int      `json:"limit"`
		Tags            []string `json:"tags"`
		SortByDate      bool     `json:"sort_by_date"`
		ExcludeOutdated *bool    `json:"exclude_outdated"`
		Scope           string   `json:"scope"`
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

	// exclude_outdated defaults to true when not explicitly set.
	excludeOutdated := true
	if req.ExcludeOutdated != nil {
		excludeOutdated = *req.ExcludeOutdated
	}

	opts := kb.SearchOptions{
		Tags:            req.Tags,
		SortByDate:      req.SortByDate,
		ExcludeOutdated: excludeOutdated,
		Scope:           req.Scope,
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
		// Links and Backlinks are omitted for documents outside the wiki graph, so
		// a knowledge base that does not use the syntax reads exactly as before.
		Links     int `json:"links,omitempty"`
		Backlinks int `json:"backlinks,omitempty"`
	}

	items := make([]resultItem, len(results))
	paths := make([]string, len(results))
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
		paths[i] = r.Document.FilePath
	}

	counts, err := t.store.LinkCountsFor(ctx, paths)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb link counts error: %v", err)), nil
	}
	for i := range items {
		if c, ok := counts[items[i].FilePath]; ok {
			items[i].Links, items[i].Backlinks = c.Outgoing, c.Backlinks
		}
	}

	out := map[string]any{
		"count":   len(items),
		"results": items,
	}

	// The best match is the one that will actually be read, so hand over its
	// neighbours: following a link beats running a second search. Only worth doing
	// when that document is part of the graph at all.
	if _, connected := counts[items[0].FilePath]; connected {
		related, err := t.store.RelatedDocuments(ctx, items[0].FilePath, 3)
		if err != nil {
			return NewTextErrorResponse(fmt.Sprintf("kb related error: %v", err)), nil
		}
		if v := kbRelatedViews(related); v != nil {
			out["related_to_top_result"] = v
		}
	}

	return NewStructuredResponse(out), nil
}

// ---- KBGetDocumentTool ----

func (t *KBGetDocumentTool) Info() ToolInfo {
	return ToolInfo{
		Name: kbGetDocumentToolName,
		Description: "Retrieves a specific document from the knowledge base by its file path. " +
			"When the document takes part in the wiki graph, the response also carries the [[wiki links]] it declares (resolved to their documents, or flagged unresolved) and the documents that link back to it; use kb_related_documents to explore further.",
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

	out := map[string]any{
		"file_path":  doc.FilePath,
		"content":    doc.Content,
		"tags":       doc.Tags,
		"metadata":   doc.Metadata,
		"created_at": doc.CreatedAt.UTC().Format(timeLayoutRFC3339),
		"updated_at": doc.UpdatedAt.UTC().Format(timeLayoutRFC3339),
	}
	if err := attachDocumentLinks(ctx, t.store, out, doc.FilePath); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb links error: %v", err)), nil
	}

	return NewStructuredResponse(out), nil
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

// ---- KBMarkOutdatedTool ----

func (t *KBMarkOutdatedTool) Info() ToolInfo {
	return ToolInfo{
		Name: kbMarkOutdatedToolName,
		Description: "Marks a knowledge-base document as outdated (superseded) without deleting it. " +
			"Outdated documents are excluded from kb_search_documents by default (they still surface when exclude_outdated is set to false), and the outdated flag is mirrored into the document's front matter. " +
			"Use this when a plan, feature note, or fix write-up has been replaced by newer work: prefer marking the stale document outdated (and adding the up-to-date one) over silently leaving contradictory documents in the knowledge base. Idempotent — re-marking an already outdated document is a no-op.",
		Parameters: map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The document path/identifier to mark as outdated.",
			},
		},
		Required: []string{"file_path"},
	}
}

func (t *KBMarkOutdatedTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		FilePath string `json:"file_path"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.FilePath == "" {
		return NewTextErrorResponse("file_path is required"), nil
	}

	// MarkDocumentOutdated silently no-ops on a missing document (the UPDATE
	// touches zero rows), so verify existence first to give the caller a clear
	// signal instead of a false confirmation.
	doc, err := t.store.GetDocument(ctx, req.FilePath)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb error: %v", err)), nil
	}
	if doc == nil {
		return NewTextResponse(fmt.Sprintf("Document not found: %s", req.FilePath)), nil
	}

	if err := t.store.MarkDocumentOutdated(ctx, req.FilePath); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("kb mark outdated error: %v", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("Document marked outdated: %s (excluded from default searches; still retrievable with exclude_outdated=false)", req.FilePath)), nil
}
