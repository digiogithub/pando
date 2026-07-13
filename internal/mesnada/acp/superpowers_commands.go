package acp

import (
	"context"

	"github.com/digiogithub/pando/internal/superpowers"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// processSuperpowersCommand handles `/superpowers [objective]`. It is a
// synchronous control command (no agent turn): it enables the per-session
// workflow policy and confirms. Re-invoking it is idempotent.
func (a *PandoACPAgent) processSuperpowersCommand(acpSession *ACPServerSession, objective string) (acpsdk.StopReason, error) {
	sessionID := acpSession.PandoSessionID()

	msg := superpowers.ActivationMessage(objective)
	if a.agentService.SuperpowersMode(sessionID) {
		msg = superpowers.AlreadyActiveMessage
	} else {
		a.agentService.SetSuperpowersMode(sessionID, true)
	}

	if err := a.sendAgentText(acpSession, msg); err != nil {
		return "", err
	}
	return acpsdk.StopReasonEndTurn, nil
}

// processSuperpowersFinishCommand handles `/superpowers-finish`. Unlike
// activation, it runs a real agent turn (verify, report, list what is left) and
// streams it back. The mode is cleared inside agent.RunSuperpowersFinish only
// when that turn succeeds, so a cancelled or failed close keeps the workflow on.
func (a *PandoACPAgent) processSuperpowersFinishCommand(ctx context.Context, acpSession *ACPServerSession) (acpsdk.StopReason, error) {
	sessionID := acpSession.PandoSessionID()

	if !a.agentService.SuperpowersMode(sessionID) {
		if err := a.sendAgentText(acpSession, superpowers.NotActiveMessage); err != nil {
			return "", err
		}
		return acpsdk.StopReasonEndTurn, nil
	}

	if err := a.sendAgentText(acpSession, "Closing the Superpowers workflow: verifying and reporting..."); err != nil {
		return "", err
	}

	eventChan, err := a.agentService.SuperpowersFinish(ctx, sessionID)
	if err != nil {
		return "", err
	}

	stopReason, err := a.processAgentEventStream(ctx, acpSession, eventChan)
	if err != nil {
		return stopReason, err
	}
	if stopReason == acpsdk.StopReasonCancelled {
		if err := a.sendAgentText(acpSession, "Close cancelled. Superpowers mode stays active."); err != nil {
			return stopReason, err
		}
		return stopReason, nil
	}
	if err := a.sendAgentText(acpSession, "Superpowers workflow closed. Back to normal mode."); err != nil {
		return stopReason, err
	}
	return stopReason, nil
}
