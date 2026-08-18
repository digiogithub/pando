package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/agentsmd"
	"github.com/digiogithub/pando/internal/caveman"
	"github.com/digiogithub/pando/internal/commands"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/learning"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/ponytail"
	"github.com/digiogithub/pando/internal/project"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/superpowers"
	"github.com/digiogithub/pando/internal/vulnhunter"
)

// globalProjectServiceForTools is the project registry the pando_setup "projects"
// command reads from. Wired once from app.go via SetProjectServiceForTools, same
// pattern as globalLuaManagerForTools: the registry is a process-wide singleton
// and threading it through every CoderAgentTools call site would only add noise.
var globalProjectServiceForTools project.Service

// SetProjectServiceForTools wires the project registry used by pando_setup's
// "projects" command. Called once at startup from app.go; a nil service is
// tolerated and leaves the command reporting "unavailable".
func SetProjectServiceForTools(svc project.Service) {
	globalProjectServiceForTools = svc
}

// setupBridge implements tools.SetupBridge. It lives in this package because
// the session modes and the session store are owned here, and internal/llm/tools
// cannot import either of them (both import it).
type setupBridge struct {
	sessions session.Service
}

// NewSetupBridge wires the pando_setup tool to the session store and the
// session-mode registry. A nil session service is tolerated: usage reporting
// then fails with a clear message instead of panicking.
func NewSetupBridge(sessions session.Service) tools.SetupBridge {
	return &setupBridge{sessions: sessions}
}

// SessionInfo returns the persisted usage counters of a session, plus the model
// currently configured for the coder agent.
func (b *setupBridge) SessionInfo(ctx context.Context, sessionID string) (tools.SetupSessionInfo, error) {
	if b.sessions == nil {
		return tools.SetupSessionInfo{}, fmt.Errorf("the session store is not available")
	}
	sess, err := b.sessions.Get(ctx, sessionID)
	if err != nil {
		return tools.SetupSessionInfo{}, err
	}

	info := tools.SetupSessionInfo{
		ID:                  sess.ID,
		Title:               sess.Title,
		MessageCount:        sess.MessageCount,
		PromptTokens:        sess.PromptTokens,
		CompletionTokens:    sess.CompletionTokens,
		CacheReadTokens:     sess.CacheReadTokens,
		CacheCreationTokens: sess.CacheCreationTokens,
		ReasoningTokens:     sess.ReasoningTokens,
		Cost:                sess.Cost,
	}

	// Report the model actually in force, which is the session override when one
	// is set — reporting the globally configured model would contradict what the
	// "model" command says.
	if id, _ := effectiveSessionModel(sessionID); id != "" {
		info.Model = string(id)
		if model, known := models.SupportedModels()[id]; known {
			info.ContextWindow = model.ContextWindow
		}
	}
	return info, nil
}

// SessionModes reports the state of every opt-in session mode.
func (b *setupBridge) SessionModes(sessionID string) []tools.SetupMode {
	return []tools.SetupMode{
		{Name: "caveman", State: CavemanMode(sessionID).String()},
		{Name: "ponytail", State: PonytailMode(sessionID).String()},
		{Name: "superpowers", State: onOff(SuperpowersMode(sessionID))},
		{Name: "learning", State: onOff(LearningMode(sessionID))},
	}
}

