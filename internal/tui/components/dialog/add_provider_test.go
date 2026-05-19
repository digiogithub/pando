package dialog

import (
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
)

func TestAddProviderTypeListIncludesAntigravityOAuthProvider(t *testing.T) {
	for _, provider := range addProviderTypeList {
		if provider.Type != models.ProviderAntigravity {
			continue
		}
		if provider.DisplayName != "Antigravity" {
			t.Fatalf("display name = %q, want %q", provider.DisplayName, "Antigravity")
		}
		if provider.RequiresAPIKey {
			t.Fatal("expected antigravity provider dialog entry not to require API key")
		}
		if provider.RequiresBaseURL {
			t.Fatal("expected antigravity provider dialog entry not to require base URL")
		}
		if !provider.SupportsOAuth {
			t.Fatal("expected antigravity provider dialog entry to support OAuth")
		}
		return
	}

	t.Fatal("expected antigravity provider to be listed in add provider dialog")
}
