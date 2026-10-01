package dialog

import (
	"strings"
	"testing"
)

func TestPersonaDialogAutoEntryShowsAppliedPersona(t *testing.T) {
	d := NewPersonaDialogCmp().(*personaDialogCmp)
	d.SetPersonas([]string{"assistant", "qa"}, "")

	if got := d.displayName(personaNoneOption); got != personaNoneOption {
		t.Fatalf("no applied persona: label = %q", got)
	}
	d.SetAutoApplied("software-engineer")
	if got := d.displayName(personaNoneOption); got != "Auto (software-engineer)" {
		t.Fatalf("label = %q", got)
	}
	if got := d.displayName("qa"); got != "qa" {
		t.Fatalf("other entries must keep their name, got %q", got)
	}
	if view := d.View(); !strings.Contains(view, "Auto (software-engineer)") {
		t.Fatalf("view lacks the applied persona:\n%s", view)
	}
}