// Projects lists the projects registered in this Pando instance's project
// registry — the same registry mesnada_spawn_agent's "project" argument
// resolves ids/paths/names against.
func (b *setupBridge) Projects(ctx context.Context) ([]tools.SetupProjectRef, error) {
	if globalProjectServiceForTools == nil {
		return nil, fmt.Errorf("the project registry is unavailable in this runtime")
	}
	list, err := globalProjectServiceForTools.List(ctx)
	if err != nil {
		return nil, err
	}
	refs := make([]tools.SetupProjectRef, 0, len(list))
	for _, p := range list {
		refs = append(refs, tools.SetupProjectRef{ID: p.ID, Name: p.Name, Path: p.Path, Status: p.Status})
	}
	return refs, nil
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

// setupRunnableSpec declares one slash command the agent may activate itself.
type setupRunnableSpec struct {
	Name    string
	Args    string
	Summary string
	// Apply changes session state and returns the confirmation to show. Exactly
	// one of Apply and Prompt is set.
	Apply func(sessionID, args string) (string, error)
	// Prompt returns instructions the agent must follow in the current turn.
	Prompt func(args string) string
}

// setupBlockedCommands are slash commands the agent must not trigger itself,
// with the reason reported to it.
var setupBlockedCommands = map[string]string{
	"goal":               "goal mode is started by the user",
	"autopilot":          "goal mode is started by the user",
	"goal-status":        "goal mode is started by the user",
	"goal-cancel":        "goal mode is started by the user",
	"compact":            "compaction cannot run inside an active turn",
	"summarize":          "compaction cannot run inside an active turn",
	"db-compact":         "database maintenance is triggered by the user",
	"superpowers-finish": "the closing turn is user-driven so the mode clears only on success",
	"learning-finish":    "the closing turn is user-driven so the mode clears only on success",
}

func setupRunnableSpecs() []setupRunnableSpec {
	return []setupRunnableSpec{
		{
			Name:    "caveman",
			Args:    "[lite|full|ultra]",
			Summary: "Shorter answers to cut output tokens",
			Apply: func(sessionID, args string) (string, error) {
				level := strings.TrimSpace(args)
				if level == "" {
					level = "full"
				}
				mode, ok := caveman.ParseMode(level)
				if !ok {
					return "", fmt.Errorf("unknown caveman level %q. %s", level, caveman.Usage)
				}
				SetCavemanMode(sessionID, mode)
				return caveman.ActivationMessage(mode), nil
			},
		},
		{
			Name:    "caveman-finish",
			Summary: "Return to normal output length",
			Apply: func(sessionID, args string) (string, error) {
				if strings.TrimSpace(args) != "" {
					return "", fmt.Errorf("%s", caveman.FinishUsage)
				}
				SetCavemanMode(sessionID, caveman.ModeOff)
				return caveman.DisabledMessage, nil
			},
		},
		{
			Name:    "ponytail",
			Args:    "[lite|full|ultra|off]",
			Summary: "Toggle lazy-senior-dev mode",
			Apply: func(sessionID, args string) (string, error) {
				level := strings.TrimSpace(args)
				if level == "" {
					level = "full"
				}
				mode, ok := ponytail.ParseMode(level)
				if !ok {
					return "", fmt.Errorf("unknown ponytail mode %q. Use lite, full, ultra or off", level)
				}
				SetPonytailMode(sessionID, mode)
				if !mode.IsActive() {
					return "Ponytail mode disabled. Back to normal.", nil
				}
				return "Ponytail mode: " + mode.String() + ".\n" + ponytail.Description(mode), nil
			},
		},
		{
			Name:    "superpowers",
			Args:    "[objective]",
			Summary: "Enable the disciplined plan-first, verify-always workflow",
			Apply: func(sessionID, args string) (string, error) {
				if SuperpowersMode(sessionID) {
					return superpowers.AlreadyActiveMessage, nil
				}
				SetSuperpowersMode(sessionID, true)
				return superpowers.ActivationMessage(args), nil
			},
		},
		{
			Name:    "learning",
			Args:    "[focus]",
			Summary: "Enable learner mode: read the KB more and document discoveries",
			Apply: func(sessionID, args string) (string, error) {
				if LearningMode(sessionID) {
					return learning.AlreadyActiveMessage, nil
				}
				SetLearningMode(sessionID, true)
				return learning.ActivationMessage(args), nil
			},
		},
		{
			Name:    "improve-agents-md",
			Args:    "[guidance]",
			Summary: "Create or reinforce AGENTS.md with the mandatory agent rules",
			Prompt:  agentsmd.Prompt,
		},
		{
			Name:    "vulnhunt",
			Args:    "[scope]",
			Summary: "Adversarial security audit from attacker input to sinks",
			Prompt:  vulnhunter.HuntPrompt,
		},
		{
			Name:    "vulnhunter-fix",
			Args:    "[finding]",
			Summary: "Test-driven remediation of confirmed vulnerabilities",
			Prompt:  vulnhunter.FixPrompt,
		},
		{
			Name:    "vulnhunt-fix-verify",
			Args:    "[findings]",
			Summary: "Independent read-only verification of claimed security fixes",
			Prompt:  vulnhunter.VerifyPrompt,
		},
	}
}

func lookupSetupRunnable(name string) (setupRunnableSpec, bool) {
	for _, spec := range setupRunnableSpecs() {
		if spec.Name == name {
			return spec, true
		}
	}
	return setupRunnableSpec{}, false
}

// Commands lists the built-in slash commands (runnable or user-only) followed by
// the custom user/project commands, which are always runnable.
func (b *setupBridge) Commands() []tools.SetupRunnableCommand {
	var out []tools.SetupRunnableCommand
	for _, spec := range setupRunnableSpecs() {
		out = append(out, tools.SetupRunnableCommand{
			Name:    spec.Name,
			Args:    spec.Args,
			Summary: spec.Summary,
		})
	}

	for name, reason := range setupBlockedCommands {
		summary := name
		for _, cmd := range commands.BuiltinCommands() {
			if cmd.Name == name {
				summary = cmd.Description
				break
			}
		}
		out = append(out, tools.SetupRunnableCommand{Name: name, Summary: summary, Reason: reason})
	}

	for _, cmd := range commands.AllCommands(setupDataDir()) {
		if !strings.Contains(cmd.Name, ":") {
			continue
		}
		out = append(out, tools.SetupRunnableCommand{Name: cmd.Name, Summary: cmd.Description})
	}
	return out
}

// RunCommand activates a slash command for the session. Mode commands mutate the
// session and return their confirmation; instruction commands return the prompt
// text the agent must act on straight away.
func (b *setupBridge) RunCommand(ctx context.Context, sessionID, name, args string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))

	if reason, blocked := setupBlockedCommands[name]; blocked {
		return "", fmt.Errorf("/%s cannot be activated from a tool call: %s", name, reason)
	}

	if spec, ok := lookupSetupRunnable(name); ok {
		if spec.Apply != nil {
			out, err := spec.Apply(sessionID, args)
			if err != nil {
				return "", err
			}
			return out + "\n\nIt applies from your next turn on.", nil
		}
		return setupInstructionResult("/"+name, spec.Prompt(args)), nil
	}

	if body, ok := commands.Content(setupDataDir(), name); ok {
		if args != "" {
			body = strings.TrimRight(body, "\n") + "\n\nArguments: " + args + "\n"
		}
		return setupInstructionResult("/"+name, body), nil
	}

	return "", fmt.Errorf("unknown slash command %q. Run pando_setup with command=\"commands\" to list them", name)
}

// setupInstructionResult frames a command body so the agent treats it as work to
// do now, not as data to report back.
func setupInstructionResult(name, body string) string {
	return fmt.Sprintf("%s is now active. Follow these instructions in this turn:\n\n%s", name, body)
}

func setupDataDir() string {
	if cfg := config.Get(); cfg != nil {
		return cfg.Data.Directory
	}
	return ""
}
