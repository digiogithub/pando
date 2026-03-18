package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	CacheReadToolName = "cache_read"

	cacheReadDescription = `Read paginated content from the session cache.

When a tool response is too large (>300 lines or >15000 chars), it is automatically
cached and you receive a compact reference with cache_id. Use this tool to read
additional sections of the cached content.

WHEN TO USE:
- When you see "[Response cached: ... cache_id: XXX]" in a tool response
- When you need to read lines beyond the first page shown
- When you want to search within a large cached output

HOW TO USE:
- Provide the cache_id from the truncated response
- Use offset to start from a specific line (0-based)
- Use limit to control how many lines to read per page (default: 200)
- Use pattern to search within the cached content

PAGINATION:
- Check has_more in the response to know if more data exists
- Use offset = previous_offset + returned_lines for the next page
- Default page size is 200 lines

SEARCH:
- Use pattern parameter to find specific content within the cache
- Matching lines are shown with 2 lines of context around them
- Use context_lines to adjust surrounding context (0-5)`
)

// CacheReadParams defines the parameters for the cache_read tool.
type CacheReadParams struct {
	CacheID      string `json:"cache_id"`                // Required: ID from truncated response
	Offset       int    `json:"offset"`                  // Line offset (0-based), default 0
	Limit        int    `json:"limit"`                   // Lines to read, default 200
	Pattern      string `json:"pattern"`                 // Optional: search within cached content
	ContextLines int    `json:"context_lines,omitempty"` // Context lines around matches (default 2)
}

// cacheReadTool implements BaseTool for reading paginated cache content.
type cacheReadTool struct{}

// NewCacheReadTool creates a new cache_read tool instance.
func NewCacheReadTool() BaseTool {
	return &cacheReadTool{}
}

func (c *cacheReadTool) Info() ToolInfo {
	return ToolInfo{
		Name:        CacheReadToolName,
		Description: cacheReadDescription,
		Parameters: map[string]any{
			"cache_id": map[string]any{
				"type":        "string",
				"description": "The cache ID from a previously cached tool response",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Line offset to start reading from (0-based, default: 0)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Number of lines to read per page (default: 200, max: 500)",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Optional text pattern to search within the cached content",
			},
			"context_lines": map[string]any{
				"type":        "integer",
				"description": "Number of context lines around pattern matches (default: 2, max: 5)",
			},
		},
		Required: []string{"cache_id"},
	}
}

func (c *cacheReadTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params CacheReadParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.CacheID == "" {
		return NewTextErrorResponse("cache_id is required"), nil
	}

	// Clamp limit
	if params.Limit <= 0 {
		params.Limit = DefaultCachePageLines
	}
	if params.Limit > 500 {
		params.Limit = 500
	}

	// Get session cache from context
	cache := GetSessionCache(ctx)
	if cache == nil {
		// Fallback: try to get by session ID from context
		sessionID, _ := GetContextValues(ctx)
		if sessionID != "" {
			var ok bool
			cache, ok = GetSessionCacheByID(sessionID)
			if !ok {
				return NewTextErrorResponse("session cache not available"), nil
			}
		} else {
			return NewTextErrorResponse("session cache not available — no active session"), nil
		}
	}

	// Search mode
	if params.Pattern != "" {
		ctxLines := params.ContextLines
		if ctxLines <= 0 {
			ctxLines = 2
		}
		if ctxLines > 5 {
			ctxLines = 5
		}

		content, meta, err := cache.SearchInCache(params.CacheID, params.Pattern, ctxLines, params.Limit)
		if err != nil {
			return NewTextErrorResponse(fmt.Sprintf("cache search error: %s", err)), nil
		}

		if content == "" {
			return WithResponseMetadata(
				NewTextResponse(fmt.Sprintf("[No matches found for pattern %q in cache %s]\n[Cache has %d lines total]",
					params.Pattern, params.CacheID, meta.TotalLines)),
				meta,
			), nil
		}

		header := fmt.Sprintf("[Cache search: pattern=%q, tool=%s, cache_id=%s]\n[Found matches in %d-line cached response]\n\n",
			params.Pattern, meta.ToolName, params.CacheID, meta.TotalLines)

		return WithResponseMetadata(NewTextResponse(header+content), meta), nil
	}

	// Pagination mode
	content, meta, err := cache.GetPage(params.CacheID, params.Offset, params.Limit)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("cache read error: %s", err)), nil
	}

	// Build numbered output
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Cache page: lines %d-%d of %d | tool: %s | cache_id: %s]\n",
		params.Offset+1, params.Offset+meta.ReturnedLines, meta.TotalLines,
		meta.ToolName, params.CacheID))

	if meta.ReturnedLines == 0 {
		sb.WriteString("[No more content — end of cached response]\n")
	} else {
		sb.WriteString("\n")
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			lineNum := params.Offset + i + 1
			sb.WriteString(fmt.Sprintf("%6d|%s\n", lineNum, line))
		}
	}

	if meta.HasMore {
		nextOffset := params.Offset + meta.ReturnedLines
		sb.WriteString(fmt.Sprintf("\n[Has more content. Use cache_read with cache_id=%q, offset=%d to continue]",
			params.CacheID, nextOffset))
	} else {
		sb.WriteString("\n[End of cached content]")
	}

	return WithResponseMetadata(NewTextResponse(sb.String()), meta), nil
}
