package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

// ModelInfo describes a model available for selection. The pricing and limit
// fields are populated from the provider's listing API when it reports them and
// from the models.dev catalog otherwise; both are absent (zero) when neither
// source knows the model, and the UI must render that as "unknown", not free.
type ModelInfo struct {
	ID                      string   `json:"id"`
	Name                    string   `json:"name"`
	Provider                string   `json:"provider"`
	AccountID               string   `json:"accountId,omitempty"`
	Description             string   `json:"description"`
	Badges                  []string `json:"badges"`
	CanReason               bool     `json:"canReason"`
	SupportsReasoningEffort bool     `json:"supportsReasoningEffort"`
	SupportsAttachments     bool     `json:"supportsAttachments"`
	ContextWindow           int64    `json:"contextWindow,omitempty"`
	MaxOutputTokens         int64    `json:"maxOutputTokens,omitempty"`
	CostPer1MIn             float64  `json:"costPer1MIn,omitempty"`
	CostPer1MOut            float64  `json:"costPer1MOut,omitempty"`
	Knowledge               string   `json:"knowledge,omitempty"`
	ReleaseDate             string   `json:"releaseDate,omitempty"`
}

// modelInfoMetadata copies the selector-visible metadata of a registered model
// onto a ModelInfo, including the badges. Cost-derived badges are preferred over
// the name heuristic: real prices beat guessing from substrings.
func modelInfoMetadata(info *ModelInfo, m models.Model) {
	info.CanReason = m.CanReason
	info.SupportsReasoningEffort = m.SupportsReasoningEffort
	info.SupportsAttachments = m.SupportsAttachments
	info.ContextWindow = m.ContextWindow
	info.MaxOutputTokens = m.DefaultMaxTokens
	info.CostPer1MIn = m.CostPer1MIn
	info.CostPer1MOut = m.CostPer1MOut
	info.Knowledge = m.Knowledge
	info.ReleaseDate = m.ReleaseDate
	if info.Description == "" {
		info.Description = m.Description
	}
	info.Badges = badgesForKnownModel(m)
}

// badgesForKnownModel derives badges from the model's real pricing when it is
// known, falling back to the ID heuristic for models no catalog covers.
func badgesForKnownModel(m models.Model) []string {
	if m.CostPer1MIn <= 0 && m.CostPer1MOut <= 0 {
		badges := badgesForModel(m.APIModel)
		if m.CanReason {
			badges = append(badges, "reasoning")
		}
		return badges
	}

	// Blended price of a 1:1 input/output million-token mix, which orders models
	// the same way users perceive them as cheap or expensive.
	blended := (m.CostPer1MIn + m.CostPer1MOut) / 2
	var badges []string
	switch {
	case blended <= 2:
		badges = []string{"fast", "cost"}
	case blended <= 12:
		badges = []string{"fast"}
	default:
		badges = []string{"capable"}
	}
	if m.CanReason {
		badges = append(badges, "reasoning")
	}
	return badges
}

// badgesForModel returns heuristic badges based on model ID. Used only when the
// model's price is unknown (see badgesForKnownModel).
func badgesForModel(id string) []string {
	id = strings.ToLower(id)
	switch {
	case strings.Contains(id, "opus") || strings.Contains(id, "gpt-4o") && !strings.Contains(id, "mini") || strings.Contains(id, "large"):
		return []string{"capable"}
	case strings.Contains(id, "haiku") || strings.Contains(id, "mini") || strings.Contains(id, "flash") || strings.Contains(id, "small"):
		return []string{"fast", "cost"}
	case strings.Contains(id, "sonnet") || strings.Contains(id, "gpt-4"):
		return []string{"fast", "cost"}
	default:
		return []string{"fast"}
	}
}

