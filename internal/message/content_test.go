package message

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
)

func TestBinaryContentStringDataURLProviders(t *testing.T) {
	bc := BinaryContent{MIMEType: "image/png", Data: []byte{0x89, 0x50}}

	for _, provider := range []models.ModelProvider{
		models.ProviderOpenAI,
		models.ProviderAzure,
		models.ProviderCopilot,
		models.ProviderOpenAICompatible,
	} {
		got := bc.String(provider)
		if !strings.HasPrefix(got, "data:image/png;base64,") {
			t.Errorf("provider %s: expected data URL, got %q", provider, got)
		}
	}
}

func TestBinaryContentStringRawBase64Providers(t *testing.T) {
	bc := BinaryContent{MIMEType: "image/png", Data: []byte{0x89, 0x50}}

	for _, provider := range []models.ModelProvider{
		models.ProviderAnthropic,
		models.ProviderGemini,
		models.ProviderBedrock,
	} {
		if got := bc.String(provider); strings.HasPrefix(got, "data:") {
			t.Errorf("provider %s: expected raw base64, got %q", provider, got)
		}
	}
}

func TestBinaryContentStringDefaultsMIMEType(t *testing.T) {
	bc := BinaryContent{Data: []byte{0x89, 0x50}}
	if got := bc.String(models.ProviderCopilot); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Errorf("expected default MIME type in data URL, got %q", got)
	}
}
