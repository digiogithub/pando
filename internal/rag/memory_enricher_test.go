package rag

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/digiogithub/pando/internal/rag/kb"
)

func TestFormatMemoryLineKeepsShortContent(t *testing.T) {
	line := formatMemoryLine(kb.MemoryResult{Document: kb.Document{
		FilePath:    "memory/project/a.md",
		Content:     "Use jj, not git stash.\nThe .kb is versioned.",
		MemoryKey:   "project.vcs",
		MemoryScope: "project/",
	}})
	want := "[key: project.vcs] Use jj, not git stash. The .kb is versioned. (scope: project/)"
	if line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
}

func TestFormatMemoryLineTruncatesOnRuneBoundaryWithPointer(t *testing.T) {
	// Multi-byte runes so a byte cut at 200 would land inside a character.
	content := strings.Repeat("ñandú ", 60)
	line := formatMemoryLine(kb.MemoryResult{Document: kb.Document{
		FilePath: "memory/project/long.md",
		Content:  content,
	}})
	if !utf8.ValidString(line) {
		t.Fatalf("truncation split a rune: %q", line)
	}
	if !strings.Contains(line, `[truncated; full text: kb_get_document file_path="memory/project/long.md"]`) {
		t.Fatalf("truncated memory does not point to its full text: %q", line)
	}
	body := line[:strings.Index(line, "…")]
	if n := utf8.RuneCountInString(body); n > memoryLineMaxRunes {
		t.Fatalf("inlined %d runes, cap is %d", n, memoryLineMaxRunes)
	}
}
