package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/llm/prompt"
	"github.com/digiogithub/pando/internal/mesnada/persona"
)

// TestGetPersonaContentAppendsSessionPrompt is the PANDO-US-0014 acceptance
// criterion "a profile Prompt reaches the session's system text": it exercises
// the exact mechanism buildSystemMessage's personaContent parameter comes
// from (getPersonaContent), reached through the SAME per-session override
// (SessionLLMOverrides.Prompt) internal/agui.Runtime.applySessionOverrides
// installs -- no second injection path is introduced.
func TestGetPersonaContentAppendsSessionPrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "backlog-bot.md"), []byte("Backlog persona instructions."), 0o644); err != nil {
		t.Fatalf("write persona: %v", err)
	}
	mgr, err := persona.NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prevMgr := GetPersonaManager()
	t.Cleanup(func() { SetPersonaManager(prevMgr) })
	SetPersonaManager(mgr)

	ctxFor := func(sessionID string) context.Context {
		return context.WithValue(context.Background(), prompt.SessionIDKey, sessionID)
	}

	t.Run("persona plus prompt", func(t *testing.T) {
		const sid = "agent-persona-prompt-session"
		SetSessionLLMOverrides(sid, SessionLLMOverrides{
			Persona: "backlog-bot", PersonaScoped: true, Prompt: "Stay terse.",
		})
		t.Cleanup(func() { SetSessionLLMOverrides(sid, SessionLLMOverrides{}) })

		got := getPersonaContent(ctxFor(sid), "do work")
		if want := "Backlog persona instructions.\n\nStay terse."; got != want {
			t.Fatalf("getPersonaContent = %q, want %q", got, want)
		}
	})

	t.Run("prompt only, no persona name", func(t *testing.T) {
		const sid = "agent-prompt-only-session"
		SetSessionLLMOverrides(sid, SessionLLMOverrides{PersonaScoped: true, Prompt: "Stay terse."})
		t.Cleanup(func() { SetSessionLLMOverrides(sid, SessionLLMOverrides{}) })

		got := getPersonaContent(ctxFor(sid), "do work")
		if got != "Stay terse." {
			t.Fatalf("getPersonaContent = %q, want the bare prompt override", got)
		}
	})

	t.Run("another session sees neither", func(t *testing.T) {
		const sid = "agent-unrelated-session"
		got := getPersonaContent(ctxFor(sid), "do work")
		if got != "" {
			t.Fatalf("getPersonaContent = %q, want empty for a session with no override at all", got)
		}
	})
}
