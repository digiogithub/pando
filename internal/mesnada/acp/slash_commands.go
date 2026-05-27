package acp

import (
	"fmt"
	"strings"
)

type slashCommandKind string

const (
	slashCommandGoal       slashCommandKind = "goal"
	slashCommandGoalStatus slashCommandKind = "goal-status"
	slashCommandGoalCancel slashCommandKind = "goal-cancel"
	slashCommandSummarize  slashCommandKind = "summarize"
)

type slashCommand struct {
	Kind      slashCommandKind
	Objective string
}

func parseSlashCommand(input string) (slashCommand, bool) {
	line := strings.TrimSpace(input)
	for strings.HasPrefix(line, "//") {
		line = strings.TrimPrefix(line, "/")
	}

	switch {
	case line == "/goal":
		return slashCommand{Kind: slashCommandGoalStatus}, true
	case strings.HasPrefix(line, "/goal "):
		return slashCommand{Kind: slashCommandGoal, Objective: strings.TrimSpace(strings.TrimPrefix(line, "/goal "))}, true
	case strings.HasPrefix(line, "/autopilot "):
		return slashCommand{Kind: slashCommandGoal, Objective: strings.TrimSpace(strings.TrimPrefix(line, "/autopilot "))}, true
	case line == "/goal-status":
		return slashCommand{Kind: slashCommandGoalStatus}, true
	case line == "/goal-cancel":
		return slashCommand{Kind: slashCommandGoalCancel}, true
	case line == "/compact", line == "/summarize":
		return slashCommand{Kind: slashCommandSummarize}, true
	default:
		return slashCommand{}, false
	}
}

func formatGoalStatus(goal *GoalStateUpdate) string {
	if goal == nil {
		return "No goal is currently tracked for this session."
	}

	lines := []string{
		fmt.Sprintf("Goal: %s", goal.Objective),
		fmt.Sprintf("Status: %s", goal.Status),
		fmt.Sprintf("Iteration: %d/%d", goal.Iteration, goal.MaxIterations),
	}
	if goal.Progress != "" {
		lines = append(lines, fmt.Sprintf("Progress: %s", goal.Progress))
	}
	if goal.NextStep != "" {
		lines = append(lines, fmt.Sprintf("Next step: %s", goal.NextStep))
	}
	if goal.ElapsedTime != "" {
		lines = append(lines, fmt.Sprintf("Elapsed: %s", goal.ElapsedTime))
	}
	return strings.Join(lines, "\n")
}
