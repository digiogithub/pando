package agent

import (
	"context"
	"sync"
)

// memoryQueryContextKey carries the user prompt that seeds the memory search of a
// session's first turn.
type memoryQueryContextKey struct{}

// withMemoryQuery returns ctx carrying query as the memory search seed.
func withMemoryQuery(ctx context.Context, query string) context.Context {
	return context.WithValue(ctx, memoryQueryContextKey{}, query)
}

func memoryQueryFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	query, _ := ctx.Value(memoryQueryContextKey{}).(string)
	return query
}

// maxFrozenMemoryBlocks bounds the per-session cache. Dropping it wholesale only
// costs each live session one rebuild (and one prompt-cache miss), so a simple
// reset is enough instead of an LRU.
const maxFrozenMemoryBlocks = 512

// frozenMemoryBlocks holds the <memories> block injected into each session's
// system prompt. The block is built once per session and reused verbatim on
// later turns: the system prompt is rebuilt every turn, and a block that moved
// between turns (new hit ordering, new search results) would change the prompt
// prefix and invalidate the provider's prompt cache for the whole history. It is
// dropped after a summary/compaction, which rebuilds the history anyway.
var frozenMemoryBlocks = struct {
	sync.Mutex
	bySession map[string]string
}{bySession: make(map[string]string)}

// sessionMemoryBlock returns the memory block for the request's session, building
// and freezing it on first use. The first build searches with the prompt carried
// by withMemoryQuery. Requests without a session are never frozen.
func sessionMemoryBlock(ctx context.Context) string {
	injector := globalMemoryInjector
	if injector == nil {
		return ""
	}
	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		return injector.BuildMemoryBlock(ctx, memoryQueryFromContext(ctx))
	}

	frozenMemoryBlocks.Lock()
	block, ok := frozenMemoryBlocks.bySession[sessionID]
	frozenMemoryBlocks.Unlock()
	if ok {
		return block
	}

	block = injector.BuildMemoryBlock(ctx, memoryQueryFromContext(ctx))

	frozenMemoryBlocks.Lock()
	defer frozenMemoryBlocks.Unlock()
	// A concurrent build for the same session may have won; keep the first one so
	// every turn sees the same bytes.
	if existing, ok := frozenMemoryBlocks.bySession[sessionID]; ok {
		return existing
	}
	if len(frozenMemoryBlocks.bySession) >= maxFrozenMemoryBlocks {
		frozenMemoryBlocks.bySession = make(map[string]string)
	}
	frozenMemoryBlocks.bySession[sessionID] = block
	return block
}

// invalidateSessionMemoryBlock drops the frozen block so the session's next turn
// rebuilds it.
func invalidateSessionMemoryBlock(sessionID string) {
	frozenMemoryBlocks.Lock()
	delete(frozenMemoryBlocks.bySession, sessionID)
	frozenMemoryBlocks.Unlock()
}
