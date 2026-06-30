// Package savings implements the append-only token-savings ledger (lean-ctx
// context-intelligence Phase 5). Every token-reduction event — a compressed
// `view` read, an unchanged-re-read F-reference, an RTK shell-output filter — is
// recorded as one JSON line in <data-dir>/savings/ledger.jsonl so the savings are
// measurable via `pando gain`, the `pando_stats` MCP tool and the Token
// Optimization settings widget.
//
// The ledger is strictly observational and fail-safe: recording never blocks the
// agent hot path (entries are handed to a background writer and dropped if its
// buffer is full) and any I/O error is swallowed. It is on by default and turned
// off by TokenOptimization.SavingsLedgerDisabled.
package savings

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// Source identifies which subsystem produced a saving.
type Source string

const (
	// SourceView is a compressed `view` read (signatures/map/auto).
	SourceView Source = "view"
	// SourceDedup is an unchanged-re-read F-reference (or changed-window diff).
	SourceDedup Source = "dedup"
	// SourceBash is an RTK-style shell-output filter application.
	SourceBash Source = "bash"
	// SourceSearch is a compacted code/search result.
	SourceSearch Source = "search"
)

// Entry is one ledger record. Token counts are estimated with a consistent
// chars→tokens heuristic (see TokensFromChars) so cross-source totals are
// comparable.
type Entry struct {
	TS             time.Time `json:"ts"`
	Source         Source    `json:"source"`
	Detail         string    `json:"detail,omitempty"`
	BaselineTokens int       `json:"baseline_tokens"`
	ActualTokens   int       `json:"actual_tokens"`
	Mode           string    `json:"mode,omitempty"`
	Saved          int       `json:"saved"`
}

const (
	// ledgerSubdir / ledgerFile locate the ledger under the data directory.
	ledgerSubdir = "savings"
	ledgerFile   = "ledger.jsonl"
	// maxLedgerBytes bounds the active ledger; on overflow it rotates to a single
	// ".1" backup so disk usage stays bounded.
	maxLedgerBytes = 8 * 1024 * 1024
	// recordBuffer is the depth of the non-blocking record channel.
	recordBuffer = 512
)

// recorder is the process-wide background ledger writer.
type recorder struct {
	mu      sync.Mutex
	started bool
	failed  bool
	ch      chan Entry
	dir     string
	path    string
	written int64
	wg      sync.WaitGroup
}

var def = &recorder{}

// Record appends a saving to the ledger unless the ledger is disabled. It never
// blocks: if the background writer is saturated the entry is dropped. Saved is
// derived from baseline-actual so callers need not set it.
func Record(e Entry) {
	if !config.Get().SavingsLedgerEnabled() {
		return
	}
	r := def
	r.mu.Lock()
	if !r.started && !r.failed {
		r.startLocked()
	}
	ch := r.ch
	failed := r.failed
	r.mu.Unlock()
	if failed || ch == nil {
		return
	}

	if e.TS.IsZero() {
		e.TS = time.Now()
	}
	e.Saved = e.BaselineTokens - e.ActualTokens
	select {
	case ch <- e:
	default:
		// Writer saturated — drop rather than stall the agent loop.
	}
}

// startLocked opens the ledger file and launches the writer goroutine. Must hold
// r.mu. On any setup error it marks the recorder failed so future records no-op.
func (r *recorder) startLocked() {
	dir := config.Get().Data.Directory
	if dir == "" {
		r.failed = true
		return
	}
	sdir := filepath.Join(dir, ledgerSubdir)
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		r.failed = true
		return
	}
	r.dir = sdir
	r.path = filepath.Join(sdir, ledgerFile)
	if fi, err := os.Stat(r.path); err == nil {
		r.written = fi.Size()
	}
	r.ch = make(chan Entry, recordBuffer)
	r.started = true
	r.wg.Add(1)
	go r.run()
}

// run is the background writer loop.
func (r *recorder) run() {
	defer r.wg.Done()
	for e := range r.ch {
		r.writeOne(e)
	}
}

// writeOne marshals and appends a single entry, rotating first if the file has
// grown past the cap. All errors are swallowed (observational subsystem).
func (r *recorder) writeOne(e Entry) {
	if r.written >= maxLedgerBytes {
		r.rotate()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	w := bufio.NewWriter(f)
	n, _ := w.Write(line)
	m, _ := w.WriteString("\n")
	_ = w.Flush()
	_ = f.Close()
	r.written += int64(n + m)
}

// rotate moves the active ledger to a single ".1" backup, bounding disk usage.
func (r *recorder) rotate() {
	_ = os.Rename(r.path, r.path+".1")
	r.written = 0
}

// Close flushes and stops the background writer. Safe to call when never started.
func Close() {
	r := def
	r.mu.Lock()
	ch := r.ch
	started := r.started
	r.ch = nil
	r.started = false
	r.mu.Unlock()
	if started && ch != nil {
		close(ch)
		r.wg.Wait()
	}
}

// TokensFromChars converts a character/byte count into an estimated token count
// using the same ~4-chars-per-token heuristic Pando uses elsewhere, so totals are
// consistent across sources.
func TokensFromChars(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}
