package telemetry

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digiogithub/pando/internal/version"
)

// maxSendAttempts bounds how many times a batch is POSTed on 5xx/network
// errors before it is dropped: one initial try plus retries, exponential
// backoff starting at retryBaseWait.
const (
	maxSendAttempts = 3
	retryBaseWait   = 500 * time.Millisecond
)

var errShipperStopped = errors.New("telemetry: shipper already stopped")

// Stats is a snapshot of Shipper counters, safe to read concurrently with
// ongoing sends.
type Stats struct {
	Sent     uint64
	Dropped  uint64
	Failed   uint64
	Disabled bool
}

// flushRequest asks the worker goroutine to flush the current batch (and, on
// Stop, to also exit afterwards) using ctx for the outgoing HTTP call(s).
type flushRequest struct {
	ctx  context.Context
	done chan struct{}
}

// Shipper batches Records and POSTs them to a Better Stack HTTP Logs source.
// It never blocks the caller: Enqueue drops records once the internal queue
// is full, and every network error is retried a bounded number of times
// before the batch is dropped. A Shipper is safe for concurrent use; all
// batching state lives in a single worker goroutine started by Start.
type Shipper struct {
	opts    Options
	appInfo AppInfo

	debugID atomic.Pointer[string]

	queue    chan Record
	flushReq chan flushRequest
	stopReq  chan flushRequest
	doneCh   chan struct{}
	stopOnce sync.Once

	sent           atomic.Uint64
	dropped        atomic.Uint64
	failed         atomic.Uint64
	disabled       atomic.Bool
	disabledWarned atomic.Bool

	// pendingDropped accumulates Enqueue drops (queue full) since the last
	// record picked them up; it is swapped to 0 as soon as a record consumes
	// it, so only the very next record shipped carries a nonzero Dropped.
	pendingDropped atomic.Uint64
}

// NewShipper builds a Shipper from opts, filling in defaults for any
// zero-valued tuning field. It does not start the background worker — call
// Start for that.
func NewShipper(opts Options) *Shipper {
	opts = opts.withDefaults()
	s := &Shipper{
		opts:     opts,
		queue:    make(chan Record, opts.QueueSize),
		flushReq: make(chan flushRequest),
		stopReq:  make(chan flushRequest, 1),
		doneCh:   make(chan struct{}),
		appInfo: AppInfo{
			Version: version.Normalize(),
			Variant: version.Variant,
			OS:      runtime.GOOS,
			Arch:    runtime.GOARCH,
			Go:      runtime.Version(),
			Mode:    opts.Mode,
		},
	}
	s.SetDebugID(opts.DebugID)
	return s
}

// SetDebugID updates the debug id attached to every record shipped from now
// on (e.g. after the user regenerates it). Safe for concurrent use.
func (s *Shipper) SetDebugID(id string) {
	s.debugID.Store(&id)
}

// DebugID returns the debug id currently attached to outgoing records.
func (s *Shipper) DebugID() string {
	p := s.debugID.Load()
	if p == nil {
		return ""
	}
	return *p
}

// Start launches the background worker that batches and ships records. ctx
// bounds the lifetime of in-flight HTTP calls made from the normal
// batching path (ticker/threshold flushes); Stop/Flush each carry their own
// ctx for their own flush.
func (s *Shipper) Start(ctx context.Context) {
	go s.run(ctx)
}

// Enqueue queues r for shipping. It never blocks: when the internal queue is
// full, r is dropped and counted instead, and the count is attached to the
// next record that is successfully queued (see Record.Dropped).
func (s *Shipper) Enqueue(r Record) {
	select {
	case s.queue <- r:
	default:
		s.dropped.Add(1)
		s.pendingDropped.Add(1)
	}
}

// Enabled reports whether the shipper currently wants records at level: it
// is not disabled (by an ingest rejection or a recovered worker panic — see
// disable/recoverPanic) and level is at or above Options.MinLevel. It
// implements internal/logging.RemoteSink's Enabled method, so the tee
// handler can gate whether a record is even built/forwarded without relying
// solely on Handle's own internal check.
func (s *Shipper) Enabled(level slog.Level) bool {
	if s.disabled.Load() {
		return false
	}
	return level >= s.opts.MinLevel
}

// Handle is a convenience wrapper around NewRecord + Enqueue for slog
// integration: it honors Enabled (MinLevel + disabled) and the SkipAttrKey
// loop guard (matched on the key's last dot-separated segment, so a marker
// prefixed by an open slog group — "g.$_telemetry_skip" — still guards),
// and stamps DebugID/App from the shipper's own state.
func (s *Shipper) Handle(ctx context.Context, t time.Time, level slog.Level, msg string, attrs []slog.Attr) {
	for _, a := range attrs {
		if isSkipAttrKey(a.Key) {
			return
		}
	}
	if !s.Enabled(level) {
		return
	}
	rec := NewRecord(t, level, msg, attrs)
	rec.DebugID = FormatDebugID(s.DebugID())
	rec.App = s.appInfo
	s.Enqueue(rec)
}

