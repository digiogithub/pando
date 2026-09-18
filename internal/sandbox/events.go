package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/redact"
)

// EventType names one of the observability events the sandbox emits. Every
// wrap site, and bash.go's denial/escalation handling, funnels through Emit
// with one of these (PANDO-US-0048).
type EventType string

const (
	// EventApplied: a command was actually spawned confined — the policy
	// covered the purpose and the backend enforced it. Emitted once per
	// long-lived spawn (e.g. the persistent shell), not per command.
	EventApplied EventType = "sandbox.applied"
	// EventUnavailable: the policy asks for confinement but the backend
	// cannot enforce it here (unsupported OS, missing kernel feature, backend
	// not implemented yet, ...). Emitted at most once per process — see Emit.
	EventUnavailable EventType = "sandbox.unavailable"
	// EventDenied: a confined command failed in a way sandbox.Classify
	// attributes to the policy, not the command itself.
	EventDenied EventType = "sandbox.denied"
	// EventEscalationRequested is the bash tool's sandbox_permissions
	// "require_escalated" path asking the user (or auto-escalation) to run a
	// command once outside the sandbox.
	EventEscalationRequested EventType = "sandbox.escalation.requested"
	// EventEscalationGranted / EventEscalationDenied are the outcome of an
	// EventEscalationRequested.
	EventEscalationGranted EventType = "sandbox.escalation.granted"
	EventEscalationDenied  EventType = "sandbox.escalation.denied"
)

// maxEventCommand bounds Event.Command after redaction, so a pathological
// one-line command can never bloat the ring buffer, the JSONL file or a
// shipped telemetry record.
const maxEventCommand = 400

// Event is one observability record: an applied/unavailable/denied/
// escalation moment in the sandbox's life. It is kept for the in-memory ring
// buffer (RecentEvents), appended to the local JSONL log, logged through
// slog (so it shows on the TUI/WebUI logs page and — when opt-in remote
// telemetry is enabled — is forwarded to Better Stack the same way every
// other log record is, via internal/logging's tee handler), and counted for
// the pando_stats tool.
type Event struct {
	Time      time.Time `json:"time"`
	Type      EventType `json:"type"`
	SessionID string    `json:"sessionId,omitempty"`
	Backend   string    `json:"backend,omitempty"`
	Mode      string    `json:"mode,omitempty"`
	// Kind/Op/Path describe a denial (see Denial); empty for other event
	// types.
	Kind string `json:"kind,omitempty"`
	Op   string `json:"op,omitempty"`
	Path string `json:"path,omitempty"`
	// Command is the offending or escalated command line. Callers pass the
	// raw command; Emit redacts (internal/redact.String/Path) and truncates
	// it before it reaches the ring buffer, the JSONL file, the log line or
	// telemetry — a caller must never pre-redact it (that would double up
	// escaping and could hide the "[REDACTED]" markers themselves).
	Command string `json:"command,omitempty"`
	// Reason explains an unavailable backend (Capability.Reason) or an
	// escalation's justification.
	Reason string `json:"reason,omitempty"`
	// AutoAllowed is set on an EventEscalationRequested: whether the policy
	// allows this escalation to be granted without an explicit approval.
	AutoAllowed bool `json:"autoAllowed,omitempty"`
}

// unavailableEmitted guards EventUnavailable so it is only ever recorded
// once per process, however many spawn sites hit the same degraded backend.
var unavailableEmitted atomic.Bool

// Emit records e: a slog Info line, the in-memory ring buffer, the local
// JSONL log, and the pando_stats counters. It never blocks the caller (the
// JSONL write is handed to a background writer) and never fails visibly:
// this is a purely observational subsystem.
//
// An EventUnavailable is recorded at most once per process; later calls are
// no-ops so a backend that stays unavailable for the whole run does not spam
// the log or counters on every spawn.
func Emit(_ context.Context, e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Command = redactCommand(e.Command)

	if e.Type == EventUnavailable && !unavailableEmitted.CompareAndSwap(false, true) {
		return
	}

	logEvent(e)
	recordEvent(e)
	countEvent(e.Type)
	writeEventLine(e)
}

