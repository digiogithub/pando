package telemetry

import (
	"log/slog"
	"net/http"
	"time"
)

// Default tuning knobs used by Options.withDefaults when the caller leaves a
// field at its zero value. Matches the plan's "bounded channel (1000)...
// every 5s or at 100 records / 1 MiB" sizing.
const (
	DefaultQueueSize     = 1000
	DefaultBatchSize     = 100
	DefaultBatchBytes    = 1 << 20 // 1 MiB
	DefaultFlushInterval = 5 * time.Second
	DefaultHTTPTimeout   = 10 * time.Second
)

// Options configures a Shipper. Endpoint/Token/DebugID/Mode are plain
// pass-through values the caller resolves beforehand (Endpoint/Token
// typically from Token()/Endpoint() in build.go, DebugID/Mode from the
// user's config and the running surface); everything else tunes the
// shipper's own batching and retry behavior.
type Options struct {
	// Endpoint is the full ingest URL POSTed to, e.g. "https://<host>".
	Endpoint string
	// Token is the Better Stack source token, sent as
	// "Authorization: Bearer <Token>".
	Token string
	// DebugID is the anonymous identifier attached to every record. It can be
	// changed live with Shipper.SetDebugID (e.g. after "Regenerate ID").
	DebugID string
	// Mode identifies the running surface: tui|serve|desktop|acp|cli.
	Mode string

	// MinLevel is the minimum slog level shipped; records below it are
	// dropped before they ever reach the queue.
	MinLevel slog.Level

	// QueueSize bounds the number of records buffered between Enqueue and the
	// worker goroutine. Enqueue never blocks: once full, new records are
	// dropped and counted instead of waiting for room.
	QueueSize int
	// BatchSize is the max number of records per HTTP POST.
	BatchSize int
	// BatchBytes is the max marshaled size (bytes) per HTTP POST.
	BatchBytes int

	// FlushInterval is how often a partial batch is flushed even when it has
	// not reached BatchSize/BatchBytes yet.
	FlushInterval time.Duration
	// HTTPTimeout bounds a single POST attempt.
	HTTPTimeout time.Duration

	// HTTPClient overrides the client used to POST batches. Mainly for
	// tests; nil means a client with Timeout: HTTPTimeout is created.
	HTTPClient *http.Client

	// Gzip enables gzip-compressing the request body
	// ("Content-Encoding: gzip"). Off by default until confirmed accepted by
	// the ingest endpoint (see the plan's Phase 2 note).
	Gzip bool
}

// withDefaults returns a copy of o with zero-value tuning fields replaced by
// the package defaults. Endpoint/Token/DebugID/Mode are left as given, even
// when empty — an empty Endpoint/Token simply means every send fails, which
// is the caller's (Init's) responsibility to avoid by checking Available().
func (o Options) withDefaults() Options {
	if o.QueueSize <= 0 {
		o.QueueSize = DefaultQueueSize
	}
	if o.BatchSize <= 0 {
		o.BatchSize = DefaultBatchSize
	}
	if o.BatchBytes <= 0 {
		o.BatchBytes = DefaultBatchBytes
	}
	if o.FlushInterval <= 0 {
		o.FlushInterval = DefaultFlushInterval
	}
	if o.HTTPTimeout <= 0 {
		o.HTTPTimeout = DefaultHTTPTimeout
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: o.HTTPTimeout}
	}
	return o
}