// isSkipAttrKey reports whether key is SkipAttrKey, either bare or with a
// slog group prefix baked in ("g.$_telemetry_skip"), which is exactly the
// shape a group-scoped logger (logger.WithGroup("g")) produces.
func isSkipAttrKey(key string) bool {
	return key == SkipAttrKey || strings.HasSuffix(key, "."+SkipAttrKey)
}

// Flush synchronously flushes whatever is currently batched (plus anything
// already queued), bounded by ctx. Used to push out a final record — e.g. a
// panic report — before the process exits.
func (s *Shipper) Flush(ctx context.Context) error {
	done := make(chan struct{})
	req := flushRequest{ctx: ctx, done: done}
	select {
	case s.flushReq <- req:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.doneCh:
		return errShipperStopped
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.doneCh:
		return nil
	}
}

// Stop asks the worker to flush any remaining records (bounded by ctx) and
// exit. It is idempotent: a second call is a no-op returning nil.
func (s *Shipper) Stop(ctx context.Context) error {
	var err error
	s.stopOnce.Do(func() {
		done := make(chan struct{})
		req := flushRequest{ctx: ctx, done: done}
		select {
		case s.stopReq <- req:
		case <-ctx.Done():
			err = ctx.Err()
			return
		case <-s.doneCh:
			return
		}
		select {
		case <-done:
		case <-ctx.Done():
			err = ctx.Err()
		case <-s.doneCh:
		}
	})
	return err
}

// Stats returns a snapshot of the shipper's counters.
func (s *Shipper) Stats() Stats {
	return Stats{
		Sent:     s.sent.Load(),
		Dropped:  s.dropped.Load(),
		Failed:   s.failed.Load(),
		Disabled: s.disabled.Load(),
	}
}

// ── worker ───────────────────────────────────────────────────────────────

func (s *Shipper) run(startCtx context.Context) {
	defer close(s.doneCh)
	defer s.recoverPanic()

	ticker := time.NewTicker(s.opts.FlushInterval)
	defer ticker.Stop()

	batch := make([]Record, 0, s.opts.BatchSize)
	batchBytes := 0

	for {
		select {
		case r := <-s.queue:
			batch, batchBytes = s.appendRecord(startCtx, batch, batchBytes, r)

		case <-ticker.C:
			batch, batchBytes = s.flushBatch(startCtx, batch, batchBytes)

		case req := <-s.flushReq:
			batch, batchBytes = s.drainQueue(req.ctx, batch, batchBytes)
			batch, batchBytes = s.flushBatch(req.ctx, batch, batchBytes)
			close(req.done)

		case req := <-s.stopReq:
			batch, batchBytes = s.drainQueue(req.ctx, batch, batchBytes)
			batch, batchBytes = s.flushBatch(req.ctx, batch, batchBytes)
			close(req.done)
			return
		}
	}
}

func (s *Shipper) recoverPanic() {
	if r := recover(); r != nil {
		// The worker goroutine is gone, so nothing will ever drain s.queue
		// again: mark the shipper disabled (Enabled() now returns false)
		// rather than let every future Enqueue silently pile up until the
		// bounded queue fills and starts dropping forever with no signal.
		s.disabled.Store(true)
		slog.Default().Error("telemetry: worker panic recovered, remote logging disabled",
			"panic", fmt.Sprint(r), SkipAttrKey, true)
	}
}

// drainQueue non-blockingly pulls every record currently sitting in the
// queue into batch, flushing mid-drain if a threshold is crossed.
func (s *Shipper) drainQueue(ctx context.Context, batch []Record, batchBytes int) ([]Record, int) {
	for {
		select {
		case r := <-s.queue:
			batch, batchBytes = s.appendRecord(ctx, batch, batchBytes, r)
		default:
			return batch, batchBytes
		}
	}
}

// appendRecord stamps a pending drop count (if any) onto r, appends it to
// batch, and flushes before or after as needed to respect BatchSize/BatchBytes.
func (s *Shipper) appendRecord(ctx context.Context, batch []Record, batchBytes int, r Record) ([]Record, int) {
	if d := s.pendingDropped.Swap(0); d > 0 {
		r.Dropped = int(d)
	}

	size := estimateSize(r)
	if len(batch) > 0 && (len(batch) >= s.opts.BatchSize || batchBytes+size > s.opts.BatchBytes) {
		batch, batchBytes = s.flushBatch(ctx, batch, batchBytes)
	}

	batch = append(batch, r)
	batchBytes += size

	if len(batch) >= s.opts.BatchSize || batchBytes >= s.opts.BatchBytes {
		batch, batchBytes = s.flushBatch(ctx, batch, batchBytes)
	}
	return batch, batchBytes
}

