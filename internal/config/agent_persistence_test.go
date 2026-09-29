package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
)

func loadPersistenceProject(t *testing.T) string {
	t.Helper()
	isolateGlobalConfig(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-fake")
	dir := t.TempDir()
	toml := "AutoCompact = true\n\n[Providers.anthropic]\nAPIKey = 'sk-test-fake'\n\n[Agents.coder]\nModel = 'anthropic.claude-3-5-sonnet-20241022'\n"
	if err := os.WriteFile(filepath.Join(dir, ".pando.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return dir
}

func TestUpdateAgentModelKeepsAutoCompact(t *testing.T) {
	dir := loadPersistenceProject(t)

	on := true
	agent := Get().Agents[AgentCoder]
	agent.AutoCompact = &on
	agent.AutoCompactThreshold = 0.6
	agent.ContextWindowOverride = 123456
	if err := UpdateAgent(AgentCoder, agent); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	if err := UpdateAgentModel(AgentCoder, models.Claude35Haiku); err != nil {
		t.Fatalf("UpdateAgentModel: %v", err)
	}

	check := func(where string) {
		t.Helper()
		got := Get().Agents[AgentCoder]
		if got.Model != models.Claude35Haiku {
			t.Fatalf("%s: model = %q", where, got.Model)
		}
		if got.AutoCompact == nil || !*got.AutoCompact {
			t.Fatalf("%s: AutoCompact lost: %v", where, got.AutoCompact)
		}
		if got.AutoCompactThreshold != 0.6 || got.ContextWindowOverride != 123456 {
			t.Fatalf("%s: threshold/override lost: %+v", where, got)
		}
	}
	check("memory")

	ResetForTests()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("reload: %v", err)
	}
	check("reloaded")
}

func TestResolveAutoCompactPrecedence(t *testing.T) {
	on, off := true, false
	cases := []struct {
		global bool
		agent  *bool
		want   bool
	}{
		{true, nil, true}, {false, nil, false},
		{true, &off, false}, {false, &on, true},
	}
	for _, c := range cases {
		if got := ResolveAutoCompact(c.global, Agent{AutoCompact: c.agent}); got != c.want {
			t.Fatalf("global=%v agent=%v: got %v want %v", c.global, c.agent, got, c.want)
		}
	}
}

func TestUpdateLSPKeepsAutostartLanguagesFilenames(t *testing.T) {
	dir := loadPersistenceProject(t)

	in := LSPConfig{
		Command:   "gopls",
		Args:      []string{"serve"},
		Languages: []string{".go"},
		Filenames: []string{"go.mod"},
		Autostart: true,
	}
	if err := UpdateLSP("go", in); err != nil {
		t.Fatalf("UpdateLSP: %v", err)
	}
	check := func(where string) {
		t.Helper()
		got := Get().LSP["go"]
		if !got.Autostart || !reflect.DeepEqual(got.Languages, in.Languages) || !reflect.DeepEqual(got.Filenames, in.Filenames) {
			t.Fatalf("%s: LSP fields lost: %+v", where, got)
		}
	}
	check("memory")

	ResetForTests()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("reload: %v", err)
	}
	check("reloaded")
}
