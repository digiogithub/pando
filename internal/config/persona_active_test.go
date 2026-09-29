package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func loadPersonaCfg(t *testing.T, global, project string) string {
	t.Helper()
	isolateGlobalConfig(t)
	home := os.Getenv("HOME")
	if global != "" {
		if err := os.WriteFile(filepath.Join(home, ".pando.toml"), []byte(global), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	if project != "" {
		if err := os.WriteFile(filepath.Join(dir, ".pando.toml"), []byte(project), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	viper.Reset()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return dir
}

func TestEffectiveActivePersonaPrecedence(t *testing.T) {
	loadPersonaCfg(t, "", "")
	if got := EffectiveActivePersona(); got != DefaultPersona {
		t.Fatalf("unset = %q, want %q", got, DefaultPersona)
	}

	loadPersonaCfg(t, "[Persona]\nActive = \"reviewer\"\n", "")
	if got := EffectiveActivePersona(); got != "reviewer" {
		t.Fatalf("global = %q, want reviewer", got)
	}

	loadPersonaCfg(t, "[Persona]\nActive = \"reviewer\"\n", "[Persona]\nActive = \"architect\"\n")
	if got := EffectiveActivePersona(); got != "architect" {
		t.Fatalf("project = %q, want architect", got)
	}

	loadPersonaCfg(t, "[Persona]\nActive = \"reviewer\"\n", "[Persona]\nActive = \"auto\"\n")
	if _, auto, set := ActivePersonaChoice(); !auto || !set {
		t.Fatalf("auto=%v set=%v, want both true", auto, set)
	}
	if got := EffectiveActivePersona(); got != "" {
		t.Fatalf("auto = %q, want empty", got)
	}
}

func TestUpdateActivePersonaRoundTrip(t *testing.T) {
	dir := loadPersonaCfg(t, "[Persona]\nActive = \"reviewer\"\n", "# project\n")

	if err := UpdateActivePersona("architect"); err != nil {
		t.Fatalf("UpdateActivePersona: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".pando.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "architect") {
		t.Fatalf("project file lacks persona: %s", data)
	}

	// Simulated restart: reload from disk.
	ResetForTests()
	viper.Reset()
	if _, err := Load(dir, false); err != nil {
		t.Fatal(err)
	}
	if got := EffectiveActivePersona(); got != "architect" {
		t.Fatalf("after restart = %q, want architect", got)
	}

	// Auto persists distinctly from unset.
	if err := UpdateActivePersona(""); err != nil {
		t.Fatal(err)
	}
	ResetForTests()
	viper.Reset()
	if _, err := Load(dir, false); err != nil {
		t.Fatal(err)
	}
	if _, auto, set := ActivePersonaChoice(); !auto || !set {
		t.Fatalf("auto=%v set=%v after restart", auto, set)
	}
}
