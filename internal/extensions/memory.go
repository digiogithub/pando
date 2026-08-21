package extensions

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/rag/kb"
	"github.com/digiogithub/pando/pkg/extension"
)

// Host side of the memory capability: it turns the knowledge base's plain
// write events into extension.MemoryEvent, enforces the opt-in gate before
// anything leaves core, and chains remembrance search wrappers.
//
// The gate lives here, not in the extension, for the obvious reason: an
// extension that decides its own permission to exfiltrate has no permission at
// all. Core answers three questions before a sink sees an event — is the
// capability on, is this scope shared, is this origin shared — and a "no" to
// any of them means the event is dropped and counted, never delivered.

// MemoryPublisher fans committed remembrance writes out to the MemorySinks in
// a manager. The zero value is not usable; build one with NewMemoryPublisher.
type MemoryPublisher struct {
	sinks []extension.MemorySink
	cfg   config.ExtensionsMemoryConfig

	scopes  []string
	paths   []string
	origins map[string]struct{}

	attribution func() Attribution

	queue chan extension.MemoryEvent
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once

	published atomic.Int64
	filtered  atomic.Int64
	dropped   atomic.Int64
	failed    atomic.Int64
}

// Attribution identifies the instance a memory event came from. It is
// attribution only: isolation belongs to whatever store the sink talks to.
type Attribution struct {
	ProjectID  string
	UserID     string
	InstanceID string
}

// HasMemorySinks reports whether any loaded extension observes memory writes.
func HasMemorySinks(mgr *extension.Manager) bool {
	if mgr == nil {
		return false
	}
	return len(extension.Capability[extension.MemorySink](mgr)) > 0
}

// HasMemoryExtensions reports whether anything in mgr uses the memory
// capability, as either a sink or a search wrapper.
func HasMemoryExtensions(mgr *extension.Manager) bool {
	if mgr == nil {
		return false
	}
	return HasMemorySinks(mgr) ||
		len(extension.Capability[extension.RemembranceSearchWrapper](mgr)) > 0
}

// NewMemoryPublisher builds a publisher for the sinks in mgr. It returns nil
// when the capability is switched off, when nothing implements MemorySink, or
// when the configuration enables the capability without naming a single scope
// — an empty scope list shares nothing, and saying so out loud beats silently
// publishing everything or silently publishing nothing.
// attr is called per event rather than captured once, because the instance ID
// is only assigned after the IPC lock is taken, which happens later in startup
// than this wiring does.
func NewMemoryPublisher(mgr *extension.Manager, cfg config.ExtensionsMemoryConfig, attr func() Attribution) *MemoryPublisher {
	if mgr == nil || !cfg.Enabled {
		return nil
	}
	if attr == nil {
		attr = func() Attribution { return Attribution{} }
	}
	sinks := extension.Capability[extension.MemorySink](mgr)
	if len(sinks) == 0 {
		return nil
	}
	scopes := normaliseList(cfg.Scopes)
	if len(scopes) == 0 {
		logging.Warn("Extension memory capability is enabled but no scope is shared; no memory event will be published",
			"sinks", len(sinks), "hint", "set [Extensions.Memory] Scopes")
		return nil
	}

	p := &MemoryPublisher{
		sinks:       sinks,
		cfg:         cfg,
		scopes:      scopes,
		paths:       normaliseList(cfg.Paths),
		origins:     originSet(cfg.Origins),
		attribution: attr,
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}

	ids := make([]string, 0, len(sinks))
	for _, s := range sinks {
		ids = append(ids, string(s.ExtensionInfo().ID))
	}
	logging.Info("Extension memory capability active — remembrance writes leave this machine",
		"sinks", strings.Join(ids, ","),
		"scopes", strings.Join(scopes, ","),
		"mode", modeName(cfg),
		"dryRun", cfg.DryRun)

	if !cfg.Synchronous() {
		p.queue = make(chan extension.MemoryEvent, cfg.MemoryQueueSize())
		go p.run()
	} else {
		close(p.done)
	}
	return p
}

func modeName(cfg config.ExtensionsMemoryConfig) string {
	if cfg.Synchronous() {
		return "sync"
	}
	return "async"
}

// Observer returns the kb.WriteObserver to install on the store.
func (p *MemoryPublisher) Observer() kb.WriteObserver {
	if p == nil {
		return nil
	}
	return p.onWrite
}

// Close stops the async worker and waits for the queue to drain. Safe to call
// on a nil publisher and safe to call twice.
func (p *MemoryPublisher) Close() {
	if p == nil {
		return
	}
	p.once.Do(func() { close(p.stop) })
	<-p.done
}

