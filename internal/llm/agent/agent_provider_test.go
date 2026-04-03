package agent

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

func TestDefaultAnthropicThinkingMode(t *testing.T) {
	tests := []struct {
		name           string
		model          models.Model
		configuredMode config.ThinkingMode
		want           config.ThinkingMode
	}{
		{
			name: "defaults to medium for anthropic reasoning model",
			model: models.Model{
				Provider: models.ProviderAnthropic,
				CanReason: true,
			},
			want: config.ThinkingMedium,
		},
		{
			name: "keeps explicit disabled mode for anthropic reasoning model",
			model: models.Model{
				Provider: models.ProviderAnthropic,
				CanReason: true,
			},
			configuredMode: config.ThinkingDisabled,
			want:           config.ThinkingDisabled,
		},
		{
			name: "keeps explicit low mode for anthropic reasoning model",
			model: models.Model{
				Provider: models.ProviderAnthropic,
				CanReason: true,
			},
			configuredMode: config.ThinkingLow,
			want:           config.ThinkingLow,
		},
		{
			name: "does not default non-reasoning anthropic model",
			model: models.Model{
				Provider: models.ProviderAnthropic,
				CanReason: false,
			},
			want: "",
		},
		{
			name: "does not default non-anthropic model",
			model: models.Model{
				Provider: models.ProviderOpenAI,
				CanReason: true,
			},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := defaultAnthropicThinkingMode(tc.model, tc.configuredMode)
			if got != tc.want {
				t.Fatalf("defaultAnthropicThinkingMode() = %q, want %q", got, tc.want)
			}
		})
	}
}
