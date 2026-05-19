package provider

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

type AntigravitySchedulingStrategy string

const (
	AntigravityPoolGateway   = "antigravity"
	AntigravityPoolGeminiCLI = "gemini-cli"

	AntigravityStrategySticky     AntigravitySchedulingStrategy = "sticky"
	AntigravityStrategyRoundRobin AntigravitySchedulingStrategy = "round-robin"
	AntigravityStrategyHybrid     AntigravitySchedulingStrategy = "hybrid"
)

type AntigravityQuotaState string

const (
	AntigravityQuotaStateAvailable AntigravityQuotaState = "available"
	AntigravityQuotaStateCooling   AntigravityQuotaState = "cooling"
	AntigravityQuotaStateExhausted AntigravityQuotaState = "exhausted"
)

type AntigravityFailureKind string

const (
	AntigravityFailureUnknown        AntigravityFailureKind = "unknown"
	AntigravityFailureQuotaExhausted AntigravityFailureKind = "quota_exhausted"
	AntigravityFailureRateLimited    AntigravityFailureKind = "rate_limited"
	AntigravityFailureCapacity       AntigravityFailureKind = "capacity"
	AntigravityFailureInvalidGrant   AntigravityFailureKind = "invalid_grant"
	AntigravityFailureProjectAccess  AntigravityFailureKind = "project_access"
	AntigravityFailureVerification   AntigravityFailureKind = "verification_required"
)

type AntigravityFailureAction struct {
	Kind             AntigravityFailureKind
	DisableAccount   bool
	Cooldown         time.Duration
	ExhaustPool      bool
	ExhaustModel     bool
	DisableGeminiCLI bool
	RequireReauth    bool
}

type AntigravitySchedulerAccountState struct {
	AccountID           string
	Email               string
	Enabled             bool
	LastUsedAt          time.Time
	CooldownUntil       time.Time
	ConsecutiveFailures int
	LastFailureAt       time.Time
	HealthScore         float64
	QuotaByPool         map[string]AntigravityQuotaState
	QuotaByModelFamily  map[string]AntigravityQuotaState
	GeminiCLISupported  bool
}

type AntigravitySelection struct {
	Account      config.ProviderAccount
	Pool         string
	Strategy     AntigravitySchedulingStrategy
	ModelFamily  string
	RuntimeState AntigravitySchedulerAccountState
}

type antigravityScheduler struct {
	mu           sync.Mutex
	strategy     AntigravitySchedulingStrategy
	roundRobin   map[string]int
	sticky       map[string]string
	accountState map[string]*AntigravitySchedulerAccountState
	now          func() time.Time
}

func newAntigravityScheduler(strategy AntigravitySchedulingStrategy) *antigravityScheduler {
	if strategy == "" {
		strategy = AntigravityStrategySticky
	}
	return &antigravityScheduler{
		strategy:     strategy,
		roundRobin:   make(map[string]int),
		sticky:       make(map[string]string),
		accountState: make(map[string]*AntigravitySchedulerAccountState),
		now:          time.Now,
	}
}

func ResetAntigravitySchedulerForTests() {
	defaultAntigravityScheduler = newAntigravityScheduler(AntigravityStrategySticky)
}

func (s *antigravityScheduler) Select(model models.Model) (AntigravitySelection, error) {
	accounts := config.AccountsForProviderType(models.ProviderAntigravity)
	return s.selectFromAccounts(accounts, model)
}

func (s *antigravityScheduler) selectFromAccounts(accounts []config.ProviderAccount, model models.Model) (AntigravitySelection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if model.Provider != models.ProviderAntigravity {
		return AntigravitySelection{}, fmt.Errorf("model %q is not an antigravity model", model.ID)
	}
	if len(accounts) == 0 {
		return AntigravitySelection{}, fmt.Errorf("no configured accounts for provider %s", models.ProviderAntigravity)
	}

	modelFamily := antigravityModelFamily(model)
	now := s.now()
	candidates := make([]config.ProviderAccount, 0, len(accounts))
	for _, account := range accounts {
		state := s.ensureAccountState(account)
		state.Enabled = !account.Disabled
		state.Email = account.Email
		state.GeminiCLISupported = antigravityGeminiCLISupported(account)
		if account.Disabled {
			continue
		}
		candidates = append(candidates, account)
	}
	if len(candidates) == 0 {
		return AntigravitySelection{}, fmt.Errorf("all antigravity accounts are disabled")
	}

	for _, pool := range antigravityPoolsForModel(model) {
		eligible := s.eligibleAccounts(candidates, pool, modelFamily, now)
		if len(eligible) == 0 {
			continue
		}
		selected := s.selectCandidate(model.ID, eligible)
		state := s.ensureAccountState(selected)
		state.LastUsedAt = now
		if state.HealthScore == 0 {
			state.HealthScore = 1
		}
		s.sticky[string(model.ID)] = selected.ID
		return AntigravitySelection{
			Account:      selected,
			Pool:         pool,
			Strategy:     s.strategy,
			ModelFamily:  modelFamily,
			RuntimeState: *cloneAntigravitySchedulerState(state),
		}, nil
	}

	return AntigravitySelection{}, fmt.Errorf("no available antigravity accounts for model %q", model.ID)
}

