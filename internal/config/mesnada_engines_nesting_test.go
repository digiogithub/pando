package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeMesnadaDisabledEngines(t *testing.T) {
	got := NormalizeMesnadaDisabledEngines([]string{" Copilot ", "claude-code", "claude", "pando", "warm-acp", "acp-server", "", "gemini-cli"})
	want := []string{"claude", "copilot", "gemini"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeMesnadaDisabledEngines = %v, want %v", got, want)
	}
	if got := NormalizeMesnadaDisabledEngines([]string{"pando", " "}); got != nil {
		t.Fatalf("only natives/blanks should normalize to nil, got %v", got)
	}
}

// Native engines in a hand-edited config are stripped on load, and the default
// (no DisabledEngines key) leaves every engine enabled.
func TestDisabledEnginesNormalizedOnLoad(t *testing.T) {
	loadTempConfig(t, "[Mesnada.Orchestrator]\nDisabledEngines = ['pando', 'Claude', 'copilot', 'claude']\n")
	want := []string{"claude", "copilot"}
	if got := cfg.Mesnada.Orchestrator.DisabledEngines; !reflect.DeepEqual(got, want) {
		t.Fatalf("DisabledEngines = %v, want %v", got, want)
	}

	loadTempConfig(t, "[Mesnada]\nEnabled = true\n")
	if got := cfg.Mesnada.Orchestrator.DisabledEngines; len(got) != 0 {
		t.Fatalf("default DisabledEngines = %v, want empty", got)
	}
}

func TestUpdateMesnadaOrchestratorNormalizesDisabledEngines(t *testing.T) {
	configPath := loadTempConfig(t, "[Mesnada]\nEnabled = true\n")
	orch := cfg.Mesnada.Orchestrator
	orch.DisabledEngines = []string{"warm-acp", "gemini", "claude-code"}
	if err := UpdateMesnadaOrchestrator(orch); err != nil {
		t.Fatalf("UpdateMesnadaOrchestrator: %v", err)
	}
	want := []string{"claude", "gemini"}
	if got := cfg.Mesnada.Orchestrator.DisabledEngines; !reflect.DeepEqual(got, want) {
		t.Fatalf("in-memory DisabledEngines = %v, want %v", got, want)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "warm-acp") || !strings.Contains(string(data), "gemini") {
		t.Fatalf("persisted config not normalized:\n%s", data)
	}
}

// allowNestedSubagents defaults to true when unset, including the viper
// nested-default shadowing case (a [Mesnada] key but no [Mesnada.Delegation]
// table), and an explicit false survives load and a save round trip.
func TestAllowNestedSubagentsDefaultAndPersistence(t *testing.T) {
	loadTempConfig(t, "[Mesnada]\nEnabled = true\n")
	d := cfg.Mesnada.Delegation
	if !d.NestedSubagentsAllowed() {
		t.Fatal("unset allowNestedSubagents must read as true")
	}
	if got := d.EffectiveMaxDepth(); got != defaultDelegationMaxDepth {
		t.Fatalf("EffectiveMaxDepth = %d, want %d", got, defaultDelegationMaxDepth)
	}

	loadTempConfig(t, "[Mesnada.Delegation]\nMaxDepth = 5\nAllowNestedSubagents = false\n")
	d = cfg.Mesnada.Delegation
	if d.NestedSubagentsAllowed() {
		t.Fatal("explicit allowNestedSubagents=false was lost on load")
	}
	if got := d.EffectiveMaxDepth(); got != 1 {
		t.Fatalf("EffectiveMaxDepth with nested disallowed = %d, want 1", got)
	}

	configPath := loadTempConfig(t, "[Mesnada]\nEnabled = true\n")
	off := false
	d = cfg.Mesnada.Delegation
	d.AllowNestedSubagents = &off
	if err := UpdateMesnadaDelegation(d); err != nil {
		t.Fatalf("UpdateMesnadaDelegation: %v", err)
	}
	viperReload(t, configPath)
	if cfg.Mesnada.Delegation.NestedSubagentsAllowed() {
		t.Fatal("allowNestedSubagents=false did not survive a save + reload")
	}
}

// viperReload reloads the configuration from the directory holding path.
func viperReload(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loadTempConfig(t, string(body))
}
