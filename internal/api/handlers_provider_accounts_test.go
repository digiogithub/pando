package api

import "testing"

func TestProviderTypesIncludesAntigravityOAuthMetadata(t *testing.T) {
	for _, provider := range providerTypes {
		if provider.Type != "antigravity" {
			continue
		}
		if provider.DisplayName != "Antigravity" {
			t.Fatalf("display name = %q, want %q", provider.DisplayName, "Antigravity")
		}
		if provider.RequiresAPIKey {
			t.Fatal("expected antigravity provider type not to require API key")
		}
		if !provider.SupportsOAuth {
			t.Fatal("expected antigravity provider type to support OAuth")
		}
		return
	}

	t.Fatal("expected antigravity provider type metadata to be present")
}
