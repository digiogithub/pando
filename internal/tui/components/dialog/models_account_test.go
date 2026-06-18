package dialog

import (
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
)

// TestDropAmbiguousStaticModels verifies that account-less static models are
// hidden only once two or more accounts of the type contribute models. With a
// single account the static models must remain (they resolve to that one
// account); with multiple accounts they are dropped because they would otherwise
// always resolve to the first/global account.
func TestDropAmbiguousStaticModels(t *testing.T) {
	static := models.Model{ID: "anthropic.claude-x", Provider: models.ProviderAnthropic}
	global := models.Model{ID: "global.claude-x", Provider: models.ProviderAnthropic, AccountID: "global"}
	project := models.Model{ID: "project.claude-x", Provider: models.ProviderAnthropic, AccountID: "project"}

	// Single account: static entries are kept.
	single := dropAmbiguousStaticModels([]models.Model{static, global})
	if len(single) != 2 {
		t.Fatalf("single-account: got %d models, want 2 (static kept)", len(single))
	}

	// Two accounts: static entries are dropped, per-account ones survive.
	multi := dropAmbiguousStaticModels([]models.Model{static, global, project})
	if len(multi) != 2 {
		t.Fatalf("multi-account: got %d models, want 2 (static dropped)", len(multi))
	}
	for _, m := range multi {
		if m.AccountID == "" {
			t.Fatalf("multi-account: ambiguous static model %q should have been dropped", m.ID)
		}
	}
}

// TestAccountLabelUsesDisplayName verifies the dialog prefixes models with the
// account Display Name (not the ID slug) when several accounts share a type, and
// leaves the bare name otherwise.
func TestAccountLabelUsesDisplayName(t *testing.T) {
	m := &modelDialogCmp{
		accountDisplayNames: map[string]string{"project": "My Project Anthropic"},
	}
	model := models.Model{Name: "Claude Sonnet", AccountID: "project"}

	if got := m.accountLabel(model, 1); got != "Claude Sonnet" {
		t.Errorf("single account: label = %q, want bare name", got)
	}
	if got := m.accountLabel(model, 2); got != "My Project Anthropic: Claude Sonnet" {
		t.Errorf("multi account: label = %q, want Display Name prefix", got)
	}

	// Falls back to the ID slug when no Display Name is known.
	noName := models.Model{Name: "Claude Sonnet", AccountID: "unknown"}
	if got := m.accountLabel(noName, 2); got != "unknown: Claude Sonnet" {
		t.Errorf("fallback: label = %q, want ID slug prefix", got)
	}
}
