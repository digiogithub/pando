package acp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/agentsmd"
	pandocommands "github.com/digiogithub/pando/internal/commands"
	"github.com/digiogithub/pando/internal/vulnhunter"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

func (a *PandoACPAgent) handleSlashCommand(
	ctx context.Context,
	sessionID acpsdk.SessionId,
	acpSession *ACPServerSession,
	command slashCommand,
) (acpsdk.StopReason, error) {
	switch command.Kind {
	case slashCommandGoal:
		if strings.TrimSpace(command.Objective) == "" {
			if err := a.sendAgentText(acpSession, slashCommandUsage(slashCommandGoal)); err != nil {
				return "", err
			}
			return acpsdk.StopReasonEndTurn, nil
		}
		return a.processGoalPrompt(ctx, sessionID, acpSession, command.Objective)
	case slashCommandGoalStatus:
		if err := a.sendAgentText(acpSession, "Checking goal status..."); err != nil {
			return "", err
		}
		goal, err := a.syncGoalState(ctx, sessionID, false)
		if err != nil {
			return "", err
		}
		a.sendGoalStateUpdate(sessionID, goal)
		if err := a.sendAgentText(acpSession, formatGoalStatus(goal)); err != nil {
			return "", err
		}
		return acpsdk.StopReasonEndTurn, nil
	case slashCommandGoalCancel:
		if err := a.sendAgentText(acpSession, "Cancelling current goal..."); err != nil {
			return "", err
		}
		acpSession.CancelGoalExecution()
		goal, err := a.sessionService.CancelGoal(context.Background(), acpSession.PandoSessionID())
		if err != nil {
			if isGoalNotFound(err) {
				if err := a.sendAgentText(acpSession, "No active goal is running."); err != nil {
					return "", err
				}
				a.sendGoalStateUpdate(sessionID, nil)
				return acpsdk.StopReasonEndTurn, nil
			}
			return "", err
		}
		update := goalStateFromDB(goal, time.Now())
		a.sendGoalStateUpdate(sessionID, update)
		if err := a.sendAgentText(acpSession, "Cancelled current goal.\n\n"+formatGoalStatus(update)); err != nil {
			return "", err
		}
		return acpsdk.StopReasonCancelled, nil
	case slashCommandSummarize:
		return a.processSummarizeCommand(ctx, acpSession)
	case slashCommandDBCompact:
		return a.processDBCompactCommand(ctx, acpSession)
	case slashCommandPonytail:
		return a.processPonytailCommand(acpSession, command.Objective)
	case slashCommandCaveman:
		return a.processCavemanCommand(acpSession, command.Objective)
	case slashCommandCavemanFinish:
		return a.processCavemanFinishCommand(acpSession, command.Objective)
	case slashCommandSuperpowers:
		return a.processSuperpowersCommand(acpSession, command.Objective)
	case slashCommandSuperpowersFinish:
		return a.processSuperpowersFinishCommand(ctx, acpSession)
	case slashCommandLearning:
		return a.processLearningCommand(acpSession, command.Objective)
	case slashCommandLearningFinish:
		return a.processLearningFinishCommand(ctx, acpSession)
	case slashCommandImproveAgentsMd:
		return a.processImproveAgentsMdCommand(ctx, acpSession, command.Objective)
	case slashCommandVulnhunt:
		return a.processVulnhunterCommand(ctx, acpSession, "Starting security audit (vulnhunt)...", vulnhunter.HuntPrompt(command.Objective))
	case slashCommandVulnhunterFix:
		return a.processVulnhunterCommand(ctx, acpSession, "Starting test-driven remediation (vulnhunter-fix)...", vulnhunter.FixPrompt(command.Objective))
	case slashCommandVulnhuntFixVerify:
		return a.processVulnhunterCommand(ctx, acpSession, "Verifying claimed security fixes (vulnhunt-fix-verify)...", vulnhunter.VerifyPrompt(command.Objective))
	case slashCommandDesign:
		return a.processDesignCommand(ctx, acpSession, command.Objective)
	case slashCommandDesignOpen:
		return a.processDesignOpenCommand(ctx, acpSession, command.Objective)
	case slashCommandDesignVersions:
		return a.processDesignVersionsCommand(ctx, acpSession, command.Objective)
	case slashCommandDesignSystem:
		return a.processDesignSystemCommand(acpSession)
	case slashCommandDesignTemplates:
		return a.processDesignTemplatesCommand(acpSession)
	case slashCommandExtension:
		return a.processExtensionCommand(ctx, acpSession, command)
	default:
		return acpsdk.StopReasonEndTurn, nil
	}
}

