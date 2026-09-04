package settings

import (
	"strings"
	"testing"
)

// A locked field is refused by the configuration write path anyway. The point
// of the flag is that the refusal is visible before the user types, so the
// section must neither open an editor on it nor emit a save for it.
func TestLockedFieldIsNotEditable(t *testing.T) {
	cases := []struct {
		name  string
		field Field
		want  bool
	}{
		{"plain", Field{Label: "Theme", Key: "tui.theme", Type: FieldText}, true},
		{"locked", Field{Label: "Theme", Key: "tui.theme", Type: FieldText, Locked: true}, false},
		{"read only", Field{Label: "Theme", Key: "tui.theme", Type: FieldText, ReadOnly: true}, false},
		{"disabled", Field{Label: "Theme", Key: "tui.theme", Type: FieldText, Disabled: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.field.Editable(); got != tc.want {
				t.Fatalf("Editable() = %v, want %v", got, tc.want)
			}

			s := &Section{Title: "General", Fields: []Field{tc.field}}
			s.SetWidth(80)
			s.startEditing()
			if editing := s.IsEditing(); editing != tc.want {
				t.Fatalf("IsEditing() after startEditing = %v, want %v", editing, tc.want)
			}
			if cmd := s.saveActiveField(); (cmd != nil) != tc.want {
				t.Fatalf("saveActiveField returned a command = %v, want %v", cmd != nil, tc.want)
			}
		})
	}
}

// The lock has to be legible in the rendered row, not just enforced.
func TestLockedFieldRendersMarker(t *testing.T) {
	s := &Section{Title: "General", Fields: []Field{
		{Label: "Theme", Key: "tui.theme", Value: "corporate", Type: FieldText, Locked: true},
		{Label: "Debug", Key: "debug", Value: "false", Type: FieldToggle},
	}}
	s.SetWidth(80)
	views := s.renderFields(80, true)
	if len(views) != 2 {
		t.Fatalf("rendered %d fields, want 2", len(views))
	}
	if !strings.Contains(views[0], lockedFieldMarker) {
		t.Fatalf("locked field rendered without the marker:\n%s", views[0])
	}
	if strings.Contains(views[1], lockedFieldMarker) {
		t.Fatalf("unlocked field rendered with the marker:\n%s", views[1])
	}
}

// A caller-supplied note replaces the generic marker, so a managed row can say
// who owns the value. It is only rendered on a locked field: a note on an
// editable one would claim the value is not the user's while still letting them
// change it.
func TestManagedNoteReplacesTheMarker(t *testing.T) {
	locked := Field{Label: "Theme", Key: "tui.theme", Value: "corporate", Type: FieldText,
		Locked: true, ManagedNote: "Managed by the operator"}
	if got := locked.LockMarker(); got != "[Managed by the operator]" {
		t.Fatalf("LockMarker() = %q", got)
	}
	editable := Field{Label: "Debug", Key: "debug", Value: "false", Type: FieldToggle,
		ManagedNote: "Managed by the operator"}
	if got := editable.LockMarker(); got != "" {
		t.Fatalf("LockMarker() on an unlocked field = %q, want empty", got)
	}

	s := &Section{Title: "General", Fields: []Field{locked, editable}}
	s.SetWidth(80)
	views := s.renderFields(80, true)
	if !strings.Contains(views[0], "Managed by the operator") {
		t.Fatalf("the note is not in the rendered row:\n%s", views[0])
	}
	if strings.Contains(views[0], lockedFieldMarker) {
		t.Fatalf("both the note and the generic marker were drawn:\n%s", views[0])
	}
	if strings.Contains(views[1], "Managed by the operator") {
		t.Fatalf("an editable field was drawn as managed:\n%s", views[1])
	}
}
