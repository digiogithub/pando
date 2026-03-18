package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	CacheStatsToolName = "cache_stats"

	cacheStatsDescription = `Show current session cache statistics and list cached entries.

Use this tool to:
- See how much memory the session cache is using
- List all cached tool responses with their cache_ids
- Check if a specific tool's output is cached
- Debug cache behavior

Returns: entry count, memory usage, and a summary of each cached entry.`
)

type cacheStatsTool struct{}

// NewCacheStatsTool returns a tool that reports session cache statistics.
func NewCacheStatsTool() BaseTool {
	return &cacheStatsTool{}
}

func (c *cacheStatsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        CacheStatsToolName,
		Description: cacheStatsDescription,
		Parameters:  map[string]any{},
		Required:    []string{},
	}
}

// CacheStatsResponse is the structured metadata attached to the stats response.
type CacheStatsResponse struct {
	Stats   CacheStats          `json:"stats"`
	Entries []CacheEntrySummary `json:"entries"`
}

// CacheEntrySummary is a compact descriptor for a single cached entry.
type CacheEntrySummary struct {
	CacheID    string `json:"cache_id"`
	ToolName   string `json:"tool_name"`
	TotalLines int    `json:"total_lines"`
	TotalBytes int    `json:"total_bytes"`
	AgeSeconds int    `json:"age_seconds"`
}

func (c *cacheStatsTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	cache := GetSessionCache(ctx)
	if cache == nil {
		sessionID, _ := GetContextValues(ctx)
		if sessionID != "" {
			var ok bool
			cache, ok = GetSessionCacheByID(sessionID)
			if !ok {
				return NewTextResponse("No session cache active"), nil
			}
		} else {
			return NewTextResponse("No session cache active"), nil
		}
	}

	stats := cache.Stats()

	// Collect entry summaries while holding read lock.
	cache.mu.RLock()
	entries := make([]CacheEntrySummary, 0, len(cache.entries))
	for _, entry := range cache.entries {
		entries = append(entries, CacheEntrySummary{
			CacheID:    entry.ID,
			ToolName:   entry.ToolName,
			TotalLines: entry.TotalLines,
			TotalBytes: entry.TotalBytes,
			AgeSeconds: int(time.Since(entry.CreatedAt).Seconds()),
		})
	}
	cache.mu.RUnlock()

	// Format human-readable output.
	var sb strings.Builder
	sb.WriteString("## Session Cache Statistics\n\n")
	sb.WriteString(fmt.Sprintf("- Entries: %d\n", stats.EntryCount))
	sb.WriteString(fmt.Sprintf("- Memory used: %s / %s\n",
		formatBytes(stats.TotalBytes), formatBytes(stats.MaxBytes)))
	sb.WriteString(fmt.Sprintf("- Evictions: %d\n\n", stats.Evictions))

	if len(entries) == 0 {
		sb.WriteString("No cached entries.\n")
	} else {
		sb.WriteString("## Cached Entries\n\n")
		sb.WriteString(fmt.Sprintf("%-38s  %-20s  %8s  %8s  %5s\n",
			"Cache ID", "Tool", "Lines", "Bytes", "Age"))
		sb.WriteString(strings.Repeat("-", 90) + "\n")
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("%-38s  %-20s  %8d  %8s  %4ds\n",
				e.CacheID, e.ToolName, e.TotalLines,
				formatBytes(int64(e.TotalBytes)), e.AgeSeconds))
		}
	}

	respData := CacheStatsResponse{Stats: stats, Entries: entries}
	return WithResponseMetadata(NewTextResponse(sb.String()), respData), nil
}

// formatBytes converts a byte count to a human-readable string (e.g. "1.5MB").
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}
