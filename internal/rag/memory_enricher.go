package rag

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/rag/kb"
)

// BuildMemoryBlock retrieves relevant memories and formats them as a <memories> XML block
// for injection into the system prompt. Returns an empty string when no memories are found
// or the KB store is unavailable.
//
// After building the block it fires goroutines to increment hit counters for each returned
// memory — these are best-effort and non-blocking.
func BuildMemoryBlock(ctx context.Context, kbStore *kb.KBStore, query string, cfg config.RemembrancesConfig) string {
	out, _ := BuildMemoryBlockWithResult(ctx, kbStore, query, cfg, nil)
	return out
}

// BuildMemoryBlockWithResult is BuildMemoryBlock with an optional relevance
// filter (nil = off, byte-identical output). Memories from a pinned scope
// (cfg.MemoryPinnedScopes) are never dropped, and hit counters are only
// incremented for memories that are actually injected.
func BuildMemoryBlockWithResult(ctx context.Context, kbStore *kb.KBStore, query string, cfg config.RemembrancesConfig, filter RelevanceFilter) (string, FilterResult) {
	var res FilterResult
	if kbStore == nil {
		return "", res
	}
	if !cfg.MemoryEnabled || !cfg.MemoryContextEnrichmentEnabled {
		return "", res
	}

	memories, err := kbStore.GetMemoriesForInjection(
		ctx,
		query,
		cfg.MemoryContextMaxItems,
		cfg.MemoryContextMaxChars,
		cfg.MemoryPinnedScopes,
	)
	if err != nil {
		logging.Debug("memory enricher: get memories failed", "error", err)
		return "", res
	}
	if len(memories) == 0 {
		return "", res
	}

	memories, res = filterMemories(ctx, memories, query, cfg, filter)
	if len(memories) == 0 {
		return "", res
	}

	var sb strings.Builder
	sb.WriteString("<memories>\n")
	for _, m := range memories {
		line := formatMemoryLine(m)
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("</memories>")

	// Increment hits asynchronously — non-blocking, best-effort.
	for _, m := range memories {
		go func(id int64) {
			_ = kbStore.IncrementMemoryHits(context.Background(), id, cfg.MemoryDefaultTTLDays)
		}(m.Document.ID)
	}

	return sb.String(), res
}

// filterMemories applies the relevance filter to memories. Pinned-scope
// memories are never dropped. A nil filter returns memories untouched.
func filterMemories(ctx context.Context, memories []kb.MemoryResult, query string, cfg config.RemembrancesConfig, filter RelevanceFilter) ([]kb.MemoryResult, FilterResult) {
	var res FilterResult
	if filter == nil || len(memories) == 0 {
		return memories, res
	}
	pinned := make(map[string]bool, len(cfg.MemoryPinnedScopes))
	for _, sc := range cfg.MemoryPinnedScopes {
		pinned[sc] = true
	}
	cands := make([]RelevanceCandidate, len(memories))
	for i, m := range memories {
		id := m.Document.MemoryKey
		if id == "" {
			id = m.Document.FilePath
		}
		cands[i] = RelevanceCandidate{
			Source: SourceMemory,
			ID:     id,
			Text:   formatMemoryLine(m),
			Score:  m.Score,
			Pinned: m.Document.MemoryScope != "" && pinned[m.Document.MemoryScope],
		}
	}
	keep, res := applyRelevanceFilter(ctx, filter, query, cands)
	kept := make([]kb.MemoryResult, 0, len(memories))
	for i, m := range memories {
		if keep[i] {
			kept = append(kept, m)
		}
	}
	return kept, res
}

// memoryLineMaxRunes caps how much of a memory is inlined into the system prompt.
const memoryLineMaxRunes = 200

// formatMemoryLine formats a single memory for injection.
// Format: [key: <key>] <content> (scope: <scope>[, importance: <fmt %.2f>])
// Content longer than memoryLineMaxRunes is cut on a rune boundary and followed by
// a pointer to the full text, so the line works as an index entry the model can
// follow instead of a dead fragment.
func formatMemoryLine(m kb.MemoryResult) string {
	content := strings.TrimSpace(strings.ReplaceAll(m.Document.Content, "\n", " "))
	if runes := []rune(content); len(runes) > memoryLineMaxRunes {
		content = strings.TrimSpace(string(runes[:memoryLineMaxRunes])) + "… " + fullMemoryPointer(m.Document)
	}

	var sb strings.Builder
	if m.Document.MemoryKey != "" {
		sb.WriteString(fmt.Sprintf("[key: %s] ", m.Document.MemoryKey))
	}
	sb.WriteString(content)

	var ann []string
	if m.Document.MemoryScope != "" {
		ann = append(ann, fmt.Sprintf("scope: %s", m.Document.MemoryScope))
	}
	// Only show importance when it deviates from the default 0.5 and is non-zero.
	if m.Document.Importance != 0 && m.Document.Importance != 0.5 {
		ann = append(ann, fmt.Sprintf("importance: %.2f", m.Document.Importance))
	}
	if len(ann) > 0 {
		sb.WriteString(fmt.Sprintf(" (%s)", strings.Join(ann, ", ")))
	}

	return sb.String()
}

// fullMemoryPointer tells the model how to read a truncated memory in full. Every
// memory is a KB document, so its path always resolves; recall only searches.
func fullMemoryPointer(doc kb.Document) string {
	return fmt.Sprintf("[truncated; full text: kb_get_document file_path=%q]", doc.FilePath)
}
