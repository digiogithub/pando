package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/caveman"
	"github.com/digiogithub/pando/internal/commands"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/ponytail"
)

func newTestSetupBridge() tools.SetupBridge { return NewSetupBridge(nil) }

func TestSetupBridgeRunModeCommand(t *testing.T) {
	bridge := newTestSetupBridge()
	sessionID := "setup-bridge-caveman"
	t.Cleanup(func() { SetCavemanMode(sessionID, caveman.ModeOff) })

	out, err := bridge.RunCommand(context.Background(), sessionID, "caveman", "ultra")
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if got := CavemanMode(sessionID); got != caveman.ModeUltra {
		t.Fatalf("caveman mode = %v, want ultra", got)
	}
	// The agent is already mid-turn, so the confirmation must say when it lands.
	if !strings.Contains(out, "next turn") {
		t.Fatalf("confirmation does not state when the mode applies:\n%s", out)
	}
}

func TestSetupBridgeRunModeCommandDefaultsAndRejects(t *testing.T) {
	bridge := newTestSetupBridge()
	sessionID := "setup-bridge-ponytail"
	t.Cleanup(func() { SetPonytailMode(sessionID, ponytail.ModeOff) })

	if _, err := bridge.RunCommand(context.Background(), sessionID, "ponytail", ""); err != nil {
		t.Fatalf("RunCommand with no argument: %v", err)
	}
	if got := PonytailMode(sessionID); !got.IsActive() {
		t.Fatalf("empty argument should default to an active mode, got %v", got)
	}

	if _, err := bridge.RunCommand(context.Background(), sessionID, "ponytail", "sideways"); err == nil {
		t.Fatal("an unknown level should be rejected instead of silently ignored")
	}
}

func TestSetupBridgeCavemanFinishRejectsStrayArgument(t *testing.T) {
	bridge := newTestSetupBridge()
	sessionID := "setup-bridge-caveman-finish"
	t.Cleanup(func() { SetCavemanMode(sessionID, caveman.ModeOff) })

	SetCavemanMode(sessionID, caveman.ModeFull)
	if _, err := bridge.RunCommand(context.Background(), sessionID, "caveman-finish", "ultra"); err == nil {
		t.Fatal("/caveman-finish takes no level; a stray argument must be reported")
	}
	if got := CavemanMode(sessionID); got != caveman.ModeFull {
		t.Fatalf("a rejected command must not change the mode, got %v", got)
	}

	if _, err := bridge.RunCommand(context.Background(), sessionID, "caveman-finish", ""); err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if CavemanMode(sessionID).IsActive() {
		t.Fatal("caveman should be off after /caveman-finish")
	}
}

func TestSetupBridgeRunInstructionCommand(t *testing.T) {
	out, err := newTestSetupBridge().RunCommand(context.Background(), "setup-bridge-vuln", "vulnhunt", "internal/api")
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if !strings.Contains(out, "Follow these instructions in this turn") {
		t.Fatalf("instruction commands must be framed as work to do now:\n%s", out)
	}
	if !strings.Contains(out, "internal/api") {
		t.Fatalf("the scope argument was dropped:\n%s", out)
	}
}

func TestSetupBridgeBlocksUserOnlyCommands(t *testing.T) {
	bridge := newTestSetupBridge()
	for _, name := range []string{"goal", "compact", "db-compact", "superpowers-finish", "learning-finish"} {
		_, err := bridge.RunCommand(context.Background(), "setup-bridge-blocked", name, "")
		if err == nil {
			t.Fatalf("/%s must not be activatable from a tool call", name)
		}
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error for /%s does not name the command: %v", name, err)
		}
	}
}

func TestSetupBridgeUnknownCommand(t *testing.T) {
	_, err := newTestSetupBridge().RunCommand(context.Background(), "setup-bridge-unknown", "does-not-exist", "")
	if err == nil {
		t.Fatal("an unknown slash command must be an error")
	}
	if !strings.Contains(err.Error(), "commands") {
		t.Fatalf("the error should point at the discovery command: %v", err)
	}
}

func TestSetupBridgeCommandsCoverRunnableAndBlocked(t *testing.T) {
	listed := newTestSetupBridge().Commands()

	byName := make(map[string]tools.SetupRunnableCommand, len(listed))
	for _, c := range listed {
		byName[c.Name] = c
	}

	caveman, ok := byName["caveman"]
	if !ok || !caveman.Runnable() {
		t.Fatalf("caveman should be listed as runnable, got %+v", caveman)
	}
	if caveman.Args == "" || caveman.Summary == "" {
		t.Fatalf("runnable commands need an argument hint and a summary, got %+v", caveman)
	}

	compact, ok := byName["compact"]
	if !ok || compact.Runnable() {
		t.Fatalf("compact should be listed as user-only, got %+v", compact)
	}
	if compact.Reason == "" {
		t.Fatal("a user-only command must carry the reason")
	}

	// Every runnable spec must be dispatchable, and vice versa.
	for _, spec := range setupRunnableSpecs() {
		if (spec.Apply == nil) == (spec.Prompt == nil) {
			t.Fatalf("%q must set exactly one of Apply and Prompt", spec.Name)
		}
		if _, blocked := setupBlockedCommands[spec.Name]; blocked {
			t.Fatalf("%q is both runnable and blocked", spec.Name)
		}
	}
}

func TestSetupBridgeSessionModes(t *testing.T) {
	sessionID := "setup-bridge-modes"
	t.Cleanup(func() {
		SetCavemanMode(sessionID, caveman.ModeOff)
		SetSuperpowersMode(sessionID, false)
	})

	SetCavemanMode(sessionID, caveman.ModeLite)
	SetSuperpowersMode(sessionID, true)

	states := make(map[string]string)
	for _, m := range newTestSetupBridge().SessionModes(sessionID) {
		states[m.Name] = m.State
	}

	if states["caveman"] != "lite" {
		t.Fatalf("caveman state = %q, want lite", states["caveman"])
	}
	if states["superpowers"] != "on" {
		t.Fatalf("superpowers state = %q, want on", states["superpowers"])
	}
	if states["learning"] != "off" {
		t.Fatalf("learning state = %q, want off", states["learning"])
	}
}

func TestSetupBridgeSessionInfoWithoutStore(t *testing.T) {
	if _, err := newTestSetupBridge().SessionInfo(context.Background(), "any"); err == nil {
		t.Fatal("SessionInfo must report the missing session store instead of panicking")
	}
}

func TestSetupBridgeRunsCustomCommand(t *testing.T) {
	dataDir := t.TempDir()
	commandsDir := filepath.Join(dataDir, "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "Review the diff and list the risky changes.\n"
	if err := os.WriteFile(filepath.Join(commandsDir, "review.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write command: %v", err)
	}

	// Content() is what the bridge uses to resolve a custom command body.
	got, ok := commands.Content(dataDir, "project:review")
	if !ok {
		t.Fatal("custom project command not found")
	}
	if got != body {
		t.Fatalf("custom command body = %q, want %q", got, body)
	}
}
