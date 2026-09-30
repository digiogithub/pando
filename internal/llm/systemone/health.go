package systemone

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// DefaultHealthTTL is the health cache lifetime.
const DefaultHealthTTL = 30 * time.Second

// HealthCache memoises HealthReports per (kind, baseURL, key fingerprint,
// model) so that status polling does not hammer the backend.
type HealthCache struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]healthEntry
}

type healthEntry struct {
	report HealthReport
	at     time.Time
}

// NewHealthCache builds a cache; ttl <= 0 uses DefaultHealthTTL.
func NewHealthCache(ttl time.Duration) *HealthCache {
	if ttl <= 0 {
		ttl = DefaultHealthTTL
	}
	return &HealthCache{ttl: ttl, now: time.Now, entries: map[string]healthEntry{}}
}

func healthKey(p DecisionProvider, model string) string {
	c := p.Client()
	sum := sha256.Sum256([]byte(c.opts.APIKey))
	return string(p.Kind()) + "|" + c.BaseURL() + "|" + hex.EncodeToString(sum[:4]) + "|" + model
}

// Get returns a cached report or probes the provider.
func (h *HealthCache) Get(ctx context.Context, p DecisionProvider, model string) HealthReport {
	key := healthKey(p, model)
	h.mu.Lock()
	if e, ok := h.entries[key]; ok && h.now().Sub(e.at) < h.ttl {
		h.mu.Unlock()
		return e.report
	}
	h.mu.Unlock()

	rep := p.Health(ctx, model)
	h.mu.Lock()
	h.entries[key] = healthEntry{report: rep, at: h.now()}
	h.mu.Unlock()
	return rep
}

// Invalidate drops every cached report (call on config change).
func (h *HealthCache) Invalidate() {
	h.mu.Lock()
	h.entries = map[string]healthEntry{}
	h.mu.Unlock()
}

// Warmup loads the router model into memory. For Ollama it sends one tiny
// request carrying keep_alive so the model stays resident; remote providers
// need no warm-up and this is a no-op.
func Warmup(ctx context.Context, p DecisionProvider, model string) error {
	if p.Kind() != KindOllama {
		return nil
	}
	ka := p.Client().KeepAlive()
	if ka == "" {
		ka = defaultOllamaKeepAlive
	}
	_, err := p.Client().Decide(ctx, probeRequest(model, ka))
	return err
}
