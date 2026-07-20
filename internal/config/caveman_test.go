package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

func TestCavemanDefaultMode(t *testing.T) {
	cases := []struct {
		field string
		want  string
	}{
		{"", ""},
		{"lite", "lite"},
		{"FULL", "full"},
		{" ultra ", "ultra"},
		{"wenyan", ""},
		{"bogus", ""},
	}
	for _, tc := range cases {
		c := &Config{Caveman: CavemanConfig{DefaultMode: tc.field}}
		if got := c.CavemanDefaultMode(); got != tc.want {
			t.Errorf("CavemanDefaultMode(%q) = %q, want %q", tc.field, got, tc.want)
		}
	}
}

func TestCavemanDefaultModeNilConfig(t *testing.T) {
	var c *Config
	if got := c.CavemanDefaultMode(); got != "" {
		t.Errorf("nil config = %q, want empty", got)
	}
}

func TestDefaultConfigTemplateShipsCavemanOff(t *testing.T) {
	assignment := regexp.MustCompile(`(?m)^DefaultMode\s*=\s*''\s*$`)
	if !assignment.MatchString(templateSection(t, "Caveman")) {
		t.Error("DefaultConfigTemplate: [Caveman] should ship DefaultMode = '' (off)")
	}
}

// withCavemanConfigFile installs a global config backed by a real .pando.toml in a
// temp working dir, so UpdateCaveman exercises the actual persistence path.
func withCavemanConfigFile(t *testing.T, body string) (dir, file string) {
	t.Helper()
	dir = t.TempDir()
	file = filepath.Join(dir, ".pando.toml")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	prev := cfg
	cfg = &Config{WorkingDir: dir, Debug: true}
	t.Cleanup(func() { cfg = prev })
	return dir, file
}

func TestUpdateCavemanPersistsToTOML(t *testing.T) {
	_, file := withCavemanConfigFile(t, "Debug = true\n")

	if err := UpdateCaveman(CavemanConfig{DefaultMode: "ULTRA"}); err != nil {
		t.Fatalf("UpdateCaveman: %v", err)
	}

	// Normalized in memory.
	if got := Get().CavemanDefaultMode(); got != "ultra" {
		t.Fatalf("in-memory default = %q, want %q", got, "ultra")
	}

	// And on disk, without losing unrelated fields.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read back config: %v", err)
	}
	var saved Config
	if err := toml.Unmarshal(data, &saved); err != nil {
		t.Fatalf("parse saved TOML: %v", err)
	}
	if got := saved.Caveman.DefaultMode; got != "ultra" {
		t.Errorf("persisted DefaultMode = %q, want %q", got, "ultra")
	}
	if !saved.Debug {
		t.Error("UpdateCaveman dropped the unrelated Debug field")
	}
}

func TestUpdateCavemanRejectsInvalidMode(t *testing.T) {
	_, file := withCavemanConfigFile(t, "Debug = true\n")
	cfg.Caveman = CavemanConfig{DefaultMode: "lite"}

	err := UpdateCaveman(CavemanConfig{DefaultMode: "shouty"})
	if err == nil {
		t.Fatal("expected an error for an unsupported level")
	}
	if got := Get().CavemanDefaultMode(); got != "lite" {
		t.Errorf("in-memory default changed to %q on a rejected update", got)
	}

	data, _ := os.ReadFile(file)
	if strings.Contains(string(data), "shouty") {
		t.Error("a rejected level must not reach the config file")
	}
}

func TestUpdateCavemanOffClearsTheDefault(t *testing.T) {
	withCavemanConfigFile(t, "Debug = true\n")
	cfg.Caveman = CavemanConfig{DefaultMode: "full"}

	if err := UpdateCaveman(CavemanConfig{DefaultMode: "off"}); err != nil {
		t.Fatalf("UpdateCaveman(off): %v", err)
	}
	if got := Get().CavemanDefaultMode(); got != "" {
		t.Errorf("default = %q after off, want empty", got)
	}
}

func TestUpdateCavemanRollsBackOnWriteFailure(t *testing.T) {
	// An unparseable config file makes the persistence step fail; the in-memory
	// config must not keep a value that never reached disk.
	withCavemanConfigFile(t, "this is = = not toml\n")
	cfg.Caveman = CavemanConfig{DefaultMode: "lite"}

	if err := UpdateCaveman(CavemanConfig{DefaultMode: "ultra"}); err == nil {
		t.Fatal("expected a persistence error")
	}
	if got := Get().CavemanDefaultMode(); got != "lite" {
		t.Errorf("in-memory default = %q after a failed write, want the previous %q", got, "lite")
	}
}

func TestLoadedTOMLWithoutCavemanSectionIsOff(t *testing.T) {
	var loaded Config
	if err := toml.Unmarshal([]byte("Debug = true\n"), &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := loaded.CavemanDefaultMode(); got != "" {
		t.Errorf("absent [Caveman] section = %q, want off", got)
	}
}
