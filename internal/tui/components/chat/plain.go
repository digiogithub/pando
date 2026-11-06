package chat

import (
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/digiogithub/pando/internal/tui/theme"
)

// plainMode switches the chat transcript to ASCII-only, colorless output. It is
// meant for terminals that are scraped as text (agent supervisors, recorders,
// dumps): no colors, no backgrounds, no unicode markers.
var plainMode atomic.Bool

// SetPlainMode enables or disables the plain chat rendering.
func SetPlainMode(on bool) { plainMode.Store(on) }

// IsPlainMode reports whether the plain chat rendering is active.
func IsPlainMode() bool { return plainMode.Load() }

// Role markers. The default mode uses one unicode glyph per role; plain mode
// falls back to ASCII so a text dump stays clean.
const (
	userMarker      = "> "
	assistantMarker = "● "
	toolMarker      = "· "
	thinkingMarker  = "~ "
	noticeMarker    = "# "

	assistantMarkerPlain = "* "
	toolMarkerPlain      = "- "

	// markerWidth is the cell width of every marker (including its space).
	markerWidth = 2
)

type roleKind int

const (
	roleUser roleKind = iota
	roleAssistant
	roleTool
	roleThinking
	roleNotice
)

func markerFor(kind roleKind) string {
	plain := IsPlainMode()
	switch kind {
	case roleUser:
		return userMarker
	case roleAssistant:
		if plain {
			return assistantMarkerPlain
		}
		return assistantMarker
	case roleTool:
		if plain {
			return toolMarkerPlain
		}
		return toolMarker
	case roleNotice:
		return noticeMarker
	default:
		return thinkingMarker
	}
}

// roleBlock prefixes body with a role marker on its first line and an equally
// wide blank indent on the following lines. It draws no border or box, so the
// text stays clean when the screen is dumped. Roles are told apart by the
// marker (and by its color outside plain mode).
func roleBlock(body string, kind roleKind) string {
	marker := markerFor(kind)
	indent := strings.Repeat(" ", markerWidth)
	if !IsPlainMode() {
		t := theme.CurrentTheme()
		color := t.Primary()
		switch kind {
		case roleUser:
			color = t.Secondary()
		case roleTool, roleThinking, roleNotice:
			color = t.TextMuted()
		}
		marker = lipgloss.NewStyle().Foreground(color).Render(marker)
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if IsPlainMode() {
			// Drop colors and the padding spaces markdown/lipgloss add to fill
			// the width, which turn into noise in a text dump.
			line = strings.TrimRight(ansi.Strip(line), " ")
		}
		if i == 0 {
			lines[i] = marker + line
		} else {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

// wrapNotice hard-wraps plain notice text to width so long ids never get cut.
func wrapNotice(text string, width int) string {
	if width < 10 {
		return text
	}
	return lipgloss.NewStyle().Width(width).Render(text)
}
