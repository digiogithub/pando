package tools

import (
	"fmt"
	"strings"
)

const (
	// CacheThresholdBytes is the minimum response size to trigger auto-caching.
	CacheThresholdBytes = 15000
	// CacheThresholdLines is the minimum line count to trigger auto-caching.
	CacheThresholdLines = 300
	// CachePreviewLines is the number of lines shown inline when a response is cached.
	CachePreviewLines = 200
)

// cacheBypassTools lists tool names that should never be auto-cached.
// These tools return small confirmations, errors, or are the cache tool itself.
var cacheBypassTools = map[string]bool{
	"edit":            true,
	"write":           true,
	"patch":           true,
	"diagnostics":     true,
	CacheReadToolName: true,
	"bash":            false, // bash CAN be large, allow caching
}

// InterceptToolResponse checks if a tool response exceeds caching thresholds.
// If so, it stores the full content in the session cache and returns a compact
// reference containing the first CachePreviewLines lines and a cache_id.
// If below thresholds or cache is unavailable, the response is returned unchanged.
func InterceptToolResponse(
	cache *SessionCache,
	toolCallID string,
	toolName string,
	response ToolResponse,
) ToolResponse {
	// Never cache errors
	if response.IsError {
		return response
	}

	// Never cache image responses
	if response.Type == ToolResponseTypeImage {
		return response
	}

	// Check bypass list
	if bypass, exists := cacheBypassTools[toolName]; exists && bypass {
		return response
	}

	// Check cache availability
	if cache == nil {
		return response
	}

	content := response.Content
	contentLen := len(content)
	lineCount := strings.Count(content, "\n") + 1

	// Check thresholds
	if contentLen < CacheThresholdBytes && lineCount < CacheThresholdLines {
		return response
	}

	// Store in cache
	cacheID := cache.Store(toolCallID, toolName, content)

	// Get first page for inline preview
	firstPage, meta, err := cache.GetPage(cacheID, 0, CachePreviewLines)
	if err != nil {
		// Cache failed — return original response
		return response
	}

	// Build compact reference response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"[Response cached: %d lines, %d bytes → cache_id: %q | tool: %s]\n",
		meta.TotalLines, meta.TotalBytes, cacheID, toolName,
	))
	sb.WriteString(fmt.Sprintf(
		"[Showing lines 1-%d of %d. Use cache_read tool for more pages.]\n\n",
		meta.ReturnedLines, meta.TotalLines,
	))

	// Add line-numbered preview
	lines := strings.Split(firstPage, "\n")
	for i, line := range lines {
		sb.WriteString(fmt.Sprintf("%6d|%s\n", i+1, line))
	}

	if meta.HasMore {
		nextOffset := meta.ReturnedLines
		sb.WriteString(fmt.Sprintf(
			"\n[%d more lines available. Call: cache_read(cache_id=%q, offset=%d)]\n",
			meta.TotalLines-meta.ReturnedLines, cacheID, nextOffset,
		))
	}

	// Attach pagination metadata alongside any existing metadata
	compactResponse := NewTextResponse(sb.String())

	// Preserve original metadata if present, merge with pagination info
	if response.Metadata != "" {
		// Keep original metadata — just wrap the content
		compactResponse.Metadata = response.Metadata
	}

	return compactResponse
}
