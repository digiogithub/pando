package acp

import (
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/caveman"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// processCavemanCommand handles `/caveman [lite|full|ultra|wenyan]`. With no
// argument it defaults to full. It is a synchronous control command (no agent
// turn): it sets the per-session output-brevity level and reports the result.
func (a *PandoACPAgent) processCavemanCommand(acpSession *ACPServerSession, arg string) (acpsdk.StopReason, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		arg = "full"
	}

	applied, ok := a.agentService.SetCavemanMode(acpSession.PandoSessionID(), arg)
	if !ok {
		if err := a.sendAgentText(acpSession, "Unknown caveman level "+strconv.Quote(arg)+".\n"+slashCommandUsage(slashCommandCaveman)); err != nil {
			return "", err
		}
		return acpsdk.StopReasonEndTurn, nil
	}

	if err := a.sendAgentText(acpSession, caveman.ActivationMessage(caveman.Mode(applied))); err != nil {
		return "", err
	}
	return acpsdk.StopReasonEndTurn, nil
}

// processCavemanFinishCommand handles `/caveman-finish`, which takes no level. A
// stray argument is reported rather than ignored, so "/caveman-finish ultra"
// cannot look like it switched levels when it actually disabled the mode.
func (a *PandoACPAgent) processCavemanFinishCommand(acpSession *ACPServerSession, arg string) (acpsdk.StopReason, error) {
	if strings.TrimSpace(arg) != "" {
		if err := a.sendAgentText(acpSession, slashCommandUsage(slashCommandCavemanFinish)); err != nil {
			return "", err
		}
		return acpsdk.StopReasonEndTurn, nil
	}

	a.agentService.SetCavemanMode(acpSession.PandoSessionID(), "off")
	if err := a.sendAgentText(acpSession, caveman.DisabledMessage); err != nil {
		return "", err
	}
	return acpsdk.StopReasonEndTurn, nil
}
