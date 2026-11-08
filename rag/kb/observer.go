package kb

import (
	"context"
	"time"
)

// Write observation and search decoration hooks.
//
// The knowledge base does not know that extensions exist, and must not: it is
// core storage, and pkg/extension is a public contract it has no business
// importing. It publishes plain structs through two optional function hooks
// instead, and internal/extensions is what plugs the extension system into
// them. Nothing is installed by default, so a standard build pays one nil
// check per write.

// WriteKind distinguishes a keyed memory from a plain knowledge-base document.
type WriteKind string

const (
	WriteKindMemory   WriteKind = "memory"
	WriteKindDocument WriteKind = "document"
)

// WriteOp is what happened to the record.
type WriteOp string

const (
	WriteCreated WriteOp = "created"
	WriteUpdated WriteOp = "updated"
	WriteDeleted WriteOp = "deleted"
)

// WriteEvent describes one committed write. Observers see it only after the
// write succeeded: an observer must never be able to report something that did
// not happen.
type WriteEvent struct {
	Kind      WriteKind
	Op        WriteOp
	FilePath  string
	Key       string
	Scope     string
	Content   string
	Tags      []string
	Metadata  map[string]any
	Origin    string
	Timestamp time.Time
}

// WriteObserver receives committed writes. It is called synchronously on the
// write path and must return promptly; whether the work behind it is async is
// the observer's decision, not the store's.
type WriteObserver func(ctx context.Context, ev WriteEvent)

// SearchNext is the remaining search chain, ending in the store's own search.
type SearchNext func(ctx context.Context, query string, limit int, opts SearchOptions) ([]SearchResult, error)

// SearchMiddleware decorates document search. It receives the rest of the
// chain and may add, drop or reorder results.
type SearchMiddleware func(ctx context.Context, query string, limit int, opts SearchOptions, next SearchNext) ([]SearchResult, error)

// SetWriteObserver installs the write observer, replacing any previous one.
// Passing nil removes it.
func (s *KBStore) SetWriteObserver(fn WriteObserver) {
	s.fsMu.Lock()
	s.writeObserver = fn
	s.fsMu.Unlock()
}

// SetSearchMiddleware installs the search middleware, replacing any previous
// one. Passing nil removes it.
func (s *KBStore) SetSearchMiddleware(fn SearchMiddleware) {
	s.fsMu.Lock()
	s.searchMiddleware = fn
	s.fsMu.Unlock()
}

func (s *KBStore) observer() WriteObserver {
	s.fsMu.RLock()
	defer s.fsMu.RUnlock()
	return s.writeObserver
}

func (s *KBStore) middleware() SearchMiddleware {
	s.fsMu.RLock()
	defer s.fsMu.RUnlock()
	return s.searchMiddleware
}

// originKey types the context value so nothing else can collide with it.
type originKey struct{}

// suppressKey marks a nested write that its caller will report itself.
type suppressKey struct{}

// WithWriteOrigin tags ctx with the subsystem responsible for the writes made
// under it ("tool", "api", "sync", "watcher", "gc", "remote"). Unset means
// "tool", which is where writes come from unless something says otherwise.
func WithWriteOrigin(ctx context.Context, origin string) context.Context {
	if origin == "" {
		return ctx
	}
	return context.WithValue(ctx, originKey{}, origin)
}

// WriteOriginOf returns the origin tagged on ctx, or "tool".
func WriteOriginOf(ctx context.Context) string {
	if v, ok := ctx.Value(originKey{}).(string); ok && v != "" {
		return v
	}
	return "tool"
}

// withSuppressedObserver marks ctx so that writes made under it publish
// nothing. UpsertMemory uses it around the AddDocument/UpdateDocument calls it
// delegates to, because it publishes a richer memory event of its own and the
// same write must not be reported twice.
func withSuppressedObserver(ctx context.Context) context.Context {
	return context.WithValue(ctx, suppressKey{}, true)
}

// WithoutWriteObserver suppresses write publication for everything done under
// ctx. The IPC dispatcher uses it when applying a write forwarded by another
// instance: that instance already published the event, and republishing it
// here would ship the same memory twice.
func WithoutWriteObserver(ctx context.Context) context.Context {
	return withSuppressedObserver(ctx)
}

func observerSuppressed(ctx context.Context) bool {
	v, _ := ctx.Value(suppressKey{}).(bool)
	return v
}

// publishWrite hands one committed write to the observer, if any.
func (s *KBStore) publishWrite(ctx context.Context, ev WriteEvent) {
	if observerSuppressed(ctx) {
		return
	}
	fn := s.observer()
	if fn == nil {
		return
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	if ev.Origin == "" {
		ev.Origin = WriteOriginOf(ctx)
	}
	if ev.Tags == nil {
		ev.Tags = ExtractTagsFromMetadata(ev.Metadata)
	}
	fn(ctx, ev)
}