// processExtensionCommand runs a slash command owned by a compiled-in
// extension. Output is shown to the user directly; Prompt starts a normal model
// turn, which is how prompt-expanding commands work. A command that returns
// neither only changed hidden state, and ends the turn silently.
func (a *PandoACPAgent) processExtensionCommand(
	ctx context.Context,
	acpSession *ACPServerSession,
	command slashCommand,
) (acpsdk.StopReason, error) {
	res, handled, err := pandocommands.RunExtension(ctx, command.Name, command.Objective)
	if err != nil {
		if sendErr := a.sendAgentText(acpSession, fmt.Sprintf("/%s failed: %v", command.Name, err)); sendErr != nil {
			return "", sendErr
		}
		return acpsdk.StopReasonEndTurn, nil
	}
	if !handled {
		// The command disappeared between parsing and running (configuration
		// reload). Say so rather than sending the raw slash text to the model.
		if sendErr := a.sendAgentText(acpSession, fmt.Sprintf("/%s is no longer available", command.Name)); sendErr != nil {
			return "", sendErr
		}
		return acpsdk.StopReasonEndTurn, nil
	}
	if res.Output != "" {
		if sendErr := a.sendAgentText(acpSession, res.Output); sendErr != nil {
			return "", sendErr
		}
	}
	if strings.TrimSpace(res.Prompt) == "" {
		return acpsdk.StopReasonEndTurn, nil
	}
	return a.processPromptWithAgent(ctx, acpSession, res.Prompt)
}

func (a *PandoACPAgent) processGoalPrompt(
	ctx context.Context,
	sessionID acpsdk.SessionId,
	acpSession *ACPServerSession,
	objective string,
) (acpsdk.StopReason, error) {
	if existingGoal, err := a.syncGoalState(ctx, sessionID, true); err != nil {
		return "", err
	} else if existingGoal != nil {
		a.sendGoalStateUpdate(sessionID, existingGoal)
		if err := a.sendAgentText(acpSession, "A goal is already running.\n\n"+formatGoalStatus(existingGoal)); err != nil {
			return "", err
		}
		return acpsdk.StopReasonEndTurn, nil
	}

	if err := a.sendAgentText(acpSession, "Starting goal mode..."); err != nil {
		return "", err
	}

	acpSession.SetMode(goalModeID)
	acpSession.SetAskPermission(defaultAskPermissionForMode(goalModeID))
	acpSession.SetPermissionConfigured(false)
	a.sendCurrentModeUpdate(ctx, sessionID, goalModeID)
	a.sendSessionConfigOptionsUpdate(ctx, sessionID)
	cleanupPermissions := a.configurePermissionMode(sessionID, goalModeID, acpSession.AskPermission())
	defer cleanupPermissions()

	goalCtx, cancel := context.WithCancel(acpSession.Context())
	acpSession.SetGoalCancel(cancel)
	defer acpSession.ClearGoalCancel()

	reconcileACPSessionModel(a.agentService, acpSession)
	reconcileACPThinkingSession(a.agentService, acpSession)
	a.agentService.SetSessionLLMOverrides(acpSession.PandoSessionID(), sessionLLMOverridesFor(acpSession))
	eventChan, err := a.agentService.RunGoal(goalCtx, acpSession.PandoSessionID(), objective)
	if err != nil {
		return "", err
	}

	if goal, err := a.loadGoalState(context.Background(), acpSession.PandoSessionID(), true); err == nil {
		a.sendGoalStateUpdate(sessionID, goal)
	}

	stopReason, err := a.processAgentEventStream(goalCtx, acpSession, eventChan)
	finalGoal, finalErr := a.loadGoalState(context.Background(), acpSession.PandoSessionID(), false)
	if finalErr != nil && !isGoalNotFound(finalErr) {
		return stopReason, finalErr
	}
	if isGoalNotFound(finalErr) {
		finalGoal = nil
	}
	a.sendGoalStateUpdate(sessionID, finalGoal)
	if finalGoal != nil && finalGoal.Status == "cancelled" {
		stopReason = acpsdk.StopReasonCancelled
	}
	if err != nil {
		return stopReason, err
	}
	if stopReason == acpsdk.StopReasonCancelled {
		if err := a.sendAgentText(acpSession, "Goal mode cancelled."); err != nil {
			return stopReason, err
		}
		return stopReason, nil
	}
	if finalGoal != nil {
		if err := a.sendAgentText(acpSession, "Goal mode complete.\n\n"+formatGoalStatus(finalGoal)); err != nil {
			return stopReason, err
		}
	} else {
		if err := a.sendAgentText(acpSession, "Goal mode complete."); err != nil {
			return stopReason, err
		}
	}
	return stopReason, nil
}

