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

// Blackboard GC defaults. Without a bound the board grows unbounded on two axes:
// each swarm's append log never sheds superseded facts, and every swarm that ever
// ran keeps its log for the store's whole lifetime. Because saveLocked rewrites
// the entire file on every Post, that unbounded growth also makes each write
// progressively more expensive. These caps keep both axes bounded.
const (
	// DefaultBlackboardMaxEntriesPerSwarm bounds one swarm's append log. Past it
	// the log is compacted to the newest entries while every key's winning
	// (last-write-wins) entry is retained so the merged Latest view never loses a
	// fact. A swarm posts decisions/interfaces/ownership, not chatter, so a few
	// hundred entries is generous headroom.
	DefaultBlackboardMaxEntriesPerSwarm = 200
	// DefaultBlackboardTTL purges a swarm whose newest entry is older than this.
	// Swarms are short-lived; a week is a safety net for boards left behind by a
	// swarm that finished without any explicit teardown, not a working lifetime.
	DefaultBlackboardTTL = 7 * 24 * time.Hour
)

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
	// maxEntriesPerSwarm and ttl bound growth; see the GC defaults above. A
	// non-positive value disables that axis of GC.
	maxEntriesPerSwarm int
	ttl                time.Duration
}

// BlackboardOption configures a Blackboard at construction.
type BlackboardOption func(*Blackboard)

// WithBlackboardLimits sets the GC bounds. A non-positive argument leaves the
// corresponding default in place, so a caller can tune one axis without knowing
// the other's default.
func WithBlackboardLimits(maxEntriesPerSwarm int, ttl time.Duration) BlackboardOption {
	return func(b *Blackboard) {
		if maxEntriesPerSwarm > 0 {
			b.maxEntriesPerSwarm = maxEntriesPerSwarm
		}
		if ttl > 0 {
			b.ttl = ttl
		}
	}
}

// NewBlackboard opens (or creates) a blackboard backed by path. A missing or
// empty file yields an empty blackboard; a malformed file is a hard error so
// corruption is surfaced rather than silently dropping shared state. GC bounds
// default to the package defaults unless overridden with WithBlackboardLimits.
func NewBlackboard(path string, opts ...BlackboardOption) (*Blackboard, error) {
	b := &Blackboard{
		path:               path,
		entries:            make(map[string][]BlackboardEntry),
		maxEntriesPerSwarm: DefaultBlackboardMaxEntriesPerSwarm,
		ttl:                DefaultBlackboardTTL,
	}
	for _, opt := range opts {
		opt(b)
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
	// Shed swarms that expired while pando was down. Done in memory only: the
	// next Post persists the shrunk map, so opening the board never rewrites it
	// (and never mutates the file merely by being read).
	b.pruneExpiredLocked(time.Now())
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
	b.compactSwarmLocked(swarmID)
	b.pruneExpiredLocked(time.Now())
	err := b.saveLocked()
	b.mu.Unlock()
	return err
}

// compactSwarmLocked bounds one swarm's append log to maxEntriesPerSwarm. It
// keeps the newest entries plus every key's winning (last) entry even when that
// winner falls outside the newest window, so the merged Latest view is never
// altered by GC — only superseded history is shed. Insertion order is preserved.
// Caller must hold b.mu.
func (b *Blackboard) compactSwarmLocked(swarmID string) {
	log := b.entries[swarmID]
	if b.maxEntriesPerSwarm <= 0 || len(log) <= b.maxEntriesPerSwarm {
		return
	}
	// Index of the last (winning) entry for each key.
	winner := make(map[string]int, len(log))
	for i, e := range log {
		winner[e.Key] = i
	}
	keepFrom := len(log) - b.maxEntriesPerSwarm
	kept := make([]BlackboardEntry, 0, b.maxEntriesPerSwarm)
	for i, e := range log {
		if i >= keepFrom || winner[e.Key] == i {
			kept = append(kept, e)
		}
	}
	b.entries[swarmID] = kept
}

// pruneExpiredLocked drops every swarm whose newest entry is older than ttl (and
// any swarm that somehow holds an empty log). This is the "purge terminated
// swarms" axis: a finished swarm stops posting, so its board ages out on its own
// without the blackboard needing to know task lifecycle state. Caller must hold
// b.mu.
func (b *Blackboard) pruneExpiredLocked(now time.Time) {
	if b.ttl <= 0 {
		return
	}
	for id, log := range b.entries {
		if len(log) == 0 {
			delete(b.entries, id)
			continue
		}
		// Entries are appended in time order, but a caller may stamp CreatedAt
		// itself, so scan for the true newest rather than trusting the tail.
		newest := log[0].CreatedAt
		for _, e := range log[1:] {
			if e.CreatedAt.After(newest) {
				newest = e.CreatedAt
			}
		}
		if now.Sub(newest) > b.ttl {
			delete(b.entries, id)
		}
	}
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
