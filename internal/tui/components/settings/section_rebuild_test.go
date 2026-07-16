package settings

import "testing"

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
	return []Section{{Title: "Agents/Models", Fields: fields}}
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
	m.SetActiveField("Agents/Models", fieldKeyAt(targetIdx))

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
