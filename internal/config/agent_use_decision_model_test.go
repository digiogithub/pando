package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadDecisionModelProject(t *testing.T, agentsToml string) string {
	t.Helper()
	isolateGlobalConfig(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-fake")
	dir := t.TempDir()
	toml := "[Providers.anthropic]\nAPIKey = 'sk-test-fake'\n\n[Agents.coder]\nModel = 'anthropic.claude-3-5-sonnet-20241022'\n" + agentsToml
	if err := os.WriteFile(filepath.Join(dir, ".pando.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return dir
}

func TestPersonaSelectorUsesDecisionModelNilConfig(t *testing.T) {
	isolateGlobalConfig(t)
	if PersonaSelectorUsesDecisionModel() {
		t.Fatal("helper must be false when config is not loaded")
	}
}

func TestUpdateAgentUseDecisionModelRoundTrip(t *testing.T) {
	dir := loadDecisionModelProject(t, "")
	if PersonaSelectorUsesDecisionModel() {
		t.Fatal("flag must default to false")
	}
	if err := UpdateAgentUseDecisionModel(AgentPersonaSelector, true); err != nil {
		t.Fatalf("UpdateAgentUseDecisionModel: %v", err)
	}
	if !PersonaSelectorUsesDecisionModel() {
		t.Fatal("flag not set in memory")
	}

	ResetForTests()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !PersonaSelectorUsesDecisionModel() {
		t.Fatal("flag lost after reload")
	}

	if err := UpdateAgentUseDecisionModel(AgentPersonaSelector, false); err != nil {
		t.Fatalf("UpdateAgentUseDecisionModel(false): %v", err)
	}
	ResetForTests()
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if PersonaSelectorUsesDecisionModel() {
		t.Fatal("flag should be false after clearing")
	}
}

func TestUpdateAgentUseDecisionModelRejectsOtherAgents(t *testing.T) {
	loadDecisionModelProject(t, "")
	if err := UpdateAgentUseDecisionModel(AgentCoder, true); err == nil {
		t.Fatal("expected error for non persona-selector agent")
	}
	if Get().Agents[AgentCoder].UseDecisionModel {
		t.Fatal("coder flag must stay false")
	}
}

func TestUseDecisionModelIgnoredOnOtherAgent(t *testing.T) {
	loadDecisionModelProject(t, "\n[Agents.title]\nUseDecisionModel = true\n")
	if Get().Agents[AgentTitle].UseDecisionModel {
		t.Fatal("flag on title agent must be cleared on load")
	}
}

func TestPersonaSelectorKeepsFlagWithoutModel(t *testing.T) {
	dir := loadDecisionModelProject(t, "\n[Agents.persona-selector]\nUseDecisionModel = true\n")
	if !PersonaSelectorUsesDecisionModel() {
		t.Fatal("flag lost on load")
	}
	if _, ok := Get().Agents[AgentPersonaSelector]; !ok {
		t.Fatal("persona-selector agent dropped")
	}
	// Persisting must not require a model either.
	if err := UpdateAgentUseDecisionModel(AgentPersonaSelector, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".pando.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(data)), "usedecisionmodel = true") {
		t.Fatalf("flag not persisted in file:\n%s", data)
	}
}
