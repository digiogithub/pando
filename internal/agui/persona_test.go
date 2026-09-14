package agui

import (
	"os"
	"path/filepath"
	"sync"
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
	r.applySessionOverrides(sessionID, nil)
	if got := agent.SessionLLMOverridesFor(sessionID); got.PersonaScoped || got.Persona != "" {
		t.Fatalf("a runtime without a persona must not install overrides, got %+v", got)
	}

	cfg := testConfig()
	cfg.Persona = "perfumer"
	r = newTestRuntime(cfg, "secret")
	r.applySessionOverrides(sessionID, nil)

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
	r.applySessionOverrides(sessionID, nil)

	got := agent.SessionLLMOverridesFor(sessionID)
	if got.Persona != "perfumer" || !got.PersonaScoped || got.ReasoningEffort != "high" {
		t.Fatalf("override = %+v, want persona merged with reasoning effort high", got)
	}
}

// PANDO-US-0014: "Per-profile persona, prompt and model override via
// SetSessionLLMOverrides".

// TestApplySessionOverrides_ProfileOverridesPersonaPromptModel: a profile's
// Persona/Prompt/Model reach the session override in place of the
// adapter-wide Persona, which a profile is declared to override.
func TestApplySessionOverrides_ProfileOverridesPersonaPromptModel(t *testing.T) {
	withTestPersonaManager(t, "backlog-bot")
	sessionID := "agui-profile-override-session"
	t.Cleanup(func() { agent.SetSessionLLMOverrides(sessionID, agent.SessionLLMOverrides{}) })

	cfg := testConfig()
	cfg.Persona = "adapter-wide-persona" // must NOT surface: the profile overrides it
	r := newTestRuntime(cfg, "secret")

	profile := &Profile{
		Name:    "backlog-assistant",
		Base:    config.AgentCoder,
		Persona: "backlog-bot",
		Prompt:  "Stay terse.",
		Model:   "claude-sonnet-4-20250514",
	}
	r.applySessionOverrides(sessionID, profile)

	got := agent.SessionLLMOverridesFor(sessionID)
	if !got.PersonaScoped {
		t.Fatal("expected the session to be persona-scoped")
	}
	if got.Persona != "backlog-bot" {
		t.Fatalf("Persona = %q, want the profile's own %q, not the adapter-wide value", got.Persona, "backlog-bot")
	}
	if got.Prompt != "Stay terse." {
		t.Fatalf("Prompt = %q, want %q", got.Prompt, "Stay terse.")
	}
	if got.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("Model = %q, want the profile's own model", got.Model)
	}
}

// TestApplySessionOverrides_ProfileWithNoPersonaFallsBackToAdapterWide is the
// PANDO-US-0014 acceptance criterion: a profile with no Persona falls back to
// the adapter-wide [AGUI] Persona; with neither set, behaviour is unchanged
// from today (no override installed at all).
func TestApplySessionOverrides_ProfileWithNoPersonaFallsBackToAdapterWide(t *testing.T) {
	sessionID := "agui-profile-fallback-session"
	t.Cleanup(func() { agent.SetSessionLLMOverrides(sessionID, agent.SessionLLMOverrides{}) })

	cfg := testConfig()
	cfg.Persona = "adapter-wide-persona"
	r := newTestRuntime(cfg, "secret")

	// ConfigFromApp already applies this fallback onto Profile.Persona before
	// runtime.go ever sees it (see deps_test.go); this test exercises
	// applySessionOverrides directly with that already-resolved shape.
	profile := &Profile{Name: "docs-assistant", Base: config.AgentTask, Persona: "adapter-wide-persona"}
	r.applySessionOverrides(sessionID, profile)

	got := agent.SessionLLMOverridesFor(sessionID)
	if !got.PersonaScoped || got.Persona != "adapter-wide-persona" {
		t.Fatalf("override = %+v, want the adapter-wide persona applied through the profile", got)
	}

	// With neither the profile nor the adapter declaring a persona (and no
	// Prompt/Model either), behaviour must be unchanged from today: no
	// override installed at all.
	sessionID2 := "agui-profile-nothing-session"
	t.Cleanup(func() { agent.SetSessionLLMOverrides(sessionID2, agent.SessionLLMOverrides{}) })
	cfg2 := testConfig()
	r2 := newTestRuntime(cfg2, "secret")
	profile2 := &Profile{Name: "bare-assistant", Base: config.AgentTask}
	r2.applySessionOverrides(sessionID2, profile2)
	if got2 := agent.SessionLLMOverridesFor(sessionID2); got2.PersonaScoped || got2.Persona != "" || got2.Prompt != "" || got2.Model != "" {
		t.Fatalf("override = %+v, want no override installed at all", got2)
	}
}

// TestApplySessionOverrides_TwoProfilesConcurrentlyStayIsolated is the
// PANDO-US-0014 headline acceptance criterion: two threads (sessions) running
// two profiles concurrently in one process resolve different personas and
// different models, interleaved rather than serialized.
func TestApplySessionOverrides_TwoProfilesConcurrentlyStayIsolated(t *testing.T) {
	withTestPersonaManager(t, "backlog-bot", "docs-bot")

	r := newTestRuntime(testConfig(), "secret")
	backlog := &Profile{Name: "backlog-assistant", Base: config.AgentCoder, Persona: "backlog-bot", Model: "backlog-model"}
	docs := &Profile{Name: "docs-assistant", Base: config.AgentTask, Persona: "docs-bot", Model: "docs-model"}

	const rounds = 50
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			r.applySessionOverrides("thread-backlog", backlog)
			if got := agent.SessionLLMOverridesFor("thread-backlog"); got.Persona != "backlog-bot" || got.Model != "backlog-model" {
				t.Errorf("thread-backlog override = %+v, want backlog-bot/backlog-model", got)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			r.applySessionOverrides("thread-docs", docs)
			if got := agent.SessionLLMOverridesFor("thread-docs"); got.Persona != "docs-bot" || got.Model != "docs-model" {
				t.Errorf("thread-docs override = %+v, want docs-bot/docs-model", got)
			}
		}
	}()
	wg.Wait()
	t.Cleanup(func() {
		agent.SetSessionLLMOverrides("thread-backlog", agent.SessionLLMOverrides{})
		agent.SetSessionLLMOverrides("thread-docs", agent.SessionLLMOverrides{})
	})

	backlogGot := agent.SessionLLMOverridesFor("thread-backlog")
	docsGot := agent.SessionLLMOverridesFor("thread-docs")
	if backlogGot.Persona != "backlog-bot" || backlogGot.Model != "backlog-model" {
		t.Fatalf("thread-backlog final override = %+v", backlogGot)
	}
	if docsGot.Persona != "docs-bot" || docsGot.Model != "docs-model" {
		t.Fatalf("thread-docs final override = %+v", docsGot)
	}
}
