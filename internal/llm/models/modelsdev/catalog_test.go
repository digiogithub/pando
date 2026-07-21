package modelsdev

import "testing"

func testCatalog() Catalog {
	return newCatalog(map[string]Provider{
		"anthropic": {
			ID:   "anthropic",
			Name: "Anthropic",
			Models: map[string]Model{
				"claude-sonnet-4-5": {
					Name:       "Claude Sonnet 4.5",
					Attachment: true,
					Reasoning:  true,
					ReasoningOptions: []ReasoningOption{
						{Type: "effort", Values: []string{"low", "high"}},
					},
					Cost:  Cost{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
					Limit: Limit{Context: 200000, Output: 64000},
				},
			},
		},
		"openrouter": {
			ID: "openrouter",
			Models: map[string]Model{
				"anthropic/claude-sonnet-4-5": {
					Cost:  Cost{Input: 3.1, Output: 15.5},
					Limit: Limit{Context: 200000},
				},
			},
		},
	})
}

func TestLookupExactID(t *testing.T) {
	c := testCatalog()
	got, ok := c.Lookup("anthropic", "claude-sonnet-4-5")
	if !ok {
		t.Fatal("exact id should resolve")
	}
	if got.Cost.Input != 3 || got.Limit.Context != 200000 {
		t.Fatalf("unexpected entry: %+v", got)
	}
	if got.ID != "claude-sonnet-4-5" {
		t.Errorf("ID = %q, want the map key backfilled", got.ID)
	}
}

// Provider APIs append dated/versioned suffixes that models.dev omits.
func TestLookupStripsVersionSuffixes(t *testing.T) {
	c := testCatalog()
	for _, id := range []string{
		"claude-sonnet-4-5-20250929",
		"claude-sonnet-4-5-latest",
		"CLAUDE-SONNET-4-5",
		"anthropic/claude-sonnet-4-5",
	} {
		if _, ok := c.Lookup("anthropic", id); !ok {
			t.Errorf("%q should resolve to the catalog entry", id)
		}
	}
}

func TestLookupUnknownProviderOrModel(t *testing.T) {
	c := testCatalog()
	if _, ok := c.Lookup("does-not-exist", "claude-sonnet-4-5"); ok {
		t.Error("unknown provider must not resolve")
	}
	if _, ok := c.Lookup("anthropic", "totally-unknown"); ok {
		t.Error("unknown model must not resolve")
	}
}

// The zero Catalog is the fallback value returned when the fetch fails; it must
// answer "not found" instead of panicking.
func TestZeroCatalogIsSafe(t *testing.T) {
	var c Catalog
	if _, ok := c.Lookup("anthropic", "claude-sonnet-4-5"); ok {
		t.Error("zero catalog must not resolve anything")
	}
	if _, ok := c.LookupAny([]string{"anthropic"}, "claude-sonnet-4-5"); ok {
		t.Error("zero catalog must not resolve anything")
	}
}

func TestLookupAnyPrefersFirstMatch(t *testing.T) {
	c := testCatalog()
	got, ok := c.LookupAny([]string{"google", "openrouter"}, "anthropic/claude-sonnet-4-5")
	if !ok {
		t.Fatal("second provider should resolve after the first misses")
	}
	if got.Cost.Input != 3.1 {
		t.Errorf("Cost.Input = %v, want the openrouter entry", got.Cost.Input)
	}
}

func TestSupportsReasoningEffort(t *testing.T) {
	c := testCatalog()
	got, _ := c.Lookup("anthropic", "claude-sonnet-4-5")
	if !got.SupportsReasoningEffort() {
		t.Error("effort-style reasoning options should be detected")
	}
	if (Model{Reasoning: true}).SupportsReasoningEffort() {
		t.Error("a model without reasoning_options must not claim effort support")
	}
}

func TestDecodeRejectsEmptyPayload(t *testing.T) {
	if _, err := decode([]byte(`{}`)); err == nil {
		t.Error("an empty catalog must be reported as an error so the caller falls back")
	}
	if _, err := decode([]byte(`not json`)); err == nil {
		t.Error("malformed payload must be reported as an error")
	}
}
