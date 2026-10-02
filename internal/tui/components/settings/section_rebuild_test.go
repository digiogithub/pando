package settings

import (
	"strings"
	"testing"
)

// buildTestSections mimics a page-level buildSections() call: every returned
// section is freshly constructed, so activeFieldIdx is always 0.
func buildTestSections() []Section {
	fields := make([]Field, 0, 40)
	for i := 0; i < 40; i++ {
		fields = append(fields, Field{
			Label: "Field",
			Key:   fieldKeyAt(i),
			Value: "value",
			Type:  FieldText,
		})
	}
	return []Section{{Title: "Agents", Fields: fields}}
}

func fieldKeyAt(i int) string {
	return "agents.a" + string(rune('a'+i%26)) + "." + string(rune('0'+i/26)) + ".model"
}

// TestSetSectionsPreservesFocusAndScroll reproduces the config-watcher rebuild
// that fires after saving a field: the page rebuilds sections without calling
// SetActiveField, which used to reset the active field to 0 and scroll the
// viewport back to the top of the section.
func TestSetSectionsPreservesFocusAndScroll(t *testing.T) {
	m := NewSettingsCmp()
	m.SetSections(buildTestSections())
	m.SetSize(120, 20)

	const targetIdx = 30
	m.SetActiveField("Agents", fieldKeyAt(targetIdx))

	offsetBefore := m.viewport.YOffset
	if offsetBefore == 0 {
		t.Fatalf("precondition failed: expected a scrolled viewport, got YOffset 0")
	}

	// The rebuild triggered by configExternalChangeMsg.
	m.SetSections(buildTestSections())
	m.SetSize(120, 20)

	if got := m.activeSection().ActiveFieldIdx(); got != targetIdx {
		t.Errorf("active field after rebuild = %d, want %d", got, targetIdx)
	}
	if got := m.viewport.YOffset; got != offsetBefore {
		t.Errorf("viewport YOffset after rebuild = %d, want %d", got, offsetBefore)
	}
}

func TestAutoScrollShowsDecorationBeforeActiveField(t *testing.T) {
	m := NewSettingsCmp()
	m.SetSections([]Section{{
		Title: "Providers",
		Fields: []Field{
			{Type: FieldHeader, Label: "Accounts"},
			{Type: FieldNote, Label: "Info", Value: "Manage provider access here."},
			{Label: "First", Key: "providers.first", Type: FieldText, Value: "a"},
			{Label: "Second", Key: "providers.second", Type: FieldText, Value: "b"},
			{Type: FieldHeader, Label: "Advanced"},
			{Type: FieldNote, Label: "Info", Value: "These rows belong with the second field."},
			{Label: "Third", Key: "providers.third", Type: FieldText, Value: "c"},
			{Label: "Fourth", Key: "providers.fourth", Type: FieldText, Value: "d"},
		},
	}})
	m.SetSize(80, 9)

	m.SetActiveField("Providers", "providers.fourth")
	m.SetActiveField("Providers", "providers.third")

	view := m.viewport.View()
	if !strings.Contains(view, "Advanced") || !strings.Contains(view, "These rows belong") {
		t.Fatalf("viewport should include the decoration rows above the active field:\n%s", view)
	}
}

func TestAutoScrollShowsTrailingNotesAfterLastField(t *testing.T) {
	m := NewSettingsCmp()
	m.SetSections([]Section{{
		Title: "Providers",
		Fields: []Field{
			{Label: "First", Key: "providers.first", Type: FieldText, Value: "a"},
			{Label: "Second", Key: "providers.second", Type: FieldText, Value: "b"},
			{Label: "Third", Key: "providers.third", Type: FieldText, Value: "c"},
			{Type: FieldNote, Label: "Info", Value: "Restart the provider after changing credentials."},
		},
	}})
	m.SetSize(80, 7)
	m.SetActiveField("Providers", "providers.third")

	view := m.viewport.View()
	if !strings.Contains(view, "Restart the provider") {
		t.Fatalf("viewport should include the note after the last focusable row:\n%s", view)
	}
}
