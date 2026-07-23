package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// BlackboardEntry is one structured fact posted by an agent to a swarm's shared
// blackboard. Entries are append-only; the merged view (LatestNotes) resolves
// duplicate keys last-write-wins while retaining the winning author for
// traceability. This mirrors Hermes Kanban Swarm's low-tech blackboard (JSON
// comments on a root card) without introducing a second scheduler.
type BlackboardEntry struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	Author    string          `json:"author"`
	TaskID    string          `json:"task_id,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// Blackboard is a durable, process-shared coordination store keyed by swarm id.
// Sibling delegated tasks that share a parent (see Orchestrator.swarmKeyForTask)
// post facts here so they can coordinate — the primitive Pando's flat DAG lacked.
// Persistence is a single JSON file written atomically; writes are low-frequency
// (agents post decisions/interfaces/ownership, not chatter) so a synchronous save
// per Post is acceptable.
type Blackboard struct {
	path string
	mu   sync.RWMutex
	// entries maps swarmID -> ordered append log.
	entries map[string][]BlackboardEntry
}

// NewBlackboard opens (or creates) a blackboard backed by path. A missing or
// empty file yields an empty blackboard; a malformed file is a hard error so
// corruption is surfaced rather than silently dropping shared state.
func NewBlackboard(path string) (*Blackboard, error) {
	b := &Blackboard{
		path:    path,
		entries: make(map[string][]BlackboardEntry),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return b, nil
		}
		return nil, fmt.Errorf("failed to read blackboard: %w", err)
	}
	if len(data) == 0 {
		return b, nil
	}
	if err := json.Unmarshal(data, &b.entries); err != nil {
		return nil, fmt.Errorf("failed to parse blackboard: %w", err)
	}
	if b.entries == nil {
		b.entries = make(map[string][]BlackboardEntry)
	}
	return b, nil
}

// Post appends one entry to a swarm's log and persists the blackboard.
func (b *Blackboard) Post(swarmID string, entry BlackboardEntry) error {
	swarmID = strings.TrimSpace(swarmID)
	if swarmID == "" {
		return fmt.Errorf("swarm_id is required")
	}
	if strings.TrimSpace(entry.Key) == "" {
		return fmt.Errorf("key is required")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	if len(entry.Value) == 0 {
		entry.Value = json.RawMessage("null")
	}
	// Canonicalize to compact JSON so stored/rendered values stay stable and
	// tidy (and survive a save/reload round-trip byte-for-byte). A non-JSON
	// value is wrapped as a JSON string so the blackboard file never corrupts.
	var compact bytes.Buffer
	if err := json.Compact(&compact, entry.Value); err != nil {
		quoted, _ := json.Marshal(string(entry.Value))
		entry.Value = json.RawMessage(quoted)
	} else {
		entry.Value = json.RawMessage(append([]byte(nil), compact.Bytes()...))
	}

	b.mu.Lock()
	b.entries[swarmID] = append(b.entries[swarmID], entry)
	err := b.saveLocked()
	b.mu.Unlock()
	return err
}

// List returns the full append log for a swarm in insertion order.
func (b *Blackboard) List(swarmID string) []BlackboardEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	log := b.entries[strings.TrimSpace(swarmID)]
	out := make([]BlackboardEntry, len(log))
	copy(out, log)
	return out
}

// Latest returns the merged view: the most recent entry for each key. Callers
// get a stable, key-sorted slice so rendering and tests are deterministic.
func (b *Blackboard) Latest(swarmID string) []BlackboardEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	merged := make(map[string]BlackboardEntry)
	for _, e := range b.entries[strings.TrimSpace(swarmID)] {
		merged[e.Key] = e // later entries overwrite earlier ones (last-write-wins)
	}
	out := make([]BlackboardEntry, 0, len(merged))
	for _, e := range merged {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// saveLocked writes the blackboard atomically. Caller must hold b.mu.
func (b *Blackboard) saveLocked() error {
	if b.path == "" {
		return nil // in-memory only (tests)
	}
	// Marshal without indentation: Indent would re-flow embedded RawMessage
	// values, so a compact-on-Post value would come back indented after a
	// reload. A compact file keeps every stored value byte-stable.
	data, err := json.Marshal(b.entries)
	if err != nil {
		return fmt.Errorf("failed to marshal blackboard: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o755); err != nil {
		return fmt.Errorf("failed to create blackboard dir: %w", err)
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("failed to write blackboard temp: %w", err)
	}
	if err := os.Rename(tmp, b.path); err != nil {
		return fmt.Errorf("failed to rename blackboard temp: %w", err)
	}
	return nil
}

// renderLatest formats the merged blackboard as a human/agent-readable block for
// injection into a worker's prompt. Returns "" when the swarm has no entries.
func renderLatest(swarmID string, latest []BlackboardEntry) string {
	if len(latest) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Shared facts posted by sibling agents (latest value per key):\n")
	for _, e := range latest {
		author := e.Author
		if author == "" {
			author = "unknown"
		}
		sb.WriteString(fmt.Sprintf("- %s (by %s): %s\n", e.Key, author, string(e.Value)))
	}
	return sb.String()
}
