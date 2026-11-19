package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	toolsPkg "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
)

// CopilotUtilityTitleModel is Copilot's "small utility" model. GitHub serves it
// free of charge (rate limited) for utility scenarios such as naming a session.
const CopilotUtilityTitleModel = "gpt-4o-mini"

const (
	copilotAutoRouteTimeout = 5 * time.Second
	// Route again this long before the session token expires, so it cannot
	// expire in the middle of a request.
	copilotAutoRenewMargin        = 5 * time.Minute
	copilotUtilityTimeout         = 20 * time.Second
	copilotUtilityFailureCooldown = 30 * time.Minute
)

// copilotAutoRoute is the model Copilot's Auto router picked for a session.
// The session token unlocks that pick and its Auto discount; Copilot rejects it
// for any other model, so both travel together.
type copilotAutoRoute struct {
	model   models.Model
	token   string
	account string
	until   time.Time
}

var (
	copilotAutoRoutesMu sync.Mutex
	copilotAutoRoutes   = map[string]copilotAutoRoute{}

	copilotUtilityFailuresMu sync.Mutex
	copilotUtilityFailures   = map[string]time.Time{}
)

// forRequest returns the client that must serve this request. For every model
// but Copilot's Auto that is the receiver itself. For Auto it is a copy bound
// to the model the router picked for the session, so concurrent sessions that
// share a client never see each other's pick.
func (c *copilotClient) forRequest(ctx context.Context, messages []message.Message) (*copilotClient, error) {
	if !models.IsCopilotAutoModel(c.providerOptions.model) {
		return c, nil
	}
	route, err := c.autoRoute(ctx, messages)
	if err != nil {
		return nil, err
	}

	routed := *c
	routed.providerOptions.model = route.model
	routed.autoSessionToken = route.token
	// The budget was sized for Auto's advertised cap, the smallest of the pool;
	// keep it inside the picked model's own cap anyway.
	if limit := route.model.DefaultMaxTokens; limit > 0 && routed.providerOptions.maxTokens > limit {
		routed.providerOptions.maxTokens = limit
	}
	return &routed, nil
}

// autoRoute returns the session's current Auto pick, asking Copilot's router
// when there is none, it is about to expire or the account changed.
func (c *copilotClient) autoRoute(ctx context.Context, messages []message.Message) (copilotAutoRoute, error) {
	c.refreshBearerToken()
	sessionID := copilotInteractionID(ctx)
	account := c.sourceToken
	if account == "" {
		account = c.options.bearerToken
	}
	now := time.Now()

	copilotAutoRoutesMu.Lock()
	cached, hasCached := copilotAutoRoutes[sessionID]
	copilotAutoRoutesMu.Unlock()
	usable := hasCached && sessionID != "" && cached.account == account

	// Compaction rewrites the prompt, so the request after it routes again, like
	// Copilot's own clients. The compaction request itself keeps the current pick.
	if c.interactionType() == CopilotInteractionCompaction && usable {
		expired := cached
		expired.until = time.Time{}
		copilotAutoRoutesMu.Lock()
		copilotAutoRoutes[sessionID] = expired
		copilotAutoRoutesMu.Unlock()
		return cached, nil
	}
	if usable && cached.until.After(now) {
		return cached, nil
	}

	route, err := c.requestAutoRoute(ctx, messages)
	if err != nil {
		if usable {
			// Keep the previous pick without its (possibly expired) token: the
			// request still works, it only loses the Auto discount.
			logging.Warn("Copilot Auto routing failed, keeping the previous model", "model", cached.model.APIModel, "error", err)
			cached.token = ""
			return cached, nil
		}
		// No pick to fall back on: use the coder model configured in Pando, and
		// ask the router again soon.
		fallback, ok := configuredCoderCopilotModel()
		if !ok {
			// The coder is not on a callable Copilot model (it is Auto itself, or
			// another provider's): use the account's default chat model.
			var fallbackErr error
			fallback, fallbackErr = c.autoFallbackModel(ctx)
			if fallbackErr != nil {
				return copilotAutoRoute{}, fmt.Errorf("copilot auto routing failed: %w (no default model: %v)", err, fallbackErr)
			}
		}
		logging.Warn("Copilot Auto routing failed, using the default model", "model", fallback.APIModel, "error", err)
		route = copilotAutoRoute{model: fallback, until: now.Add(copilotAutoRenewMargin)}
	}
	route.account = account

	if sessionID != "" {
		copilotAutoRoutesMu.Lock()
		for id, existing := range copilotAutoRoutes {
			if existing.until.Before(now) {
				delete(copilotAutoRoutes, id)
			}
		}
		copilotAutoRoutes[sessionID] = route
		copilotAutoRoutesMu.Unlock()
	}
	return route, nil
}

