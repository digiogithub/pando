package engines

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

func setConfig(t *testing.T, mutate func(c *config.Config)) {
	t.Helper()
	prev := config.Get()
	c := &config.Config{}
	mutate(c)
	config.SetForTests(c)
	t.Cleanup(func() { config.SetForTests(prev) })
}

func TestDisabledPolicyIsLiveAndSparesNatives(t *testing.T) {
	setConfig(t, func(c *config.Config) {
		c.Mesnada.Orchestrator.DisabledEngines = []string{"claude", "pando"}
	})
	if !IsDisabled("claude") || !IsDisabled("claude-code") {
		t.Fatal("claude (and its alias) should be disabled")
	}
	if IsDisabled("pando") || IsDisabled("") || IsDisabled("warm-acp") {
		t.Fatal("native engines can never be disabled")
	}
	err := CheckEnabled("claude")
	var de *DisabledError
	if !errors.As(err, &de) {
		t.Fatalf("CheckEnabled(claude) = %v, want *DisabledError", err)
	}
	if want := `engine "claude" is disabled in Mesnada settings; use engine=pando`; err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
	if got := FilterEnabled([]string{"pando", "claude", "copilot"}); strings.Join(got, ",") != "pando,copilot" {
		t.Fatalf("FilterEnabled = %v", got)
	}

	// Live: re-enabling takes effect on the next call, no restart.
	config.Get().Mesnada.Orchestrator.DisabledEngines = nil
	if IsDisabled("claude") {
		t.Fatal("re-enabled engine still reported disabled")
	}
}

func TestSpawnDepthCap(t *testing.T) {
	setConfig(t, func(c *config.Config) { c.Mesnada.Delegation.MaxDepth = 1 })
	if err := CheckSpawnDepth(1); err != nil {
		t.Fatalf("main session (level 0) must be able to spawn level 1: %v", err)
	}
	err := CheckSpawnDepth(2)
	var de *DepthError
	if !errors.As(err, &de) || de.NestedDisabled {
		t.Fatalf("level 2 with MaxDepth=1 = %v, want maxDepth DepthError", err)
	}
	if !strings.Contains(err.Error(), "level 2 > max 1") || !strings.Contains(err.Error(), "do this work yourself") {
		t.Fatalf("unexpected message %q", err.Error())
	}
	if !CanDelegate(0) || CanDelegate(1) {
		t.Fatal("with MaxDepth=1 only level 0 may delegate")
	}

	// Unset MaxDepth reads as the default.
	config.Get().Mesnada.Delegation.MaxDepth = 0
	if MaxDepth() != DefaultMaxDepth || CheckSpawnDepth(3) != nil || CheckSpawnDepth(4) == nil {
		t.Fatal("default MaxDepth should allow level 3 and refuse level 4")
	}
}

func TestNestedSubagentsToggleCapsAtOne(t *testing.T) {
	off := false
	setConfig(t, func(c *config.Config) {
		c.Mesnada.Delegation.MaxDepth = 5
		c.Mesnada.Delegation.AllowNestedSubagents = &off
	})
	if MaxDepth() != 1 {
		t.Fatalf("MaxDepth with nested disallowed = %d, want 1", MaxDepth())
	}
	if err := CheckSpawnDepth(1); err != nil {
		t.Fatalf("main session must still delegate: %v", err)
	}
	err := CheckSpawnDepth(2)
	var de *DepthError
	if !errors.As(err, &de) || !de.NestedDisabled {
		t.Fatalf("level 2 = %v, want nested-disabled DepthError", err)
	}
	if !strings.HasPrefix(err.Error(), "nested subagents are disabled in Mesnada settings: do this work yourself") {
		t.Fatalf("unexpected message %q", err.Error())
	}
	if CanDelegate(1) {
		t.Fatal("a subagent must not be able to delegate when nested subagents are off")
	}

	on := true
	config.Get().Mesnada.Delegation.AllowNestedSubagents = &on
	if !CanDelegate(1) || MaxDepth() != 5 {
		t.Fatal("re-allowing nested subagents must restore maxDepth live")
	}
}

func TestDetect(t *testing.T) {
	prev := lookPath
	t.Cleanup(func() { lookPath = prev })
	lookPath = func(bin string) (string, error) {
		switch bin {
		case "claude", "my-agent":
			return "/opt/bin/" + bin, nil
		}
		return "", exec.ErrNotFound
	}

	got := Detect([]CustomEngine{{Name: "my-agent", Command: "my-agent"}, {Name: "copilot", Command: "copilot2"}}, []string{"claude", "pando"})
	by := map[string]Status{}
	for _, s := range got {
		by[s.Name] = s
	}

	if p := by["pando"]; p.Kind != KindNative || !p.Locked || !p.Enabled {
		t.Fatalf("pando = %+v, want locked native enabled", p)
	}
	if c := by["claude"]; c.Kind != KindCLI || c.Enabled || !c.Installed || c.Path != "/opt/bin/claude" || c.Locked {
		t.Fatalf("claude = %+v", c)
	}
	if g := by["gemini"]; g.Installed || !g.Enabled || g.Binary != "gemini" {
		t.Fatalf("gemini = %+v", g)
	}
	if m := by["my-agent"]; m.Kind != KindCustom || !m.Installed || !m.Enabled {
		t.Fatalf("custom = %+v", m)
	}
	// A custom template shadowing a built-in name replaces that entry.
	if c := by["copilot"]; c.Kind != KindCustom || c.Binary != "copilot2" {
		t.Fatalf("shadowed copilot = %+v", c)
	}
	seen := map[string]int{}
	for _, s := range got {
		seen[s.Name]++
		if seen[s.Name] > 1 {
			t.Fatalf("duplicate entry %q", s.Name)
		}
	}
}
