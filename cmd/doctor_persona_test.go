package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
	"github.com/digiogithub/pando/internal/mesnada/persona"
)

func personaDoctorCfg(baseURL, routerModel string, decision bool, agentModel models.ModelID) *config.Config {
	return &config.Config{
		Providers:         map[models.ModelProvider]config.Provider{models.ProviderAnthropic: {APIKey: "k"}},
		PersonaAutoSelect: config.PersonaAutoSelectConfig{Enabled: true},
		Agents: map[config.AgentName]config.Agent{
			config.AgentPersonaSelector: {Model: agentModel, UseDecisionModel: decision},
		},
		ModelAutoMode: config.ModelAutoModeConfig{
			Router: config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: baseURL, Model: routerModel},
		},
	}
}

func personaMgr(t *testing.T, n int) *persona.Manager {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		name := "p" + strings.Repeat("x", i) + ".md"
		if err := os.WriteFile(filepath.Join(dir, name), []byte("---\ndescription: d\n---\n# P\nbody"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mgr, err := persona.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

func runPersonaDoctor(t *testing.T, cfg *config.Config, mgr *persona.Manager) (string, int) {
	t.Helper()
	var buf bytes.Buffer
	n := doctorPersonaAutoSelect(context.Background(), cfg, mgr, &buf)
	return buf.String(), n
}

func TestDoctorPersonaAutoSelect(t *testing.T) {
	good := systemonetest.NewOllama035(t)

	// Disabled: one neutral line.
	out, n := runPersonaDoctor(t, &config.Config{}, nil)
	if n != 0 || !strings.Contains(out, "disabled") || strings.Count(out, "\n") != 2 {
		t.Fatalf("disabled: n=%d out=%s", n, out)
	}

	// Option off: the agent's model is what selects.
	out, n = runPersonaDoctor(t, personaDoctorCfg(good.URL, "", false, models.Claude35Haiku), personaMgr(t, 3))
	if n != 0 || !strings.Contains(out, "3 persona(s) offered") || !strings.Contains(out, "decision model: off") {
		t.Fatalf("option off: n=%d\n%s", n, out)
	}
	out, n = runPersonaDoctor(t, personaDoctorCfg(good.URL, "", false, ""), personaMgr(t, 1))
	if n == 0 || !strings.Contains(out, "no model") {
		t.Fatalf("option off without model: n=%d\n%s", n, out)
	}

	// Healthy router.
	out, n = runPersonaDoctor(t, personaDoctorCfg(good.URL, "tev1:0.8b", true, models.Claude35Haiku), personaMgr(t, 2))
	if n != 0 {
		t.Fatalf("healthy setup reported %d problems:\n%s", n, out)
	}
	for _, want := range []string{"decision model option on", "reachable", "decision-capable", "fallback model"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}

	// No router model.
	out, n = runPersonaDoctor(t, personaDoctorCfg(good.URL, "", true, models.Claude35Haiku), personaMgr(t, 1))
	if n == 0 || !strings.Contains(out, "no router model") {
		t.Fatalf("no router model: n=%d\n%s", n, out)
	}

	// Unreachable router.
	out, n = runPersonaDoctor(t, personaDoctorCfg("http://127.0.0.1:1", "tev1:0.8b", true, models.Claude35Haiku), personaMgr(t, 1))
	if n == 0 || !strings.Contains(out, "not reachable") {
		t.Fatalf("unreachable: n=%d\n%s", n, out)
	}

	// Missing fallback model is only a warning when the router is the main path.
	out, n = runPersonaDoctor(t, personaDoctorCfg(good.URL, "tev1:0.8b", true, ""), personaMgr(t, 1))
	if n != 0 || !strings.Contains(out, "no fallback model") {
		t.Fatalf("no fallback: n=%d\n%s", n, out)
	}

	// More than 25 personas.
	out, n = runPersonaDoctor(t, personaDoctorCfg(good.URL, "tev1:0.8b", true, models.Claude35Haiku), personaMgr(t, 28))
	if n != 0 || !strings.Contains(out, "3 dropped by the 25-persona cap") {
		t.Fatalf("cap: n=%d\n%s", n, out)
	}
}
