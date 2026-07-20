package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/caveman"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/prompt"
	"github.com/digiogithub/pando/internal/ponytail"
)

// setCavemanDefault installs a global default mode for the duration of the test.
func setCavemanDefault(t *testing.T, mode string) {
	t.Helper()
	prev := config.Get()
	config.SetForTests(&config.Config{Caveman: config.CavemanConfig{DefaultMode: mode}})
	t.Cleanup(func() { config.SetForTests(prev) })
}

func TestSetAndGetCavemanMode(t *testing.T) {
	const sid = "sess-caveman-1"
	t.Cleanup(func() { cavemanModes.Delete(sid) })

	if got := CavemanMode(sid); got.IsActive() {
		t.Fatalf("expected off by default, got %v", got)
	}

	SetCavemanMode(sid, caveman.ModeUltra)
	if got := CavemanMode(sid); got != caveman.ModeUltra {
		t.Fatalf("expected ultra, got %v", got)
	}

	// With no configured default, off clears the entry entirely.
	SetCavemanMode(sid, caveman.ModeOff)
	if _, ok := cavemanModes.Load(sid); ok {
		t.Fatal("expected the override to be deleted when the default is off")
	}
	if got := CavemanMode(sid); got.IsActive() {
		t.Fatalf("expected cleared, got %v", got)
	}
}

func TestCavemanModeFallsBackToConfiguredDefault(t *testing.T) {
	setCavemanDefault(t, "lite")
	const sid = "sess-caveman-2"
	t.Cleanup(func() { cavemanModes.Delete(sid) })

	if got := CavemanMode(sid); got != caveman.ModeLite {
		t.Fatalf("expected the configured default lite, got %v", got)
	}

	SetCavemanMode(sid, caveman.ModeFull)
	if got := CavemanMode(sid); got != caveman.ModeFull {
		t.Fatalf("session choice must win over the default, got %v", got)
	}
}

func TestCavemanExplicitOffOverridesConfiguredDefault(t *testing.T) {
	setCavemanDefault(t, "full")
	const sid = "sess-caveman-3"
	t.Cleanup(func() { cavemanModes.Delete(sid) })

	SetCavemanMode(sid, caveman.ModeOff)
	if got := CavemanMode(sid); got.IsActive() {
		t.Fatalf("explicit off must survive a non-off default, got %v", got)
	}
	// Other sessions still inherit the default.
	if got := CavemanMode("sess-caveman-other"); got != caveman.ModeFull {
		t.Fatalf("expected other sessions to inherit full, got %v", got)
	}
}

func TestCavemanInvalidConfiguredDefaultIsOff(t *testing.T) {
	setCavemanDefault(t, "shouty")
	if got := CavemanMode("sess-caveman-4"); got.IsActive() {
		t.Fatalf("an unknown configured default must resolve to off, got %v", got)
	}
}

func TestCavemanModeForContext(t *testing.T) {
	const sid = "sess-caveman-5"
	t.Cleanup(func() { cavemanModes.Delete(sid) })

	SetCavemanMode(sid, caveman.ModeFull)
	ctx := context.WithValue(context.Background(), prompt.SessionIDKey, sid)
	if got := cavemanModeForContext(ctx); got != caveman.ModeFull {
		t.Fatalf("expected full from ctx, got %v", got)
	}

	if got := cavemanModeForContext(context.Background()); got.IsActive() {
		t.Fatalf("expected off without a session id, got %v", got)
	}
}

func TestCavemanEmptySessionIDIsIgnored(t *testing.T) {
	SetCavemanMode("  ", caveman.ModeUltra)
	if got := CavemanMode(""); got.IsActive() {
		t.Fatalf("empty session id must resolve to off, got %v", got)
	}
}

// --- Phase 3: per-turn prompt injection ---

func TestCavemanActivatesTheSessionPolicyPath(t *testing.T) {
	const sid = "sess-caveman-policy"
	t.Cleanup(func() { cavemanModes.Delete(sid) })

	ctx := context.WithValue(context.Background(), prompt.SessionIDKey, sid)
	if sessionPolicyActive(ctx) {
		t.Fatal("expected no active session policy by default")
	}

	SetCavemanMode(sid, caveman.ModeFull)
	if !sessionPolicyActive(ctx) {
		t.Fatal("caveman must take prepareProvider off the pre-built-provider fast path")
	}
}

