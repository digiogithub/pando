package tools

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultCacheMaxBytes  = 50 * 1024 * 1024 // 50MB per session
	DefaultCachePageLines = 200               // Default page size
)

// CacheEntry holds a cached tool response with pagination support.
type CacheEntry struct {
	ID         string
	ToolCallID string
	ToolName   string
	Content    string   // Full raw response
	Lines      []string // Pre-split lines for O(1) pagination
	TotalLines int
	TotalBytes int
	CreatedAt  time.Time
	LastUsed   time.Time
}

// PaginationMetadata describes a paginated result from the cache.
type PaginationMetadata struct {
	CacheID       string `json:"cache_id"`
	TotalLines    int    `json:"total_lines"`
	TotalBytes    int    `json:"total_bytes"`
	Offset        int    `json:"offset"`
	Limit         int    `json:"limit"`
	HasMore       bool   `json:"has_more"`
	ReturnedLines int    `json:"returned_lines"`
	ToolName      string `json:"tool_name"`
}

// CacheStats provides statistics about the cache state.
type CacheStats struct {
	EntryCount int   `json:"entry_count"`
	TotalBytes int64 `json:"total_bytes"`
	MaxBytes   int64 `json:"max_bytes"`
	Evictions  int   `json:"evictions"`
}

// SessionCache is a thread-safe, LRU in-memory cache scoped to a single session.
type SessionCache struct {
	mu          sync.RWMutex
	entries     map[string]*CacheEntry // key: cacheID
	order       []string               // LRU order (oldest first)
	sessionID   string
	maxBytes    int64
	currentSize int64
	evictions   int
}

// NewSessionCache creates a new session-scoped cache.
func NewSessionCache(sessionID string) *SessionCache {
	return &SessionCache{
		entries:   make(map[string]*CacheEntry),
		order:     make([]string, 0),
		sessionID: sessionID,
		maxBytes:  DefaultCacheMaxBytes,
	}
}

// Store saves content in the cache and returns a unique cacheID.
func (c *SessionCache) Store(toolCallID, toolName, content string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := uuid.New().String()
	lines := strings.Split(content, "\n")
	entry := &CacheEntry{
		ID:         id,
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Content:    content,
		Lines:      lines,
		TotalLines: len(lines),
		TotalBytes: len(content),
		CreatedAt:  time.Now(),
		LastUsed:   time.Now(),
	}

	// Evict LRU entries if over limit
	entryBytes := int64(len(content))
	for c.currentSize+entryBytes > c.maxBytes && len(c.order) > 0 {
		c.evictOldest()
	}

	c.entries[id] = entry
	c.order = append(c.order, id)
	c.currentSize += entryBytes
	return id
}

// Get retrieves a cache entry by ID.
func (c *SessionCache) Get(cacheID string) (*CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[cacheID]
	if ok {
		entry.LastUsed = time.Now()
		c.touchLRU(cacheID)
	}
	return entry, ok
}

// GetPage returns a paginated slice of lines from a cached entry.
func (c *SessionCache) GetPage(cacheID string, offset, limit int) (string, PaginationMetadata, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[cacheID]
	if !ok {
		return "", PaginationMetadata{}, fmt.Errorf("cache entry not found: %s", cacheID)
	}
	entry.LastUsed = time.Now()
	c.touchLRU(cacheID)

	if limit <= 0 {
		limit = DefaultCachePageLines
	}
	if offset < 0 {
		offset = 0
	}

	totalLines := len(entry.Lines)
	if offset >= totalLines {
		meta := PaginationMetadata{
			CacheID: cacheID, TotalLines: totalLines,
			TotalBytes: entry.TotalBytes, Offset: offset,
			Limit: limit, HasMore: false, ReturnedLines: 0,
			ToolName: entry.ToolName,
		}
		return "", meta, nil
	}

	end := offset + limit
	if end > totalLines {
		end = totalLines
	}

	pageLines := entry.Lines[offset:end]
	content := strings.Join(pageLines, "\n")
	returned := end - offset

	meta := PaginationMetadata{
		CacheID:       cacheID,
		TotalLines:    totalLines,
		TotalBytes:    entry.TotalBytes,
		Offset:        offset,
		Limit:         limit,
		HasMore:       end < totalLines,
		ReturnedLines: returned,
		ToolName:      entry.ToolName,
	}
	return content, meta, nil
}

// SearchInCache searches for a pattern within cached content and returns matching lines with context.
func (c *SessionCache) SearchInCache(cacheID, pattern string, contextLines, limit int) (string, PaginationMetadata, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[cacheID]
	if !ok {
		return "", PaginationMetadata{}, fmt.Errorf("cache entry not found: %s", cacheID)
	}
	entry.LastUsed = time.Now()

	if limit <= 0 {
		limit = 100
	}
	if contextLines < 0 {
		contextLines = 2
	}

	lowerPattern := strings.ToLower(pattern)
	var resultLines []string
	matchCount := 0

	for i, line := range entry.Lines {
		if strings.Contains(strings.ToLower(line), lowerPattern) {
			matchCount++
			if matchCount > limit {
				break
			}
			// Add context before
			start := i - contextLines
			if start < 0 {
				start = 0
			}
			end := i + contextLines + 1
			if end > len(entry.Lines) {
				end = len(entry.Lines)
			}
			if len(resultLines) > 0 {
				resultLines = append(resultLines, "---")
			}
			for j := start; j < end; j++ {
				lineNum := j + 1
				prefix := "  "
				if j == i {
					prefix = "> "
				}
				resultLines = append(resultLines, fmt.Sprintf("%s%4d: %s", prefix, lineNum, entry.Lines[j]))
			}
		}
	}

	content := strings.Join(resultLines, "\n")
	meta := PaginationMetadata{
		CacheID: cacheID, TotalLines: len(entry.Lines),
		TotalBytes: entry.TotalBytes, ReturnedLines: len(resultLines),
		ToolName: entry.ToolName,
	}
	return content, meta, nil
}

// Delete removes a single entry from the cache.
func (c *SessionCache) Delete(cacheID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[cacheID]; ok {
		c.currentSize -= int64(entry.TotalBytes)
		delete(c.entries, cacheID)
		c.removeFromOrder(cacheID)
	}
}

// Clear removes all entries from the cache.
func (c *SessionCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
	c.order = make([]string, 0)
	c.currentSize = 0
}

// Stats returns current cache statistics.
func (c *SessionCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		EntryCount: len(c.entries),
		TotalBytes: c.currentSize,
		MaxBytes:   c.maxBytes,
		Evictions:  c.evictions,
	}
}

// evictOldest removes the least recently used entry. Must be called with lock held.
func (c *SessionCache) evictOldest() {
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	if entry, ok := c.entries[oldest]; ok {
		c.currentSize -= int64(entry.TotalBytes)
		delete(c.entries, oldest)
		c.evictions++
	}
}

// touchLRU moves an entry to the end of the LRU order. Must be called with lock held.
func (c *SessionCache) touchLRU(id string) {
	for i, v := range c.order {
		if v == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, id)
			return
		}
	}
}

// removeFromOrder removes an entry from the LRU order. Must be called with lock held.
func (c *SessionCache) removeFromOrder(id string) {
	for i, v := range c.order {
		if v == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}