func (s *Shipper) flushBatch(ctx context.Context, batch []Record, _ int) ([]Record, int) {
	if len(batch) == 0 {
		return batch, 0
	}
	toSend := make([]Record, len(batch))
	copy(toSend, batch)
	s.send(ctx, toSend)
	return batch[:0], 0
}

// ── HTTP ─────────────────────────────────────────────────────────────────

func estimateSize(r Record) int {
	b, err := json.Marshal(r)
	if err != nil {
		return 0
	}
	return len(b)
}

func (s *Shipper) send(ctx context.Context, batch []Record) {
	if len(batch) == 0 {
		return
	}
	if s.disabled.Load() {
		// Drain & drop: the source rejected our credentials/quota, keep
		// counting but never POST again until restart/re-toggle.
		s.dropped.Add(uint64(len(batch)))
		return
	}

	body, err := json.Marshal(batch)
	if err != nil {
		s.failed.Add(uint64(len(batch)))
		return
	}
	s.postWithRetry(ctx, batch, body)
}

func (s *Shipper) postWithRetry(ctx context.Context, batch []Record, body []byte) {
	wait := retryBaseWait
	for attempt := 1; attempt <= maxSendAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				s.failed.Add(uint64(len(batch)))
				return
			}
			wait *= 2
		}

		status, err := s.post(ctx, body)
		if err != nil {
			continue // network error: retry
		}

		switch {
		case status >= 200 && status < 300:
			s.sent.Add(uint64(len(batch)))
			return
		case status == http.StatusPaymentRequired, status == http.StatusForbidden:
			// 402 quota exceeded, 403 invalid token.
			s.disable(status)
			s.dropped.Add(uint64(len(batch)))
			return
		case status == http.StatusNotAcceptable, status == http.StatusRequestEntityTooLarge:
			// 406 invalid payload, 413 too large: split once and stop retrying
			// this shape of failure.
			s.splitAndRetry(ctx, batch)
			return
		case status >= 500:
			continue // retry
		default:
			s.failed.Add(uint64(len(batch)))
			return
		}
	}
	// Exhausted retries on network error / 5xx.
	s.failed.Add(uint64(len(batch)))
}

// disable stops all future sends. It is safe to call repeatedly; only the
// first call emits the local warning, tagged with SkipAttrKey so it is never
// re-shipped (that would just re-trigger the same 402/403).
func (s *Shipper) disable(status int) {
	s.disabled.Store(true)
	if !s.disabledWarned.CompareAndSwap(false, true) {
		return
	}
	reason := "invalid token"
	if status == http.StatusPaymentRequired {
		reason = "quota exceeded"
	}
	slog.Default().Warn("telemetry: remote logging disabled by ingest server, no further records will be sent",
		"status", status, "reason", reason, SkipAttrKey, true)
}

// splitAndRetry halves batch once and sends each half independently. A half
// that still fails is dropped rather than split further, so a single bad
// batch can never spiral into unbounded retries.
func (s *Shipper) splitAndRetry(ctx context.Context, batch []Record) {
	if len(batch) <= 1 {
		s.dropped.Add(uint64(len(batch)))
		return
	}
	mid := len(batch) / 2
	for _, half := range [][]Record{batch[:mid], batch[mid:]} {
		body, err := json.Marshal(half)
		if err != nil {
			s.failed.Add(uint64(len(half)))
			continue
		}
		s.sendHalf(ctx, half, body)
	}
}

func (s *Shipper) sendHalf(ctx context.Context, half []Record, body []byte) {
	status, err := s.post(ctx, body)
	if err != nil {
		s.failed.Add(uint64(len(half)))
		return
	}
	switch {
	case status >= 200 && status < 300:
		s.sent.Add(uint64(len(half)))
	case status == http.StatusPaymentRequired, status == http.StatusForbidden:
		s.disable(status)
		s.dropped.Add(uint64(len(half)))
	default:
		s.dropped.Add(uint64(len(half)))
	}
}

// post performs a single POST attempt and returns the response status code.
// A non-nil error means the request never got a response (network error,
// timeout, ctx cancellation) and should be treated as retryable.
func (s *Shipper) post(ctx context.Context, body []byte) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, s.opts.HTTPTimeout)
	defer cancel()

	var reader io.Reader = bytes.NewReader(body)
	encoding := ""
	if s.opts.Gzip {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		if _, err := gw.Write(body); err != nil {
			return 0, err
		}
		if err := gw.Close(); err != nil {
			return 0, err
		}
		reader = &buf
		encoding = "gzip"
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, s.opts.Endpoint, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.opts.Token)
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}

	resp, err := s.opts.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
