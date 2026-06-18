package models

import (
	"context"
	"testing"
)

// TestAccountScopedStaticModelsMultiAccount verifies that a static-only provider
// (Azure) produces account-prefixed, account-bound copies of its static models
// when more than one account of the type exists — the mechanism that lets such
// providers be selected per-account instead of always resolving to the first.
func TestAccountScopedStaticModelsMultiAccount(t *testing.T) {
	single := AccountScopedStaticModels(ProviderAzure, "azure-a", 1)
	if len(single) != 0 {
		t.Fatalf("single account: got %d models, want 0 (static models used as-is)", len(single))
	}

	multi := AccountScopedStaticModels(ProviderAzure, "azure-a", 2)
	if len(multi) == 0 {
		t.Fatal("multi account: expected per-account static copies")
	}
	for _, m := range multi {
		if m.AccountID != "azure-a" {
			t.Errorf("model %q AccountID = %q, want azure-a", m.ID, m.AccountID)
		}
		if want := ModelID("azure-a." + m.APIModel); m.ID != want {
			t.Errorf("model ID = %q, want %q", m.ID, want)
		}
		if m.Provider != ProviderAzure {
			t.Errorf("model %q Provider = %q, want azure", m.ID, m.Provider)
		}
	}
}

// TestRefreshProviderModelsForAccountStaticProvider verifies the refresh path
// registers per-account static models for a non-listing provider and that the two
// accounts do not clobber each other.
func TestRefreshProviderModelsForAccountStaticProvider(t *testing.T) {
	for _, id := range []string{"azure-a", "azure-b"} {
		params := AccountModelRefreshParams{
			AccountID:         id,
			ProviderType:      ProviderAzure,
			AllAccountsOfType: 2,
		}
		if err := RefreshProviderModelsForAccount(context.Background(), params); err != nil {
			t.Fatalf("refresh %s: %v", id, err)
		}
	}
	t.Cleanup(func() {
		for id := range SupportedModels {
			m := SupportedModels[id]
			if m.Provider == ProviderAzure && (m.AccountID == "azure-a" || m.AccountID == "azure-b") {
				delete(SupportedModels, id)
				dynamicModels.Delete(id)
			}
		}
	})

	var aCount, bCount int
	for _, m := range SupportedModels {
		switch m.AccountID {
		case "azure-a":
			aCount++
		case "azure-b":
			bCount++
		}
	}
	if aCount == 0 || bCount == 0 {
		t.Fatalf("expected per-account Azure models for both accounts, got a=%d b=%d", aCount, bCount)
	}
	if aCount != bCount {
		t.Errorf("accounts should expose the same static catalog: a=%d b=%d", aCount, bCount)
	}
}
