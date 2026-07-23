// Package events provides a durable, append-only log of delegation signals with
// cursor-based, at-least-once delivery.
//
// Pando's delegation feedback loop (Case A live injection, Case B resurrection)
// is driven by an in-memory completion bus whose sends are non-blocking: a slow
// or full subscriber silently drops the event, and a restart loses every signal
// that had not been consumed yet. This log is the durable counterpart, mirroring
// Hermes Kanban Swarm's task_events table plus its cursor-advanced notifier:
//
//   - Every terminal task outcome (and the integrity signals around it) is
//     appended to a JSONL file before it is broadcast.
//   - A consumer identified by a subscription id reads its Unseen events in
//     order and Acks them once handled, which persists its cursor.
//   - An event that was never acked is redelivered — after a crash, a restart,
//     or a dropped in-memory broadcast — so delivery is at-least-once and
//     consumers stay responsible for their own idempotency.
//
// The log deliberately stores only identifiers and a short summary, never the
// whole task: consumers re-read the task from the store, so a replayed event can
// never resurrect a stale snapshot of it.
package events

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Kind classifies a delegation signal.
type Kind string

const (
	// KindCompleted / KindFailed / KindCancelled are the terminal task outcomes.
	// They are the events the delegation supervisor acts on.
	KindCompleted Kind = "completed"
	KindFailed    Kind = "failed"
	KindCancelled Kind = "cancelled"
	// KindGateFailed records a dependent task held pending because a gate
	// dependency completed without passing its conclusion gate.
	KindGateFailed Kind = "gate_failed"
	// KindBreakerTripped records a task that reached the consecutive-failure
	// limit and can no longer be respawned without a fix or an explicit force.
	KindBreakerTripped Kind = "breaker_tripped"
	// KindRespawnRefused records a Relaunch/Retry denied by the respawn guard.
	KindRespawnRefused Kind = "respawn_refused"
	// KindReclaimed records a task whose claim lease expired (or whose worker
	// process died) and that was returned to the dispatchable pool.
	KindReclaimed Kind = "reclaimed"
)

// IsTerminal reports whether the kind describes a task reaching a terminal
// state, i.e. the events a delegation consumer must react to.
func (k Kind) IsTerminal() bool {
	switch k {
	case KindCompleted, KindFailed, KindCancelled:
		return true
	}
	return false
}

