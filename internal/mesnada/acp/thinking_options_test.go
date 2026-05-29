package acp

import (
	"testing"

	llmmodels "github.com/digiogithub/pando/internal/llm/models"
)

func TestNormalizeACPThinkingSettings_DefaultsSupportedOptions(t *testing.T) {
	anthropicReasoningModel := llmmodels.Model{
		ID:        llmmodels.Claude46Sonnet,
		Provider:  llmmodels.ProviderAnthropic,
		CanReason: true,
	}

	got := normalizeACPThinkingSettings(anthropicReasoningModel, true, acpThinkingSettings{})
	if got.ThinkingStreamMode != defaultACPThinkingStreamMode {
		t.Fatalf("ThinkingStreamMode = %q, want %q", got.ThinkingStreamMode, defaultACPThinkingStreamMode)
	}
	if got.ReasoningEffort != "" {
		t.Fatalf("ReasoningEffort = %q, want empty", got.ReasoningEffort)
	}
	if got.ThinkingMode != defaultACPThinkingMode {
		t.Fatalf("ThinkingMode = %q, want %q", got.ThinkingMode, defaultACPThinkingMode)
	}
}

func TestNormalizeACPThinkingSettings_ClearsUnsupportedAndDefaultsInvalid(t *testing.T) {
	reasoningEffortModel := llmmodels.Model{
		ID:                      llmmodels.ModelID("test.reasoning"),
		Provider:                llmmodels.ProviderOpenAI,
		CanReason:               true,
		SupportsReasoningEffort: true,
	}

	got := normalizeACPThinkingSettings(reasoningEffortModel, true, acpThinkingSettings{
		ThinkingStreamMode: " FULL ",
		ReasoningEffort:    "bogus",
		ThinkingMode:       thinkingModeHigh,
	})
	if got.ThinkingStreamMode != thinkingStreamModeFull {
		t.Fatalf("ThinkingStreamMode = %q, want %q", got.ThinkingStreamMode, thinkingStreamModeFull)
	}
	if got.ReasoningEffort != defaultACPReasoningEffort {
		t.Fatalf("ReasoningEffort = %q, want %q", got.ReasoningEffort, defaultACPReasoningEffort)
	}
	if got.ThinkingMode != "" {
		t.Fatalf("ThinkingMode = %q, want empty", got.ThinkingMode)
	}
}

func TestValidateACPThinkingConfigValue_NormalizesAndRejectsUnsupported(t *testing.T) {
	reasoningEffortModel := llmmodels.Model{
		ID:                      llmmodels.ModelID("test.reasoning"),
		Provider:                llmmodels.ProviderOpenAI,
		CanReason:               true,
		SupportsReasoningEffort: true,
	}

	got, err := validateACPThinkingConfigValue(reasoningEffortModel, true, string(reasoningEffortModel.ID), sessionConfigReasoningEffortID, " HIGH ")
	if err != nil {
		t.Fatalf("validateACPThinkingConfigValue(reasoning_effort) error = %v", err)
	}
	if got != reasoningEffortHigh {
		t.Fatalf("validateACPThinkingConfigValue(reasoning_effort) = %q, want %q", got, reasoningEffortHigh)
	}

	got, err = validateACPThinkingConfigValue(reasoningEffortModel, true, string(reasoningEffortModel.ID), sessionConfigThinkingStreamModeID, " FULL ")
	if err != nil {
		t.Fatalf("validateACPThinkingConfigValue(thinking_stream_mode) error = %v", err)
	}
	if got != thinkingStreamModeFull {
		t.Fatalf("validateACPThinkingConfigValue(thinking_stream_mode) = %q, want %q", got, thinkingStreamModeFull)
	}

	if _, err := validateACPThinkingConfigValue(reasoningEffortModel, true, string(reasoningEffortModel.ID), sessionConfigThinkingModeID, thinkingModeHigh); err == nil {
		t.Fatal("expected thinking_mode validation to fail for non-Anthropic reasoning model")
	}
}
