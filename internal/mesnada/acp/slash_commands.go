package acp

import (
	"fmt"
	"strings"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

type slashCommandKind string

const (
	slashCommandGoal            slashCommandKind = "goal"
	slashCommandGoalStatus      slashCommandKind = "goal-status"
	slashCommandGoalCancel      slashCommandKind = "goal-cancel"
	slashCommandSummarize       slashCommandKind = "summarize"
	slashCommandDBCompact       slashCommandKind = "db-compact"
	slashCommandPonytail        slashCommandKind = "ponytail"
	slashCommandImproveAgentsMd slashCommandKind = "improve-agents-md"
)

type slashCommand struct {
	Kind      slashCommandKind
	Objective string
}

type slashCommandSpec struct {
	Token       string
	Kind        slashCommandKind
	Description string
	InputHint   string
	Aliases     []string
	Usage       string
}

func slashCommandSpecs() []slashCommandSpec {
	return []slashCommandSpec{
		{
			Token:       goalCommandToken,
			Kind:        slashCommandGoal,
			Description: "Start goal mode with a persistent objective",
			InputHint:   "objective to pursue",
			Aliases:     []string{autopilotCommandToken},
			Usage:       "Usage: /goal <objective>\nAlias: /autopilot <objective>",
		},
		{
			Token:       goalStatusCommandToken,
			Kind:        slashCommandGoalStatus,
			Description: "Show the status of the current goal",
		},
		{
			Token:       goalCancelCommandToken,
			Kind:        slashCommandGoalCancel,
			Description: "Cancel the current goal execution",
		},
		{
			Token:       compactCommandToken,
			Kind:        slashCommandSummarize,
			Description: "Create a manual compact summary for the current session",
			Aliases:     []string{summarizeCommandToken},
		},
		{
			Token:       dbCompactCommandToken,
			Kind:        slashCommandDBCompact,
			Description: "Compact the database (VACUUM) and reclaim free space",
		},
		{
			Token:       ponytailCommandToken,
			Kind:        slashCommandPonytail,
			Description: "Toggle lazy-senior-dev (ponytail) mode at a chosen intensity",
			InputHint:   "lite | full | ultra | off",
			Usage:       "Usage: /ponytail [lite|full|ultra|off]\nNo argument defaults to full. Use /ponytail off to disable.",
		},
		{
			Token:       improveAgentsMdCommandToken,
			Kind:        slashCommandImproveAgentsMd,
			Description: "Create or reinforce AGENTS.md with the mandatory AI-agent operating rules",
			InputHint:   "optional extra guidance",
		},
	}
}

func availableCommands() []acpsdk.AvailableCommand {
	specs := slashCommandSpecs()
	commands := make([]acpsdk.AvailableCommand, 0, len(specs)+2)
	for _, spec := range specs {
		commands = append(commands, spec.toAvailableCommand(spec.Token))
		for _, alias := range spec.Aliases {
			commands = append(commands, spec.toAvailableCommand(alias))
		}
	}
	return commands
}

func (s slashCommandSpec) toAvailableCommand(name string) acpsdk.AvailableCommand {
	command := acpsdk.AvailableCommand{
		Name:        name,
		Description: s.descriptionFor(name),
	}
	if strings.TrimSpace(s.InputHint) != "" {
		command.Input = &acpsdk.AvailableCommandInput{
			Unstructured: &acpsdk.UnstructuredCommandInput{Hint: s.InputHint},
		}
	}
	return command
}

func (s slashCommandSpec) descriptionFor(name string) string {
	if name == s.Token {
		return s.Description
	}
	return fmt.Sprintf("Alias for /%s", s.Token)
}

func parseSlashCommand(input string) (slashCommand, bool) {
	line := strings.TrimSpace(input)
	if !strings.HasPrefix(line, "/") {
		return slashCommand{}, false
	}

	commandText := strings.TrimPrefix(line, "/")
	parts := strings.SplitN(commandText, " ", 2)
	name := strings.TrimSpace(parts[0])
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	for _, spec := range slashCommandSpecs() {
		if command, ok := spec.parse(name, args); ok {
			return command, true
		}
	}
	return slashCommand{}, false
}

func (s slashCommandSpec) parse(name, args string) (slashCommand, bool) {
	if !s.matches(name) {
		return slashCommand{}, false
	}
	if strings.TrimSpace(s.InputHint) != "" {
		return slashCommand{Kind: s.Kind, Objective: args}, true
	}
	return slashCommand{Kind: s.Kind}, true
}

func (s slashCommandSpec) matches(name string) bool {
	if name == s.Token {
		return true
	}
	for _, alias := range s.Aliases {
		if name == alias {
			return true
		}
	}
	return false
}

func slashCommandUsage(kind slashCommandKind) string {
	for _, spec := range slashCommandSpecs() {
		if spec.Kind == kind {
			return spec.Usage
		}
	}
	return ""
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
