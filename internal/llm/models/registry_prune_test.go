package models

import "testing"

// TestPruneDynamicModelsForAccountIsAccountScoped verifies that pruning the
// models of one provider account does not delete the models of another account
// of the same provider type. This guards the regression where a global and a
// project-level Anthropic account would clobber each other on refresh, leaving
// only the last-refreshed account's models visible in the selector.
func TestPruneDynamicModelsForAccountIsAccountScoped(t *testing.T) {
	globalID := ModelID("anthropic-global.claude-x")
	projectID := ModelID("anthropic-project.claude-x")

	RegisterDynamicModel(Model{ID: globalID, Provider: ProviderAnthropic, AccountID: "anthropic-global"})
	RegisterDynamicModel(Model{ID: projectID, Provider: ProviderAnthropic, AccountID: "anthropic-project"})
	t.Cleanup(func() {
		delete(SupportedModels, globalID)
		delete(SupportedModels, projectID)
		dynamicModels.Delete(globalID)
		dynamicModels.Delete(projectID)
	})

	// Simulate refreshing only the project account: keepIDs holds only its model.
	keep := map[ModelID]struct{}{projectID: {}}
	pruneDynamicModelsForAccount(ProviderAnthropic, "anthropic-project", keep)

	if _, ok := SupportedModels[projectID]; !ok {
		t.Fatal("project model was wrongly pruned")
	}
	if _, ok := SupportedModels[globalID]; !ok {
		t.Fatal("global model was pruned by a project-account refresh (account scoping broken)")
	}

	// Refreshing the project account with an empty result must remove its own
	// stale model but still leave the global account untouched.
	pruneDynamicModelsForAccount(ProviderAnthropic, "anthropic-project", map[ModelID]struct{}{})
	if _, ok := SupportedModels[projectID]; ok {
		t.Fatal("stale project model should have been pruned for its own account")
	}
	if _, ok := SupportedModels[globalID]; !ok {
		t.Fatal("global model must survive pruning of the project account")
	}
}