// Event is one durable delegation signal. Seq is assigned by the log and is
// strictly increasing across the log's lifetime; consumers use it as their
// cursor. The payload is intentionally minimal — enough to route and explain the
// signal without duplicating task state that may since have changed.
type Event struct {
	Seq             int64  `json:"seq"`
	Kind            Kind   `json:"kind"`
	TaskID          string `json:"task_id"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
	CorrelationID   string `json:"correlation_id,omitempty"`
	// Detail is a short human-readable note (conclusion summary, refusal reason,
	// gate dependency id). It exists for logs and UIs, never for control flow.
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// DefaultMaxEntries bounds the retained log. The file is compacted once it grows
// past twice this many events.
const DefaultMaxEntries = 5000

// Log is a durable append-only event log with per-subscription cursors. A nil
// *Log is a valid no-op log: every method is nil-safe, so callers can hold a nil
// pointer when the feature is disabled instead of branching at each call site.
type Log struct {
	path       string
	cursorPath string
	maxEntries int

	mu      sync.Mutex
	events  []Event
	lastSeq int64
	cursors map[string]int64
}

// Open loads (or creates) the log at path. maxEntries <= 0 uses
// DefaultMaxEntries. A malformed line is skipped rather than failing the open:
// an event log must never be the reason the orchestrator cannot start.
func Open(path string, maxEntries int) (*Log, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("event log path is required")
	}
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	l := &Log{
		path:       path,
		cursorPath: path + ".cursors.json",
		maxEntries: maxEntries,
		cursors:    make(map[string]int64),
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create event log dir: %w", err)
	}
	if err := l.load(); err != nil {
		return nil, err
	}
	l.loadCursors()
	return l, nil
}

func (l *Log) load() error {
	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to open event log: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Detail is short by construction, but a pathological summary must not abort
	// the scan with bufio.ErrTooLong.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // skip a torn or corrupt line, keep the rest of the history
		}
		l.events = append(l.events, e)
		if e.Seq > l.lastSeq {
			l.lastSeq = e.Seq
		}
	}
	return nil
}

func (l *Log) loadCursors() {
	data, err := os.ReadFile(l.cursorPath)
	if err != nil || len(data) == 0 {
		return
	}
	var cursors map[string]int64
	if err := json.Unmarshal(data, &cursors); err != nil {
		return // a corrupt cursor file degrades to "replay what is retained"
	}
	maps.Copy(l.cursors, cursors)
}

// Append records an event, assigning it the next sequence number, and returns
// the stored copy. On a nil log it is a no-op returning the zero Event.
func (l *Log) Append(e Event) (Event, error) {
	if l == nil {
		return Event{}, nil
	}
	if strings.TrimSpace(string(e.Kind)) == "" {
		return Event{}, fmt.Errorf("event kind is required")
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.lastSeq++
	e.Seq = l.lastSeq
	l.events = append(l.events, e)

	if err := l.appendLineLocked(e); err != nil {
		return e, err
	}
	if len(l.events) > 2*l.maxEntries {
		l.compactLocked()
	}
	return e, nil
}

// appendLineLocked writes one JSON line to the log file. Caller holds l.mu.
func (l *Log) appendLineLocked(e Event) error {
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open event log for append: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to append event: %w", err)
	}
	return nil
}

// compactLocked trims the log to the most recent maxEntries events and rewrites
// the file. Cursors that pointed into the dropped range are clamped forward to
// the new floor: bounded, explicit signal loss is preferred to a log that grows
// without limit because one consumer stopped acking. Caller holds l.mu.
func (l *Log) compactLocked() {
	keep := l.events[len(l.events)-l.maxEntries:]
	trimmed := make([]Event, len(keep))
	copy(trimmed, keep)
	l.events = trimmed

	floor := l.events[0].Seq - 1
	for sub, cursor := range l.cursors {
		if cursor < floor {
			l.cursors[sub] = floor
		}
	}

	var sb strings.Builder
	for _, e := range l.events {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		sb.Write(data)
		sb.WriteByte('\n')
	}
	writeFileAtomic(l.path, []byte(sb.String()))
	l.saveCursorsLocked()
}

// Unseen returns the events a subscription has not acked yet, oldest first. An
// unknown subscription starts at the beginning of the retained log, so a
// consumer that lost its cursor file replays what is still on disk rather than
// silently skipping it.
func (l *Log) Unseen(subID string) []Event {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	cursor := l.cursors[subID]
	var out []Event
	for _, e := range l.events {
		if e.Seq > cursor {
			out = append(out, e)
		}
	}
	return out
}

// Ack advances a subscription's cursor to seq (never backwards) and persists it.
// Acking event N also acks everything before it: events are delivered in order,
// so a consumer that reaches N has already decided what to do with its
// predecessors.
func (l *Log) Ack(subID string, seq int64) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if seq <= l.cursors[subID] {
		return nil
	}
	l.cursors[subID] = seq
	return l.saveCursorsLocked()
}

// Cursor reports a subscription's acked watermark.
func (l *Log) Cursor(subID string) int64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cursors[subID]
}

// LastSeq reports the sequence number of the most recently appended event.
func (l *Log) LastSeq() int64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastSeq
}

// Recent returns up to limit of the most recent events, newest last. Intended
// for inspection surfaces (CLI/UI), not for control flow.
func (l *Log) Recent(limit int) []Event {
	if l == nil || limit <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	start := 0
	if len(l.events) > limit {
		start = len(l.events) - limit
	}
	out := make([]Event, len(l.events)-start)
	copy(out, l.events[start:])
	return out
}

// saveCursorsLocked persists the cursor map atomically. Caller holds l.mu.
func (l *Log) saveCursorsLocked() error {
	data, err := json.Marshal(l.cursors)
	if err != nil {
		return fmt.Errorf("failed to marshal event cursors: %w", err)
	}
	return writeFileAtomic(l.cursorPath, data)
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", filepath.Base(tmp), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("failed to rename %s: %w", filepath.Base(tmp), err)
	}
	return nil
}
