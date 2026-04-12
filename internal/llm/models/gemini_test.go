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
		id              ModelID
		wantAPIModel    string
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
