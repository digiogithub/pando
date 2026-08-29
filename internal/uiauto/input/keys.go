// Package input implements the cross-platform physical (synthetic)
// mouse/keyboard input layer of the Pando Desktop Controller. It is the
// PhysicalInput fallback the core.ActionResolver uses when a backend cannot
// perform an action natively (see internal/uiauto/core/action.go).
//
// Each platform-specific file (input_windows.go, input_linux.go,
// input_darwin.go, input_other.go) provides its own New() (core.PhysicalInput,
// error) factory and CapabilitiesProbe() function, selected at compile time
// by //go:build tags. No file in this package uses cgo.
package input

import (
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// Modifier identifies a keyboard modifier used in a chord such as "ctrl+s".
type Modifier string

const (
	ModCtrl Modifier = "ctrl"
	ModAlt  Modifier = "alt"
	// ModShift is the shift modifier.
	ModShift Modifier = "shift"
	// ModCmd is the platform "command" modifier: Cmd on macOS, Super/Win on
	// Linux/Windows. Accepts the aliases cmd/command/meta/win/windows/super
	// on input; always canonicalized to ModCmd.
	ModCmd Modifier = "cmd"
)

// Chord is a parsed key press request: zero or more modifiers plus a single
// canonical key name. Key is either one of the named keys in keyAliases
// (e.g. "enter", "f1", "pageup") or a single-rune printable character
// (lowercased letters keep their case-insensitivity; the caller is
// responsible for applying ModShift to get an uppercase letter or shifted
// symbol on platforms where that matters).
type Chord struct {
	Modifiers []Modifier
	Key       string
}

// HasModifier reports whether m is present in the chord.
func (c Chord) HasModifier(m Modifier) bool {
	for _, x := range c.Modifiers {
		if x == m {
			return true
		}
	}
	return false
}

// modifierAliases maps every accepted modifier spelling (case-insensitive)
// to its canonical Modifier.
var modifierAliases = map[string]Modifier{
	"ctrl": ModCtrl, "control": ModCtrl,
	"alt": ModAlt, "option": ModAlt, "opt": ModAlt,
	"shift": ModShift,
	"cmd":   ModCmd, "command": ModCmd, "meta": ModCmd,
	"win": ModCmd, "windows": ModCmd, "super": ModCmd,
}

// namedKeys is the platform-independent vocabulary of non-printable key
// names accepted by PressKey/ParseChord, mapping every accepted spelling
// (case-insensitive) to its canonical name. A key not listed here is
// assumed to be a single printable rune (a letter, digit or punctuation
// mark) and is canonicalized to its lowercase form.
var namedKeys = map[string]string{
	"enter": "enter", "return": "enter",
	"tab": "tab",
	"esc": "escape", "escape": "escape",
	"space": "space", "spacebar": "space",
	"backspace": "backspace", "bs": "backspace",
	"delete": "delete", "del": "delete",
	"up": "up", "arrowup": "up",
	"down": "down", "arrowdown": "down",
	"left": "left", "arrowleft": "left",
	"right": "right", "arrowright": "right",
	"home":   "home",
	"end":    "end",
	"pageup": "pageup", "pgup": "pageup",
	"pagedown": "pagedown", "pgdn": "pagedown", "pgdown": "pagedown",
	"insert": "insert", "ins": "insert",
	"capslock": "capslock",
	"f1":       "f1", "f2": "f2", "f3": "f3", "f4": "f4",
	"f5": "f5", "f6": "f6", "f7": "f7", "f8": "f8",
	"f9": "f9", "f10": "f10", "f11": "f11", "f12": "f12",
}

// NamedKeys returns the set of canonical named-key identifiers (excluding
// single printable characters), sorted is not guaranteed.
func NamedKeys() []string {
	seen := make(map[string]bool, len(namedKeys))
	out := make([]string, 0, len(namedKeys))
	for _, canon := range namedKeys {
		if !seen[canon] {
			seen[canon] = true
			out = append(out, canon)
		}
	}
	return out
}

// ParseChord parses a key/chord identifier such as "s", "Enter", "ctrl+s",
// or "cmd+alt+shift+x" into a Chord with canonicalized modifiers and key
// name. It is case-insensitive and tolerant of surrounding whitespace
// around "+"-separated parts. An empty string, a chord with no key
// component (e.g. "ctrl+"), or an unrecognized modifier returns an
// INVALID_ARGS *core.DesktopError.
func ParseChord(s string) (Chord, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return Chord{}, core.NewInvalidArgsError("key/chord must not be empty")
	}
	if raw == "+" {
		// The literal "+" key, with no modifiers.
		return Chord{Key: "+"}, nil
	}
	parts := strings.Split(raw, "+")
	trimmed := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return Chord{}, core.NewInvalidArgsError("key/chord " + strconv.Quote(s) + " has an empty part")
		}
		trimmed = append(trimmed, p)
	}
	if len(trimmed) == 0 {
		return Chord{}, core.NewInvalidArgsError("key/chord " + strconv.Quote(s) + " has no key component")
	}

	var mods []Modifier
	keyPart := trimmed[len(trimmed)-1]
	for _, m := range trimmed[:len(trimmed)-1] {
		canon, ok := modifierAliases[strings.ToLower(m)]
		if !ok {
			return Chord{}, core.NewInvalidArgsError("unknown modifier " + strconv.Quote(m) + " in key/chord " + strconv.Quote(s))
		}
		mods = append(mods, canon)
	}

	key, err := canonicalKeyName(keyPart)
	if err != nil {
		return Chord{}, err
	}
	return Chord{Modifiers: mods, Key: key}, nil
}

// canonicalKeyName canonicalizes a single (non-modifier) key token: a
// recognized named key, or a single printable rune lowercased.
func canonicalKeyName(tok string) (string, error) {
	lower := strings.ToLower(tok)
	if canon, ok := namedKeys[lower]; ok {
		return canon, nil
	}
	runes := []rune(tok)
	if len(runes) == 1 {
		return strings.ToLower(tok), nil
	}
	return "", core.NewInvalidArgsError("unrecognized key name " + strconv.Quote(tok))
}
