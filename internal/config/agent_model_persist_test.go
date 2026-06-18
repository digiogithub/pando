package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/spf13/viper"
)

// TestAgentModelSurvivesReloadAfterResolve verifies that an agent model saved with
// a non-canonical (bare) ID is resolved to its registered canonical form during
// validation instead of being silently reverted to a provider default. This mirrors
// the web-UI persistence bug where selecting e.g. "gpt-5.4-mini" lost the change on
// the next config reload because the registry only knew "copilot.gpt-5.4-mini".
func TestAgentModelSurvivesReloadAfterResolve(t *testing.T) {
	// Register the canonical model as the model cache / app refresh would.
	canonical := models.ModelID("copilot.gpt-5.4-mini")
	models.RegisterDynamicModel(models.Model{
		ID:               canonical,
		Name:             "Copilot: gpt-5.4-mini",
		Provider:         models.ProviderCopilot,
		APIModel:         "gpt-5.4-mini",
		ContextWindow:    128_000,
		DefaultMaxTokens: 4096,
	})
	t.Cleanup(func() { delete(models.SupportedModels, canonical) })

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".pando.toml")
	// Config holds the bare ID, as written by the buggy web-UI path.
	original := "[Agents]\n[Agents.coder]\nModel = 'gpt-5.4-mini'\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })
	if _, err := Load(tmpDir, false); err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if got := cfg.Agents[AgentCoder].Model; got != canonical {
		t.Fatalf("coder model = %q, want %q (model was reverted instead of resolved)", got, canonical)
	}
}

// TestResolveModelID covers the resolver used to repair non-canonical IDs.
func TestResolveModelID(t *testing.T) {
	canonical := models.ModelID("copilot.gpt-5.4-mini")
	models.RegisterDynamicModel(models.Model{
		ID:       canonical,
		Provider: models.ProviderCopilot,
		APIModel: "gpt-5.4-mini",
	})
	t.Cleanup(func() { delete(models.SupportedModels, canonical) })

	if got, ok := models.ResolveModelID("gpt-5.4-mini"); !ok || got != canonical {
		t.Fatalf("ResolveModelID(bare) = %q,%v want %q,true", got, ok, canonical)
	}
	if got, ok := models.ResolveModelID(canonical); !ok || got != canonical {
		t.Fatalf("ResolveModelID(canonical) = %q,%v want %q,true", got, ok, canonical)
	}
	if got, ok := models.ResolveModelID("totally-unknown-model"); ok {
		t.Fatalf("ResolveModelID(unknown) = %q,%v want input,false", got, ok)
	}
}
