package config

import (
	"fmt"
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

const (
	// ModelAutoModeMaxRoutes is the maximum number of enabled routes.
	ModelAutoModeMaxRoutes = 25
	// ModelAutoModeMaxFallbacks is the maximum number of fallbacks per route.
	ModelAutoModeMaxFallbacks = 2
	// ModelAutoModeMaxDescription is the maximum route description length (runes).
	ModelAutoModeMaxDescription = 500
	// ModelAutoModeReservedRouteID is reserved for the "no route matches" choice.
	ModelAutoModeReservedRouteID = "none"

	defaultModelAutoThreshold  = 0.60
	maxModelAutoHistoryPrompts = 20
)

// ModelAutoModeConfig configures model auto mode: each user prompt is routed to
// one of the configured models by a small decision model (System One). The
// decision provider itself lives in the top-level DecisionModel block.
type ModelAutoModeConfig struct {
	// Enabled turns the feature on. Default: false.
	Enabled bool `json:"enabled" toml:"Enabled"`
	// DefaultAuto makes new sessions start in Auto. Default: true.
	DefaultAuto bool `json:"defaultAuto" toml:"DefaultAuto"`
	// Selected is the GLOBAL Auto selection used by WebUI/TUI. nil means "use DefaultAuto".
	Selected *bool `json:"selected,omitempty" toml:"Selected,omitempty"`
	// Threshold is the minimum probability of the chosen route. Default 0.60.
	Threshold float64 `json:"threshold" toml:"Threshold"`
	// MinConfidence is an optional entropy-concentration guard. Default 0 (off).
	MinConfidence float64 `json:"minConfidence" toml:"MinConfidence"`
	// HistoryPrompts is how many previous user prompts are added to the state. Default 0.
	HistoryPrompts int `json:"historyPrompts" toml:"HistoryPrompts"`
	// Routes are the task routes.
	Routes []ModelAutoRoute `json:"routes" toml:"Routes,omitempty"`

	// LegacyRouter and LegacyTimeoutMs only exist so files written before the
	// top-level decisionModel block still decode. They are read by the loader
	// migration (migrateLegacyDecisionModel) and cleared afterwards; they are
	// never written back and never used at runtime.
	LegacyRouter    *DecisionRouterConfig `json:"router,omitempty" mapstructure:"router" toml:"Router,omitempty"`
	LegacyTimeoutMs int                   `json:"timeoutMs,omitempty" mapstructure:"timeoutMs" toml:"TimeoutMs,omitempty"`
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
// Field is relative to the block, e.g. "routes[2].fallbacks" (modelAutoMode) or
// "router.baseURL" (decisionModel).
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

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

// ValidateModelAutoMode validates the block. Errors block a save; warnings
// (unknown models) never do. The decision provider is validated by
// ValidateDecisionModel; d is only consulted for the shared model requirement.
func ValidateModelAutoMode(m ModelAutoModeConfig, d DecisionModelConfig) (errs []FieldError, warnings []string) {
	addErr := func(field, format string, args ...any) {
		errs = append(errs, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	if m.Enabled && strings.TrimSpace(d.Router.Model) == "" {
		addErr("router.model", "decisionModel.router.model is required when model auto mode is enabled")
	}

	if m.Threshold < 0 || m.Threshold > 1 {
		addErr("threshold", "threshold must be in (0,1]")
	}
	if m.MinConfidence < 0 || m.MinConfidence > 1 {
		addErr("minConfidence", "minConfidence must be in [0,1]")
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
	cfg.DecisionModel = normalizeDecisionModel(cfg.DecisionModel)
}

// logModelAutoModeValidation reports problems of the loaded block without
// failing the load: a bad hand edit must never brick startup.
func logModelAutoModeValidation() {
	if cfg == nil || (!cfg.ModelAutoMode.Enabled && len(cfg.ModelAutoMode.Routes) == 0) {
		return
	}
	errs, warnings := ValidateModelAutoMode(cfg.ModelAutoMode, cfg.DecisionModel)
	dErrs, dWarnings := ValidateDecisionModel(cfg.DecisionModel)
	for _, e := range errs {
		logging.Warn("invalid modelAutoMode configuration", "field", e.Field, "error", e.Message)
	}
	for _, e := range dErrs {
		logging.Warn("invalid decisionModel configuration", "field", e.Field, "error", e.Message)
	}
	for _, w := range append(warnings, dWarnings...) {
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
	if errs, _ := ValidateModelAutoMode(m, cfg.DecisionModel); len(errs) > 0 {
		parts := make([]string, len(errs))
		for i, e := range errs {
			parts[i] = e.Error()
		}
		return fmt.Errorf("invalid modelAutoMode configuration: %s", strings.Join(parts, "; "))
	}

	old := cfg.ModelAutoMode
	m.LegacyRouter, m.LegacyTimeoutMs = nil, 0
	if m.Selected == nil {
		m.Selected = old.Selected
	}
	cfg.ModelAutoMode = m

	if err := updateCfgFile(func(c *Config) {
		c.ModelAutoMode = m
	}); err != nil {
		cfg.ModelAutoMode = old
		return err
	}
	publishModelAutoModeChange()
	return nil
}

// ClearModelAutoModeAPIKey removes the stored decision provider API key.
//
// Deprecated: the key lives in the decisionModel block; use ClearDecisionModelAPIKey.
func ClearModelAutoModeAPIKey() error { return ClearDecisionModelAPIKey() }

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
