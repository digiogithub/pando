package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/agent"
)

func TestGoalStatusExitCode(t *testing.T) {
	tests := map[string]int{
		"":                        0,
		agent.GoalStatusCompleted: 0,
		agent.GoalStatusBlocked:   1,
		agent.GoalStatusCancelled: 1,
		agent.GoalStatusStalled:   1,
		agent.GoalStatusFailed:    2,
		agent.GoalStatusTimeout:   2,
		"unexpected":              1,
	}

	for status, want := range tests {
		if got := goalStatusExitCode(status); got != want {
			t.Fatalf("goalStatusExitCode(%q) = %d, want %d", status, got, want)
		}
	}
}

func TestRunACPServerWithOptions_ConfiguresSecondaryIPCFailoverPath(t *testing.T) {
	source, err := os.ReadFile("root.go")
	if err != nil {
		t.Fatalf("read root.go: %v", err)
	}
	body := string(source)

	checks := []string{
		"pandoApp.SetIPCSecondaryContext(",
		"rt.Watcher.SetPromoteCallback(pandoApp.PromoteToPrimary)",
		"pandoApp.SetupIPC(acpBus)",
		"rt.Watcher.Start(ctx)",
	}
	for _, needle := range checks {
		if !strings.Contains(body, needle) {
			t.Fatalf("runACPServerWithOptions is missing %q", needle)
		}
	}
}

func TestRunACPServerWithOptions_RequiresLogFileWhenDebugEnabled(t *testing.T) {
	err := runACPServerWithOptions(t.TempDir(), true, "", false)
	if err == nil {
		t.Fatal("expected error when ACP debug is enabled without log file")
	}
	if !strings.Contains(err.Error(), "requires --log-file") {
		t.Fatalf("unexpected error: %v", err)
	}
}
