package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/logging"
)

// AutoModelID is the pseudo model id that selects "Auto" mode in every model
// selector. It is never registered in the models catalogue: the agent resolves
// it to a concrete model per user prompt through the decision provider.
const AutoModelID = "auto"

// DecisionProviderKind names the System One decision provider that routes prompts.
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

	// ModelAutoModeMaxRoutes is the maximum number of enabled routes.
	ModelAutoModeMaxRoutes = 25
	// ModelAutoModeMaxFallbacks is the maximum number of fallbacks per route.
	ModelAutoModeMaxFallbacks = 2
	// ModelAutoModeMaxDescription is the maximum route description length (runes).
	ModelAutoModeMaxDescription = 500
	// ModelAutoModeReservedRouteID is reserved for the "no route matches" choice.
	ModelAutoModeReservedRouteID = "none"

	defaultModelAutoThreshold     = 0.60
	defaultModelAutoKeepAlive     = "30m"
	defaultModelAutoOllamaTimeout = 1500 * time.Millisecond
	defaultModelAutoRemoteTimeout = 3000 * time.Millisecond
	maxModelAutoHistoryPrompts    = 20
)

// ModelAutoModeConfig configures model auto mode: each user prompt is routed to
// one of the configured models by a small decision model (System One).
type ModelAutoModeConfig struct {
	// Enabled turns the feature on. Default: false.
	Enabled bool `json:"enabled" toml:"Enabled"`
	// DefaultAuto makes new sessions start in Auto. Default: true.
	DefaultAuto bool `json:"defaultAuto" toml:"DefaultAuto"`
	// Selected is the GLOBAL Auto selection used by WebUI/TUI. nil means "use DefaultAuto".
	Selected *bool `json:"selected,omitempty" toml:"Selected,omitempty"`
	// Router is the decision provider.
	Router DecisionRouterConfig `json:"router" toml:"Router"`
	// Threshold is the minimum probability of the chosen route. Default 0.60.
	Threshold float64 `json:"threshold" toml:"Threshold"`
	// MinConfidence is an optional entropy-concentration guard. Default 0 (off).
	MinConfidence float64 `json:"minConfidence" toml:"MinConfidence"`
	// TimeoutMs bounds a routing call. 0 means 1500 (ollama) / 3000 (remote).
	TimeoutMs int `json:"timeoutMs" toml:"TimeoutMs"`
	// HistoryPrompts is how many previous user prompts are added to the state. Default 0.
	HistoryPrompts int `json:"historyPrompts" toml:"HistoryPrompts"`
	// Routes are the task routes.
	Routes []ModelAutoRoute `json:"routes" toml:"Routes,omitempty"`
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

// ModelAutoRoute describes one kind of task and the model that should handle it.
type ModelAutoRoute struct {
	// ID is a stable slug used as the choice key.
	ID string `json:"id" toml:"ID"`
	// Description is the natural-language task description (max 500 chars).
	Description string `json:"description" toml:"Description"`
	// Model is the primary model.
	Model models.ModelID `json:"model" toml:"Model"`
	// Fallbacks are tried in order when the primary provider fails (max 2).
	Fallbacks []models.ModelID `json:"fallbacks,omitempty" toml:"Fallbacks,omitempty"`
	// Disabled removes the route from the routing question.
	Disabled bool `json:"disabled,omitempty" toml:"Disabled,omitempty"`
}

// FieldError is a validation problem attached to a field of the block.
// Field is relative to modelAutoMode, e.g. "router.baseURL" or "routes[2].fallbacks".
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

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

// EffectiveTimeout returns the per-call routing timeout.
func (m ModelAutoModeConfig) EffectiveTimeout() time.Duration {
	if m.TimeoutMs > 0 {
		return time.Duration(m.TimeoutMs) * time.Millisecond
	}
	if m.Router.kind() == DecisionProviderOllama {
		return defaultModelAutoOllamaTimeout
	}
	return defaultModelAutoRemoteTimeout
}

// EffectiveThreshold returns the minimum route probability (default 0.60).
func (m ModelAutoModeConfig) EffectiveThreshold() float64 {
	if m.Threshold <= 0 || m.Threshold > 1 {
		return defaultModelAutoThreshold
	}
	return m.Threshold
}

// EnabledRoutes returns the routes that are not disabled, in order.
func (m ModelAutoModeConfig) EnabledRoutes() []ModelAutoRoute {
	out := make([]ModelAutoRoute, 0, len(m.Routes))
	for _, r := range m.Routes {
		if !r.Disabled {
			out = append(out, r)
		}
	}
	return out
}

// AutoSelected reports whether Auto is the globally selected model.
func (m ModelAutoModeConfig) AutoSelected() bool {
	if !m.Enabled {
		return false
	}
	if m.Selected != nil {
		return *m.Selected
	}
	return m.DefaultAuto
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

// ValidateModelAutoMode validates the block. Errors block a save; warnings
// (unknown models, missing TypeSafe key) never do.
func ValidateModelAutoMode(m ModelAutoModeConfig) (errs []FieldError, warnings []string) {
	addErr := func(field, format string, args ...any) {
		errs = append(errs, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	switch m.Router.kind() {
	case DecisionProviderOllama, DecisionProviderTypeSafe, DecisionProviderCustom:
	default:
		addErr("router.provider", "unknown provider %q (allowed: ollama, typesafe, custom)", m.Router.Provider)
	}

	base := strings.TrimSpace(m.Router.BaseURL)
	if m.Router.kind() == DecisionProviderCustom && base == "" {
		addErr("router.baseURL", "baseURL is required for the custom provider")
	} else if base != "" && !validHTTPURL(base) {
		addErr("router.baseURL", "baseURL must be a valid http(s) URL")
	}

	model := strings.TrimSpace(m.Router.Model)
	if m.Enabled && model == "" {
		addErr("router.model", "router model is required when model auto mode is enabled")
	}
	if m.Router.kind() == DecisionProviderOllama && strings.HasSuffix(strings.ToLower(model), ":cloud") {
		addErr("router.model", "System One requires a local Ollama model; %q is a cloud model", model)
	}
	if m.Router.kind() == DecisionProviderTypeSafe && m.Router.EffectiveAPIKey() == "" {
		warnings = append(warnings, "typesafe provider has no API key: set router.apiKey or $"+TypeSafeAPIKeyEnv)
	}

	if m.Threshold < 0 || m.Threshold > 1 {
		addErr("threshold", "threshold must be in (0,1]")
	}
	if m.MinConfidence < 0 || m.MinConfidence > 1 {
		addErr("minConfidence", "minConfidence must be in [0,1]")
	}
	if m.TimeoutMs < 0 {
		addErr("timeoutMs", "timeoutMs must not be negative")
	}
	if m.HistoryPrompts < 0 || m.HistoryPrompts > maxModelAutoHistoryPrompts {
		addErr("historyPrompts", "historyPrompts must be between 0 and %d", maxModelAutoHistoryPrompts)
	}

	known := models.SupportedModels()
	warnUnknown := func(field string, id models.ModelID) {
		if _, ok := known[id]; !ok {
			warnings = append(warnings, fmt.Sprintf("%s: unknown model %q (candidates with unknown models are skipped at runtime)", field, id))
		}
	}

	if n := len(m.EnabledRoutes()); n > ModelAutoModeMaxRoutes {
		addErr("routes", "at most %d enabled routes are allowed (got %d)", ModelAutoModeMaxRoutes, n)
	}
	seen := make(map[string]int, len(m.Routes))
	for i, r := range m.Routes {
		p := fmt.Sprintf("routes[%d]", i)
		id := strings.TrimSpace(r.ID)
		switch {
		case id == "":
			addErr(p+".id", "route id must not be blank")
		case strings.EqualFold(id, ModelAutoModeReservedRouteID):
			addErr(p+".id", "%q is a reserved route id", ModelAutoModeReservedRouteID)
		default:
			if first, dup := seen[strings.ToLower(id)]; dup {
				addErr(p+".id", "duplicate route id %q (also used by routes[%d])", id, first)
			} else {
				seen[strings.ToLower(id)] = i
			}
		}
		desc := strings.TrimSpace(r.Description)
		if desc == "" {
			addErr(p+".description", "route description is required")
		} else if utf8.RuneCountInString(desc) > ModelAutoModeMaxDescription {
			addErr(p+".description", "route description must be at most %d characters", ModelAutoModeMaxDescription)
		}
		primary := models.ModelID(strings.TrimSpace(string(r.Model)))
		if primary == "" {
			addErr(p+".model", "route model is required")
		} else {
			warnUnknown(p+".model", primary)
		}
		if len(r.Fallbacks) > ModelAutoModeMaxFallbacks {
			addErr(p+".fallbacks", "at most %d fallbacks are allowed (got %d)", ModelAutoModeMaxFallbacks, len(r.Fallbacks))
		}
		dups := make(map[models.ModelID]bool, len(r.Fallbacks))
		for _, fb := range r.Fallbacks {
			fb = models.ModelID(strings.TrimSpace(string(fb)))
			switch {
			case fb == "":
				addErr(p+".fallbacks", "fallback model must not be blank")
			case fb == primary:
				addErr(p+".fallbacks", "fallback %q equals the primary model", fb)
			case dups[fb]:
				addErr(p+".fallbacks", "duplicate fallback %q", fb)
			default:
				warnUnknown(p+".fallbacks", fb)
			}
			dups[fb] = true
		}
	}
	return errs, warnings
}

// normalizeModelAutoMode applies defaults and trims values. Empty collections
// become nil so an in-memory value equals what a reload yields.
func normalizeModelAutoMode(m ModelAutoModeConfig) ModelAutoModeConfig {
	m.Router.Provider = m.Router.kind()
	m.Router.BaseURL = strings.TrimSpace(m.Router.BaseURL)
	m.Router.Model = strings.TrimSpace(m.Router.Model)
	m.Router.KeepAlive = strings.TrimSpace(m.Router.KeepAlive)
	if m.Router.KeepAlive == "" && m.Router.Provider == DecisionProviderOllama {
		m.Router.KeepAlive = defaultModelAutoKeepAlive
	}
	if len(m.Router.Headers) == 0 {
		m.Router.Headers = nil
	}
	if m.Threshold == 0 {
		m.Threshold = defaultModelAutoThreshold
	}
	routes := make([]ModelAutoRoute, 0, len(m.Routes))
	for _, r := range m.Routes {
		r.ID = strings.TrimSpace(r.ID)
		r.Description = strings.TrimSpace(r.Description)
		r.Model = models.ModelID(strings.TrimSpace(string(r.Model)))
		var fbs []models.ModelID
		for _, fb := range r.Fallbacks {
			fbs = append(fbs, models.ModelID(strings.TrimSpace(string(fb))))
		}
		r.Fallbacks = fbs
		routes = append(routes, r)
	}
	if len(routes) == 0 {
		routes = nil
	}
	m.Routes = routes
	return m
}

// normalizeModelAutoModeDefaults applies defaults to the loaded config.
func normalizeModelAutoModeDefaults() {
	if cfg == nil {
		return
	}
	cfg.ModelAutoMode = normalizeModelAutoMode(cfg.ModelAutoMode)
}

// logModelAutoModeValidation reports problems of the loaded block without
// failing the load: a bad hand edit must never brick startup.
func logModelAutoModeValidation() {
	if cfg == nil || (!cfg.ModelAutoMode.Enabled && len(cfg.ModelAutoMode.Routes) == 0) {
		return
	}
	errs, warnings := ValidateModelAutoMode(cfg.ModelAutoMode)
	for _, e := range errs {
		logging.Warn("invalid modelAutoMode configuration", "field", e.Field, "error", e.Message)
	}
	for _, w := range warnings {
		logging.Warn("modelAutoMode configuration warning", "warning", w)
	}
}

// UpdateModelAutoMode validates, persists and applies the block. The router API
// key is stored encrypted; an empty incoming key keeps the stored one.
func UpdateModelAutoMode(m ModelAutoModeConfig) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if err := ErrIfLocked("modelAutoMode"); err != nil {
		return err
	}
	m = normalizeModelAutoMode(m)
	if errs, _ := ValidateModelAutoMode(m); len(errs) > 0 {
		parts := make([]string, len(errs))
		for i, e := range errs {
			parts[i] = e.Error()
		}
		return fmt.Errorf("invalid modelAutoMode configuration: %s", strings.Join(parts, "; "))
	}

	old := cfg.ModelAutoMode
	keepKey := strings.TrimSpace(m.Router.APIKey) == ""
	if keepKey {
		m.Router.APIKey = old.Router.APIKey
	}
	if m.Selected == nil {
		m.Selected = old.Selected
	}
	cfg.ModelAutoMode = m

	if err := updateCfgFile(func(c *Config) {
		next := m
		if keepKey {
			next.Router.APIKey = c.ModelAutoMode.Router.APIKey
		}
		c.ModelAutoMode = next
	}); err != nil {
		cfg.ModelAutoMode = old
		return err
	}
	publishModelAutoModeChange()
	return nil
}

// ClearModelAutoModeAPIKey removes the stored router API key.
func ClearModelAutoModeAPIKey() error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if err := ErrIfLocked("modelAutoMode.router.apiKey"); err != nil {
		return err
	}
	old := cfg.ModelAutoMode
	cfg.ModelAutoMode.Router.APIKey = ""
	if err := updateCfgFile(func(c *Config) {
		c.ModelAutoMode.Router.APIKey = ""
	}); err != nil {
		cfg.ModelAutoMode = old
		return err
	}
	publishModelAutoModeChange()
	return nil
}

// SetModelAutoSelected persists the global Auto selection used by WebUI/TUI.
func SetModelAutoSelected(selected bool) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if err := ErrIfLocked("modelAutoMode.selected"); err != nil {
		return err
	}
	old := cfg.ModelAutoMode.Selected
	v := selected
	cfg.ModelAutoMode.Selected = &v
	if err := updateCfgFile(func(c *Config) {
		s := selected
		c.ModelAutoMode.Selected = &s
	}); err != nil {
		cfg.ModelAutoMode.Selected = old
		return err
	}
	publishModelAutoModeChange()
	return nil
}

func publishModelAutoModeChange() {
	Bus.Publish(ConfigChangeEvent{
		Section:     "modelAutoMode",
		Timestamp:   time.Now(),
		Source:      "config",
		ChangedKeys: []string{"modelAutoMode"},
	})
}
