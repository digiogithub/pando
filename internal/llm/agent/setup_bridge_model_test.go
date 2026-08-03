package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

const (
	testModelCheap   models.ModelID = "__mock.cheap"
	testModelPricey  models.ModelID = "__mock.pricey"
	testModelUnknown models.ModelID = "__mock.unpriced"
)

// setupModelTestEnv installs a mock catalogue and a configuration whose coder
// agent runs the cheap model, with the switch capability enabled unless stated
// otherwise.
func setupModelTestEnv(t *testing.T, switchEnabled bool) {
	t.Helper()

	catalogue := map[models.ModelID]models.Model{
		testModelCheap: {
			ID: testModelCheap, Name: "Cheap", Provider: models.ProviderMock,
			ContextWindow: 100_000, CostPer1MIn: 0.25, CostPer1MOut: 2,
		},
		testModelPricey: {
			ID: testModelPricey, Name: "Pricey", Provider: models.ProviderMock,
			ContextWindow: 200_000, CostPer1MIn: 5, CostPer1MOut: 25,
		},
		testModelUnknown: {
			ID: testModelUnknown, Name: "Unpriced", Provider: models.ProviderMock,
			ContextWindow: 50_000,
		},
	}
	models.SetSupportedModels(catalogue)

	prev := config.Get()
	config.SetForTests(&config.Config{
		WorkingDir: t.TempDir(),
		Agents: map[config.AgentName]config.Agent{
			config.AgentCoder: {Model: testModelCheap},
		},
		ProviderAccounts: []config.ProviderAccount{
			{ID: "mock-1", Type: models.ProviderMock, APIKey: "key"},
		},
		Providers:     map[models.ModelProvider]config.Provider{},
		LSP:           map[string]config.LSPConfig{},
		InternalTools: config.InternalToolsConfig{SetupModelSwitchEnabled: switchEnabled},
	})

	t.Cleanup(func() {
		config.SetForTests(prev)
		for id := range catalogue {
			models.DeleteSupportedModels(id)
		}
	})
}

// newModelTestSession returns a bridge and a session id whose overrides and
// pending quotes are cleaned up afterwards.
func newModelTestSession(t *testing.T, name string) (*setupBridge, string) {
	t.Helper()
	sessionID := "setup-model-" + name
	t.Cleanup(func() {
		sessionLLMOverrides.Delete(sessionID)
		setupModelProposals.Delete(sessionID)
	})
	return &setupBridge{}, sessionID
}

func TestSetupBridgeCurrentModelReportsConfiguredThenOverride(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "current")

	state, err := bridge.CurrentModel(sessionID)
	if err != nil {
		t.Fatalf("CurrentModel: %v", err)
	}
	if state.ID != string(testModelCheap) || state.Overridden {
		t.Fatalf("state = %+v, want the configured cheap model without an override", state)
	}
	if !state.CostKnown || state.CostIn != 0.25 {
		t.Fatalf("cost not reported: %+v", state)
	}

	SetSessionModelOverride(sessionID, testModelPricey)
	state, err = bridge.CurrentModel(sessionID)
	if err != nil {
		t.Fatalf("CurrentModel after override: %v", err)
	}
	if state.ID != string(testModelPricey) || !state.Overridden {
		t.Fatalf("state = %+v, want the overridden pricey model", state)
	}
}

func TestSetupBridgeSetSessionModelBlockedWhenDisabled(t *testing.T) {
	setupModelTestEnv(t, false)
	bridge, sessionID := newModelTestSession(t, "disabled")

	_, err := bridge.SetSessionModel(sessionID, string(testModelPricey), true)
	if err == nil {
		t.Fatal("expected the switch to be refused while the capability is disabled")
	}
	if !strings.Contains(err.Error(), "SetupModelSwitchEnabled") {
		t.Fatalf("error does not name the setting to enable: %v", err)
	}
	if got := SessionLLMOverridesFor(sessionID).Model; got != "" {
		t.Fatalf("model override = %q, want none", got)
	}

	// Reading stays available regardless of the flag.
	if _, err := bridge.CurrentModel(sessionID); err != nil {
		t.Fatalf("CurrentModel while disabled: %v", err)
	}
}