// handleListModels handles GET /api/v1/models.
// Fetches models from all configured (non-disabled) provider accounts in parallel and
// returns both the model list and a per-account error map for diagnostics.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	accounts := config.GetProviderAccounts()

	// Count non-disabled accounts per provider type (for display label disambiguation)
	typeCount := make(map[models.ModelProvider]int)
	for _, acc := range accounts {
		if !acc.Disabled {
			typeCount[acc.Type]++
		}
	}

	type accountEntry struct {
		account     config.ProviderAccount
		bearerToken string
		skip        bool
		skipReason  string
	}

	entries := make([]accountEntry, 0, len(accounts))
	for _, acc := range accounts {
		if acc.Disabled {
			continue
		}
		entry := accountEntry{account: acc}
		switch acc.Type {
		case models.ProviderCopilot:
			if token, err := auth.LoadGitHubOAuthToken(); err == nil && token != "" {
				entry.bearerToken = token
			} else if session, err := auth.LoadCopilotSession(); err == nil && session != nil {
				entry.bearerToken = session.AccessToken
			}
			if entry.bearerToken == "" {
				entry.skip = true
				entry.skipReason = "no GitHub OAuth token found — run 'pando auth login'"
			}
		case models.ProviderAnthropic:
			// OAuth-only Anthropic accounts have no API key; authenticate the model
			// listing with the Claude.ai OAuth token so the account's models appear
			// in the selector instead of falling back to another account's.
			if acc.APIKey == "" {
				if token, err := auth.LoadClaudeBearerToken(); err == nil && token != "" {
					entry.bearerToken = token
				} else {
					entry.skip = true
					entry.skipReason = "no Anthropic credentials found — set an API key or run Claude.ai login"
				}
			}
		}
		entries = append(entries, entry)
	}

	type accountResult struct {
		accountID string
		provider  models.ModelProvider
		items     []ModelInfo
		err       string
	}

	resultCh := make(chan accountResult, len(entries))
	var wg sync.WaitGroup

	for _, e := range entries {
		e := e
		wg.Add(1)
		go func() {
			defer wg.Done()
			acc := e.account
			if e.skip {
				resultCh <- accountResult{accountID: acc.ID, provider: acc.Type, err: e.skipReason}
				return
			}

			// Providers with no listing API (Azure, Vertex AI, …) expose a static
			// catalog. Surface per-account copies so each account is independently
			// selectable instead of erroring on the (unsupported) fetch.
			if !models.ProviderSupportsModelListing(acc.Type) && acc.Type != models.ProviderAntigravity {
				resultCh <- accountResult{
					accountID: acc.ID,
					provider:  acc.Type,
					items:     staticModelInfosForAccount(acc, typeCount[acc.Type]),
				}
				return
			}

			fetched, err := models.FetchModelsFromProvider(ctx, acc.Type, acc.APIKey, e.bearerToken, acc.BaseURL)
			if err != nil {
				resultCh <- accountResult{accountID: acc.ID, provider: acc.Type, err: err.Error()}
				return
			}
			if acc.Type == models.ProviderAntigravity {
				resultCh <- accountResult{accountID: acc.ID, provider: acc.Type}
				return
			}

			sameTypeCount := typeCount[acc.Type]
			items := make([]ModelInfo, 0, len(fetched))
			for _, m := range fetched {
				name := m.Name
				if name == "" {
					name = m.ID
				}

				// Build the canonical, provider-prefixed model ID (e.g.
				// "copilot.gpt-5.4-mini") so the ID exposed to the UI matches the one
				// registered by the model cache and accepted by validateAgent. Using a
				// bare ID here makes the web-UI save an unrecognised agent model that
				// gets reverted to a default on the next config reload.
				modelID := models.CanonicalAccountModelID(acc.Type, acc.ID, sameTypeCount, m.ID)
				if _, exists := models.SupportedModels[modelID]; !exists {
					registered := models.Model{
						ID:                  modelID,
						Name:                name,
						Provider:            acc.Type,
						APIModel:            m.ID,
						ContextWindow:       m.ContextWindow,
						DefaultMaxTokens:    m.MaxOutputTokens,
						AccountID:           acc.ID,
						Description:         m.Description,
						CanReason:           m.CanReason,
						SupportsAttachments: m.SupportsAttachments,
					}
					// Pull pricing/limits from models.dev before falling back to
					// guessed defaults, so the selector and the cost panel see the
					// real numbers for providers that report neither.
					models.EnrichModelFromModelsDev(ctx, &registered)
					if registered.ContextWindow <= 0 {
						registered.ContextWindow = 128_000
					}
					if registered.DefaultMaxTokens <= 0 {
						registered.DefaultMaxTokens = 4096
						if registered.ContextWindow < registered.DefaultMaxTokens {
							registered.DefaultMaxTokens = registered.ContextWindow / 2
						}
					}
					models.RegisterDynamicModel(registered)
				}

				// Disambiguate display name when multiple accounts share the same provider type.
				// Prefer the human-friendly account Display Name; fall back to the ID slug.
				displayName := name
				if sameTypeCount > 1 {
					accLabel := strings.TrimSpace(acc.DisplayName)
					if accLabel == "" {
						accLabel = acc.ID
					}
					displayName = accLabel + ": " + name
				}
				knownModel := models.SupportedModels[modelID]
				if knownModel.APIModel == "" {
					knownModel.APIModel = m.ID
				}
				info := ModelInfo{
					ID:          string(modelID),
					Name:        displayName,
					Provider:    string(acc.Type),
					AccountID:   acc.ID,
					Description: m.Description,
				}
				modelInfoMetadata(&info, knownModel)
				items = append(items, info)
			}
			resultCh <- accountResult{accountID: acc.ID, provider: acc.Type, items: items}
		}()
	}

	wg.Wait()
	close(resultCh)

	allModels := make([]ModelInfo, 0)
	providerErrors := make(map[string]string)

	for res := range resultCh {
		if res.err != "" {
			key := res.accountID
			if key == "" {
				key = string(res.provider)
			}
			providerErrors[key] = res.err
		} else {
			allModels = append(allModels, res.items...)
		}
	}

	// Fallback: when no dynamic models were fetched (no accounts or all disabled/failed),
	// return static models from SupportedModels for each configured and enabled provider.
	// This mirrors the TUI model dialog behaviour and ensures the selector is never empty
	// for users who have providers configured via the legacy Providers map or via
	// ProviderAccounts that failed to fetch.
	if len(allModels) == 0 {
		seenProviders := make(map[models.ModelProvider]bool)
		for _, acc := range accounts {
			seenProviders[acc.Type] = true
		}
		// Also honour the legacy Providers map (populated by syncProvidersFromAccounts).
		for provider, providerCfg := range cfg.Providers {
			if providerCfg.Disabled {
				continue
			}
			seenProviders[provider] = true
		}
		for provider := range seenProviders {
			for _, m := range models.SupportedModels {
				if m.Provider != provider {
					continue
				}
				name := m.Name
				if name == "" {
					name = string(m.ID)
				}
				info := ModelInfo{
					ID:       string(m.ID),
					Name:     name,
					Provider: string(m.Provider),
				}
				modelInfoMetadata(&info, m)
				allModels = append(allModels, info)
			}
		}
	}

	allModels = append(allModels, logicalAntigravityModelInfos(accounts)...)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": allModels,
		"errors": providerErrors,
	})
}

