package models

import "testing"

func TestGeminiStaticModelsAreRegistered(t *testing.T) {
	for _, id := range []ModelID{
		// Gemini 3 preview
		Gemini31ProPreview,
		Gemini31ProPreviewBase,
		Gemini31FlashLitePreview,
		Gemini30ProPreview,
		Gemini30Flash,
		Gemini30FlashLegacy,
		// Gemini 2.5 stable
		Gemini25,
		Gemini25Flash,
		Gemini25FlashLite,
		// Gemini 2.5 legacy preview aliases
		Gemini25LegacyPreview,
		Gemini25FlashLegacyPreview,
		// Gemini 2.0 stable
		Gemini20Flash,
		Gemini20FlashLite,
	} {
		if _, ok := SupportedModels[id]; !ok {
			t.Fatalf("SupportedModels missing Gemini model %q", id)
		}
	}
}

func TestGeminiLegacyAliasesPointToCurrentAPIModel(t *testing.T) {
	checks := []struct {
		id           ModelID
		wantAPIModel string
	}{
		{Gemini25LegacyPreview, "gemini-2.5-pro"},
		{Gemini25FlashLegacyPreview, "gemini-2.5-flash"},
		{Gemini30FlashLegacy, "gemini-3-flash-preview"},
	}
	for _, tc := range checks {
		m, ok := SupportedModels[tc.id]
		if !ok {
			t.Fatalf("SupportedModels missing legacy alias %q", tc.id)
		}
		if m.APIModel != tc.wantAPIModel {
			t.Errorf("legacy alias %q: APIModel = %q, want %q", tc.id, m.APIModel, tc.wantAPIModel)
		}
	}
}

func TestAntigravityStaticModelsAreRegistered(t *testing.T) {
	for _, id := range []ModelID{
		AntigravityGemini30Pro,
		AntigravityGemini31Pro,
		AntigravityGemini30Flash,
	} {
		m, ok := SupportedModels[id]
		if !ok {
			t.Fatalf("SupportedModels missing Antigravity model %q", id)
		}
		if m.Provider != ProviderAntigravity {
			t.Fatalf("model %q provider = %q, want %q", id, m.Provider, ProviderAntigravity)
		}
		if m.AccountID != "" {
			t.Fatalf("model %q account binding = %q, want empty", id, m.AccountID)
		}
	}
}

func TestNormalizeModelIDSupportsAntigravityAliases(t *testing.T) {
	checks := map[string]ModelID{
		string(AntigravityGemini30Pro):   AntigravityGemini30Pro,
		"antigravity.gemini-3-pro":       AntigravityGemini30Pro,
		string(AntigravityGemini31Pro):   AntigravityGemini31Pro,
		"antigravity.gemini-3.1-pro":     AntigravityGemini31Pro,
		string(AntigravityGemini30Flash): AntigravityGemini30Flash,
		"antigravity.gemini-3-flash":     AntigravityGemini30Flash,
	}
	for input, want := range checks {
		if got := NormalizeModelID(input); got != want {
			t.Fatalf("NormalizeModelID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestModelFromFetchedAccountModelUsesLogicalAntigravityIDs(t *testing.T) {
	params := AccountModelRefreshParams{
		AccountID:         "ag-work",
		ProviderType:      ProviderAntigravity,
		AllAccountsOfType: 2,
	}
	fetched := FetchedModel{ID: "gemini-3-pro-preview", Name: "Gemini 3 Pro", ContextWindow: 1_000_000}

	model := modelFromFetchedAccountModel(params, fetched)
	if model.ID != ModelID("antigravity.gemini-3-pro-preview") {
		t.Fatalf("model ID = %q, want %q", model.ID, "antigravity.gemini-3-pro-preview")
	}
	if model.AccountID != "ag-work" {
		t.Fatalf("account ID = %q, want %q", model.AccountID, "ag-work")
	}
}
