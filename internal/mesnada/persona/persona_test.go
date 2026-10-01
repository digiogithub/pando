package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/digiogithub/pando/internal/mesnada/persona/builtin"
)

func writePersona(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFrontMatterParsedAndStripped(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "fm", "---\ndescription: \"Handles billing questions\"\n---\n# Billing\n\nBody text.\n")
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := m.GetPersona("fm"), "# Billing\n\nBody text.\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if got := m.Description("fm"); got != "Handles billing questions" {
		t.Fatalf("description = %q", got)
	}
	if applied := m.ApplyPersona("fm", "do it"); strings.Contains(applied, "---") || strings.Contains(applied, "description") {
		t.Fatalf("front matter leaked into prompt: %q", applied)
	}
}

func TestNoFrontMatterUnchanged(t *testing.T) {
	dir := t.TempDir()
	raw := "# Plain Persona\n\nSome text\n---\nnot front matter\n"
	writePersona(t, dir, "plain", raw)
	m, _ := NewManager(dir)
	if got := m.GetPersona("plain"); got != raw {
		t.Fatalf("content changed: %q", got)
	}
	if got := m.Description("plain"); got != "Plain Persona" {
		t.Fatalf("description = %q", got)
	}
}

func TestUnterminatedFrontMatterUnchanged(t *testing.T) {
	dir := t.TempDir()
	raw := "---\ndescription: x\n# Title\n"
	writePersona(t, dir, "open", raw)
	m, _ := NewManager(dir)
	if got := m.GetPersona("open"); got != raw {
		t.Fatalf("content changed: %q", got)
	}
}

func TestDescriptionTruncatesRuneSafe(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("é世 ", 400)
	writePersona(t, dir, "long", "---\ndescription: \""+long+"\"\n---\nbody\n")
	m, _ := NewManager(dir)
	d := m.Description("long")
	if n := utf8.RuneCountInString(d); n > MaxDescriptionLen || n < MaxDescriptionLen-1 {
		t.Fatalf("rune count = %d", n)
	}
	if !utf8.ValidString(d) {
		t.Fatal("invalid UTF-8 after truncation")
	}
}

func TestDescriptionCollapsesWhitespace(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "ws", "\n\n##   Spaced   Title \t here\n")
	m, _ := NewManager(dir)
	if got := m.Description("ws"); got != "Spaced Title here" {
		t.Fatalf("description = %q", got)
	}
}

func TestUnknownPersonaDescription(t *testing.T) {
	m, _ := NewManager("")
	if got := m.Description("nope"); got != "" {
		t.Fatalf("got %q", got)
	}
	if len(m.Descriptions()) != 0 {
		t.Fatal("expected empty map")
	}
}

func TestUserOverrideReplacesDescription(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "qa", "---\ndescription: custom qa\n---\n# Custom\n")
	m, err := NewManagerWithBuiltins(builtin.FS, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Description("qa"); got != "custom qa" {
		t.Fatalf("description = %q", got)
	}

	// Override without front matter falls back to its own heading, not the built-in one.
	writePersona(t, dir, "qa", "# Override Heading\n")
	m, _ = NewManagerWithBuiltins(builtin.FS, dir)
	if got := m.Description("qa"); got != "Override Heading" {
		t.Fatalf("description = %q", got)
	}
}

func TestBuiltinDescriptions(t *testing.T) {
	m, err := NewManagerWithBuiltins(builtin.FS, "")
	if err != nil {
		t.Fatal(err)
	}
	descs := m.Descriptions()
	for _, name := range []string{"assistant", "qa", "software-engineer", "system-engineer"} {
		d := descs[name]
		if d == "" {
			t.Fatalf("%s has empty description", name)
		}
		if len(d) >= 300 {
			t.Errorf("%s description too long (%d)", name, len(d))
		}
		if strings.HasPrefix(m.GetPersona(name), "---") {
			t.Errorf("%s content still has front matter", name)
		}
	}
	seen := map[string]string{}
	for name, d := range descs {
		if other, dup := seen[d]; dup {
			t.Errorf("%s and %s share a description", name, other)
		}
		seen[d] = name
	}
	// Returned map is a copy.
	descs["qa"] = "mutated"
	if m.Description("qa") == "mutated" {
		t.Fatal("Descriptions returned internal state")
	}
}

func TestFrontMatterRobustness(t *testing.T) {
	cases := []struct {
		name, raw, content, desc string
	}{
		{"invalid yaml colon", "---\ndescription: Use for X: Y\n---\n# T\nbody\n", "# T\nbody\n", "Use for X: Y"},
		{"invalid yaml quoted", "---\ndescription: \"Use for X: Y\" z\n---\n# T\nbody\n", "# T\nbody\n", "\"Use for X: Y\" z"},
		{"description list", "---\ndescription: [a, b\n---\n# T\nbody\n", "# T\nbody\n", "[a, b"},
		{"invalid yaml no description", "---\nname: a: b\n---\n# Title\nbody\n", "# Title\nbody\n", "Title"},
		{"horizontal rule", "---\n# Title\n---\nbody\n", "---\n# Title\n---\nbody\n", "Title"},
		{"bom front matter", "\xef\xbb\xbf---\ndescription: hi\n---\nbody\n", "body\n", "hi"},
		{"bom plain", "\xef\xbb\xbf# Plain\nbody\n", "\xef\xbb\xbf# Plain\nbody\n", "Plain"},
		{"trailing spaces", "--- \t\ndescription: hi\n---  \nbody\n", "body\n", "hi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writePersona(t, dir, "p", tc.raw)
			m, err := NewManager(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := m.GetPersona("p"); got != tc.content {
				t.Fatalf("content = %q, want %q", got, tc.content)
			}
			got := m.Description("p")
			if got == "---" {
				t.Fatal("description must never be ---")
			}
			if got != tc.desc {
				t.Fatalf("description = %q, want %q", got, tc.desc)
			}
		})
	}
}
