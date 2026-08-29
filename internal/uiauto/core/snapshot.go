package core

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// snapshotIDAlphabet is the base36 alphabet used to generate snapshot ids.
const snapshotIDAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// Snapshot is a captured, immutable view of an accessibility (sub)tree at a
// point in time. Every Element it contains is addressable through a
// qualified ElementRef scoped to this snapshot's ID.
type Snapshot struct {
	ID        string
	CreatedAt time.Time
	Backend   string
	AppID     string
	WindowID  string
	Root      *Element
	// Elements indexes every element in the snapshot by its bare element
	// id (e.g. "e17", without the "@<snapshotID>:" prefix).
	Elements map[string]*Element
	// Origin is the selector (if any) that produced this snapshot, kept so
	// the snapshot can be re-resolved after it goes stale.
	Origin *Selector
	// NativeHandles lets a backend stash opaque per-element native
	// handles (e.g. a COM pointer or a D-Bus object path) keyed by bare
	// element id, without leaking backend types into core.
	NativeHandles map[string]any
}

// newSnapshotID generates a snapshot id of the form "s" + 8 random base36
// characters.
func newSnapshotID() string {
	var sb [8]byte
	for i := range sb {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(snapshotIDAlphabet))))
		if err != nil {
			// crypto/rand failures are effectively never in practice; fall
			// back to a time-derived byte rather than panicking.
			sb[i] = snapshotIDAlphabet[time.Now().UnixNano()%int64(len(snapshotIDAlphabet))]
			continue
		}
		sb[i] = snapshotIDAlphabet[n.Int64()]
	}
	return "s" + string(sb[:])
}

// NewElementID returns the traversal-order element id for index n
// (1-based), e.g. NewElementID(1) == "e1".
func NewElementID(n int) string {
	return fmt.Sprintf("e%d", n)
}

// snapshotEntry wraps a Snapshot with store bookkeeping.
type snapshotEntry struct {
	snap       *Snapshot
	lastAccess time.Time
}

// SnapshotStore is a goroutine-safe, TTL-based, size-bounded (LRU) store of
// Snapshots, and the single place that resolves qualified ElementRefs.
type SnapshotStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	max     int
	entries map[string]*snapshotEntry
}

// NewSnapshotStore creates a SnapshotStore. ttl <= 0 disables expiry; max
// <= 0 disables the LRU cap.
func NewSnapshotStore(ttl time.Duration, max int) *SnapshotStore {
	return &SnapshotStore{
		ttl:     ttl,
		max:     max,
		entries: make(map[string]*snapshotEntry),
	}
}

// Put stores snap, assigning it a fresh ID if it does not already have one,
// and evicts expired/excess entries.
func (s *SnapshotStore) Put(snap *Snapshot) *Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if snap.ID == "" {
		snap.ID = newSnapshotID()
	}
	if snap.CreatedAt.IsZero() {
		snap.CreatedAt = time.Now()
	}
	now := time.Now()
	s.entries[snap.ID] = &snapshotEntry{snap: snap, lastAccess: now}
	s.pruneLocked(now)
	return snap
}

// Get retrieves a snapshot by id, returning SNAPSHOT_NOT_FOUND or STALE_REF
// as appropriate.
func (s *SnapshotStore) Get(id string) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(id, time.Now())
}

func (s *SnapshotStore) getLocked(id string, now time.Time) (*Snapshot, error) {
	entry, ok := s.entries[id]
	if !ok {
		return nil, NewSnapshotNotFoundError(fmt.Sprintf("snapshot %q was not found", id))
	}
	if s.ttl > 0 && now.Sub(entry.snap.CreatedAt) > s.ttl {
		delete(s.entries, id)
		return nil, NewStaleRefError(fmt.Sprintf("snapshot %q expired", id))
	}
	entry.lastAccess = now
	return entry.snap, nil
}

// Resolve looks up the element addressed by a qualified ElementRef,
// returning the owning snapshot and the element. Errors: INVALID_ARGS for
// a malformed ref, SNAPSHOT_NOT_FOUND / STALE_REF for the snapshot lookup,
// ELEMENT_NOT_FOUND when the snapshot no longer contains that element id.
func (s *SnapshotStore) Resolve(ref ElementRef) (*Snapshot, *Element, error) {
	snapshotID, elemID, err := ParseElementRef(string(ref))
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.getLocked(snapshotID, time.Now())
	if err != nil {
		return nil, nil, err
	}
	el, ok := snap.Elements[elemID]
	if !ok {
		return nil, nil, NewElementNotFoundError(fmt.Sprintf("element %q not found in snapshot %q", elemID, snapshotID))
	}
	return snap, el, nil
}

// Prune removes expired entries and, if over capacity, evicts the least
// recently accessed entries down to max.
func (s *SnapshotStore) Prune() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
}

func (s *SnapshotStore) pruneLocked(now time.Time) {
	if s.ttl > 0 {
		for id, entry := range s.entries {
			if now.Sub(entry.snap.CreatedAt) > s.ttl {
				delete(s.entries, id)
			}
		}
	}
	if s.max > 0 && len(s.entries) > s.max {
		type idAccess struct {
			id string
			at time.Time
		}
		list := make([]idAccess, 0, len(s.entries))
		for id, entry := range s.entries {
			list = append(list, idAccess{id, entry.lastAccess})
		}
		// Simple selection of the oldest entries to evict; snapshot
		// counts are expected to stay small (tens, not thousands), so an
		// O(n^2) pass is fine and keeps this dependency-free.
		for len(s.entries) > s.max {
			oldestIdx := -1
			for i, e := range list {
				if e.id == "" {
					continue
				}
				if oldestIdx == -1 || e.at.Before(list[oldestIdx].at) {
					oldestIdx = i
				}
			}
			if oldestIdx == -1 {
				break
			}
			delete(s.entries, list[oldestIdx].id)
			list[oldestIdx].id = ""
		}
	}
}

// Len returns the current number of stored snapshots (test/introspection
// helper).
func (s *SnapshotStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}
