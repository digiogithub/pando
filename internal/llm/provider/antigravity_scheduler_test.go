package provider

import (
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

func TestAntigravitySchedulerStickySelectionStaysOnHealthyAccount(t *testing.T) {
	scheduler := newAntigravityScheduler(AntigravityStrategySticky)
	model := models.AntigravityModels[models.AntigravityGemini30Pro]
	accounts := []config.ProviderAccount{
		{ID: "work", Type: models.ProviderAntigravity, Email: "work@example.com", ProjectID: "proj-work"},
		{ID: "personal", Type: models.ProviderAntigravity, Email: "personal@example.com", ProjectID: "proj-personal"},
	}

	first, err := scheduler.selectFromAccounts(accounts, model)
	if err != nil {
		t.Fatalf("selectFromAccounts() error = %v", err)
	}
	second, err := scheduler.selectFromAccounts(accounts, model)
	if err != nil {
		t.Fatalf("selectFromAccounts() second error = %v", err)
	}

	if first.Account.ID != second.Account.ID {
		t.Fatalf("sticky selection changed accounts: first=%q second=%q", first.Account.ID, second.Account.ID)
	}
	if first.Pool != AntigravityPoolGateway {
		t.Fatalf("first pool = %q, want %q", first.Pool, AntigravityPoolGateway)
	}
}

func TestAntigravitySchedulerRoundRobinRotatesAccounts(t *testing.T) {
	scheduler := newAntigravityScheduler(AntigravityStrategyRoundRobin)
	model := models.AntigravityModels[models.AntigravityGemini30Flash]
	accounts := []config.ProviderAccount{
		{ID: "b-account", Type: models.ProviderAntigravity, ProjectID: "proj-b"},
		{ID: "a-account", Type: models.ProviderAntigravity, ProjectID: "proj-a"},
	}

	first, err := scheduler.selectFromAccounts(accounts, model)
	if err != nil {
		t.Fatalf("selectFromAccounts() error = %v", err)
	}
	second, err := scheduler.selectFromAccounts(accounts, model)
	if err != nil {
		t.Fatalf("selectFromAccounts() second error = %v", err)
	}
	third, err := scheduler.selectFromAccounts(accounts, model)
	if err != nil {
		t.Fatalf("selectFromAccounts() third error = %v", err)
	}

	if first.Account.ID != "a-account" {
		t.Fatalf("first account = %q, want %q", first.Account.ID, "a-account")
	}
	if second.Account.ID != "b-account" {
		t.Fatalf("second account = %q, want %q", second.Account.ID, "b-account")
	}
	if third.Account.ID != "a-account" {
		t.Fatalf("third account = %q, want %q", third.Account.ID, "a-account")
	}
}

func TestAntigravitySchedulerFallsBackToGeminiCLIPool(t *testing.T) {
	scheduler := newAntigravityScheduler(AntigravityStrategySticky)
	model := models.AntigravityModels[models.AntigravityGemini31Pro]
	accounts := []config.ProviderAccount{
		{ID: "gateway-only", Type: models.ProviderAntigravity},
		{ID: "cli-capable", Type: models.ProviderAntigravity, ProjectID: "proj-cli"},
	}
	scheduler.ApplyFailureAction("gateway-only", AntigravityPoolGateway, model, AntigravityFailureAction{Kind: AntigravityFailureQuotaExhausted, ExhaustPool: true})
	scheduler.ApplyFailureAction("cli-capable", AntigravityPoolGateway, model, AntigravityFailureAction{Kind: AntigravityFailureQuotaExhausted, ExhaustPool: true})

	selection, err := scheduler.selectFromAccounts(accounts, model)
	if err != nil {
		t.Fatalf("selectFromAccounts() error = %v", err)
	}
	if selection.Pool != AntigravityPoolGeminiCLI {
		t.Fatalf("pool = %q, want %q", selection.Pool, AntigravityPoolGeminiCLI)
	}
	if selection.Account.ID != "cli-capable" {
		t.Fatalf("account = %q, want %q", selection.Account.ID, "cli-capable")
	}
}

func TestAntigravitySchedulerSkipsCoolingDownAccount(t *testing.T) {
	now := time.Date(2026, 5, 19, 22, 0, 0, 0, time.UTC)
	scheduler := newAntigravityScheduler(AntigravityStrategySticky)
	scheduler.now = func() time.Time { return now }
	model := models.AntigravityModels[models.AntigravityGemini30Pro]
	accounts := []config.ProviderAccount{
		{ID: "primary", Type: models.ProviderAntigravity, ProjectID: "proj-1"},
		{ID: "secondary", Type: models.ProviderAntigravity, ProjectID: "proj-2"},
	}

	scheduler.ReportFailure("primary", AntigravityPoolGateway, model, 5*time.Minute)
	selection, err := scheduler.selectFromAccounts(accounts, model)
	if err != nil {
		t.Fatalf("selectFromAccounts() error = %v", err)
	}
	if selection.Account.ID != "secondary" {
		t.Fatalf("account = %q, want %q", selection.Account.ID, "secondary")
	}
}

func TestAntigravitySchedulerReportSuccessClearsCooldownAndFailures(t *testing.T) {
	now := time.Date(2026, 5, 19, 22, 0, 0, 0, time.UTC)
	scheduler := newAntigravityScheduler(AntigravityStrategySticky)
	scheduler.now = func() time.Time { return now }
	model := models.AntigravityModels[models.AntigravityGemini30Pro]

	scheduler.ReportFailure("acct-1", AntigravityPoolGateway, model, 2*time.Minute)
	scheduler.ReportSuccess("acct-1", AntigravityPoolGateway, model)

	state, ok := scheduler.Snapshot("acct-1")
	if !ok {
		t.Fatal("expected scheduler state to exist")
	}
	if state.ConsecutiveFailures != 0 {
		t.Fatalf("consecutive failures = %d, want 0", state.ConsecutiveFailures)
	}
	if !state.CooldownUntil.IsZero() {
		t.Fatalf("cooldown until = %v, want zero", state.CooldownUntil)
	}
	if got := state.QuotaByPool[AntigravityPoolGateway]; got != AntigravityQuotaStateAvailable {
		t.Fatalf("gateway quota state = %q, want %q", got, AntigravityQuotaStateAvailable)
	}
	if got := state.QuotaByModelFamily[antigravityModelFamily(model)]; got != AntigravityQuotaStateAvailable {
		t.Fatalf("model family quota state = %q, want %q", got, AntigravityQuotaStateAvailable)
	}
	if state.LastUsedAt != now {
		t.Fatalf("last used at = %v, want %v", state.LastUsedAt, now)
	}
}

func TestAntigravitySchedulerRejectsNonAntigravityModel(t *testing.T) {
	scheduler := newAntigravityScheduler(AntigravityStrategySticky)
	model := models.Model{ID: "openai/gpt-5", Provider: models.ProviderOpenAI, APIModel: "gpt-5"}
	accounts := []config.ProviderAccount{{ID: "acct-1", Type: models.ProviderAntigravity}}

	_, err := scheduler.selectFromAccounts(accounts, model)
	if err == nil || err.Error() != "model \"openai/gpt-5\" is not an antigravity model" {
		t.Fatalf("selectFromAccounts() error = %v", err)
	}
}

func TestAntigravitySchedulerRejectsWhenAllAccountsDisabled(t *testing.T) {
	scheduler := newAntigravityScheduler(AntigravityStrategySticky)
	model := models.AntigravityModels[models.AntigravityGemini30Pro]
	accounts := []config.ProviderAccount{{ID: "acct-1", Type: models.ProviderAntigravity, Disabled: true}}

	_, err := scheduler.selectFromAccounts(accounts, model)
	if err == nil || err.Error() != "all antigravity accounts are disabled" {
		t.Fatalf("selectFromAccounts() error = %v", err)
	}
}

func TestAntigravityHelpers(t *testing.T) {
	geminiModel := models.AntigravityModels[models.AntigravityGemini31Pro]
	if pools := antigravityPoolsForModel(geminiModel); len(pools) != 2 || pools[0] != AntigravityPoolGateway || pools[1] != AntigravityPoolGeminiCLI {
		t.Fatalf("antigravityPoolsForModel() = %v", pools)
	}

	customModel := models.Model{Provider: models.ProviderAntigravity, APIModel: "claude-sonnet-4"}
	if pools := antigravityPoolsForModel(customModel); len(pools) != 1 || pools[0] != AntigravityPoolGateway {
		t.Fatalf("antigravityPoolsForModel(custom) = %v", pools)
	}

	if family := antigravityModelFamily(geminiModel); family != "gemini-3.1" {
		t.Fatalf("antigravityModelFamily(gemini31) = %q", family)
	}
	if family := antigravityModelFamily(models.Model{APIModel: "gemini-2.5-flash"}); family != "gemini-2.5" {
		t.Fatalf("antigravityModelFamily(gemini25) = %q", family)
	}
	if family := antigravityModelFamily(customModel); family != "claude-sonnet-4" {
		t.Fatalf("antigravityModelFamily(custom) = %q", family)
	}

	if !antigravityGeminiCLISupported(config.ProviderAccount{ProjectID: "proj-1"}) {
		t.Fatal("expected project-backed account to support gemini cli")
	}
	if antigravityGeminiCLISupported(config.ProviderAccount{}) {
		t.Fatal("expected account without project to disable gemini cli")
	}
}

func TestAntigravityHealthScore(t *testing.T) {
	now := time.Date(2026, 5, 19, 22, 0, 0, 0, time.UTC)
	if got := antigravityHealthScore(nil, now); got != 0 {
		t.Fatalf("health score for nil state = %v, want 0", got)
	}
	if got := antigravityHealthScore(&AntigravitySchedulerAccountState{Enabled: false}, now); got != 0 {
		t.Fatalf("health score for disabled state = %v, want 0", got)
	}

	state := &AntigravitySchedulerAccountState{Enabled: true, ConsecutiveFailures: 3, CooldownUntil: now.Add(1 * time.Minute)}
	if got := antigravityHealthScore(state, now); got < 0.19 || got > 0.21 {
		t.Fatalf("health score = %v, want approximately 0.2", got)
	}
}

func TestCloneAntigravitySchedulerStateDeepCopiesMaps(t *testing.T) {
	original := &AntigravitySchedulerAccountState{
		AccountID:          "acct-1",
		Enabled:            true,
		QuotaByPool:        map[string]AntigravityQuotaState{AntigravityPoolGateway: AntigravityQuotaStateAvailable},
		QuotaByModelFamily: map[string]AntigravityQuotaState{"gemini-3": AntigravityQuotaStateCooling},
	}

	clone := cloneAntigravitySchedulerState(original)
	clone.QuotaByPool[AntigravityPoolGateway] = AntigravityQuotaStateExhausted
	clone.QuotaByModelFamily["gemini-3"] = AntigravityQuotaStateAvailable

	if original.QuotaByPool[AntigravityPoolGateway] != AntigravityQuotaStateAvailable {
		t.Fatalf("original pool quota mutated: %q", original.QuotaByPool[AntigravityPoolGateway])
	}
	if original.QuotaByModelFamily["gemini-3"] != AntigravityQuotaStateCooling {
		t.Fatalf("original family quota mutated: %q", original.QuotaByModelFamily["gemini-3"])
	}
}