// EmitSpawn records the outcome of wrapping one spawn for purpose at a wrap
// site other than the persistent shell (ACP terminals, skills, MCP stdio,
// sub-agents): an EventApplied, with the purpose as Reason, when covered is
// true and p is enabled and c enforced; an EventUnavailable (once per
// process) when covered but the backend cannot enforce; nothing when the
// spawn was not covered. It carries no command line.
func EmitSpawn(ctx context.Context, purpose Purpose, covered bool, p Policy, c Capability) {
	if !covered || !p.Enabled() {
		return
	}
	e := Event{Backend: c.Backend, Mode: string(p.Mode), Reason: "purpose=" + string(purpose)}
	if c.Enforced {
		e.Type = EventApplied
	} else {
		e.Type = EventUnavailable
		e.Reason = c.Reason
	}
	Emit(ctx, e)
}

// redactCommand scrubs and truncates a raw command line so it is safe to
// log, persist and ship: known secret patterns and the user's home
// directory are removed the same way internal/telemetry redacts a log
// record (internal/redact.String then Path), then the result is capped to
// maxEventCommand.
func redactCommand(command string) string {
	if command == "" {
		return ""
	}
	return redact.Truncate(redact.Path(redact.String(command)), maxEventCommand)
}

// logEvent emits e through the normal logging pipeline: slog Info with
// session_id/backend (and whichever other fields are set), tagged with the
// caller-visible message being the event type itself (e.g. "sandbox.denied")
// exactly as bash.go logged it before this package existed.
func logEvent(e Event) {
	args := make([]any, 0, 16)
	args = append(args, "session_id", e.SessionID, "backend", e.Backend)
	if e.Mode != "" {
		args = append(args, "mode", e.Mode)
	}
	if e.Kind != "" {
		args = append(args, "kind", e.Kind)
	}
	if e.Op != "" {
		args = append(args, "op", e.Op)
	}
	if e.Path != "" {
		args = append(args, "path", e.Path)
	}
	if e.Command != "" {
		args = append(args, "command", e.Command)
	}
	if e.Reason != "" {
		args = append(args, "reason", e.Reason)
	}
	if e.Type == EventEscalationRequested {
		args = append(args, "auto_allowed", e.AutoAllowed)
	}
	logging.Info(string(e.Type), args...)
}

// ── ring buffer ──────────────────────────────────────────────────────────

// maxRingEvents bounds the in-memory event history RecentEvents returns.
const maxRingEvents = 200

var (
	ringMu sync.Mutex
	ring   []Event
)

// recordEvent appends e to the ring buffer, dropping the oldest entry once
// the buffer is full.
func recordEvent(e Event) {
	ringMu.Lock()
	defer ringMu.Unlock()
	ring = append(ring, e)
	if len(ring) > maxRingEvents {
		ring = ring[len(ring)-maxRingEvents:]
	}
}

// RecentEvents returns a copy of the last (up to 200) sandbox events, oldest
// first. Safe for concurrent use; used by CLI/status/debug surfaces.
func RecentEvents() []Event {
	ringMu.Lock()
	defer ringMu.Unlock()
	out := make([]Event, len(ring))
	copy(out, ring)
	return out
}

// ── counters ─────────────────────────────────────────────────────────────

var (
	counterApplied             atomic.Uint64
	counterDenied              atomic.Uint64
	counterEscalationRequested atomic.Uint64
	counterEscalationGranted   atomic.Uint64
	counterEscalationDenied    atomic.Uint64
	counterUnavailable         atomic.Uint64
)

// Counters is a snapshot of the process-wide sandbox event counts, exposed
// through the pando_stats tool.
type Counters struct {
	Applied             uint64 `json:"applied"`
	Denied              uint64 `json:"denied"`
	EscalationRequested uint64 `json:"escalationRequested"`
	EscalationGranted   uint64 `json:"escalationGranted"`
	EscalationDenied    uint64 `json:"escalationDenied"`
	Unavailable         uint64 `json:"unavailable"`
}

func countEvent(t EventType) {
	switch t {
	case EventApplied:
		counterApplied.Add(1)
	case EventDenied:
		counterDenied.Add(1)
	case EventEscalationRequested:
		counterEscalationRequested.Add(1)
	case EventEscalationGranted:
		counterEscalationGranted.Add(1)
	case EventEscalationDenied:
		counterEscalationDenied.Add(1)
	case EventUnavailable:
		counterUnavailable.Add(1)
	}
}

// EventCounters returns a snapshot of the sandbox event counters.
func EventCounters() Counters {
	return Counters{
		Applied:             counterApplied.Load(),
		Denied:              counterDenied.Load(),
		EscalationRequested: counterEscalationRequested.Load(),
		EscalationGranted:   counterEscalationGranted.Load(),
		EscalationDenied:    counterEscalationDenied.Load(),
		Unavailable:         counterUnavailable.Load(),
	}
}

// ── JSONL log ────────────────────────────────────────────────────────────

