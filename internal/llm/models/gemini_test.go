package models

import "testing"

func TestGeminiStaticModelsAreRegistered(t *testing.T) {
	for _, id := range []ModelID{
		Gemini31ProPreview,
		Gemini30Flash,
		Gemini25Flash,
		Gemini25,
		Gemini20Flash,
		Gemini20FlashLite,
	} {
		if _, ok := SupportedModels[id]; !ok {
			t.Fatalf("SupportedModels missing Gemini model %q", id)
		}
	}
}
