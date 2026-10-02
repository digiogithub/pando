package settings

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/tui/styles"
)

func TestNavigationSkipsNonFocusableRows(t *testing.T) {
	s := &Section{
		Fields: []Field{
			{Type: FieldHeader, Label: "Providers"},
			{Type: FieldNote, Value: "Read this first."},
			{Label: "Enabled", Type: FieldToggle, Value: "true"},
			{Label: "Add provider", Type: FieldAction},
		},
	}

	s.SetActiveFieldIdx(0)
	if got := s.ActiveFieldIdx(); got != 2 {
		t.Fatalf("initial active index = %d, want 2", got)
	}

	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := s.ActiveFieldIdx(); got != 3 {
		t.Fatalf("after down active index = %d, want 3", got)
	}

	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := s.ActiveFieldIdx(); got != 2 {
		t.Fatalf("after wrap-down active index = %d, want 2", got)
	}

	s.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := s.ActiveFieldIdx(); got != 3 {
		t.Fatalf("after wrap-up active index = %d, want 3", got)
	}
}

func TestSectionWithOnlyNonFocusableRowsStaysUnselected(t *testing.T) {
	s := &Section{
		Fields: []Field{
			{Type: FieldHeader, Label: "Providers"},
			{Type: FieldNote, Value: "Nothing to edit here."},
		},
	}

	s.SetActiveFieldIdx(0)
	if got := s.ActiveFieldIdx(); got != -1 {
		t.Fatalf("active index = %d, want -1", got)
	}
	if field := s.ActiveField(); field != nil {
		t.Fatalf("ActiveField() = %#v, want nil", *field)
	}
	if cmd := s.Update(tea.KeyMsg{Type: tea.KeyDown}); cmd != nil {
		t.Fatalf("Update returned %v, want nil", cmd)
	}
}

func TestTypedRowRenderingAndGlyphFallback(t *testing.T) {
	t.Cleanup(func() { styles.SetNerdFonts(true) })

	s := &Section{
		Fields: []Field{
			{Label: "Enabled", Type: FieldToggle, Value: "true"},
			{Label: "Model", Type: FieldSelect, Value: "claude-sonnet"},
			{Label: "API Key", Type: FieldText, Value: "secret"},
			{Label: "Current provider", Type: FieldText, Value: "anthropic", ReadOnly: true},
			{Label: "Add provider", Type: FieldAction},
		},
	}
	s.SetActiveFieldIdx(0)

	rendered := strings.Join(s.renderFields(64, true), "\n")
	for _, want := range []string{"● on", "claude-sonnet ▾", "secret  ✎", "Current provider", "[ Add provider ]"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered settings are missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "anthropic  ✎") {
		t.Fatalf("read-only text row should not show an edit marker:\n%s", rendered)
	}

	styles.SetNerdFonts(false)
	rendered = strings.Join(s.renderFields(64, true), "\n")
	for _, want := range []string{"[x] on", "claude-sonnet v", "secret  >", "> "} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("ASCII fallback rendering is missing %q:\n%s", want, rendered)
		}
	}
}

func TestCardTitleAndStatusRenderOnSharedBorder(t *testing.T) {
	s := &Section{
		Fields: []Field{
			{Label: "Name", Type: FieldText, Value: "anthropic", Card: "Provider A", CardStatus: "Connected"},
			{Label: "Model", Type: FieldSelect, Value: "claude-sonnet", Card: "Provider A"},
		},
	}
	s.SetActiveFieldIdx(0)

	views := s.renderFields(60, true)
	if len(views) != 2 {
		t.Fatalf("rendered %d fields, want 2", len(views))
	}
	if !strings.Contains(views[0], "Provider A") || !strings.Contains(views[0], "Connected") || !strings.Contains(views[0], "╭") {
		t.Fatalf("first card field rendered without the titled border:\n%s", views[0])
	}
	if !strings.Contains(views[1], "╰") {
		t.Fatalf("last card field rendered without the closing border:\n%s", views[1])
	}
}

func TestCardStatusUsesFirstNonEmptyStatusInRun(t *testing.T) {
	s := &Section{
		Fields: []Field{
			{Label: "Name", Type: FieldText, Value: "anthropic", Card: "Provider A"},
			{Label: "Enabled", Type: FieldToggle, Value: "true", Card: "Provider A", CardStatus: "Connected"},
		},
	}
	s.SetActiveFieldIdx(0)

	views := s.renderFields(60, true)
	if !strings.Contains(views[0], "Connected") {
		t.Fatalf("card status did not survive when the first row had none:\n%s", views[0])
	}
}

func TestCardIDSeparatesCardsWithSameTitle(t *testing.T) {
	s := &Section{
		Fields: []Field{
			{Label: "Name", Type: FieldText, Value: "alpha", Card: "Provider", CardID: "account-a"},
			{Label: "Name", Type: FieldText, Value: "beta", Card: "Provider", CardID: "account-b"},
		},
	}
	s.SetActiveFieldIdx(0)

	views := s.renderFields(60, true)
	if len(views) != 2 {
		t.Fatalf("rendered %d fields, want 2", len(views))
	}
	for i, view := range views {
		if !strings.Contains(view, "╭") || !strings.Contains(view, "╰") {
			t.Fatalf("field %d should render as its own card:\n%s", i, view)
		}
	}
}