// staticModelInfosForAccount builds the selectable model list for a provider that
// has no listing API. With multiple accounts of the type it emits account-scoped
// copies (prefixed IDs + AccountID, registered so validateAgent accepts them),
// labelled with the account Display Name; with a single account it emits the
// account-less static models, which resolve to that one account by type.
func staticModelInfosForAccount(acc config.ProviderAccount, sameTypeCount int) []ModelInfo {
	accLabel := strings.TrimSpace(acc.DisplayName)
	if accLabel == "" {
		accLabel = acc.ID
	}

	var src []models.Model
	prefixed := sameTypeCount > 1
	if prefixed {
		src = models.AccountScopedStaticModels(acc.Type, acc.ID, sameTypeCount)
		for _, m := range src {
			if _, exists := models.SupportedModels[m.ID]; !exists {
				models.RegisterDynamicModel(m)
			}
		}
	} else {
		src = models.StaticModelsForProvider(acc.Type)
	}

	items := make([]ModelInfo, 0, len(src))
	for _, m := range src {
		name := m.Name
		if name == "" {
			name = string(m.ID)
		}
		displayName := name
		if prefixed {
			displayName = accLabel + ": " + name
		}
		info := ModelInfo{
			ID:        string(m.ID),
			Name:      displayName,
			Provider:  string(acc.Type),
			AccountID: acc.ID,
		}
		modelInfoMetadata(&info, m)
		items = append(items, info)
	}
	return items
}

func logicalAntigravityModelInfos(accounts []config.ProviderAccount) []ModelInfo {
	hasAntigravity := false
	for _, acc := range accounts {
		if !acc.Disabled && acc.Type == models.ProviderAntigravity {
			hasAntigravity = true
			break
		}
	}
	if !hasAntigravity {
		return nil
	}

	ids := make([]models.ModelID, 0, len(models.AntigravityModels))
	for id := range models.AntigravityModels {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	items := make([]ModelInfo, 0, len(ids))
	for _, id := range ids {
		model := models.AntigravityModels[id]
		info := ModelInfo{
			ID:          string(model.ID),
			Name:        model.Name,
			Provider:    string(model.Provider),
			Description: model.APIModel,
		}
		modelInfoMetadata(&info, model)
		items = append(items, info)
	}
	return items
}

// handleSetActiveModel handles PUT /api/v1/models/active.
func (s *Server) handleSetActiveModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid request body: 'model' field required")
		return
	}

	if config.Get() == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	if err := s.setCoderModel(models.ModelID(req.Model)); err != nil {
		writeError(w, http.StatusBadRequest, "failed to update model: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"model": req.Model})
}

// setCoderModel persists the coder agent model and, when a live agent exists,
// recreates its provider so the change takes effect in the current process.
//
// Calling config.UpdateAgentModel alone only writes the config file; the
// already-running CoderAgent keeps its previous provider (which is nil when no
// model was selected at startup, e.g. on a freshly configured machine). That
// left the web-UI/desktop in a state where the model appeared selected but the
// agent still returned "no model configured" on the next message until a
// restart. The TUI never hit this because it routes selections through
// CoderAgent.Update, which both persists and rebuilds the provider; this
// mirrors that behaviour for the API.
func (s *Server) setCoderModel(modelID models.ModelID) error {
	if s.app != nil && s.app.CoderAgent != nil {
		_, err := s.app.CoderAgent.Update(config.AgentCoder, modelID)
		return err
	}
	return config.UpdateAgentModel(config.AgentCoder, modelID)
}
