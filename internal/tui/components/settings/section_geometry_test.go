package settings

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/styles"
)

func TestFieldGeometryMixedRows(t *testing.T) {
	s := &Section{
		Fields: []Field{
			{Type: FieldHeader, Label: "Providers"},
			{Type: FieldNote, Label: "Info", Value: "This section configures providers."},
			{Label: "Name", Type: FieldText, Value: "anthropic", Card: "Provider A", CardStatus: "Connected"},
			{Label: "Model", Type: FieldSelect, Value: "claude-sonnet", Options: []string{"claude-sonnet"}, Hint: "Recommended for most tasks.", Card: "Provider A"},
			{Label: "Add provider", Type: FieldAction},
		},
	}
	s.SetActiveFieldIdx(3)

	const width = 48
	heights := s.FieldHeights(width)
	wantHeights := []int{2, 1, 2, 3, 1}
	if len(heights) != len(wantHeights) {
		t.Fatalf("expected %d heights, got %d", len(wantHeights), len(heights))
	}
	for i, want := range wantHeights {
		if heights[i] != want {
			t.Fatalf("FieldHeights()[%d] = %d, want %d (%v)", i, heights[i], want, heights)
		}
	}

	wantOffsets := []int{0, 2, 3, 5, 8}
	for idx, want := range wantOffsets {
		if got := s.FieldLineOffset(width, idx); got != want {
			t.Errorf("FieldLineOffset(%d) = %d, want %d", idx, got, want)
		}
	}

	cases := map[int]int{
		0:  -1,
		1:  -1,
		2:  -1,
		3:  2,
		4:  2,
		5:  3,
		6:  3,
		7:  3,
		8:  4,
		9:  -1,
		-1: -1,
	}
	for line, want := range cases {
		if got := s.FieldAtLine(width, line); got != want {
			t.Errorf("FieldAtLine(%d) = %d, want %d", line, got, want)
		}
	}
}

func TestSectionViewHeightMatchesGeometryAcrossWidthsAndGlyphModes(t *testing.T) {
	t.Cleanup(func() { styles.SetNerdFonts(true) })

	for _, nerdFonts := range []bool{true, false} {
		styles.SetNerdFonts(nerdFonts)

		s := &Section{
			Fields: []Field{
				{Type: FieldHeader, Label: "Providers"},
				{Type: FieldNote, Label: "Info", Value: "token /very/long/unbreakable/path/with/no/spaces/that/must/hard-break cleanly"},
				{Label: "Name", Type: FieldText, Value: "anthropic", Card: "Provider A"},
				{Label: "Enabled", Type: FieldToggle, Value: "true", Hint: "This hint should wrap without changing the measured geometry.", Card: "Provider A", CardStatus: "Connected"},
				{Label: "Model", Type: FieldSelect, Value: "claude-sonnet", Options: []string{"claude-sonnet"}, Card: "Provider A"},
				{Label: "Add a very long provider action label", Type: FieldAction, Value: "Do something with a long summary that still has to stay on one line."},
			},
		}
		s.SetActiveFieldIdx(3)

		for width := 1; width <= 120; width++ {
			got := lipgloss.Height(s.View(width, true))
			heights := s.FieldHeights(width)
			want := 0
			for _, h := range heights {
				want += h
			}
			if got != want {
				t.Fatalf("nerdFonts=%v width=%d View height = %d, want %d (heights=%v)\n%s", nerdFonts, width, got, want, heights, s.View(width, true))
			}
		}
	}
}

func TestEditingLongValueKeepsPlannedHeight(t *testing.T) {
	longValue := strings.Repeat("x", 300)

	for _, width := range []int{40, 80, 120} {
		s := &Section{
			Fields: []Field{
				{Label: "Args", Type: FieldText, Value: longValue, Card: "Server"},
				{Label: "Enabled", Type: FieldToggle, Value: "true", Card: "Server"},
			},
		}
		s.SetActiveFieldIdx(0)
		if cmd := s.startEditing(); cmd == nil {
			t.Fatalf("width=%d startEditing() returned nil", width)
		}

		got := lipgloss.Height(s.View(width, true))
		heights := s.FieldHeights(width)
		want := 0
		for _, h := range heights {
			want += h
		}
		if got != want {
			t.Fatalf("width=%d editing View height = %d, want %d (heights=%v)\n%s", width, got, want, heights, s.View(width, true))
		}
	}
}
