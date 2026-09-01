package agui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/mesnada/persona"
)

// withTestPersonaManager installs a manager that knows the given persona names,
// restoring the previous global on cleanup.
func withTestPersonaManager(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte("persona body"), 0o644); err != nil {
			t.Fatalf("write persona %s: %v", name, err)
		}
	}
	mgr, err := persona.NewManager(dir)
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	prev := agent.GetPersonaManager()
	agent.SetPersonaManager(mgr)
	t.Cleanup(func() { agent.SetPersonaManager(prev) })
}

// TestConfigFromAppCarriesPersona: the config field must reach the adapter's
// resolved Config, or `pando agui-serve --persona` would silently do nothing.
func TestConfigFromAppCarriesPersona(t *testing.T) {
	if got := ConfigFromApp(config.AGUIConfig{Persona: "perfumer"}).Persona; got != "perfumer" {
		t.Fatalf("persona = %q, want %q", got, "perfumer")
	}
	if got := ConfigFromApp(config.AGUIConfig{}).Persona; got != "" {
		t.Fatalf("persona = %q, want empty", got)
	}
}

// TestValidatePersona: an unknown persona must fail at startup, not on the
// first run of a client that expects it.
func TestValidatePersona(t *testing.T) {
	withTestPersonaManager(t, "perfumer")

	if err := validatePersona(""); err != nil {
		t.Fatalf("empty persona must validate: %v", err)
	}
	if err := validatePersona("perfumer"); err != nil {
		t.Fatalf("known persona must validate: %v", err)
	}
	if err := validatePersona("nope"); err == nil {
		t.Fatal("unknown persona must fail")
	}
}

// TestApplySessionPersonaScopesOverridePerSession: the configured persona must
// land in the session's override and leave the process-wide active persona
// alone, so a desktop sharing the process keeps its own.
func TestApplySessionPersonaScopesOverridePerSession(t *testing.T) {
	sessionID := "agui-persona-test-session"
	t.Cleanup(func() { agent.SetSessionLLMOverrides(sessionID, agent.SessionLLMOverrides{}) })

	r := newTestRuntime(testConfig(), "secret")
	r.applySessionPersona(sessionID)
	if got := agent.SessionLLMOverridesFor(sessionID); got.PersonaScoped || got.Persona != "" {
		t.Fatalf("a runtime without a persona must not install overrides, got %+v", got)
	}

	cfg := testConfig()
	cfg.Persona = "perfumer"
	r = newTestRuntime(cfg, "secret")
	r.applySessionPersona(sessionID)

	got := agent.SessionLLMOverridesFor(sessionID)
	if !got.PersonaScoped || got.Persona != "perfumer" {
		t.Fatalf("override = %+v, want persona %q scoped", got, "perfumer")
	}
	if active := agent.GetActivePersona(); active != "" {
		t.Fatalf("global active persona = %q, want empty (per-session only)", active)
	}
}

// TestApplySessionPersonaMergesExistingOverrides: installing the persona must
// not drop override fields another surface already wrote for the session.
func TestApplySessionPersonaMergesExistingOverrides(t *testing.T) {
	sessionID := "agui-persona-merge-session"
	t.Cleanup(func() { agent.SetSessionLLMOverrides(sessionID, agent.SessionLLMOverrides{}) })

	agent.SetSessionLLMOverrides(sessionID, agent.SessionLLMOverrides{ReasoningEffort: "high"})

	cfg := testConfig()
	cfg.Persona = "perfumer"
	r := newTestRuntime(cfg, "secret")
	r.applySessionPersona(sessionID)

	got := agent.SessionLLMOverridesFor(sessionID)
	if got.Persona != "perfumer" || !got.PersonaScoped || got.ReasoningEffort != "high" {
		t.Fatalf("override = %+v, want persona merged with reasoning effort high", got)
	}
}