func (s *antigravityScheduler) ReportFailure(accountID, pool string, model models.Model, cooldown time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.ensureAccountState(config.ProviderAccount{ID: accountID})
	now := s.now()
	state.ConsecutiveFailures++
	state.LastFailureAt = now
	if cooldown > 0 {
		state.CooldownUntil = now.Add(cooldown)
	}
	if state.QuotaByPool == nil {
		state.QuotaByPool = make(map[string]AntigravityQuotaState)
	}
	if pool != "" {
		state.QuotaByPool[pool] = AntigravityQuotaStateCooling
	}
	family := antigravityModelFamily(model)
	if family != "" {
		if state.QuotaByModelFamily == nil {
			state.QuotaByModelFamily = make(map[string]AntigravityQuotaState)
		}
		state.QuotaByModelFamily[family] = AntigravityQuotaStateCooling
	}
	state.HealthScore = antigravityHealthScore(state, now)
}

func (s *antigravityScheduler) ReportQuotaExhausted(accountID, pool string, model models.Model) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.ensureAccountState(config.ProviderAccount{ID: accountID})
	state.ConsecutiveFailures++
	state.LastFailureAt = s.now()
	if state.QuotaByPool == nil {
		state.QuotaByPool = make(map[string]AntigravityQuotaState)
	}
	if pool != "" {
		state.QuotaByPool[pool] = AntigravityQuotaStateExhausted
	}
	family := antigravityModelFamily(model)
	if family != "" && pool == AntigravityPoolGateway {
		if state.QuotaByModelFamily == nil {
			state.QuotaByModelFamily = make(map[string]AntigravityQuotaState)
		}
		state.QuotaByModelFamily[family] = AntigravityQuotaStateExhausted
	}
	state.HealthScore = antigravityHealthScore(state, s.now())
}

func (s *antigravityScheduler) ApplyFailureAction(accountID, pool string, model models.Model, action AntigravityFailureAction) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.ensureAccountState(config.ProviderAccount{ID: accountID})
	now := s.now()
	state.ConsecutiveFailures++
	state.LastFailureAt = now
	if action.Cooldown > 0 {
		state.CooldownUntil = now.Add(action.Cooldown)
	}
	if action.DisableAccount {
		state.Enabled = false
	}
	if state.QuotaByPool == nil {
		state.QuotaByPool = make(map[string]AntigravityQuotaState)
	}
	if state.QuotaByModelFamily == nil {
		state.QuotaByModelFamily = make(map[string]AntigravityQuotaState)
	}
	if pool != "" {
		switch {
		case action.ExhaustPool:
			state.QuotaByPool[pool] = AntigravityQuotaStateExhausted
		case action.Cooldown > 0:
			state.QuotaByPool[pool] = AntigravityQuotaStateCooling
		}
	}
	family := antigravityModelFamily(model)
	if family != "" {
		switch {
		case action.ExhaustModel:
			state.QuotaByModelFamily[family] = AntigravityQuotaStateExhausted
		case action.Cooldown > 0:
			state.QuotaByModelFamily[family] = AntigravityQuotaStateCooling
		}
	}
	if action.DisableGeminiCLI {
		state.GeminiCLISupported = false
		state.QuotaByPool[AntigravityPoolGeminiCLI] = AntigravityQuotaStateExhausted
	}
	state.HealthScore = antigravityHealthScore(state, now)
}

func (s *antigravityScheduler) Snapshot(accountID string) (AntigravitySchedulerAccountState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.accountState[accountID]
	if !ok {
		return AntigravitySchedulerAccountState{}, false
	}
	return *cloneAntigravitySchedulerState(state), true
}

func (s *antigravityScheduler) ReportSuccess(accountID, pool string, model models.Model) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.ensureAccountState(config.ProviderAccount{ID: accountID})
	state.ConsecutiveFailures = 0
	state.CooldownUntil = time.Time{}
	state.LastUsedAt = s.now()
	if state.QuotaByPool == nil {
		state.QuotaByPool = make(map[string]AntigravityQuotaState)
	}
	if pool != "" {
		state.QuotaByPool[pool] = AntigravityQuotaStateAvailable
	}
	family := antigravityModelFamily(model)
	if family != "" {
		if state.QuotaByModelFamily == nil {
			state.QuotaByModelFamily = make(map[string]AntigravityQuotaState)
		}
		state.QuotaByModelFamily[family] = AntigravityQuotaStateAvailable
	}
	state.HealthScore = antigravityHealthScore(state, s.now())
}

