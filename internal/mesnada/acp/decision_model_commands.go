package acp

import (
	"context"
	"strings"

	acpsdk "github.com/madeindigio/acp-go-sdk"

	"github.com/digiogithub/pando/internal/llm/modelrouter"
)

// processDecisionModelCommand handles `/decision-model [test]`. It is a control
// command like /design: it answers locally (no model turn) with the same report
// as `pando_setup decision-model show`, API key masked.
func (a *PandoACPAgent) processDecisionModelCommand(ctx context.Context, acpSession *ACPServerSession, arg string) (acpsdk.StopReason, error) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "", "show", "test":
	default:
		return a.designReply(acpSession, slashCommandUsage(slashCommandDecisionModel))
	}
	out, err := modelrouter.RenderStatus(ctx, true)
	if err != nil {
		out = "/decision-model failed: " + err.Error()
	}
	return a.designReply(acpSession, out)
}
