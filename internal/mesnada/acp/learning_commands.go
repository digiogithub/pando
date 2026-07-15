package acp

import (
	"context"

	"github.com/digiogithub/pando/internal/learning"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// processLearningCommand handles `/learning [focus]`. It is a synchronous control
// command (no agent turn): it enables the per-session learner/documentarian policy
// and confirms. Re-invoking it is idempotent.
func (a *PandoACPAgent) processLearningCommand(acpSession *ACPServerSession, focus string) (acpsdk.StopReason, error) {
	sessionID := acpSession.PandoSessionID()

	msg := learning.ActivationMessage(focus)
	if a.agentService.LearningMode(sessionID) {
		msg = learning.AlreadyActiveMessage
	} else {
		a.agentService.SetLearningMode(sessionID, true)
	}

	if err := a.sendAgentText(acpSession, msg); err != nil {
		return "", err
	}
	return acpsdk.StopReasonEndTurn, nil
}

// processLearningFinishCommand handles `/learning-finish`. Unlike activation, it
// runs a real agent turn (consolidate learnings into KB/memory, mark stale docs,
// report) and streams it back. The mode is cleared inside agent.RunLearningFinish
// only when that turn succeeds, so a cancelled or failed close keeps the mode on.
func (a *PandoACPAgent) processLearningFinishCommand(ctx context.Context, acpSession *ACPServerSession) (acpsdk.StopReason, error) {
	sessionID := acpSession.PandoSessionID()

	if !a.agentService.LearningMode(sessionID) {
		if err := a.sendAgentText(acpSession, learning.NotActiveMessage); err != nil {
			return "", err
		}
		return acpsdk.StopReasonEndTurn, nil
	}

	if err := a.sendAgentText(acpSession, "Closing Learning mode: consolidating what was learned..."); err != nil {
		return "", err
	}

	eventChan, err := a.agentService.LearningFinish(ctx, sessionID)
	if err != nil {
		return "", err
	}

	stopReason, err := a.processAgentEventStream(ctx, acpSession, eventChan)
	if err != nil {
		return stopReason, err
	}
	if stopReason == acpsdk.StopReasonCancelled {
		if err := a.sendAgentText(acpSession, "Close cancelled. Learning mode stays active."); err != nil {
			return stopReason, err
		}
		return stopReason, nil
	}
	if err := a.sendAgentText(acpSession, "Learning mode closed. Back to normal mode."); err != nil {
		return stopReason, err
	}
	return stopReason, nil
}