func (s *antigravityScheduler) selectCandidate(modelID models.ModelID, candidates []config.ProviderAccount) config.ProviderAccount {
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].ID < candidates[j].ID
	})

	if s.strategy == AntigravityStrategySticky || s.strategy == AntigravityStrategyHybrid {
		if stickyID := s.sticky[string(modelID)]; stickyID != "" {
			for _, candidate := range candidates {
				if candidate.ID == stickyID {
					return candidate
				}
			}
		}
	}

	if s.strategy == AntigravityStrategyRoundRobin || s.strategy == AntigravityStrategyHybrid {
		key := string(modelID)
		index := s.roundRobin[key] % len(candidates)
		selected := candidates[index]
		s.roundRobin[key] = (index + 1) % len(candidates)
		return selected
	}

	return candidates[0]
}

func (s *antigravityScheduler) eligibleAccounts(accounts []config.ProviderAccount, pool, family string, now time.Time) []config.ProviderAccount {
	eligible := make([]config.ProviderAccount, 0, len(accounts))
	for _, account := range accounts {
		state := s.ensureAccountState(account)
		if !state.Enabled {
			continue
		}
		if pool == AntigravityPoolGeminiCLI && !state.GeminiCLISupported {
			continue
		}
		if !state.CooldownUntil.IsZero() && now.Before(state.CooldownUntil) {
			continue
		}
		if state.QuotaByPool[pool] == AntigravityQuotaStateExhausted {
			continue
		}
		if family != "" && state.QuotaByModelFamily[family] == AntigravityQuotaStateExhausted {
			continue
		}
		state.HealthScore = antigravityHealthScore(state, now)
		eligible = append(eligible, account)
	}
	return eligible
}

func (s *antigravityScheduler) ensureAccountState(account config.ProviderAccount) *AntigravitySchedulerAccountState {
	state, ok := s.accountState[account.ID]
	if !ok {
		state = &AntigravitySchedulerAccountState{
			AccountID:          account.ID,
			Email:              account.Email,
			Enabled:            !account.Disabled,
			HealthScore:        1,
			QuotaByPool:        map[string]AntigravityQuotaState{},
			QuotaByModelFamily: map[string]AntigravityQuotaState{},
			GeminiCLISupported: antigravityGeminiCLISupported(account),
		}
		s.accountState[account.ID] = state
	}
	if state.QuotaByPool == nil {
		state.QuotaByPool = map[string]AntigravityQuotaState{}
	}
	if state.QuotaByModelFamily == nil {
		state.QuotaByModelFamily = map[string]AntigravityQuotaState{}
	}
	if account.ID != "" {
		state.AccountID = account.ID
		state.Email = account.Email
		state.Enabled = !account.Disabled
		state.GeminiCLISupported = antigravityGeminiCLISupported(account)
	}
	return state
}

func antigravityPoolsForModel(model models.Model) []string {
	pools := []string{AntigravityPoolGateway}
	if strings.HasPrefix(model.APIModel, "gemini-") {
		pools = append(pools, AntigravityPoolGeminiCLI)
	}
	return pools
}

func antigravityModelFamily(model models.Model) string {
	apiModel := strings.ToLower(strings.TrimSpace(model.APIModel))
	switch {
	case strings.HasPrefix(apiModel, "gemini-3.1"):
		return "gemini-3.1"
	case strings.HasPrefix(apiModel, "gemini-3"):
		return "gemini-3"
	case strings.HasPrefix(apiModel, "gemini-2.5"):
		return "gemini-2.5"
	case strings.HasPrefix(apiModel, "gemini-"):
		return "gemini"
	default:
		return apiModel
	}
}

func antigravityGeminiCLISupported(account config.ProviderAccount) bool {
	return strings.TrimSpace(account.ProjectID) != ""
}

func antigravityHealthScore(state *AntigravitySchedulerAccountState, now time.Time) float64 {
	if state == nil || !state.Enabled {
		return 0
	}
	score := 1.0
	if !state.CooldownUntil.IsZero() && now.Before(state.CooldownUntil) {
		score -= 0.5
	}
	score -= float64(state.ConsecutiveFailures) * 0.1
	if score < 0 {
		return 0
	}
	return score
}

func cloneAntigravitySchedulerState(state *AntigravitySchedulerAccountState) *AntigravitySchedulerAccountState {
	if state == nil {
		return nil
	}
	clone := *state
	clone.QuotaByPool = make(map[string]AntigravityQuotaState, len(state.QuotaByPool))
	for k, v := range state.QuotaByPool {
		clone.QuotaByPool[k] = v
	}
	clone.QuotaByModelFamily = make(map[string]AntigravityQuotaState, len(state.QuotaByModelFamily))
	for k, v := range state.QuotaByModelFamily {
		clone.QuotaByModelFamily[k] = v
	}
	return &clone
}

var defaultAntigravityScheduler = newAntigravityScheduler(AntigravityStrategySticky)

func SelectAntigravityAccount(model models.Model) (config.ProviderAccount, error) {
	selection, err := defaultAntigravityScheduler.Select(model)
	if err != nil {
		return config.ProviderAccount{}, err
	}
	return selection.Account, nil
}