// copilotAutoSelection is the body of a successful POST /auto.
type copilotAutoSelection struct {
	SessionToken  string             `json:"session_token"`
	ExpiresAt     int64              `json:"expires_at"`
	SelectedModel copilotRemoteModel `json:"selected_model"`
}

func (c *copilotClient) requestAutoRoute(ctx context.Context, messages []message.Message) (copilotAutoRoute, error) {
	prompt, hasImage := "", false
	for idx := len(messages) - 1; idx >= 0; idx-- {
		if messages[idx].Role == message.User {
			prompt = messages[idx].Content().String()
			hasImage = len(messages[idx].BinaryContent()) > 0
			break
		}
	}
	payload := map[string]any{"prompt": prompt}
	if hasImage {
		payload["has_image"] = true
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return copilotAutoRoute{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, copilotAutoRouteTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.baseURL, "/")+"/auto", bytes.NewReader(body))
	if err != nil {
		return copilotAutoRoute{}, err
	}
	auth.ApplyCopilotClientHeaders(req.Header)
	for key, value := range c.options.extraHeaders {
		req.Header.Set(key, value)
	}
	req.Header.Set("Authorization", "Bearer "+c.options.bearerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return copilotAutoRoute{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return copilotAutoRoute{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return copilotAutoRoute{}, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var selection copilotAutoSelection
	if err := json.Unmarshal(data, &selection); err != nil {
		return copilotAutoRoute{}, fmt.Errorf("parse response: %w", err)
	}
	if strings.TrimSpace(selection.SelectedModel.ID) == "" {
		return copilotAutoRoute{}, fmt.Errorf("response names no model")
	}

	until := time.Now().Add(copilotAutoRenewMargin)
	if selection.ExpiresAt > 0 {
		until = time.Unix(selection.ExpiresAt, 0).Add(-copilotAutoRenewMargin)
	}
	return copilotAutoRoute{
		model: c.catalogModel(selection.SelectedModel),
		token: selection.SessionToken,
		until: until,
	}, nil
}

// copilotRemoteModel is what Copilot reports about a model, both in the router's
// answer and in the model listing.
type copilotRemoteModel struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	ModelPickerEnabled bool     `json:"model_picker_enabled"`
	IsChatFallback     bool     `json:"is_chat_fallback"`
	SupportedEndpoints []string `json:"supported_endpoints"`
	Policy             *struct {
		State string `json:"state"`
	} `json:"policy"`
	Billing *struct {
		AutoDiscount *float64 `json:"auto_discount"`
	} `json:"billing"`
	Capabilities struct {
		Type   string `json:"type"`
		Limits struct {
			MaxContextWindowTokens int64 `json:"max_context_window_tokens"`
			MaxOutputTokens        int64 `json:"max_output_tokens"`
			MaxPromptTokens        int64 `json:"max_prompt_tokens"`
		} `json:"limits"`
		Supports struct {
			ReasoningEffort []string `json:"reasoning_effort"`
			Vision          bool     `json:"vision"`
			ToolCalls       bool     `json:"tool_calls"`
		} `json:"supports"`
	} `json:"capabilities"`
}

// configuredCoderCopilotModel returns the model configured for Pando's coder
// agent when this client can serve it: a Copilot model other than Auto.
func configuredCoderCopilotModel() (models.Model, bool) {
	cfg := config.Get()
	if cfg == nil {
		return models.Model{}, false
	}
	coder, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return models.Model{}, false
	}
	model, ok := models.SupportedModels()[coder.Model]
	if !ok || model.Provider != models.ProviderCopilot || models.IsCopilotAutoModel(model) {
		return models.Model{}, false
	}
	return model, true
}

// autoFallbackModel returns the model that serves an Auto session when neither
// the router nor the configured coder model can: the one Copilot marks as the account's default chat
// model (is_chat_fallback), or else the first model of the Auto pool.
func (c *copilotClient) autoFallbackModel(ctx context.Context) (models.Model, error) {
	ctx, cancel := context.WithTimeout(ctx, copilotAutoRouteTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.baseURL, "/")+"/models", nil)
	if err != nil {
		return models.Model{}, err
	}
	auth.ApplyCopilotClientHeaders(req.Header)
	for key, value := range c.options.extraHeaders {
		req.Header.Set(key, value)
	}
	req.Header.Set("Authorization", "Bearer "+c.options.bearerToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return models.Model{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return models.Model{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return models.Model{}, fmt.Errorf("models status %d", resp.StatusCode)
	}
	var listing struct {
		Data []copilotRemoteModel `json:"data"`
	}
	if err := json.Unmarshal(data, &listing); err != nil {
		return models.Model{}, fmt.Errorf("parse models: %w", err)
	}

	var pool *copilotRemoteModel
	for i := range listing.Data {
		remote := &listing.Data[i]
		usable := remote.ModelPickerEnabled && remote.Capabilities.Supports.ToolCalls &&
			(remote.Policy == nil || remote.Policy.State != "disabled") &&
			(remote.Capabilities.Type == "" || remote.Capabilities.Type == "chat")
		if !usable {
			continue
		}
		if remote.IsChatFallback {
			return c.catalogModel(*remote), nil
		}
		if pool == nil && remote.Billing != nil && remote.Billing.AutoDiscount != nil {
			pool = remote
		}
	}
	if pool != nil {
		return c.catalogModel(*pool), nil
	}
	return models.Model{}, fmt.Errorf("the account lists no default chat model")
}

// catalogModel resolves a model Copilot named to a catalog model, which
// carries the curated metadata (costs, reasoning support). One the catalog
// does not list is built from what Copilot itself reported about it.
func (c *copilotClient) catalogModel(picked copilotRemoteModel) models.Model {
	accountID := c.providerOptions.model.AccountID
	for _, candidate := range models.SupportedModels() {
		if candidate.Provider == models.ProviderCopilot && candidate.APIModel == picked.ID && candidate.AccountID == accountID {
			return candidate
		}
	}

	contextWindow := picked.Capabilities.Limits.MaxContextWindowTokens
	if contextWindow == 0 {
		contextWindow = picked.Capabilities.Limits.MaxPromptTokens
	}
	name := picked.Name
	if name == "" {
		name = picked.ID
	}
	supportsEffort := len(picked.Capabilities.Supports.ReasoningEffort) > 0
	return models.Model{
		ID:                      models.ModelID(string(models.ProviderCopilot) + "." + picked.ID),
		Name:                    "Copilot: " + name,
		Provider:                models.ProviderCopilot,
		APIModel:                picked.ID,
		AccountID:               accountID,
		ContextWindow:           contextWindow,
		DefaultMaxTokens:        picked.Capabilities.Limits.MaxOutputTokens,
		CanReason:               supportsEffort,
		SupportsReasoningEffort: supportsEffort,
		SupportsAttachments:     picked.Capabilities.Supports.Vision,
		SupportedEndpoints:      picked.SupportedEndpoints,
	}
}

// sendWithUtilityModel serves a tool-less, non-streaming request with Copilot's
// free utility model. It reports false whenever that is not possible, so the
// caller falls through to the configured (billable) model.
func (c *copilotClient) sendWithUtilityModel(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) (*ProviderResponse, bool) {
	utilityModel := strings.TrimSpace(c.options.utilityModel)
	if utilityModel == "" || len(tools) > 0 || c.providerOptions.model.APIModel == utilityModel {
		return nil, false
	}

	failureKey := c.baseURL + "\x00" + utilityModel
	copilotUtilityFailuresMu.Lock()
	failedAt, failed := copilotUtilityFailures[failureKey]
	copilotUtilityFailuresMu.Unlock()
	if failed && time.Since(failedAt) < copilotUtilityFailureCooldown {
		return nil, false
	}

	// The utility model is only served over Chat Completions and takes neither
	// reasoning parameters nor images.
	utility := *c
	utility.autoSessionToken = ""
	utility.providerOptions.model = models.Model{
		ID:                 models.ModelID(string(models.ProviderCopilot) + "." + utilityModel),
		Provider:           models.ProviderCopilot,
		APIModel:           utilityModel,
		SupportedEndpoints: []string{models.CopilotEndpointChatCompletions},
	}

	ctx, cancel := context.WithTimeout(ctx, copilotUtilityTimeout)
	defer cancel()
	params := utility.preparedParams(utility.convertMessages(messages), nil)
	client := utility.requestClient(ctx, messages)
	completion, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		copilotUtilityFailuresMu.Lock()
		copilotUtilityFailures[failureKey] = time.Now()
		copilotUtilityFailuresMu.Unlock()
		logging.Debug("Copilot utility model request failed, using the configured model", "model", utilityModel, "error", err)
		return nil, false
	}
	if len(completion.Choices) == 0 || strings.TrimSpace(completion.Choices[0].Message.Content) == "" {
		return nil, false
	}
	return &ProviderResponse{
		Content:      completion.Choices[0].Message.Content,
		Usage:        utility.usage(*completion),
		FinishReason: utility.finishReason(string(completion.Choices[0].FinishReason)),
	}, true
}
