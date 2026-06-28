package acp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/ponytail"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// processPonytailCommand handles `/ponytail [lite|full|ultra|off]`. With no
// argument it defaults to full. It is a synchronous control command (no agent
// turn): it sets the per-session ponytail mode and reports the result.
func (a *PandoACPAgent) processPonytailCommand(acpSession *ACPServerSession, arg string) (acpsdk.StopReason, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		arg = "full"
	}

	applied, ok := a.agentService.SetPonytailMode(acpSession.PandoSessionID(), arg)
	if !ok {
		if err := a.sendAgentText(acpSession, "Unknown ponytail mode "+strconv.Quote(arg)+".\n"+slashCommandUsage(slashCommandPonytail)); err != nil {
			return "", err
		}
		return acpsdk.StopReasonEndTurn, nil
	}

	var msg string
	if applied == "off" {
		msg = "Ponytail mode disabled. Back to normal."
	} else {
		msg = fmt.Sprintf("Ponytail mode: %s.\n%s", applied, ponytail.Description(ponytail.Mode(applied)))
	}
	if err := a.sendAgentText(acpSession, msg); err != nil {
		return "", err
	}
	return acpsdk.StopReasonEndTurn, nil
}
