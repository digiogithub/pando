package models

import (
	"os"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models/modelsdev"
)

// TestLiveRegistryEnrichment exercises the whole path against the real
// models.dev endpoint: a dynamically discovered model with no provider-reported
// pricing must come out of the registry with costs and limits attached, which is
// what makes the session cost panel work.
// Skipped by default; run with
// PANDO_MODELSDEV_LIVE=1 go test ./internal/llm/models -run Live -v
func TestLiveRegistryEnrichment(t *testing.T) {
	if os.Getenv("PANDO_MODELSDEV_LIVE") == "" {
		t.Skip("set PANDO_MODELSDEV_LIVE=1 to query models.dev")
	}

	modelsdev.SetDisabled(false)
	modelsdev.Reset()
	t.Cleanup(func() {
		modelsdev.SetDisabled(true)
		modelsdev.Reset()
	})

	params := AccountModelRefreshParams{AccountID: "copilot-live", ProviderType: ProviderCopilot}
	got := modelFromFetchedAccountModel(t.Context(), params, FetchedModel{ID: "gpt-5.4", Name: "GPT-5.4"})

	t.Logf("%s: ctx=%d max=%d in=%v out=%v cacheWrite=%v cacheRead=%v",
		got.ID, got.ContextWindow, got.DefaultMaxTokens,
		got.CostPer1MIn, got.CostPer1MOut, got.CostPer1MInCached, got.CostPer1MOutCached)

	if got.CostPer1MIn <= 0 || got.CostPer1MOut <= 0 {
		t.Errorf("costs not enriched: %+v", got)
	}
	if got.ContextWindow <= 128_000 {
		t.Errorf("ContextWindow = %d, want the catalog value rather than the 128K fallback", got.ContextWindow)
	}
}