// onWrite converts and gates one committed write.
func (p *MemoryPublisher) onWrite(ctx context.Context, ev kb.WriteEvent) {
	if p == nil {
		return
	}
	me := p.convert(ev)
	if !p.allowed(me) {
		p.filtered.Add(1)
		return
	}
	if p.cfg.Synchronous() {
		p.deliver(ctx, me)
		return
	}
	select {
	case p.queue <- me:
	default:
		// Bounded on purpose: a sink that cannot keep up must cost events, not
		// the host's memory. A sink that must not lose events spools its own.
		p.dropped.Add(1)
		logging.Warn("Extension memory event dropped, sink queue full",
			"path", me.Path, "scope", me.Scope, "dropped", p.dropped.Load())
	}
}

func (p *MemoryPublisher) convert(ev kb.WriteEvent) extension.MemoryEvent {
	kind := extension.KindDocument
	if ev.Kind == kb.WriteKindMemory {
		kind = extension.KindMemory
	}
	var op extension.MemoryOp
	switch ev.Op {
	case kb.WriteCreated:
		op = extension.MemoryCreated
	case kb.WriteDeleted:
		op = extension.MemoryDeleted
	default:
		op = extension.MemoryUpdated
	}
	attr := p.attribution()
	return extension.MemoryEvent{
		Kind:       kind,
		Op:         op,
		Scope:      ev.Scope,
		Key:        ev.Key,
		Path:       ev.FilePath,
		Content:    ev.Content,
		Tags:       ev.Tags,
		Metadata:   ev.Metadata,
		ProjectID:  attr.ProjectID,
		UserID:     attr.UserID,
		InstanceID: attr.InstanceID,
		Origin:     extension.MemoryOrigin(ev.Origin),
		Timestamp:  ev.Timestamp,
		DryRun:     p.cfg.DryRun,
	}
}

// allowed applies the opt-in gate. Every branch defaults to "no".
func (p *MemoryPublisher) allowed(ev extension.MemoryEvent) bool {
	// A write that came from a remote store is never pushed back out: that is
	// how two stores start echoing each other forever.
	if ev.Origin == extension.OriginRemote {
		return false
	}
	if len(p.origins) > 0 {
		if _, ok := p.origins[string(ev.Origin)]; !ok {
			return false
		}
	}
	if !matchesPrefix(ev.Scope, p.scopes) {
		return false
	}
	if len(p.paths) > 0 && !matchesPrefix(ev.Path, p.paths) {
		return false
	}
	return true
}

// matchesPrefix reports whether value starts with any of the prefixes. An
// empty prefix in the list matches the empty value only — a scopeless document
// must be shared deliberately, not as a side effect of listing "project/".
func matchesPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix == "" {
			if value == "" {
				return true
			}
			continue
		}
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func (p *MemoryPublisher) run() {
	defer close(p.done)
	for {
		select {
		case <-p.stop:
			// Drain what is already queued, then leave. The events are in
			// memory; dropping them on shutdown would lose exactly the writes
			// made just before the user quit.
			for {
				select {
				case ev := <-p.queue:
					p.deliver(context.Background(), ev)
				default:
					return
				}
			}
		case ev := <-p.queue:
			p.deliver(context.Background(), ev)
		}
	}
}

// deliver hands one event to every sink, each with its own timeout and its own
// panic containment: one broken sink must not cost the others their events.
func (p *MemoryPublisher) deliver(ctx context.Context, ev extension.MemoryEvent) {
	for _, sink := range p.sinks {
		p.deliverOne(ctx, sink, ev)
	}
	p.published.Add(1)
}

func (p *MemoryPublisher) deliverOne(ctx context.Context, sink extension.MemorySink, ev extension.MemoryEvent) {
	defer func() {
		if r := recover(); r != nil {
			p.failed.Add(1)
			logging.Error("Extension memory sink panicked",
				"extension", sink.ExtensionInfo().ID, "path", ev.Path, "panic", r)
		}
	}()

	callCtx, cancel := context.WithTimeout(ctx, p.cfg.MemoryTimeout())
	defer cancel()

	if err := sink.OnMemoryWrite(callCtx, ev); err != nil {
		p.failed.Add(1)
		logging.Warn("Extension memory sink failed",
			"extension", sink.ExtensionInfo().ID, "path", ev.Path, "error", err)
	}
}

// PublisherStats are the host's own counters, independent of what each sink
// reports about itself.
type PublisherStats struct {
	Sinks     int   `json:"sinks"`
	Published int64 `json:"published"`
	Filtered  int64 `json:"filtered"`
	Dropped   int64 `json:"dropped"`
	Failed    int64 `json:"failed"`
	Queued    int   `json:"queued"`
}

// Stats returns the host counters. Safe on a nil publisher.
func (p *MemoryPublisher) Stats() PublisherStats {
	if p == nil {
		return PublisherStats{}
	}
	return PublisherStats{
		Sinks:     len(p.sinks),
		Published: p.published.Load(),
		Filtered:  p.filtered.Load(),
		Dropped:   p.dropped.Load(),
		Failed:    p.failed.Load(),
		Queued:    len(p.queue),
	}
}

func normaliseList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		out = append(out, strings.TrimSpace(v))
	}
	return out
}

func originSet(in []string) map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(in))
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			out[v] = struct{}{}
		}
	}
	return out
}
