package readmode

import (
	"path/filepath"
	"strings"
	"sync"
)

// BounceTracker makes the `auto` read mode safe to enable by default by learning,
// within a session, when compression backfires. A "bounce" is a compressed read
// (signatures/map) of a path followed by a full read of the same path — a strong
// signal the agent needed the raw bytes anyway. High-bounce paths and extensions
// get their next `auto` read escalated toward full (signatures → map → full), and
// the bytes spent on the bounced compressed read are tallied as waste so the
// Phase-5 savings ledger can subtract them from reported savings.
//
// The tracker is deterministic by default: escalation depends only on the count
// of observed hard bounces. When learning is enabled (TokenOptimization
// .ReadModeLearning) a per-extension Beta(α,β) posterior over the bounce rate can
// add one extra escalation step before enough hard bounces have accumulated.
//
// All methods are safe for concurrent use.
type BounceTracker struct {
	mu sync.Mutex
	// pending maps a normalized path to the last compressed read still awaiting a
	// possible bounce. Cleared once the bounce is observed or the path is read
	// compressed again.
	pending map[string]pendingRead
	// pathBounces / extBounces count observed bounces per path and per extension.
	pathBounces map[string]int
	extBounces  map[string]int
	// beta holds a per-extension Beta(α,β) over "compression held" (α) vs "bounced"
	// (β), used only for the optional learning escalation.
	beta map[string]*betaCounter
	// wastedBytes accumulates the rendered size of compressed reads that bounced.
	wastedBytes int64
	// bounces is the total observed bounce count.
	bounces int
}

type pendingRead struct {
	mode  Mode
	bytes int
}

type betaCounter struct{ alpha, beta float64 }

// learningEscalateThreshold is the posterior-mean bounce rate at or above which
// the learning layer adds an escalation step before any hard bounce is recorded.
const learningEscalateThreshold = 0.5

// minLearningObservations is how many compressed reads of an extension must be
// seen before the learning escalation trusts its posterior.
const minLearningObservations = 3

// NewBounceTracker returns an empty tracker.
func NewBounceTracker() *BounceTracker {
	return &BounceTracker{
		pending:     make(map[string]pendingRead),
		pathBounces: make(map[string]int),
		extBounces:  make(map[string]int),
		beta:        make(map[string]*betaCounter),
	}
}

// RecordCompressed notes that a compressed read (signatures/map) of path was just
// delivered, of rendered size compressedBytes. It arms a pending bounce for the
// path and tentatively credits the extension's posterior toward "compression
// held"; a following full read of the same path reverts that credit into a bounce.
func (t *BounceTracker) RecordCompressed(path string, mode Mode, compressedBytes int) {
	if t == nil || mode == ModeFull || mode == ModeAuto {
		return
	}
	key := normPath(path)
	ext := extOf(path)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending[key] = pendingRead{mode: mode, bytes: compressedBytes}
	b := t.betaForLocked(ext)
	b.alpha++
}

// RecordFull notes that a full read of path was just delivered. If a compressed
// read of the same path was pending it counts as a bounce: per-path and
// per-extension counters increment, the wasted compressed bytes are tallied, and
// the extension's posterior shifts toward "bounced". Returns true on a bounce.
func (t *BounceTracker) RecordFull(path string) bool {
	if t == nil {
		return false
	}
	key := normPath(path)
	ext := extOf(path)

	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.pending[key]
	if !ok {
		return false
	}
	delete(t.pending, key)
	t.pathBounces[key]++
	t.extBounces[ext]++
	t.bounces++
	t.wastedBytes += int64(p.bytes)

	b := t.betaForLocked(ext)
	if b.alpha > 0 {
		b.alpha-- // revert the tentative credit from RecordCompressed
	}
	b.beta++
	return true
}

// Upgrade returns the mode to actually use for an `auto`-resolved read of path,
// escalating away from aggressive compression when this path/extension has a
// bounce history this session. The escalation order is signatures → map → full;
// each observed bounce (max of path/extension counts) advances one step. When
// learning is true a high per-extension posterior bounce rate may add one step.
// base must be a concrete compressed mode (signatures/map); ModeFull/ModeAuto are
// returned unchanged.
func (t *BounceTracker) Upgrade(path string, base Mode, learning bool) Mode {
	if t == nil || base == ModeFull || base == ModeAuto {
		return base
	}
	key := normPath(path)
	ext := extOf(path)

	t.mu.Lock()
	defer t.mu.Unlock()
	steps := t.pathBounces[key]
	if e := t.extBounces[ext]; e > steps {
		steps = e
	}
	if learning && steps == 0 {
		if b, ok := t.beta[ext]; ok {
			obs := b.alpha + b.beta
			if obs >= minLearningObservations && b.beta/obs >= learningEscalateThreshold {
				steps = 1
			}
		}
	}
	return escalate(base, steps)
}

// BounceStats is a read-only snapshot of the tracker for reporting.
type BounceStats struct {
	Bounces     int   `json:"bounces"`
	WastedBytes int64 `json:"wasted_bytes"`
	HotPaths    int   `json:"hot_paths"`
	HotExts     int   `json:"hot_exts"`
}

// Stats returns a snapshot of the tracker counters.
func (t *BounceTracker) Stats() BounceStats {
	if t == nil {
		return BounceStats{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return BounceStats{
		Bounces:     t.bounces,
		WastedBytes: t.wastedBytes,
		HotPaths:    len(t.pathBounces),
		HotExts:     len(t.extBounces),
	}
}

// Reset clears all tracker state (used when a session cache is cleared).
func (t *BounceTracker) Reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending = make(map[string]pendingRead)
	t.pathBounces = make(map[string]int)
	t.extBounces = make(map[string]int)
	t.beta = make(map[string]*betaCounter)
	t.wastedBytes = 0
	t.bounces = 0
}

// betaForLocked returns (creating if needed) the Beta counter for an extension.
// Must be called with t.mu held.
func (t *BounceTracker) betaForLocked(ext string) *betaCounter {
	if t.beta == nil {
		t.beta = make(map[string]*betaCounter)
	}
	b, ok := t.beta[ext]
	if !ok {
		b = &betaCounter{}
		t.beta[ext] = b
	}
	return b
}

// escalate advances base toward full by the given number of steps along the
// signatures → map → full ladder, clamping at full.
func escalate(base Mode, steps int) Mode {
	ladder := []Mode{ModeSignatures, ModeMap, ModeFull}
	idx := -1
	for i, m := range ladder {
		if m == base {
			idx = i
			break
		}
	}
	if idx < 0 {
		return base
	}
	idx += steps
	if idx >= len(ladder) {
		idx = len(ladder) - 1
	}
	return ladder[idx]
}

// normPath canonicalizes a path for keying so different spellings of the same
// file share bounce counters.
func normPath(path string) string {
	return filepath.Clean(path)
}

// extOf returns the lower-cased file extension (including the dot), or "<none>"
// for extension-less files.
func extOf(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return "<none>"
	}
	return ext
}
