package extensions

import (
	"context"
	"strings"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/rag/kb"
	"github.com/digiogithub/pando/pkg/extension"
)

// Read side of the memory capability: chaining RemembranceSearchWrappers in
// front of the knowledge base's own search.
//
// One choke point, KBStore.SearchDocumentsWithOptions, covers every read the
// agent makes — the kb_search_documents tool, hybrid search, the context
// enricher and memory injection all go through it. Wrapping anywhere else
// would mean picking which of those four see corporate results, and there is
// no honest way to pick.

// remoteSourceKey is the metadata key that labels a hit a wrapper contributed.
// It is metadata rather than a new field because the search tool already
// renders metadata, so the label reaches the model without a format change.
const remoteSourceKey = "remembrance_source"

// SearchMiddleware builds the kb.SearchMiddleware for the wrappers in mgr, or
// nil when there are none. Chaining runs in registration order, so the first
// registered wrapper ends up outermost.
func SearchMiddleware(mgr *extension.Manager) kb.SearchMiddleware {
	if mgr == nil {
		return nil
	}
	wrappers := extension.Capability[extension.RemembranceSearchWrapper](mgr)
	if len(wrappers) == 0 {
		return nil
	}

	ids := make([]string, 0, len(wrappers))
	for _, w := range wrappers {
		ids = append(ids, string(w.ExtensionInfo().ID))
	}
	logging.Info("Extension remembrance search wrappers active",
		"extensions", strings.Join(ids, ","))

	return func(ctx context.Context, query string, limit int, opts kb.SearchOptions, next kb.SearchNext) ([]kb.SearchResult, error) {
		// The chain is built per call rather than once at startup so that a
		// wrapper cannot capture and reuse one request's local searcher.
		var searcher extension.RemembranceSearcher = extension.RemembranceSearcherFunc(
			func(ctx context.Context, q extension.RemembranceQuery) ([]extension.Remembrance, error) {
				results, err := next(ctx, q.Query, q.Limit, opts)
				if err != nil {
					return nil, err
				}
				return toRemembrances(results), nil
			})

		for i := len(wrappers) - 1; i >= 0; i-- {
			searcher = wrapSafely(wrappers[i], searcher)
		}

		out, err := searcher.SearchRemembrances(ctx, extension.RemembranceQuery{
			Query: query,
			Limit: limit,
			Tags:  opts.Tags,
			Scope: opts.Scope,
		})
		if err != nil {
			// A wrapper failing must not cost the agent its own memory: fall
			// back to the local store rather than propagating the error.
			logging.Warn("Extension remembrance search failed, falling back to local results", "error", err)
			return next(ctx, query, limit, opts)
		}
		return fromRemembrances(out), nil
	}
}

// wrapSafely contains a wrapper that panics while building the chain, and one
// that returns nil instead of a searcher.
func wrapSafely(w extension.RemembranceSearchWrapper, next extension.RemembranceSearcher) (out extension.RemembranceSearcher) {
	out = next
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Extension remembrance search wrapper panicked",
				"extension", w.ExtensionInfo().ID, "panic", r)
			out = next
		}
	}()
	if wrapped := w.WrapRemembranceSearch(next); wrapped != nil {
		return wrapped
	}
	return next
}

func toRemembrances(results []kb.SearchResult) []extension.Remembrance {
	out := make([]extension.Remembrance, 0, len(results))
	for _, r := range results {
		out = append(out, extension.Remembrance{
			Path:      r.Document.FilePath,
			Content:   r.ChunkContent,
			Score:     r.Score,
			Tags:      r.Document.Tags,
			Scope:     r.Document.MemoryScope,
			Source:    "local",
			UpdatedAt: r.Document.UpdatedAt,
		})
	}
	return out
}

func fromRemembrances(in []extension.Remembrance) []kb.SearchResult {
	out := make([]kb.SearchResult, 0, len(in))
	for i, r := range in {
		doc := kb.Document{
			FilePath:    r.Path,
			Content:     r.Content,
			Tags:        r.Tags,
			MemoryScope: r.Scope,
			UpdatedAt:   r.UpdatedAt,
		}
		// A merged result set that does not say which hits are remote is
		// indistinguishable from a local one, which is precisely what the
		// visibility requirement forbids.
		if src := strings.TrimSpace(r.Source); src != "" && src != "local" {
			doc.Metadata = map[string]any{remoteSourceKey: src}
		}
		out = append(out, kb.SearchResult{
			Document:     doc,
			ChunkContent: r.Content,
			Score:        r.Score,
			Rank:         i + 1,
		})
	}
	return out
}
