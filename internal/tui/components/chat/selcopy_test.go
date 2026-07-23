package chat

import (
	"strings"
	"testing"
)

func TestSelectedTextBasic(t *testing.T) {
	m := &messagesCmp{
		contentLines: []string{
			"hello world",
			"second line here",
			"third",
		},
		selectionActive: true,
		selectionStartY: 0,
		selectionStartX: 6, // "world"
		selectionEndY:   1,
		selectionEndX:   6, // "second"
	}
	got := m.selectedText()
	t.Logf("selectedText=%q", got)
	if strings.TrimSpace(got) == "" {
		t.Fatal("selectedText empty for a valid multi-line selection")
	}

	// single line
	m2 := &messagesCmp{
		contentLines:    []string{"hello world"},
		selectionActive: true,
		selectionStartY: 0, selectionStartX: 0,
		selectionEndY: 0, selectionEndX: 5,
	}
	t.Logf("single=%q", m2.selectedText())
}