// processImproveAgentsMdCommand runs a normal agent turn whose task is to create
// or reinforce the project's AGENTS.md with the canonical MANDATORY operating
// rules. It expands the slash command into a full instruction prompt and streams
// the resulting agent turn back to the client.
func (a *PandoACPAgent) processImproveAgentsMdCommand(ctx context.Context, acpSession *ACPServerSession, extra string) (acpsdk.StopReason, error) {
	if err := a.sendAgentText(acpSession, "Improving AGENTS.md..."); err != nil {
		return "", err
	}
	return a.processPromptWithAgent(ctx, acpSession, agentsmd.Prompt(extra))
}

// processVulnhunterCommand runs a normal agent turn whose task is one of the
// security-audit workflows (/vulnhunt, /vulnhunter-fix, /vulnhunt-fix-verify).
// The slash command has already been expanded into the full workflow prompt; this
// sends a short intro line and streams the resulting agent turn back to the
// client, exactly like /improve-agents-md.
func (a *PandoACPAgent) processVulnhunterCommand(ctx context.Context, acpSession *ACPServerSession, intro, prompt string) (acpsdk.StopReason, error) {
	if err := a.sendAgentText(acpSession, intro); err != nil {
		return "", err
	}
	return a.processPromptWithAgent(ctx, acpSession, prompt)
}

func (a *PandoACPAgent) processSummarizeCommand(ctx context.Context, acpSession *ACPServerSession) (acpsdk.StopReason, error) {
	if err := a.sendAgentText(acpSession, "Starting session summary..."); err != nil {
		return "", err
	}

	eventChan, err := a.startManualSummary(ctx, acpSession.PandoSessionID())
	if err != nil {
		return "", err
	}

	stopReason, err := a.processAgentEventStream(ctx, acpSession, eventChan)
	if err != nil {
		return stopReason, err
	}
	if stopReason != acpsdk.StopReasonCancelled {
		if err := a.sendAgentText(acpSession, "Session summary complete."); err != nil {
			return stopReason, err
		}
	}
	return stopReason, nil
}

// processDBCompactCommand runs a database VACUUM in response to /db-compact and
// reports the reclaimed space. It is a synchronous maintenance action (no agent
// turn), so it ends the turn after sending the result text.
func (a *PandoACPAgent) processDBCompactCommand(ctx context.Context, acpSession *ACPServerSession) (acpsdk.StopReason, error) {
	if a.dbCompactor == nil {
		if err := a.sendAgentText(acpSession, "Database compaction is not available in this mode."); err != nil {
			return "", err
		}
		return acpsdk.StopReasonEndTurn, nil
	}

	if err := a.sendAgentText(acpSession, "Compacting database (VACUUM)..."); err != nil {
		return "", err
	}

	res, err := a.dbCompactor.CompactDatabase(ctx, false, true)
	if err != nil {
		if sendErr := a.sendAgentText(acpSession, "Database compaction failed: "+err.Error()); sendErr != nil {
			return "", sendErr
		}
		return acpsdk.StopReasonEndTurn, nil
	}

	msg := fmt.Sprintf("Database compacted (%s). Freed %s (%s → %s).",
		res.Mode, formatACPBytes(res.Freed), formatACPBytes(res.SizeBefore), formatACPBytes(res.SizeAfter))
	if err := a.sendAgentText(acpSession, msg); err != nil {
		return "", err
	}
	return acpsdk.StopReasonEndTurn, nil
}

// formatACPBytes renders a byte count in human-readable units for slash-command output.
func formatACPBytes(n int64) string {
	neg := ""
	if n < 0 {
		neg = "-"
		n = -n
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%s%d B", neg, n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%s%.1f %cB", neg, float64(n)/float64(div), "KMGTPE"[exp])
}

func (a *PandoACPAgent) startManualSummary(ctx context.Context, sessionID string) (<-chan AgentEvent, error) {
	// Summarize returns a channel that stays open until the (asynchronous) summary
	// actually finishes, so processAgentEventStream blocks on real completion
	// instead of ending the turn immediately.
	return a.agentService.Summarize(ctx, sessionID)
}

func (a *PandoACPAgent) sendAgentText(acpSession *ACPServerSession, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	messageID := acpSession.PandoSessionID() + "-slash-command"
	return acpSession.SendUpdate(updateAgentMessageTextWithID(text, messageID))
}
