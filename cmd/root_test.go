package cmd

import (
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

func TestReleaseArch(t *testing.T) {
	tests := map[string]string{
		"amd64": "x64",
		"arm64": "arm64",
		"386":   "386",
	}

	for input, want := range tests {
		if got := releaseArch(input); got != want {
			t.Fatalf("releaseArch(%q) = %q, want %q", input, got, want)
		}
	}
}
