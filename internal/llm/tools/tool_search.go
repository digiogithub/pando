package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolSearchProvider is the interface the tool_search tool uses to query the
// active tool registry. It is satisfied by *tooldiscovery.Registry — we use
// an interface here to avoid an import cycle.
type ToolSearchProvider interface {
	// Search returns up to limit results ranked by relevance to query.
	// Each result must be JSON-marshallable.
	SearchTools(query string, limit int) []ToolSearchEntry
	// MarkDiscovered marks a canonical tool name as visible for this session.
	MarkDiscovered(name string)
}

// ToolSearchEntry is the shape returned by a ToolSearchProvider.
type ToolSearchEntry struct {
	CanonicalName string   `json:"name"`
	Aliases       []string `json:"aliases,omitempty"`
	ServerName    string   `json:"server,omitempty"`
	Source        string   `json:"source,omitempty"`
	Description   string   `json:"description"`
}

type toolSearchTool struct {
	provider ToolSearchProvider
}

// NewToolSearchTool creates the built-in tool_search tool backed by provider.
// provider should be set to the active *tooldiscovery.Registry after it is
// wrapped with a thin adapter (see tooldiscovery.RegistrySearchAdapter).
func NewToolSearchTool(provider ToolSearchProvider) BaseTool {
	return &toolSearchTool{provider: provider}
}

func (t *toolSearchTool) Info() ToolInfo {
	return ToolInfo{
		Name: "tool_search",
		Description: "Search for available tools by natural-language query. " +
			"Use this when you need a capability but are unsure of the exact tool name. " +
			"Returns a ranked list of matching tools with their names and descriptions. " +
			"Tool names returned here can be used directly in subsequent calls.",
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural language description of the capability you are looking for.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default 8, max 20).",
			},
			"source": map[string]any{
				"type":        "string",
				"description": "Optional source filter: core, mcp, internal, lua, mesnada, rag, gateway.",
			},
		},
		Required: []string{"query"},
	}
}

func (t *toolSearchTool) Run(_ context.Context, params ToolCall) (ToolResponse, error) {
	var input struct {
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(params.Input), &input); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %s", err)), nil
	}
	if input.Query == "" {
		return NewTextErrorResponse("query is required"), nil
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 8
	}
	if limit > 20 {
		limit = 20
	}

	results := t.provider.SearchTools(input.Query, limit)

	// Filter by source if requested.
	if input.Source != "" {
		filtered := results[:0]
		for _, r := range results {
			if r.Source == input.Source {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	if len(results) == 0 {
		return NewTextResponse("No tools found matching the query. Try broader terms."), nil
	}

	// Mark results as discovered so the selection policy can expose them.
	for _, r := range results {
		t.provider.MarkDiscovered(r.CanonicalName)
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return NewTextErrorResponse("failed to marshal results"), nil
	}
	return NewTextResponse(string(out)), nil
}
