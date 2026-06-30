package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/savings"
)

const (
	SavingsStatsToolName = "pando_stats"

	savingsStatsDescription = `Report cumulative token savings from Pando's context-optimization features.

Aggregates the append-only savings ledger and returns how many tokens were saved
by:
- compressed file reads (view signatures/map/auto modes)
- unchanged-re-read F-references (dedup)
- RTK-style shell-output filtering (bash)

Returns totals, percentage reduction, and a per-source breakdown. Use it to verify
that token optimization is actually paying off this project.`
)

type savingsStatsTool struct{}

// NewSavingsStatsTool returns the MCP tool that reports token-savings analytics.
func NewSavingsStatsTool() BaseTool {
	return &savingsStatsTool{}
}

func (s *savingsStatsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        SavingsStatsToolName,
		Description: savingsStatsDescription,
		Parameters: map[string]any{
			"days": map[string]any{
				"type":        "integer",
				"description": "Only count savings from the last N days (0 or omitted = all time).",
			},
		},
		Required: []string{},
	}
}

// SavingsStatsResponse is the structured metadata attached to the response.
type SavingsStatsResponse struct {
	Report savings.Report `json:"report"`
}

func (s *savingsStatsTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params struct {
		Days int `json:"days"`
	}
	_ = DecodeToolInput(call.Input, &params)

	cfg := config.Get()
	if cfg == nil || cfg.Data.Directory == "" {
		return NewTextResponse("No data directory configured; savings ledger unavailable."), nil
	}

	opts := savings.SummaryOptions{}
	if params.Days > 0 {
		opts.Since = time.Now().AddDate(0, 0, -params.Days)
	}

	rep, err := savings.Summarize(cfg.Data.Directory, opts)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("failed to read savings ledger: %s", err)), nil
	}

	var sb strings.Builder
	sb.WriteString("## Token Savings\n\n")
	if !cfg.SavingsLedgerEnabled() {
		sb.WriteString("_(savings ledger is currently disabled in config; showing recorded history)_\n\n")
	}
	if rep.Events == 0 {
		sb.WriteString("No savings recorded yet.\n")
		return WithResponseMetadata(NewTextResponse(sb.String()), SavingsStatsResponse{Report: rep}), nil
	}

	scope := "all time"
	if params.Days > 0 {
		scope = fmt.Sprintf("last %d days", params.Days)
	}
	sb.WriteString(fmt.Sprintf("- Scope: %s (%d events)\n", scope, rep.Events))
	sb.WriteString(fmt.Sprintf("- Saved: %d tokens (%.1f%% reduction)\n", rep.SavedTokens, rep.ReductionPct))
	sb.WriteString(fmt.Sprintf("- Baseline → actual: %d → %d tokens\n\n", rep.BaselineTokens, rep.ActualTokens))

	sb.WriteString("### By source\n\n")
	for _, st := range rep.BySource {
		sb.WriteString(fmt.Sprintf("- %-7s %d saved (%d events)\n", st.Source, st.Saved, st.Events))
	}

	return WithResponseMetadata(NewTextResponse(sb.String()), SavingsStatsResponse{Report: rep}), nil
}