const (
	// eventsFileName is the append-only event log, under the data directory
	// (config.Data.Directory, typically .pando/data), written by Pando only.
	eventsFileName = "sandbox-events.jsonl"
	// maxEventsFileBytes bounds the active log; on overflow it rotates to a
	// single ".1" backup so disk usage stays bounded.
	maxEventsFileBytes = 5 * 1024 * 1024
	// eventsRecordBuffer is the depth of the non-blocking record channel.
	eventsRecordBuffer = 256
)

// eventWriter is the process-wide background JSONL writer, mirroring
// internal/savings' ledger recorder: never blocks the caller, swallows every
// I/O error (this is a purely observational subsystem), and rotates once the
// file crosses maxEventsFileBytes.
type eventWriter struct {
	mu      sync.Mutex
	started bool
	failed  bool
	ch      chan Event
	path    string
	written int64
	wg      sync.WaitGroup
}

var defaultEventWriter = &eventWriter{}

// writeEventLine hands e to the background writer, starting it on first use.
// It never blocks: a saturated writer or a missing data directory silently
// drops the line rather than stall the caller (bash.go's hot path).
func writeEventLine(e Event) {
	w := defaultEventWriter
	w.mu.Lock()
	if !w.started && !w.failed {
		w.startLocked()
	}
	ch := w.ch
	failed := w.failed
	w.mu.Unlock()
	if failed || ch == nil {
		return
	}
	select {
	case ch <- e:
	default:
		// Writer saturated — drop rather than block the caller.
	}
}

// startLocked opens the events file and launches the writer goroutine. Must
// hold w.mu. On any setup error it marks the writer failed so future lines
// no-op until ResetEventsForTests (or a process restart) clears it.
func (w *eventWriter) startLocked() {
	dir := ""
	if cfg := config.Get(); cfg != nil {
		dir = cfg.Data.Directory
	}
	if dir == "" {
		w.failed = true
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		w.failed = true
		return
	}
	w.path = filepath.Join(dir, eventsFileName)
	if fi, err := os.Stat(w.path); err == nil {
		w.written = fi.Size()
	}
	ch := make(chan Event, eventsRecordBuffer)
	w.ch = ch
	w.started = true
	w.wg.Add(1)
	// ch is passed explicitly (rather than read back from w.ch inside run)
	// so a CloseEventWriter racing with the goroutine's own startup can never
	// make it range over a field that was concurrently nilled out — which
	// would range over a nil channel and block forever.
	go w.run(ch)
}

// run is the background writer loop.
func (w *eventWriter) run(ch chan Event) {
	defer w.wg.Done()
	for e := range ch {
		w.writeOne(e)
	}
}

// writeOne marshals and appends a single event, rotating first if the file
// has grown past the cap. All errors are swallowed.
func (w *eventWriter) writeOne(e Event) {
	if w.written >= maxEventsFileBytes {
		w.rotate()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	bw := bufio.NewWriter(f)
	n, _ := bw.Write(line)
	m, _ := bw.WriteString("\n")
	_ = bw.Flush()
	_ = f.Close()
	w.written += int64(n + m)
}

// rotate moves the active log to a single ".1" backup, bounding disk usage.
func (w *eventWriter) rotate() {
	_ = os.Rename(w.path, w.path+".1")
	w.written = 0
}

// CloseEventWriter flushes and stops the background JSONL writer. Safe to
// call when it was never started. Callers that need every queued event
// durably written before reading the file back (e.g. tests) should call this
// first.
func CloseEventWriter() {
	w := defaultEventWriter
	w.mu.Lock()
	ch := w.ch
	started := w.started
	w.ch = nil
	w.started = false
	w.mu.Unlock()
	if started && ch != nil {
		close(ch)
		w.wg.Wait()
	}
}

// ResetEventsForTests clears every piece of process-wide event state: the
// ring buffer, the counters, the EventUnavailable once-guard and the JSONL
// writer. For tests only.
func ResetEventsForTests() {
	CloseEventWriter()

	ringMu.Lock()
	ring = nil
	ringMu.Unlock()

	counterApplied.Store(0)
	counterDenied.Store(0)
	counterEscalationRequested.Store(0)
	counterEscalationGranted.Store(0)
	counterEscalationDenied.Store(0)
	counterUnavailable.Store(0)
	unavailableEmitted.Store(false)

	defaultEventWriter.mu.Lock()
	defaultEventWriter.failed = false
	defaultEventWriter.path = ""
	defaultEventWriter.written = 0
	defaultEventWriter.mu.Unlock()
}
