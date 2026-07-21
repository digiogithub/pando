package models

import (
	"context"
	"testing"
)

// TestModelFromFetchedAccountModelInheritsStaticMetadata verifies that a
// per-account model whose APIModel matches a curated static model inherits that
// static model's rich metadata (reasoning support, costs, context window, name)
// while overriding only the identity fields. This is what lets the selectors hide
// the ambiguous account-less static entries without losing thinking-mode/cost
// data for multi-account provider types.
func TestModelFromFetchedAccountModelInheritsStaticMetadata(t *testing.T) {
	// Pick a static Anthropic model that supports reasoning to make the assertion meaningful.
	var staticAPIModel string
	var staticName string
	for _, m := range AnthropicModels {
		if m.CanReason && m.CostPer1MIn > 0 {
			staticAPIModel = m.APIModel
			staticName = m.Name
			break
		}
	}
	if staticAPIModel == "" {
		t.Skip("no reasoning-capable static Anthropic model found to exercise enrichment")
	}

	params := AccountModelRefreshParams{
		AccountID:         "anthropic-project",
		ProviderType:      ProviderAnthropic,
		AllAccountsOfType: 2, // forces an account-prefixed ID
	}
	got := modelFromFetchedAccountModel(context.Background(), params, FetchedModel{ID: staticAPIModel, Name: "ignored remote name"})

	if got.ID != ModelID("anthropic-project."+staticAPIModel) {
		t.Fatalf("ID = %q, want account-prefixed id", got.ID)
	}
	if got.AccountID != "anthropic-project" {
		t.Fatalf("AccountID = %q, want anthropic-project", got.AccountID)
	}
	if got.APIModel != staticAPIModel {
		t.Fatalf("APIModel = %q, want %q", got.APIModel, staticAPIModel)
	}
	if !got.CanReason {
		t.Error("CanReason should be inherited from the static model")
	}
	if got.CostPer1MIn == 0 {
		t.Error("cost metadata should be inherited from the static model")
	}
	if got.ContextWindow == 0 {
		t.Error("context window should be inherited from the static model")
	}
	if got.Name != staticName {
		t.Errorf("Name = %q, want inherited static name %q", got.Name, staticName)
	}
}

// TestModelFromFetchedAccountModelFallsBackWithoutStatic verifies the legacy path
// (no matching static model) still produces a usable account-scoped model.
func TestModelFromFetchedAccountModelFallsBackWithoutStatic(t *testing.T) {
	params := AccountModelRefreshParams{
		AccountID:         "acct",
		ProviderType:      ProviderOpenAICompatible,
		AllAccountsOfType: 1,
	}
	got := modelFromFetchedAccountModel(context.Background(), params, FetchedModel{ID: "totally-unknown-model"})

	if got.AccountID != "acct" {
		t.Fatalf("AccountID = %q, want acct", got.AccountID)
	}
	if got.APIModel != "totally-unknown-model" {
		t.Fatalf("APIModel = %q", got.APIModel)
	}
	if got.ContextWindow == 0 {
		t.Error("a default context window should be applied when no static model matches")
	}
}