func TestSetupBridgeCheaperSwitchAppliesWithoutConfirmation(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "cheaper")
	SetSessionModelOverride(sessionID, testModelPricey)

	switched, err := bridge.SetSessionModel(sessionID, string(testModelCheap), false)
	if err != nil {
		t.Fatalf("SetSessionModel: %v", err)
	}
	if !switched.Applied || switched.Target.ID != string(testModelCheap) {
		t.Fatalf("switch = %+v, want the cheap model applied", switched)
	}
	if got := SessionLLMOverridesFor(sessionID).Model; got != testModelCheap {
		t.Fatalf("override = %q, want %q", got, testModelCheap)
	}
}

func TestSetupBridgePricierSwitchNeedsTwoCalls(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "pricier")

	quoted, err := bridge.SetSessionModel(sessionID, string(testModelPricey), false)
	if err != nil {
		t.Fatalf("SetSessionModel (quote): %v", err)
	}
	if quoted.Applied {
		t.Fatal("the first call must not apply a cost increase")
	}
	if !strings.Contains(quoted.ConfirmationReason, "increases the cost") {
		t.Fatalf("reason = %q, want a cost-increase explanation", quoted.ConfirmationReason)
	}
	if got := SessionLLMOverridesFor(sessionID).Model; got != "" {
		t.Fatalf("override = %q, want the session left untouched", got)
	}

	confirmed, err := bridge.SetSessionModel(sessionID, string(testModelPricey), true)
	if err != nil {
		t.Fatalf("SetSessionModel (confirm): %v", err)
	}
	if !confirmed.Applied {
		t.Fatalf("switch = %+v, want it applied after confirmation", confirmed)
	}
	if got := SessionLLMOverridesFor(sessionID).Model; got != testModelPricey {
		t.Fatalf("override = %q, want %q", got, testModelPricey)
	}
}

func TestSetupBridgeConfirmWithoutQuoteIsRefused(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "unquoted")

	// --confirm on a switch that was never quoted must re-quote instead of
	// applying: otherwise the model could approve its own upgrade in one call.
	switched, err := bridge.SetSessionModel(sessionID, string(testModelPricey), true)
	if err != nil {
		t.Fatalf("SetSessionModel: %v", err)
	}
	if switched.Applied {
		t.Fatal("a confirmation without a matching quote must not apply the switch")
	}
	if got := SessionLLMOverridesFor(sessionID).Model; got != "" {
		t.Fatalf("override = %q, want none", got)
	}
}

func TestSetupBridgeConfirmForAnotherModelIsRefused(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "mismatch")

	if _, err := bridge.SetSessionModel(sessionID, string(testModelPricey), false); err != nil {
		t.Fatalf("quote: %v", err)
	}
	// The user approved the pricey model, not the unpriced one.
	switched, err := bridge.SetSessionModel(sessionID, string(testModelUnknown), true)
	if err != nil {
		t.Fatalf("SetSessionModel: %v", err)
	}
	if switched.Applied {
		t.Fatal("a quote for one model must not authorize a switch to another")
	}
	if got := SessionLLMOverridesFor(sessionID).Model; got != "" {
		t.Fatalf("override = %q, want none", got)
	}
}

func TestSetupBridgeExpiredQuoteIsRefused(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "expired")

	setupModelProposals.Store(sessionID, setupModelProposal{
		Model:    testModelPricey,
		QuotedAt: time.Now().Add(-setupModelProposalTTL - time.Minute),
	})

	switched, err := bridge.SetSessionModel(sessionID, string(testModelPricey), true)
	if err != nil {
		t.Fatalf("SetSessionModel: %v", err)
	}
	if switched.Applied {
		t.Fatal("an expired quote must not be confirmable")
	}
}

