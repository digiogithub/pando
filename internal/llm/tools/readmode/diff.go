package readmode

import (
	"fmt"
	"strings"
)

// maxDiffLines bounds how many changed lines a side may contribute before the
// diff collapses to a summary line (keeps the "compact diff" actually compact).
const maxDiffLines = 200

// DiffWindows renders a compact line diff between two reads of the same window.
// It trims the common prefix and suffix and reports the changed middle block as
// removed (-) / added (+) lines, each annotated with its real source line number
// (lineOffset is the window's 0-based start line). Used by Phase 2 when a re-read
// of a previously-delivered window has changed.
func DiffWindows(oldContent, newContent string, lineOffset int) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// Common prefix.
	p := 0
	for p < len(oldLines) && p < len(newLines) && oldLines[p] == newLines[p] {
		p++
	}
	// Common suffix (not overlapping the prefix).
	s := 0
	for s < len(oldLines)-p && s < len(newLines)-p &&
		oldLines[len(oldLines)-1-s] == newLines[len(newLines)-1-s] {
		s++
	}

	removed := oldLines[p : len(oldLines)-s]
	added := newLines[p : len(newLines)-s]

	if len(removed) == 0 && len(added) == 0 {
		return "(no textual change)"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "@@ change near line %d: -%d +%d @@\n", lineOffset+p+1, len(removed), len(added))

	if len(removed) > maxDiffLines || len(added) > maxDiffLines {
		fmt.Fprintf(&b, "(large change: %d lines removed, %d lines added — use mode=full to see it all)", len(removed), len(added))
		return b.String()
	}

	for i, line := range removed {
		fmt.Fprintf(&b, "- %d: %s\n", lineOffset+p+i+1, line)
	}
	for i, line := range added {
		fmt.Fprintf(&b, "+ %d: %s\n", lineOffset+p+i+1, line)
	}
	return strings.TrimRight(b.String(), "\n")
}
