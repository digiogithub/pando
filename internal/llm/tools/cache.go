package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/llm/tools/readmode"
	"github.com/google/uuid"
)

const (
	DefaultCacheMaxBytes  = 50 * 1024 * 1024 // 50MB per session
	DefaultCachePageLines = 200              // Default page size
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
	// DedupHits counts unchanged-re-read collapses (Phase 2 F-references).
	DedupHits int `json:"dedup_hits"`
	// Bounces counts compressed→full read bounces observed by the Phase 3 tracker.
	Bounces int `json:"bounces"`
	// BounceWastedBytes is the rendered size of compressed reads that later bounced.
	BounceWastedBytes int64 `json:"bounce_wasted_bytes"`
}

// ReadDedupStatus classifies a `view` re-read against what was already delivered
// for the same (path, window) this session.
type ReadDedupStatus string

const (
	// ReadDedupNew means this window has not been delivered before.
	ReadDedupNew ReadDedupStatus = "new"
	// ReadDedupUnchanged means an identical window was already delivered.
	ReadDedupUnchanged ReadDedupStatus = "unchanged"
	// ReadDedupChanged means the same window was delivered before but its content changed.
	ReadDedupChanged ReadDedupStatus = "changed"
	// ReadDedupUpgraded means the content is unchanged but the caller now wants a
	// higher fidelity than was previously delivered (e.g. a full read after an
	// earlier signatures read); the read must proceed for real rather than collapse.
	ReadDedupUpgraded ReadDedupStatus = "upgraded"
)

// ReadDedupResult is returned by RecordRead.
type ReadDedupResult struct {
	// Status is new / unchanged / changed.
	Status ReadDedupStatus
	// Label is the stable F-reference label for this window (e.g. "F1").
	Label string
	// PrevContent holds the previously-delivered window content; populated only
	// when Status is ReadDedupChanged so the caller can render a diff.
	PrevContent string
}

// readWindowRecord tracks the last content delivered for one (path, window) key.
type readWindowRecord struct {
	label   string
	hash    string
	content string
	// mode is the fidelity actually delivered for this window ("full", "signatures"
	// or "map"). A re-read only collapses to an "unchanged" stub when the delivered
	// fidelity already covers what the caller now wants (see canCollapseFidelity).
	mode string
}

// maxDedupContentBytes caps how much window content is retained per record for
// later diffing, bounding the dedup map's memory footprint.
const maxDedupContentBytes = 256 * 1024

// SessionCache is a thread-safe, LRU in-memory cache scoped to a single session.
type SessionCache struct {
	mu          sync.RWMutex
	entries     map[string]*CacheEntry // key: cacheID
	order       []string               // LRU order (oldest first)
	sessionID   string
	maxBytes    int64
	currentSize int64
	evictions   int

	// readWindows tracks delivered (path, window) reads for Phase 2 dedup.
	readWindows map[string]*readWindowRecord
	readLabels  int
	dedupHits   int

	// bounce is the Phase 3 adaptive auto-mode safety tracker.
	bounce *readmode.BounceTracker
}

// NewSessionCache creates a new session-scoped cache.
func NewSessionCache(sessionID string) *SessionCache {
	return &SessionCache{
		entries:     make(map[string]*CacheEntry),
		order:       make([]string, 0),
		sessionID:   sessionID,
		maxBytes:    DefaultCacheMaxBytes,
		readWindows: make(map[string]*readWindowRecord),
		bounce:      readmode.NewBounceTracker(),
	}
}

// BounceTracker returns the session's Phase 3 auto-mode bounce tracker.
func (c *SessionCache) BounceTracker() *readmode.BounceTracker {
	return c.bounce
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

// RecordRead classifies and records a `view` read of a (path, window) at a given
// delivered fidelity (deliveredMode: "full", "signatures" or "map"). It returns
// whether the window is new, unchanged, changed, or upgraded since the last
// delivery, plus the stable F-reference label for the window:
//   - new: never delivered before — a fresh label is assigned.
//   - unchanged: identical content AND the prior fidelity already covers what is
//     now wanted — the caller may collapse to a stub (DedupHits incremented).
//   - upgraded: identical content but the caller now wants higher fidelity than
//     was delivered (e.g. full after signatures) — the read must proceed; the
//     recorded fidelity is bumped and no stub is emitted.
//   - changed: same window but the content differs — previous content is returned
//     for diffing and the record is updated.
func (c *SessionCache) RecordRead(path string, startLine, endLine int, content, deliveredMode string) ReadDedupResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.readWindows == nil {
		c.readWindows = make(map[string]*readWindowRecord)
	}

	key := fmt.Sprintf("%s\x00%d-%d", path, startLine, endLine)
	hash := hashContent(content)
	mode := normalizeDeliveredMode(deliveredMode)

	rec, ok := c.readWindows[key]
	if !ok {
		c.readLabels++
		label := fmt.Sprintf("F%d", c.readLabels)
		c.readWindows[key] = &readWindowRecord{
			label:   label,
			hash:    hash,
			content: capContent(content),
			mode:    mode,
		}
		return ReadDedupResult{Status: ReadDedupNew, Label: label}
	}

	if rec.hash != hash {
		prev := rec.content
		rec.hash = hash
		rec.content = capContent(content)
		rec.mode = mode
		return ReadDedupResult{Status: ReadDedupChanged, Label: rec.label, PrevContent: prev}
	}

	// Identical content. Only collapse to a stub when the previously delivered
	// fidelity already covers what the caller now wants; otherwise the read must
	// proceed at the higher fidelity.
	if canCollapseFidelity(rec.mode, mode) {
		c.dedupHits++
		return ReadDedupResult{Status: ReadDedupUnchanged, Label: rec.label}
	}
	rec.mode = mode
	return ReadDedupResult{Status: ReadDedupUpgraded, Label: rec.label}
}

// normalizeDeliveredMode maps an arbitrary mode string to the fidelity tiers the
// dedup tracker reasons about, defaulting to full.
func normalizeDeliveredMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "signatures":
		return "signatures"
	case "map":
		return "map"
	default:
		return "full"
	}
}

// canCollapseFidelity reports whether a window previously delivered at `prev`
// fidelity can satisfy a re-read now wanting `now` fidelity. A full prior delivery
// covers everything; otherwise only an identical projection collapses.
func canCollapseFidelity(prev, now string) bool {
	if prev == "full" {
		return true
	}
	return prev == now
}

// hashContent returns a hex SHA-256 of the content.
func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// capContent bounds retained window content for diffing.
func capContent(content string) string {
	if len(content) > maxDedupContentBytes {
		return content[:maxDedupContentBytes]
	}
	return content
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
	c.readWindows = make(map[string]*readWindowRecord)
	c.readLabels = 0
	c.dedupHits = 0
	c.bounce.Reset()
}

// Stats returns current cache statistics.
func (c *SessionCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	bs := c.bounce.Stats()
	return CacheStats{
		EntryCount:        len(c.entries),
		TotalBytes:        c.currentSize,
		MaxBytes:          c.maxBytes,
		Evictions:         c.evictions,
		DedupHits:         c.dedupHits,
		Bounces:           bs.Bounces,
		BounceWastedBytes: bs.WastedBytes,
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
