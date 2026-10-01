package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/logging"
)

// DecisionProviderKind names the System One decision provider.
type DecisionProviderKind string

const (
	// DecisionProviderOllama uses a local Ollama (>= 0.35) decision model.
	DecisionProviderOllama DecisionProviderKind = "ollama"
	// DecisionProviderTypeSafe uses the hosted TypeSafe Jev API.
	DecisionProviderTypeSafe DecisionProviderKind = "typesafe"
	// DecisionProviderCustom uses any Jev-compatible gateway (OpenRouter, LiteLLM, Kev...).
	DecisionProviderCustom DecisionProviderKind = "custom"
)

const (
	// DefaultTypeSafeBaseURL is the TypeSafe Jev API root.
	DefaultTypeSafeBaseURL = "https://api.typesafe.ai"
	// TypeSafeAPIKeyEnv is the env var used when the TypeSafe key is empty.
	TypeSafeAPIKeyEnv = "TYPESAFE_API_KEY"

	defaultModelAutoKeepAlive     = "30m"
	defaultModelAutoOllamaTimeout = 1500 * time.Millisecond
	defaultModelAutoRemoteTimeout = 3000 * time.Millisecond
)

// DecisionModelConfig is the shared decision model (System One / Jev) used by
// model auto mode, persona auto-select and any other feature that asks a small
// model a routing or relevance question.
type DecisionModelConfig struct {
	// Router is the decision provider.
	Router DecisionRouterConfig `json:"router" toml:"Router"`
	// TimeoutMs bounds a decision call. 0 means 1500 (ollama) / 3000 (remote).
	TimeoutMs int `json:"timeoutMs" toml:"TimeoutMs"`
}

// DecisionRouterConfig selects and configures the decision provider.
type DecisionRouterConfig struct {
	// Provider is "ollama" (default), "typesafe" or "custom".
	Provider DecisionProviderKind `json:"provider" toml:"Provider"`
	// BaseURL is the API root; the client appends /v1/systemone and /v1/models.
	BaseURL string `json:"baseURL,omitempty" toml:"BaseURL,omitempty"`
	// APIKey is encrypted at rest; "$ENV_VAR" references are allowed.
	APIKey string `json:"apiKey,omitempty" toml:"APIKey,omitempty"`
	// Model is the decision model, e.g. "tev1:0.8b", "jev-latest", "typesafe/jev-1.13".
	Model string `json:"model" toml:"Model"`
	// KeepAlive is the Ollama keep_alive value. Default "30m".
	KeepAlive string `json:"keepAlive,omitempty" toml:"KeepAlive,omitempty"`
	// Headers are extra HTTP headers for gateways.
	Headers map[string]string `json:"headers,omitempty" toml:"Headers,omitempty"`
}

// kind returns the provider kind with the "ollama" default applied.
func (r DecisionRouterConfig) kind() DecisionProviderKind {
	k := DecisionProviderKind(strings.ToLower(strings.TrimSpace(string(r.Provider))))
	if k == "" {
		return DecisionProviderOllama
	}
	return k
}

// EffectiveProvider returns the provider kind, defaulting to ollama.
func (r DecisionRouterConfig) EffectiveProvider() DecisionProviderKind { return r.kind() }

// EffectiveBaseURL returns the API root for the decision provider.
func (r DecisionRouterConfig) EffectiveBaseURL() string {
	configured := strings.TrimSpace(r.BaseURL)
	switch r.kind() {
	case DecisionProviderTypeSafe:
		if configured == "" {
			return DefaultTypeSafeBaseURL
		}
		return strings.TrimRight(configured, "/")
	case DecisionProviderCustom:
		return strings.TrimRight(configured, "/")
	default:
		if configured != "" {
			return models.ResolveOllamaRawBaseURL(configured)
		}
		ollamaBase := ""
		if cfg != nil {
			if p, ok := cfg.Providers[models.ProviderOllama]; ok {
				ollamaBase = p.BaseURL
			}
		}
		return models.ResolveOllamaRawBaseURL(ollamaBase)
	}
}

