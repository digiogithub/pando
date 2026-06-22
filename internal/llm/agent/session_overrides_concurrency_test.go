package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/prompt"
	"github.com/digiogithub/pando/internal/mesnada/persona"
)

// TestSessionOverridesConcurrentIsolation is the Phase 7.1 concurrency-hardening
// guarantee: two sessions running in parallel with different per-session model
// and persona overrides each resolve their own values, and neither clobbers the
// other nor the package-global active persona. Run with -race.
func TestSessionOverridesConcurrentIsolation(t *testing.T) {
	// Build a persona manager from a temp dir with two distinct personas.
	dir := t.TempDir()
	writePersona := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write persona %s: %v", name, err)
		}
	}
	writePersona("persona-a", "# Persona A\nAlpha instructions.")
	writePersona("persona-b", "# Persona B\nBravo instructions.")

	mgr, err := persona.NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Save and restore mutated package globals.
	prevMgr := globalPersonaManager
	prevActive := activePersonaName
	t.Cleanup(func() {
		globalPersonaManager = prevMgr
		activePersonaName = prevActive
	})
	SetPersonaManager(mgr)
	// A global active persona that must NOT leak into PersonaScoped sessions.
	activePersonaName = "persona-b"

	const (
		sessA   = "session-alpha"
		sessB   = "session-beta"
		modelA  = models.ModelID("model-alpha")
		modelB  = models.ModelID("model-beta")
		personA = "persona-a"
		personB = "persona-b"
	)

	SetSessionLLMOverrides(sessA, SessionLLMOverrides{Model: modelA, Persona: personA, PersonaScoped: true})
	SetSessionLLMOverrides(sessB, SessionLLMOverrides{Model: modelB, Persona: personB, PersonaScoped: true})
	t.Cleanup(func() {
		SetSessionLLMOverrides(sessA, SessionLLMOverrides{})
		SetSessionLLMOverrides(sessB, SessionLLMOverrides{})
	})

	ctxFor := func(sessionID string) context.Context {
		return context.WithValue(context.Background(), prompt.SessionIDKey, sessionID)
	}

	check := func(t *testing.T, sessionID string, wantModel models.ModelID, wantPersona, wantPersonaMarker string) {
		ctx := ctxFor(sessionID)
		if got := sessionLLMOverridesForContext(ctx).Model; got != wantModel {
			t.Errorf("session %s: model = %q, want %q", sessionID, got, wantModel)
		}
		if got := effectiveActivePersona(ctx); got != wantPersona {
			t.Errorf("session %s: effective persona = %q, want %q", sessionID, got, wantPersona)
		}
		if got := getPersonaContent(ctx, "do work"); !strings.Contains(got, wantPersonaMarker) {
			t.Errorf("session %s: persona content = %q, want marker %q", sessionID, got, wantPersonaMarker)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); check(t, sessA, modelA, personA, "Alpha instructions") }()
		go func() { defer wg.Done(); check(t, sessB, modelB, personB, "Bravo instructions") }()
	}
	wg.Wait()
}

// TestSetSessionLLMOverridesEmptyDeletes verifies the entry is removed when all
// fields are zero so a closed session leaves no stale per-session state behind.
func TestSetSessionLLMOverridesEmptyDeletes(t *testing.T) {
	const sid = "session-empty"
	SetSessionLLMOverrides(sid, SessionLLMOverrides{Model: "m", PersonaScoped: true})
	if _, ok := sessionLLMOverrides.Load(sid); !ok {
		t.Fatalf("expected override entry to be stored")
	}
	SetSessionLLMOverrides(sid, SessionLLMOverrides{})
	if _, ok := sessionLLMOverrides.Load(sid); ok {
		t.Fatalf("expected override entry to be deleted when empty")
	}
}