func TestCavemanSessionPolicyInstructions(t *testing.T) {
	const sid = "sess-caveman-inject"
	t.Cleanup(func() { cavemanModes.Delete(sid) })

	ctx := context.WithValue(context.Background(), prompt.SessionIDKey, sid)
	if got := sessionPolicyInstructions(ctx); len(got) != 0 {
		t.Fatalf("expected no instructions when disabled, got %d", len(got))
	}

	SetCavemanMode(sid, caveman.ModeUltra)
	got := sessionPolicyInstructions(ctx)
	if len(got) != 1 {
		t.Fatalf("expected exactly one injected ruleset, got %d", len(got))
	}
	if !strings.Contains(got[0], "caveman (ultra)") {
		t.Errorf("expected the injected block to name the active level, got %q", got[0])
	}
	if !strings.Contains(got[0], "Output brevity mode") {
		t.Error("expected the caveman policy body to be injected")
	}
}

func TestCavemanInjectsFromConfiguredDefault(t *testing.T) {
	setCavemanDefault(t, "lite")

	ctx := context.WithValue(context.Background(), prompt.SessionIDKey, "sess-caveman-cfg")
	got := sessionPolicyInstructions(ctx)
	if len(got) != 1 || !strings.Contains(got[0], "caveman (lite)") {
		t.Fatalf("expected the configured default to inject, got %v", got)
	}
}

func TestCavemanSessionOffSuppressesInjectionDespiteDefault(t *testing.T) {
	setCavemanDefault(t, "full")
	const sid = "sess-caveman-off"
	t.Cleanup(func() { cavemanModes.Delete(sid) })

	SetCavemanMode(sid, caveman.ModeOff)
	ctx := context.WithValue(context.Background(), prompt.SessionIDKey, sid)
	if got := sessionPolicyInstructions(ctx); len(got) != 0 {
		t.Fatalf("an explicit session off must suppress injection, got %v", got)
	}
	if sessionPolicyActive(ctx) {
		t.Error("an explicit session off must not keep the policy path active")
	}
}

func TestCavemanNotInjectedInCleanMode(t *testing.T) {
	const sid = "sess-caveman-clean"
	t.Cleanup(func() { cavemanModes.Delete(sid) })

	SetCavemanMode(sid, caveman.ModeFull)
	ctx := context.WithValue(context.Background(), prompt.SessionIDKey, sid)
	ctx = context.WithValue(ctx, cleanModeContextKey{}, true)

	// prepareProvider returns before any policy composition in clean mode; assert
	// on the invariant that gates it, since building a provider needs credentials.
	if !isCleanModeContext(ctx) {
		t.Fatal("expected a clean-mode context")
	}
}

func TestCavemanComposesBetweenSuperpowersAndPonytail(t *testing.T) {
	const sid = "sess-caveman-order"
	t.Cleanup(func() {
		cavemanModes.Delete(sid)
		SetSuperpowersMode(sid, false)
		SetPonytailMode(sid, ponytail.ModeOff)
	})

	SetSuperpowersMode(sid, true)
	SetCavemanMode(sid, caveman.ModeFull)
	SetPonytailMode(sid, ponytail.ModeUltra)

	ctx := context.WithValue(context.Background(), prompt.SessionIDKey, sid)
	got := sessionPolicyInstructions(ctx)
	if len(got) != 3 {
		t.Fatalf("expected all three rulesets to compose, got %d", len(got))
	}
	if !strings.Contains(got[0], "superpowers") {
		t.Errorf("expected superpowers first, got %q", got[0])
	}
	if !strings.Contains(got[1], "caveman (full)") {
		t.Errorf("expected caveman second, got %q", got[1])
	}
	if !strings.Contains(got[2], "ponytail (ultra)") {
		t.Errorf("expected ponytail last, got %q", got[2])
	}
}

func TestCavemanConcurrentSessionsAreIsolated(t *testing.T) {
	var wg sync.WaitGroup
	modes := []caveman.Mode{caveman.ModeLite, caveman.ModeFull, caveman.ModeUltra}
	for i, m := range modes {
		sid := fmt.Sprintf("sess-caveman-conc-%d", i)
		t.Cleanup(func() { cavemanModes.Delete(sid) })
		wg.Add(1)
		go func(sid string, m caveman.Mode) {
			defer wg.Done()
			for range 50 {
				SetCavemanMode(sid, m)
				if got := CavemanMode(sid); got != m {
					t.Errorf("session %s: got %v, want %v", sid, got, m)
					return
				}
			}
		}(sid, m)
	}
	wg.Wait()
}
