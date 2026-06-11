package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/rag/kb"
)

const (
	rememberToolName = "remember"
	recallToolName   = "recall"
	forgetToolName   = "forget"
)

// ---- RememberTool ----

// RememberTool stores or updates a persistent memory document.
type RememberTool struct {
	store          *kb.KBStore
	defaultTTLDays int
}

// NewRememberTool creates a new RememberTool.
func NewRememberTool(store *kb.KBStore, defaultTTLDays int) BaseTool {
	return &RememberTool{store: store, defaultTTLDays: defaultTTLDays}
}

func (t *RememberTool) Info() ToolInfo {
	return ToolInfo{
		Name:        rememberToolName,
		Description: "Store or update a persistent memory across sessions. Use for user preferences, environment facts, project decisions, or corrections. If key is provided, performs upsert replacing any previous memory with the same key. Tag 'memory' is added automatically. TTL defaults to 6 months, extended on each recall.",
		Parameters: map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The fact or preference to remember.",
			},
			"key": map[string]any{
				"type":        "string",
				"description": "Optional upsert key, e.g. \"user.preferred_lang\". Memories with the same key are merged.",
			},
			"scope": map[string]any{
				"type":        "string",
				"description": "Optional scope prefix: \"user/\", \"project/\", \"session/\". Defaults to \"user/\".",
			},
			"importance": map[string]any{
				"type":        "number",
				"description": "Weight for injection ranking, 0.0–1.0. Defaults to 0.5.",
			},
			"ttl_days": map[string]any{
				"type":        "integer",
				"description": "Override default TTL in days. Defaults to the configured value (180 days).",
			},
		},
		Required: []string{"content"},
	}
}

func (t *RememberTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		Content    string  `json:"content"`
		Key        string  `json:"key"`
		Scope      string  `json:"scope"`
		Importance float64 `json:"importance"`
		TTLDays    int     `json:"ttl_days"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.Content == "" {
		return NewTextErrorResponse("content is required"), nil
	}

	if req.Scope == "" {
		req.Scope = "user/"
	}

	ttl := t.defaultTTLDays
	if req.TTLDays > 0 {
		ttl = req.TTLDays
	}

	importance := req.Importance
	if importance <= 0 {
		importance = 0.5
	}

	var filePath string
	if req.Key != "" {
		filePath = fmt.Sprintf("memory/%s%s.md", req.Scope, req.Key)
	} else {
		filePath = fmt.Sprintf("memory/%s%d.md", req.Scope, time.Now().UnixNano())
	}

	opts := kb.MemoryUpsertOptions{
		FilePath:       filePath,
		Key:            req.Key,
		Scope:          req.Scope,
		Content:        req.Content,
		Tags:           []string{"memory"},
		Importance:     importance,
		DefaultTTLDays: ttl,
	}

	created, err := t.store.UpsertMemory(ctx, opts)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("remember error: %v", err)), nil
	}

	label := filePath
	if req.Key != "" {
		label = req.Key
	}
	action := "updated"
	if created {
		action = "stored"
	}
	return NewTextResponse(fmt.Sprintf("Memory %s: %s", action, label)), nil
}

// ---- RecallTool ----

// RecallTool searches persistent memories and increments hit counters for returned items.
type RecallTool struct {
	store          *kb.KBStore
	defaultTTLDays int
}

// NewRecallTool creates a new RecallTool.
func NewRecallTool(store *kb.KBStore, defaultTTLDays int) BaseTool {
	return &RecallTool{store: store, defaultTTLDays: defaultTTLDays}
}

func (t *RecallTool) Info() ToolInfo {
	return ToolInfo{
		Name:        recallToolName,
		Description: "Search persistent memories stored via 'remember'. Returns relevant memories sorted by relevance, recency, and access frequency. Automatically increments the hit counter for returned memories.",
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural language query to search memories.",
			},
			"scope": map[string]any{
				"type":        "string",
				"description": "Optional scope prefix to filter results (e.g. \"user/\", \"project/\").",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 5, max: 20).",
			},
		},
		Required: []string{"query"},
	}
}

func (t *RecallTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		Query string `json:"query"`
		Scope string `json:"scope"`
		Limit int    `json:"limit"`
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

	var pinnedScopes []string
	if req.Scope != "" {
		pinnedScopes = []string{req.Scope}
	}

	results, err := t.store.GetMemoriesForInjection(ctx, req.Query, req.Limit, 0, pinnedScopes)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("recall error: %v", err)), nil
	}

	type memoryItem struct {
		Key        string  `json:"key,omitempty"`
		Scope      string  `json:"scope,omitempty"`
		Content    string  `json:"content"`
		Score      float64 `json:"score"`
		Hits       int     `json:"hits"`
		Importance float64 `json:"importance"`
		ExpiresAt  string  `json:"expires_at,omitempty"`
	}

	items := make([]memoryItem, 0, len(results))
	for _, r := range results {
		// Increment hits best-effort; errors are non-fatal.
		_ = t.store.IncrementMemoryHits(ctx, r.Document.ID, t.defaultTTLDays)

		item := memoryItem{
			Key:        r.Document.MemoryKey,
			Scope:      r.Document.MemoryScope,
			Content:    r.Document.Content,
			Score:      r.Score,
			Hits:       r.Document.Hits,
			Importance: r.Document.Importance,
		}
		if r.Document.ExpiresAt != nil {
			item.ExpiresAt = r.Document.ExpiresAt.UTC().Format(timeLayoutRFC3339)
		}
		items = append(items, item)
	}

	return NewStructuredResponse(map[string]any{
		"count":    len(items),
		"memories": items,
	}), nil
}

// ---- ForgetTool ----

// ForgetTool deletes a memory by key or file path.
type ForgetTool struct {
	store *kb.KBStore
}

// NewForgetTool creates a new ForgetTool.
func NewForgetTool(store *kb.KBStore) BaseTool {
	return &ForgetTool{store: store}
}

func (t *ForgetTool) Info() ToolInfo {
	return ToolInfo{
		Name:        forgetToolName,
		Description: "Delete a memory by its key or file path. Use when information is no longer valid or the user asks to forget it.",
		Parameters: map[string]any{
			"key": map[string]any{
				"type":        "string",
				"description": "The upsert key of the memory to delete.",
			},
			"file_path": map[string]any{
				"type":        "string",
				"description": "Direct document path of the memory to delete.",
			},
		},
		Required: []string{},
	}
}

func (t *ForgetTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		Key      string `json:"key"`
		FilePath string `json:"file_path"`
	}
	if err := DecodeToolInput(params.Input, &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}

	filePath := req.FilePath

	if req.Key != "" {
		doc, err := t.store.GetMemoryByKey(ctx, req.Key)
		if err != nil {
			return NewTextErrorResponse(fmt.Sprintf("forget error: %v", err)), nil
		}
		if doc == nil {
			return NewTextResponse(fmt.Sprintf("Memory not found: key=%s", req.Key)), nil
		}
		filePath = doc.FilePath
	}

	if filePath == "" {
		return NewTextErrorResponse("either key or file_path is required"), nil
	}

	if err := t.store.DeleteDocument(ctx, filePath); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("forget delete error: %v", err)), nil
	}
	if err := t.store.DeleteDocumentFromFilesystem(filePath); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("forget filesystem error: %v", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("Memory deleted: %s", filePath)), nil
}
