package input

import (
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

func TestParseChord(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Chord
		wantErr bool
	}{
		{"single letter", "s", Chord{Key: "s"}, false},
		{"uppercase letter lowercased", "S", Chord{Key: "s"}, false},
		{"named key", "Enter", Chord{Key: "enter"}, false},
		{"named key alias return", "return", Chord{Key: "enter"}, false},
		{"esc alias", "Esc", Chord{Key: "escape"}, false},
		{"pgup alias", "PgUp", Chord{Key: "pageup"}, false},
		{"pgdn alias", "PgDn", Chord{Key: "pagedown"}, false},
		{"pgdown alias", "pgdown", Chord{Key: "pagedown"}, false},
		{"function key", "F5", Chord{Key: "f5"}, false},
		{"arrow alias", "ArrowLeft", Chord{Key: "left"}, false},
		{"ctrl+s", "ctrl+s", Chord{Modifiers: []Modifier{ModCtrl}, Key: "s"}, false},
		{"cmd alt shift x", "cmd+alt+shift+X", Chord{Modifiers: []Modifier{ModCmd, ModAlt, ModShift}, Key: "x"}, false},
		{"whitespace tolerant", " ctrl + s ", Chord{Modifiers: []Modifier{ModCtrl}, Key: "s"}, false},
		{"win alias", "win+r", Chord{Modifiers: []Modifier{ModCmd}, Key: "r"}, false},
		{"meta alias", "meta+r", Chord{Modifiers: []Modifier{ModCmd}, Key: "r"}, false},
		{"super alias", "super+r", Chord{Modifiers: []Modifier{ModCmd}, Key: "r"}, false},
		{"control alias", "control+a", Chord{Modifiers: []Modifier{ModCtrl}, Key: "a"}, false},
		{"option alias", "option+tab", Chord{Modifiers: []Modifier{ModAlt}, Key: "tab"}, false},
		{"empty", "", Chord{}, true},
		{"trailing plus with nothing", "ctrl+", Chord{}, true},
		{"unknown modifier", "foo+s", Chord{}, true},
		{"unrecognized multi-char key", "foobar", Chord{}, true},
		{"literal plus key alone", "+", Chord{Key: "+"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseChord(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseChord(%q) expected error, got %+v", tt.in, got)
				}
				if de, ok := core.AsDesktopError(err); !ok || de.Code != core.ErrInvalidArgs {
					t.Fatalf("ParseChord(%q) expected INVALID_ARGS DesktopError, got %v", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseChord(%q) unexpected error: %v", tt.in, err)
			}
			if got.Key != tt.want.Key {
				t.Fatalf("ParseChord(%q).Key = %q, want %q", tt.in, got.Key, tt.want.Key)
			}
			if len(got.Modifiers) != len(tt.want.Modifiers) {
				t.Fatalf("ParseChord(%q).Modifiers = %v, want %v", tt.in, got.Modifiers, tt.want.Modifiers)
			}
			for i := range got.Modifiers {
				if got.Modifiers[i] != tt.want.Modifiers[i] {
					t.Fatalf("ParseChord(%q).Modifiers[%d] = %v, want %v", tt.in, i, got.Modifiers[i], tt.want.Modifiers[i])
				}
			}
		})
	}
}

func TestChordHasModifier(t *testing.T) {
	c := Chord{Modifiers: []Modifier{ModCtrl, ModShift}, Key: "a"}
	if !c.HasModifier(ModCtrl) || !c.HasModifier(ModShift) {
		t.Fatal("expected ctrl and shift to be present")
	}
	if c.HasModifier(ModAlt) {
		t.Fatal("did not expect alt to be present")
	}
}

func TestNamedKeys(t *testing.T) {
	keys := NamedKeys()
	if len(keys) == 0 {
		t.Fatal("expected a non-empty named key vocabulary")
	}
	want := map[string]bool{"enter": true, "tab": true, "escape": true, "space": true,
		"backspace": true, "delete": true, "up": true, "down": true, "left": true, "right": true,
		"home": true, "end": true, "pageup": true, "pagedown": true, "f1": true, "f12": true}
	got := make(map[string]bool, len(keys))
	for _, k := range keys {
		got[k] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("expected named key vocabulary to include %q", k)
		}
	}
}
