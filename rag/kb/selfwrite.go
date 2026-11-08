package kb

import (
	"time"
)

// selfWriteTTL bounds how long a recorded self-write is remembered.
// It must be comfortably longer than the watcher's 250ms per-path debounce
// (watcher.go) — the delay between a mirror write and the fsnotify event
// reaching handleWatchEvent — but short enough that a stale entry cannot
// linger and mask a later, legitimate hand edit of the same file.
const selfWriteTTL = 3 * time.Second

// selfWriteCap bounds how many entries are kept at once, so a burst of
// mirror writes (e.g. many kb_add_document calls in a row) cannot grow the
// map without limit. Recording past the cap first sweeps expired entries and,
// if that alone isn't enough, evicts the entry closest to expiry.
const selfWriteCap = 2048

// selfWriteEntry is one recorded filesystem mirror write or delete: the mtime
// the store itself just produced for a path (meaningless for a delete), and
// when the entry stops being honored.
type selfWriteEntry struct {
	mtimeUnix int64
	isDelete  bool
	expiresAt time.Time
}

// recordSelfWrite notes that the store itself just wrote absPath with the
// given mtime, so the watcher can recognize and drop the fsnotify event that
// write is about to generate instead of re-indexing the file and discarding
// the metadata/tags the write just stored.
func (s *KBStore) recordSelfWrite(absPath string, mtimeUnix int64) {
	s.putSelfWrite(absPath, selfWriteEntry{mtimeUnix: mtimeUnix})
}

// recordSelfDelete notes that the store itself just removed the mirrored file
// at absPath. A removed file has no meaningful mtime, so any watcher event
// for the path while the entry is live is treated as the same self-delete.
func (s *KBStore) recordSelfDelete(absPath string) {
	s.putSelfWrite(absPath, selfWriteEntry{isDelete: true})
}

func (s *KBStore) putSelfWrite(absPath string, entry selfWriteEntry) {
	now := time.Now()
	entry.expiresAt = now.Add(selfWriteTTL)

	s.selfWriteMu.Lock()
	defer s.selfWriteMu.Unlock()

	if s.selfWrites == nil {
		s.selfWrites = make(map[string]selfWriteEntry)
	}
	s.sweepSelfWritesLocked(now)
	if _, exists := s.selfWrites[absPath]; !exists && len(s.selfWrites) >= selfWriteCap {
		s.evictOldestSelfWriteLocked()
	}
	s.selfWrites[absPath] = entry
}

// consumeSelfWrite reports whether absPath/mtimeUnix matches a recent,
// unexpired self-write recorded by recordSelfWrite. The entry is NOT removed
// on a match: a single filesystem write commonly surfaces as more than one
// fsnotify event for the same path and mtime (e.g. a new file's Create
// followed by its Write), and every one of them must be recognized as the
// same self-write, not just the first. Entries are instead reclaimed by TTL
// expiry, swept opportunistically on the next recordSelfWrite/recordSelfDelete
// call (see sweepSelfWritesLocked).
func (s *KBStore) consumeSelfWrite(absPath string, mtimeUnix int64) bool {
	entry, ok := s.peekSelfWrite(absPath)
	if !ok || entry.isDelete {
		return false
	}
	return entry.mtimeUnix == mtimeUnix
}

// consumeSelfDelete reports whether absPath matches a recent, unexpired
// self-delete recorded by recordSelfDelete. Like consumeSelfWrite, the entry
// is not removed on a match, for the same multiple-events-per-write reason.
func (s *KBStore) consumeSelfDelete(absPath string) bool {
	entry, ok := s.peekSelfWrite(absPath)
	if !ok {
		return false
	}
	return entry.isDelete
}

// peekSelfWrite returns the entry for absPath, if any and unexpired, without
// removing it.
func (s *KBStore) peekSelfWrite(absPath string) (selfWriteEntry, bool) {
	now := time.Now()

	s.selfWriteMu.Lock()
	defer s.selfWriteMu.Unlock()

	entry, ok := s.selfWrites[absPath]
	if !ok || now.After(entry.expiresAt) {
		return selfWriteEntry{}, false
	}
	return entry, true
}

// selfWriteCount reports how many entries are currently held, for tests that
// assert the map stays within selfWriteCap.
func (s *KBStore) selfWriteCount() int {
	s.selfWriteMu.Lock()
	defer s.selfWriteMu.Unlock()
	return len(s.selfWrites)
}

// sweepSelfWritesLocked removes expired entries. Callers must hold selfWriteMu.
func (s *KBStore) sweepSelfWritesLocked(now time.Time) {
	for path, entry := range s.selfWrites {
		if now.After(entry.expiresAt) {
			delete(s.selfWrites, path)
		}
	}
}

// evictOldestSelfWriteLocked drops the entry closest to expiry, making room
// for a new one when the cap is reached even though nothing has expired yet
// (e.g. a large burst within a single TTL window). Callers must hold
// selfWriteMu.
func (s *KBStore) evictOldestSelfWriteLocked() {
	var oldestPath string
	var oldestExpiry time.Time
	found := false
	for path, entry := range s.selfWrites {
		if !found || entry.expiresAt.Before(oldestExpiry) {
			oldestPath = path
			oldestExpiry = entry.expiresAt
			found = true
		}
	}
	if found {
		delete(s.selfWrites, oldestPath)
	}
}
