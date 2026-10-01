package dialog

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

func withAutoDialogConfig(t *testing.T, enabled bool, selected *bool) {
	t.Helper()
	prev := config.Get()
	t.Cleanup(func() { config.SetForTests(prev) })

	var provider models.ModelProvider
	var modelID models.ModelID
	for id, m := range models.SupportedModels() {
		provider, modelID = m.Provider, id
		break
	}
	config.SetForTests(&config.Config{
		Providers: map[models.ModelProvider]config.Provider{provider: {APIKey: "k"}},
		Agents:    map[config.AgentName]config.Agent{config.AgentCoder: {Model: modelID}},
		ModelAutoMode: config.ModelAutoModeConfig{
			Enabled:  enabled,
			Selected: selected,
		},
		DecisionModel: config.DecisionModelConfig{
			Router: config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, Model: "tev1:0.8b"},
		},
	})
}

func TestModelsDialogAutoFirst(t *testing.T) {
	yes := true
	withAutoDialogConfig(t, true, &yes)

	d := NewModelDialogCmp().(*modelDialogCmp)
	d.setupModels()

	if len(d.filteredModels) == 0 {
		t.Fatal("no models listed")
	}
	first := d.filteredModels[0]
	if string(first.ID) != config.AutoModelID || first.Name != "Auto" {
		t.Fatalf("first row = %q (%s), want Auto", first.ID, first.Name)
	}
	if d.selectedIdx != 0 {
		t.Errorf("Auto is selected globally: selectedIdx = %d, want 0", d.selectedIdx)
	}
	if _, ok := models.SupportedModels()[first.ID]; ok {
		t.Error("auto must never be registered in SupportedModels")
	}
	if !strings.Contains(d.selectedModelDetails(), "tev1:0.8b") {
		t.Errorf("Auto details should name the router model, got %q", d.selectedModelDetails())
	}

	// Enter on Auto emits ModelSelectedMsg for the pseudo model.
	if msg, ok := d.emitSelection(first).(ModelSelectedMsg); !ok || string(msg.Model.ID) != config.AutoModelID {
		t.Errorf("selection message = %#v", msg)
	}

	// Auto stays first when switching provider pages.
	d.hScrollPossible = true
	d.availableProviders = append(d.availableProviders, d.availableProviders...)
	d.switchProvider(1)
	if string(d.filteredModels[0].ID) != config.AutoModelID {
		t.Error("Auto must be the first row on every provider page")
	}
}

func TestModelsDialogNotAutoSelectedHighlightsCurrentModel(t *testing.T) {
	no := false
	withAutoDialogConfig(t, true, &no)

	d := NewModelDialogCmp().(*modelDialogCmp)
	d.setupModels()
	if string(d.filteredModels[0].ID) != config.AutoModelID {
		t.Fatal("Auto must still be listed first")
	}
	if d.selectedIdx == 0 {
		t.Error("Auto must not be highlighted when a concrete model is active")
	}
}

func TestModelsDialogAutoHiddenWhenDisabled(t *testing.T) {
	withAutoDialogConfig(t, false, nil)

	d := NewModelDialogCmp().(*modelDialogCmp)
	d.setupModels()
	for _, m := range d.filteredModels {
		if string(m.ID) == config.AutoModelID {
			t.Fatal("Auto entry must disappear when auto mode is disabled")
		}
	}
}
