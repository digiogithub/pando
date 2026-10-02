package settings

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/styles"
)

type fieldGlyphSet struct {
	ToggleOn       string
	ToggleOff      string
	Dropdown       string
	ActiveMarker   string
	EditMarker     string
	HeaderRule     string
	NoteBar        string
	CardHorizontal string
}

func currentFieldGlyphs() fieldGlyphSet {
	if styles.NerdFontsEnabled() {
		return fieldGlyphSet{
			ToggleOn:       "●",
			ToggleOff:      "○",
			Dropdown:       "▾",
			ActiveMarker:   "▌",
			EditMarker:     "✎",
			HeaderRule:     "─",
			NoteBar:        "│",
			CardHorizontal: "─",
		}
	}

	return fieldGlyphSet{
		ToggleOn:       "[x]",
		ToggleOff:      "[ ]",
		Dropdown:       "v",
		ActiveMarker:   ">",
		EditMarker:     ">",
		HeaderRule:     "-",
		NoteBar:        "|",
		CardHorizontal: "-",
	}
}

func truncatePlain(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}

	var b strings.Builder
	for _, r := range text {
		candidate := b.String() + string(r)
		if lipgloss.Width(candidate)+1 > width {
			break
		}
		b.WriteRune(r)
	}

	result := b.String()
	if result == "" {
		return "…"
	}
	return result + "…"
}

func padPlain(text string, width int) string {
	text = truncatePlain(text, width)
	if pad := width - lipgloss.Width(text); pad > 0 {
		return text + strings.Repeat(" ", pad)
	}
	return text
}

func wrapPlain(text string, width int) []string {
	return wrapPlainWithWidths(text, width, width)
}

func wrapPlainWithWidths(text string, firstWidth, otherWidth int) []string {
	if firstWidth <= 0 && otherWidth <= 0 {
		return []string{text}
	}

	paragraphs := strings.Split(text, "\n")
	lines := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}

		currentWidth := firstWidth
		if currentWidth <= 0 {
			currentWidth = otherWidth
		}
		if currentWidth <= 0 {
			currentWidth = 1
		}

		current := ""
		for _, word := range words {
			for {
				if current == "" {
					head, tail := splitPlainWidth(word, currentWidth)
					current = head
					if tail == "" {
						break
					}
					lines = append(lines, current)
					current = ""
					currentWidth = max(1, otherWidth)
					word = tail
					continue
				}

				candidate := current + " " + word
				if lipgloss.Width(candidate) <= currentWidth {
					current = candidate
					break
				}

				lines = append(lines, current)
				current = ""
				currentWidth = max(1, otherWidth)
			}
		}
		if current != "" {
			lines = append(lines, current)
		}
	}

	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func splitPlainWidth(text string, width int) (head, tail string) {
	width = max(1, width)
	if lipgloss.Width(text) <= width {
		return text, ""
	}

	var b strings.Builder
	lastIdx := 0
	for idx, r := range text {
		candidate := b.String() + string(r)
		if lipgloss.Width(candidate) > width {
			break
		}
		b.WriteRune(r)
		lastIdx = idx + len(string(r))
	}

	if b.Len() == 0 {
		return truncatePlain(text, width), ""
	}
	return b.String(), text[lastIdx:]
}