// EffectiveAPIKey returns the decrypted key with "$ENV" references expanded.
// For the typesafe provider an empty key falls back to $TYPESAFE_API_KEY.
func (r DecisionRouterConfig) EffectiveAPIKey() string {
	key := strings.TrimSpace(r.APIKey)
	if IsEncryptedSecretString(key) {
		if dec, err := decryptSecretString(key); err == nil {
			key = strings.TrimSpace(dec)
		} else {
			key = ""
		}
	}
	if strings.HasPrefix(key, "$") {
		name := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(key, "$"), "{"), "}")
		key = strings.TrimSpace(os.Getenv(name))
	}
	if key == "" && r.kind() == DecisionProviderTypeSafe {
		key = strings.TrimSpace(os.Getenv(TypeSafeAPIKeyEnv))
	}
	return key
}

// EffectiveTimeout returns the per-call decision timeout.
func (d DecisionModelConfig) EffectiveTimeout() time.Duration {
	if d.TimeoutMs > 0 {
		return time.Duration(d.TimeoutMs) * time.Millisecond
	}
	if d.Router.kind() == DecisionProviderOllama {
		return defaultModelAutoOllamaTimeout
	}
	return defaultModelAutoRemoteTimeout
}

// MaskAPIKey returns a masked form of key that is safe to expose over REST.
func MaskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	r := []rune(key)
	if len(r) <= 4 || strings.HasPrefix(key, "$") {
		return "••••"
	}
	return "••••" + string(r[len(r)-4:])
}

func validHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// ValidateDecisionModel validates the block. Field names are relative to
// decisionModel. Errors block a save; warnings (missing TypeSafe key) never do.
// An empty model is valid here: features that need it check for it themselves.
func ValidateDecisionModel(d DecisionModelConfig) (errs []FieldError, warnings []string) {
	addErr := func(field, format string, args ...any) {
		errs = append(errs, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	switch d.Router.kind() {
	case DecisionProviderOllama, DecisionProviderTypeSafe, DecisionProviderCustom:
	default:
		addErr("router.provider", "unknown provider %q (allowed: ollama, typesafe, custom)", d.Router.Provider)
	}

	base := strings.TrimSpace(d.Router.BaseURL)
	if d.Router.kind() == DecisionProviderCustom && base == "" {
		addErr("router.baseURL", "baseURL is required for the custom provider")
	} else if base != "" && !validHTTPURL(base) {
		addErr("router.baseURL", "baseURL must be a valid http(s) URL")
	}

	model := strings.TrimSpace(d.Router.Model)
	if d.Router.kind() == DecisionProviderOllama && strings.HasSuffix(strings.ToLower(model), ":cloud") {
		addErr("router.model", "System One requires a local Ollama model; %q is a cloud model", model)
	}
	if d.Router.kind() == DecisionProviderTypeSafe && d.Router.EffectiveAPIKey() == "" {
		warnings = append(warnings, "typesafe provider has no API key: set router.apiKey or $"+TypeSafeAPIKeyEnv)
	}
	if d.TimeoutMs < 0 {
		addErr("timeoutMs", "timeoutMs must not be negative")
	}
	return errs, warnings
}

// normalizeDecisionModel applies defaults and trims values. Empty collections
// become nil so an in-memory value equals what a reload yields.
func normalizeDecisionModel(d DecisionModelConfig) DecisionModelConfig {
	d.Router.Provider = d.Router.kind()
	d.Router.BaseURL = strings.TrimSpace(d.Router.BaseURL)
	d.Router.Model = strings.TrimSpace(d.Router.Model)
	d.Router.KeepAlive = strings.TrimSpace(d.Router.KeepAlive)
	if d.Router.KeepAlive == "" && d.Router.Provider == DecisionProviderOllama {
		d.Router.KeepAlive = defaultModelAutoKeepAlive
	}
	if len(d.Router.Headers) == 0 {
		d.Router.Headers = nil
	}
	return d
}

// migrateLegacyDecisionModel moves the pre-decisionModel router
// (modelAutoMode.router / modelAutoMode.timeoutMs) into the top-level block.
// It is idempotent: once the legacy fields are cleared there is nothing to do.
// When decisionModel already has a model it wins and the legacy values are
// only dropped from memory. The legacy router (including its encrypted key) is
// copied untouched.
func migrateLegacyDecisionModel() {
	if cfg == nil {
		return
	}
	legacy := cfg.ModelAutoMode.LegacyRouter
	legacyTimeout := cfg.ModelAutoMode.LegacyTimeoutMs
	if legacy == nil && legacyTimeout == 0 {
		return
	}
	cfg.ModelAutoMode.LegacyRouter, cfg.ModelAutoMode.LegacyTimeoutMs = nil, 0

	if legacy == nil || strings.TrimSpace(legacy.Model) == "" {
		return // nothing meaningful to migrate (e.g. only a stale default provider)
	}
	if strings.TrimSpace(cfg.DecisionModel.Router.Model) != "" {
		return // decisionModel wins
	}

	moved := *legacy
	cfg.DecisionModel.Router = moved
	if legacyTimeout > 0 && cfg.DecisionModel.TimeoutMs == 0 {
		cfg.DecisionModel.TimeoutMs = legacyTimeout
	}

	if err := ErrIfLocked("decisionModel"); err != nil {
		logging.Warn("config file is locked: decisionModel migrated in memory only", "error", err)
		return
	}
	if err := updateCfgFile(func(c *Config) {
		if strings.TrimSpace(c.DecisionModel.Router.Model) == "" {
			if c.ModelAutoMode.LegacyRouter != nil && strings.TrimSpace(c.ModelAutoMode.LegacyRouter.Model) != "" {
				c.DecisionModel.Router = *c.ModelAutoMode.LegacyRouter
				if c.DecisionModel.TimeoutMs == 0 {
					c.DecisionModel.TimeoutMs = c.ModelAutoMode.LegacyTimeoutMs
				}
			}
		}
		c.ModelAutoMode.LegacyRouter = nil
		c.ModelAutoMode.LegacyTimeoutMs = 0
	}); err != nil {
		logging.Warn("could not persist decisionModel migration; kept in memory", "error", err)
		return
	}
	logging.Info("migrated modelAutoMode.router to the decisionModel block")
}

// UpdateDecisionModel validates, persists and applies the block. The router API
// key is stored encrypted; an empty incoming key keeps the stored one.
func UpdateDecisionModel(d DecisionModelConfig) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if err := ErrIfLocked("decisionModel"); err != nil {
		return err
	}
	d = normalizeDecisionModel(d)
	if errs, _ := ValidateDecisionModel(d); len(errs) > 0 {
		parts := make([]string, len(errs))
		for i, e := range errs {
			parts[i] = e.Error()
		}
		return fmt.Errorf("invalid decisionModel configuration: %s", strings.Join(parts, "; "))
	}

	old := cfg.DecisionModel
	keepKey := strings.TrimSpace(d.Router.APIKey) == ""
	if keepKey {
		d.Router.APIKey = old.Router.APIKey
	}
	cfg.DecisionModel = d

	if err := updateCfgFile(func(c *Config) {
		next := d
		if keepKey {
			next.Router.APIKey = c.DecisionModel.Router.APIKey
		}
		c.DecisionModel = next
	}); err != nil {
		cfg.DecisionModel = old
		return err
	}
	publishDecisionModelChange()
	return nil
}

// ClearDecisionModelAPIKey removes the stored decision provider API key.
func ClearDecisionModelAPIKey() error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if err := ErrIfLocked("decisionModel.router.apiKey"); err != nil {
		return err
	}
	old := cfg.DecisionModel
	cfg.DecisionModel.Router.APIKey = ""
	if err := updateCfgFile(func(c *Config) {
		c.DecisionModel.Router.APIKey = ""
	}); err != nil {
		cfg.DecisionModel = old
		return err
	}
	publishDecisionModelChange()
	return nil
}

func publishDecisionModelChange() {
	Bus.Publish(ConfigChangeEvent{
		Section:     "decisionModel",
		Timestamp:   time.Now(),
		Source:      "config",
		ChangedKeys: []string{"decisionModel"},
	})
}