func TestSetupBridgeUnknownCostNeedsConfirmation(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "unpriced")

	quoted, err := bridge.SetSessionModel(sessionID, string(testModelUnknown), false)
	if err != nil {
		t.Fatalf("SetSessionModel: %v", err)
	}
	if quoted.Applied {
		t.Fatal("an unpriced target must not be switched to without confirmation")
	}
	if !strings.Contains(quoted.ConfirmationReason, "cannot be compared") {
		t.Fatalf("reason = %q, want the missing-price explanation", quoted.ConfirmationReason)
	}
	if quoted.Target.CostKnown {
		t.Fatalf("target = %+v, want its price reported as unknown", quoted.Target)
	}

	confirmed, err := bridge.SetSessionModel(sessionID, string(testModelUnknown), true)
	if err != nil {
		t.Fatalf("SetSessionModel (confirm): %v", err)
	}
	if !confirmed.Applied {
		t.Fatal("the confirmed switch to an unpriced model must apply")
	}
}

func TestSetupBridgeClearRestoresConfiguredModel(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "clear")
	SetSessionModelOverride(sessionID, testModelPricey)

	switched, err := bridge.SetSessionModel(sessionID, "", false)
	if err != nil {
		t.Fatalf("SetSessionModel: %v", err)
	}
	if !switched.Applied || !switched.Cleared {
		t.Fatalf("switch = %+v, want the override cleared", switched)
	}
	if switched.Target.ID != string(testModelCheap) || switched.Target.Overridden {
		t.Fatalf("target = %+v, want the configured model back", switched.Target)
	}
	if got := SessionLLMOverridesFor(sessionID).Model; got != "" {
		t.Fatalf("override = %q, want none", got)
	}
}

func TestSetupBridgeSetSessionModelRejectsUnknownAndUnusableModels(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "invalid")

	if _, err := bridge.SetSessionModel(sessionID, "does.not.exist", true); err == nil {
		t.Fatal("expected an unknown model id to be rejected")
	}

	// A model whose provider has no configured account must be refused here
	// rather than blowing up later, mid-request, in createAgentProvider.
	orphan := models.ModelID("__orphan.model")
	models.SetSupportedModel(models.Model{
		ID: orphan, Name: "Orphan", Provider: models.ModelProvider("__no-account"),
		CostPer1MIn: 0.1, CostPer1MOut: 0.1,
	})
	t.Cleanup(func() { models.DeleteSupportedModels(orphan) })

	if _, err := bridge.SetSessionModel(sessionID, string(orphan), true); err == nil {
		t.Fatal("expected a model without a provider account to be rejected")
	}
	if got := SessionLLMOverridesFor(sessionID).Model; got != "" {
		t.Fatalf("override = %q, want none", got)
	}
}

func TestSetSessionModelOverridePreservesOtherOverrides(t *testing.T) {
	sessionID := "setup-model-merge"
	t.Cleanup(func() { sessionLLMOverrides.Delete(sessionID) })

	SetSessionLLMOverrides(sessionID, SessionLLMOverrides{
		Persona:         "architect",
		PersonaScoped:   true,
		ReasoningEffort: "high",
	})

	SetSessionModelOverride(sessionID, testModelPricey)
	got := SessionLLMOverridesFor(sessionID)
	if got.Model != testModelPricey {
		t.Fatalf("model = %q, want %q", got.Model, testModelPricey)
	}
	if got.Persona != "architect" || !got.PersonaScoped || got.ReasoningEffort != "high" {
		t.Fatalf("overrides = %+v, want the persona and reasoning effort preserved", got)
	}

	// Clearing only the model must leave the rest in place.
	SetSessionModelOverride(sessionID, "")
	got = SessionLLMOverridesFor(sessionID)
	if got.Model != "" {
		t.Fatalf("model = %q, want it cleared", got.Model)
	}
	if got.Persona != "architect" || got.ReasoningEffort != "high" {
		t.Fatalf("overrides = %+v, want the persona and reasoning effort preserved", got)
	}
}
