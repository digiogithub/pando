package acp

import (
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/caveman"
	pandocommands "github.com/digiogithub/pando/internal/commands"
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
	slashCommandSuperpowers     slashCommandKind = "superpowers"

	slashCommandSuperpowersFinish slashCommandKind = "superpowers-finish"

	slashCommandCaveman       slashCommandKind = "caveman"
	slashCommandCavemanFinish slashCommandKind = "caveman-finish"

	slashCommandLearning       slashCommandKind = "learning"
	slashCommandLearningFinish slashCommandKind = "learning-finish"

	slashCommandVulnhunt          slashCommandKind = "vulnhunt"
	slashCommandVulnhunterFix     slashCommandKind = "vulnhunter-fix"
	slashCommandVulnhuntFixVerify slashCommandKind = "vulnhunt-fix-verify"

	slashCommandDesign          slashCommandKind = "design"
	slashCommandDesignOpen      slashCommandKind = "design-open"
	slashCommandDesignVersions  slashCommandKind = "design-versions"
	slashCommandDesignSystem    slashCommandKind = "design-system"
	slashCommandDesignTemplates slashCommandKind = "design-templates"

	// slashCommandExtension is the catch-all kind for a command contributed by
	// a compiled-in extension. The concrete name lives in slashCommand.Name,
	// because there is no compile-time constant for something a build adds.
	slashCommandExtension slashCommandKind = "extension"
)

type slashCommand struct {
	Kind      slashCommandKind
	Objective string
	// Name is only set for slashCommandExtension: it is the command the user
	// typed, used to route back to the owning extension.
	Name string
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
			Token:       superpowersCommandToken,
			Kind:        slashCommandSuperpowers,
			Description: "Enable the opt-in disciplined development workflow (plan-first, verify-always)",
			InputHint:   "optional objective",
			Usage:       "Usage: /superpowers [objective]\nRun /superpowers-finish to verify, report and return to normal mode.",
		},
		{
			Token:       superpowersFinishCommandToken,
			Kind:        slashCommandSuperpowersFinish,
			Description: "Verify and close the active Superpowers workflow, then return to normal mode",
			Usage:       "Usage: /superpowers-finish",
		},
		{
			Token:       cavemanCommandToken,
			Kind:        slashCommandCaveman,
			Description: "Shorter answers to cut output tokens (code, commands and verification stay intact)",
			InputHint:   "lite | full | ultra",
			Usage:       caveman.Usage,
		},
		{
			Token:       cavemanFinishCommandToken,
			Kind:        slashCommandCavemanFinish,
			Description: "Disable caveman output brevity and return to normal output",
			Usage:       caveman.FinishUsage,
		},
		{
			Token:       learningCommandToken,
			Kind:        slashCommandLearning,
			Description: "Enable learner mode: read the KB more, document discoveries, ask questions, keep docs current",
			InputHint:   "optional focus",
			Usage:       "Usage: /learning [focus]\nRun /learning-finish to consolidate what was learned and return to normal mode.",
		},
		{
			Token:       learningFinishCommandToken,
			Kind:        slashCommandLearningFinish,
			Description: "Consolidate what was learned into KB/memory and return to normal mode",
			Usage:       "Usage: /learning-finish",
		},
		{
			Token:       improveAgentsMdCommandToken,
			Kind:        slashCommandImproveAgentsMd,
			Description: "Create or reinforce AGENTS.md with the mandatory AI-agent operating rules",
			InputHint:   "optional extra guidance",
		},
		{
			Token:       designCommandToken,
			Kind:        slashCommandDesign,
			Description: "List the design artifacts of this project, or show one",
			InputHint:   "optional artifact slug or id",
			Usage:       "Usage: /design [artifact]\nNo argument lists every artifact. Use /design-open to preview one.",
		},
		{
			Token:       designOpenCommandToken,
			Kind:        slashCommandDesignOpen,
			Description: "Preview a design artifact: live URL plus an inline screenshot",
			InputHint:   "artifact and optional slide number",
			Usage:       "Usage: /design-open [artifact] [slide]\nNo argument opens the most recently updated artifact.",
		},
		{
			Token:       designVersionsCommandToken,
			Kind:        slashCommandDesignVersions,
			Description: "Show the version history of a design artifact",
			InputHint:   "optional artifact slug or id",
			Usage:       "Usage: /design-versions [artifact]",
		},
		{
			Token:       designSystemCommandToken,
			Kind:        slashCommandDesignSystem,
			Description: "Show the design system every artifact of this project is held to",
			InputHint:   "",
			Usage:       "Usage: /design-system\nReads the committed tokens. Extraction and edits go through the design_system tool.",
		},
		{
			Token:       designTemplatesCommandToken,
			Kind:        slashCommandDesignTemplates,
			Description: "List the design templates an artifact can be built from",
			InputHint:   "",
			Usage:       "Usage: /design-templates\nLists the bundled templates and the brief each one expects.",
		},
		{
			Token:       vulnhuntCommandToken,
			Kind:        slashCommandVulnhunt,
			Description: "Adversarial security audit: trace attacker input to sinks and report exploitable vulnerabilities",
			InputHint:   "optional target scope (subdir/package/emphasis)",
		},
		{
			Token:       vulnhunterFixCommandToken,
			Kind:        slashCommandVulnhunterFix,
			Description: "Test-driven remediation of confirmed vulnerabilities (exploit -> failing test -> fix -> verify)",
			InputHint:   "optional finding/cluster to remediate",
		},
		{
			Token:       vulnhuntFixVerifyCommandToken,
			Kind:        slashCommandVulnhuntFixVerify,
			Description: "Read-only independent verification of claimed security fixes, per-finding verdict",
			InputHint:   "optional findings/fixes to verify",
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
	// Extension commands are advertised alongside the built-ins so a client
	// like Zed offers them in its palette.
	for _, ec := range pandocommands.ExtensionCommands() {
		cmd := acpsdk.AvailableCommand{Name: ec.Name, Description: ec.Description}
		if ec.AcceptsArgs {
			cmd.Input = &acpsdk.AvailableCommandInput{
				Unstructured: &acpsdk.UnstructuredCommandInput{Hint: "arguments"},
			}
		}
		commands = append(commands, cmd)
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
	// Built-ins are matched first, so an extension can never shadow one.
	if pandocommands.IsExtensionCommand(name) {
		return slashCommand{Kind: slashCommandExtension, Name: strings.ToLower(name), Objective: args}, true
	}
	return slashCommand{}, false
}

// parse builds the command, always carrying whatever the user typed after the
// token. Argument-free commands keep it too: most ignore it, but a command like
// /caveman-finish needs to see a stray argument to reject it with its usage
// instead of silently swallowing it.
func (s slashCommandSpec) parse(name, args string) (slashCommand, bool) {
	if !s.matches(name) {
		return slashCommand{}, false
	}
	return slashCommand{Kind: s.Kind, Objective: args}, true
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
