package provider

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

type testStatusError struct {
	status int
	msg    string
}

func (e testStatusError) Error() string   { return e.msg }
func (e testStatusError) StatusCode() int { return e.status }

func setProviderConfigForTests(t *testing.T, accounts ...config.ProviderAccount) {
	t.Helper()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".pando.json")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	cfg := &config.Config{
		WorkingDir:       tmpDir,
		ProviderAccounts: append([]config.ProviderAccount(nil), accounts...),
		Providers:        map[models.ModelProvider]config.Provider{},
		MCPServers:       map[string]config.MCPServer{},
		LSP:              map[string]config.LSPConfig{},
	}
	config.SetForTests(cfg)
}

func TestClassifyAntigravityFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		pool string
		want AntigravityFailureAction
	}{
		{
			name: "quota exhausted",
			err:  errors.New("Resource exhausted: quota exceeded for model"),
			pool: AntigravityPoolGateway,
			want: AntigravityFailureAction{Kind: AntigravityFailureQuotaExhausted, ExhaustPool: true, ExhaustModel: true},
		},
		{
			name: "rate limited",
			err:  testStatusError{status: http.StatusTooManyRequests, msg: "too many requests"},
			pool: AntigravityPoolGateway,
			want: AntigravityFailureAction{Kind: AntigravityFailureRateLimited, Cooldown: antigravityRateLimitCooldown},
		},
		{
			name: "capacity exhausted",
			err:  testStatusError{status: http.StatusServiceUnavailable, msg: "model capacity exhausted"},
			pool: AntigravityPoolGateway,
			want: AntigravityFailureAction{Kind: AntigravityFailureCapacity, Cooldown: antigravityCapacityCooldown},
		},
		{
			name: "invalid grant",
			err:  errors.New("oauth error: invalid_grant: token revoked"),
			pool: AntigravityPoolGateway,
			want: AntigravityFailureAction{Kind: AntigravityFailureInvalidGrant, DisableAccount: true, RequireReauth: true},
		},
		{
			name: "project access on gemini cli",
			err:  errors.New("missing projectId for Gemini CLI quota"),
			pool: AntigravityPoolGeminiCLI,
			want: AntigravityFailureAction{Kind: AntigravityFailureProjectAccess, DisableGeminiCLI: true, ExhaustPool: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyAntigravityFailure(tt.err, tt.pool)
			if got.Kind != tt.want.Kind || got.Cooldown != tt.want.Cooldown || got.DisableAccount != tt.want.DisableAccount || got.RequireReauth != tt.want.RequireReauth || got.DisableGeminiCLI != tt.want.DisableGeminiCLI || got.ExhaustPool != tt.want.ExhaustPool || got.ExhaustModel != tt.want.ExhaustModel {
				t.Fatalf("classifyAntigravityFailure() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAntigravitySchedulerApplyFailureActionRateLimit(t *testing.T) {
	now := time.Date(2026, 5, 19, 22, 0, 0, 0, time.UTC)
	scheduler := newAntigravityScheduler(AntigravityStrategySticky)
	scheduler.now = func() time.Time { return now }
	model := models.AntigravityModels[models.AntigravityGemini30Pro]

	scheduler.ApplyFailureAction("acct-1", AntigravityPoolGateway, model, AntigravityFailureAction{
		Kind:     AntigravityFailureRateLimited,
		Cooldown: antigravityRateLimitCooldown,
	})

	state, ok := scheduler.Snapshot("acct-1")
	if !ok {
		t.Fatal("expected scheduler state to exist")
	}
	if got := state.QuotaByPool[AntigravityPoolGateway]; got != AntigravityQuotaStateCooling {
		t.Fatalf("gateway quota state = %q, want %q", got, AntigravityQuotaStateCooling)
	}
	if got := state.QuotaByModelFamily[antigravityModelFamily(model)]; got != AntigravityQuotaStateCooling {
		t.Fatalf("model family quota state = %q, want %q", got, AntigravityQuotaStateCooling)
	}
	if state.CooldownUntil != now.Add(antigravityRateLimitCooldown) {
		t.Fatalf("cooldown until = %v, want %v", state.CooldownUntil, now.Add(antigravityRateLimitCooldown))
	}
}

func TestAntigravitySchedulerApplyFailureActionProjectAccessDisablesGeminiCLI(t *testing.T) {
	scheduler := newAntigravityScheduler(AntigravityStrategySticky)
	model := models.AntigravityModels[models.AntigravityGemini31Pro]
	accounts := []config.ProviderAccount{
		{ID: "gateway-only", Type: models.ProviderAntigravity},
		{ID: "cli-capable", Type: models.ProviderAntigravity, ProjectID: "proj-cli"},
	}

	scheduler.ApplyFailureAction("cli-capable", AntigravityPoolGeminiCLI, model, AntigravityFailureAction{
		Kind:             AntigravityFailureProjectAccess,
		DisableGeminiCLI: true,
		ExhaustPool:      true,
	})

	state, ok := scheduler.Snapshot("cli-capable")
	if !ok {
		t.Fatal("expected scheduler state to exist")
	}
	if state.GeminiCLISupported {
		t.Fatal("expected gemini cli support to be disabled")
	}
	if got := state.QuotaByPool[AntigravityPoolGeminiCLI]; got != AntigravityQuotaStateExhausted {
		t.Fatalf("gemini cli quota state = %q, want %q", got, AntigravityQuotaStateExhausted)
	}

	selection, err := scheduler.selectFromAccounts(accounts, model)
	if err != nil {
		t.Fatalf("selectFromAccounts() error = %v", err)
	}
	if selection.Pool != AntigravityPoolGateway {
		t.Fatalf("pool = %q, want %q", selection.Pool, AntigravityPoolGateway)
	}
}

func TestAntigravitySchedulerApplyFailureActionQuotaExhausted(t *testing.T) {
	scheduler := newAntigravityScheduler(AntigravityStrategySticky)
	model := models.AntigravityModels[models.AntigravityGemini30Flash]

	scheduler.ApplyFailureAction("acct-1", AntigravityPoolGateway, model, AntigravityFailureAction{
		Kind:         AntigravityFailureQuotaExhausted,
		ExhaustPool:  true,
		ExhaustModel: true,
	})

	state, ok := scheduler.Snapshot("acct-1")
	if !ok {
		t.Fatal("expected scheduler state to exist")
	}
	if got := state.QuotaByPool[AntigravityPoolGateway]; got != AntigravityQuotaStateExhausted {
		t.Fatalf("gateway quota state = %q, want %q", got, AntigravityQuotaStateExhausted)
	}
	if got := state.QuotaByModelFamily[antigravityModelFamily(model)]; got != AntigravityQuotaStateExhausted {
		t.Fatalf("model family quota state = %q, want %q", got, AntigravityQuotaStateExhausted)
	}
}

func TestAntigravityErrorStatusCodeHandlesMissingStatus(t *testing.T) {
	if got := antigravityErrorStatusCode(errors.New("boom")); got != 0 {
		t.Fatalf("antigravityErrorStatusCode() = %d, want 0", got)
	}
}

func TestApplyAntigravityFailureDisablesPersistedAccountAndMarksReauth(t *testing.T) {
	account := config.ProviderAccount{
		ID:                "acct-1",
		Type:              models.ProviderAntigravity,
		OAuthRefreshToken: "refresh-123",
		ProjectID:         "project-123",
		ExtraHeaders:      map[string]string{"existing": "value"},
	}
	setProviderConfigForTests(t, account)
	ResetAntigravitySchedulerForTests()
	defer ResetAntigravitySchedulerForTests()

	model := models.AntigravityModels[models.AntigravityGemini30Pro]
	err := applyAntigravityFailure(account, AntigravityPoolGateway, model, errors.New("oauth error: invalid_grant: token revoked"))
	if err == nil || err.Error() != "oauth error: invalid_grant: token revoked" {
		t.Fatalf("applyAntigravityFailure() error = %v", err)
	}

	persisted, ok := config.GetProviderAccount(account.ID)
	if !ok {
		t.Fatal("expected persisted account")
	}
	if !persisted.Disabled {
		t.Fatal("expected account to be disabled")
	}
	if !persisted.ReauthRequired {
		t.Fatal("expected reauth marker to be set")
	}
	if persisted.ExtraHeaders["existing"] != "value" {
		t.Fatalf("existing metadata = %q, want value", persisted.ExtraHeaders["existing"])
	}

	state, ok := defaultAntigravityScheduler.Snapshot(account.ID)
	if !ok {
		t.Fatal("expected scheduler snapshot")
	}
	if state.Enabled {
		t.Fatal("expected scheduler state to mark account disabled")
	}
}

func TestApplyAntigravityFailureClearsGeminiCLIProjectID(t *testing.T) {
	account := config.ProviderAccount{
		ID:        "acct-cli",
		Type:      models.ProviderAntigravity,
		ProjectID: "project-123",
	}
	setProviderConfigForTests(t, account)
	ResetAntigravitySchedulerForTests()
	defer ResetAntigravitySchedulerForTests()

	model := models.AntigravityModels[models.AntigravityGemini31Pro]
	err := applyAntigravityFailure(account, AntigravityPoolGeminiCLI, model, errors.New("missing projectId for Gemini CLI quota"))
	if err == nil || err.Error() != "missing projectId for Gemini CLI quota" {
		t.Fatalf("applyAntigravityFailure() error = %v", err)
	}

	persisted, ok := config.GetProviderAccount(account.ID)
	if !ok {
		t.Fatal("expected persisted account")
	}
	if persisted.ProjectID != "" {
		t.Fatalf("project ID = %q, want empty", persisted.ProjectID)
	}

	state, ok := defaultAntigravityScheduler.Snapshot(account.ID)
	if !ok {
		t.Fatal("expected scheduler snapshot")
	}
	if state.GeminiCLISupported {
		t.Fatal("expected gemini cli support to be disabled")
	}
	if got := state.QuotaByPool[AntigravityPoolGeminiCLI]; got != AntigravityQuotaStateExhausted {
		t.Fatalf("gemini cli quota state = %q, want %q", got, AntigravityQuotaStateExhausted)
	}
}
