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
	if kbStore == nil {
		return ""
	}
	if !cfg.MemoryEnabled || !cfg.MemoryContextEnrichmentEnabled {
		return ""
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
		return ""
	}
	if len(memories) == 0 {
		return ""
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

	return sb.String()
}

// formatMemoryLine formats a single memory for injection.
// Format: [key: <key>] <content[:200]> (scope: <scope>[, importance: <fmt %.2f>])
func formatMemoryLine(m kb.MemoryResult) string {
	content := m.Document.Content
	if len(content) > 200 {
		content = content[:200]
	}
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.TrimSpace(content)

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
