package config

import (
	"strings"
	"testing"
)

func serverByName(servers []ResolvedLSPServer, name string) (ResolvedLSPServer, bool) {
	for _, s := range servers {
		if s.Name == name {
			return s, true
		}
	}
	return ResolvedLSPServer{}, false
}

func extHandledBy(servers []ResolvedLSPServer, name string) bool {
	_, ok := serverByName(servers, name)
	return ok
}

// With no user config, the built-in presets act as the default catalogue and
// are all available for activation, but none autostart.
func TestLSPRegistry_DefaultsFromPresets(t *testing.T) {
	c := &Config{}

	reg := c.LSPRegistry()
	if len(reg) != len(LSPPresets()) {
		t.Fatalf("expected %d servers, got %d", len(LSPPresets()), len(reg))
	}

	gopls, ok := serverByName(reg, "gopls")
	if !ok {
		t.Fatal("gopls preset missing from registry")
	}
	if gopls.Command != "gopls" || gopls.Source != "preset" {
		t.Fatalf("unexpected gopls resolution: %+v", gopls)
	}
	if gopls.Disabled || gopls.Autostart {
		t.Fatalf("preset should default to enabled & lazy: %+v", gopls)
	}

	if got := c.LSPServersForExt(".go"); !extHandledBy(got, "gopls") {
		t.Fatalf(".go should be handled by gopls, got %+v", got)
	}
	if got := c.LSPAutostartServers(); len(got) != 0 {
		t.Fatalf("no server should autostart by default, got %+v", got)
	}
}

// A user entry sharing a preset name inherits the preset command/args/languages
// when left empty, so enabling autostart needs only the one field.
func TestLSPRegistry_UserOverrideInheritsPreset(t *testing.T) {
	c := &Config{LSP: map[string]LSPConfig{
		"gopls": {Autostart: true},
	}}

	gopls, ok := serverByName(c.LSPRegistry(), "gopls")
	if !ok {
		t.Fatal("gopls missing")
	}
	if gopls.Command != "gopls" {
		t.Fatalf("expected inherited command 'gopls', got %q", gopls.Command)
	}
	if !gopls.Autostart || gopls.Source != "user+preset" {
		t.Fatalf("unexpected resolution: %+v", gopls)
	}
	if !gopls.HandlesExt(".go") {
		t.Fatalf("expected inherited .go language, got %+v", gopls.Languages)
	}

	auto := c.LSPAutostartServers()
	if !extHandledBy(auto, "gopls") {
		t.Fatalf("gopls should autostart, got %+v", auto)
	}
}

// Disabling a preset removes it from the candidates for its extension while
// leaving other servers for the same language available.
func TestLSPRegistry_DisablePreset(t *testing.T) {
	c := &Config{LSP: map[string]LSPConfig{
		"pyright": {Disabled: true},
	}}

	py := c.LSPServersForExt(".py")
	if extHandledBy(py, "pyright") {
		t.Fatalf("disabled pyright should not handle .py: %+v", py)
	}
	if !extHandledBy(py, "pylsp") {
		t.Fatalf("pylsp should still handle .py: %+v", py)
	}
}

// A user-only server (no matching preset) is appended and matched by its
// configured extension, including extensions written without a leading dot.
func TestLSPRegistry_UserOnlyServerAndExtNormalization(t *testing.T) {
	c := &Config{LSP: map[string]LSPConfig{
		"myls": {Command: "myls", Languages: []string{"foo", ".BAR"}},
	}}

	s, ok := serverByName(c.LSPRegistry(), "myls")
	if !ok {
		t.Fatal("user-only server missing")
	}
	if s.Source != "user" {
		t.Fatalf("expected source 'user', got %q", s.Source)
	}

	if got := c.LSPServersForExt(".foo"); !extHandledBy(got, "myls") {
		t.Fatalf(".foo should match myls, got %+v", got)
	}
	if got := c.LSPServersForExt(".bar"); !extHandledBy(got, "myls") {
		t.Fatalf(".bar (case-insensitive) should match myls, got %+v", got)
	}
	if got := c.LSPServersForExt(".nope"); extHandledBy(got, "myls") {
		t.Fatalf(".nope should not match myls, got %+v", got)
	}
}

// Every built-in preset must have a unique name, a plausible single-token
// binary name (no spaces, not a path), and at least one normalized file
// extension it claims to handle. This catches malformed/invented entries like
// a command string that was never a real installable binary.
func TestLSPPresets_Sane(t *testing.T) {
	seen := make(map[string]bool)
	for _, p := range LSPPresets() {
		if p.Name == "" {
			t.Fatalf("preset with empty name: %+v", p)
		}
		if seen[p.Name] {
			t.Fatalf("duplicate preset name %q", p.Name)
		}
		seen[p.Name] = true

		cmd := p.Config.Command
		if cmd == "" {
			t.Fatalf("preset %q has empty Command", p.Name)
		}
		if strings.ContainsAny(cmd, " \t/\\") {
			t.Fatalf("preset %q Command %q looks like a path or has whitespace, expected a bare binary name", p.Name, cmd)
		}

		if len(p.Config.Languages) == 0 {
			t.Fatalf("preset %q declares no Languages", p.Name)
		}
		for _, ext := range p.Config.Languages {
			if !strings.HasPrefix(ext, ".") {
				t.Fatalf("preset %q language %q must start with '.'", p.Name, ext)
			}
			if ext != strings.ToLower(ext) {
				t.Fatalf("preset %q language %q must be lowercase", p.Name, ext)
			}
		}
	}
}
