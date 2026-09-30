package page

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/modelrouter"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/systemone"
	"github.com/digiogithub/pando/internal/ollamasetup"
	"github.com/digiogithub/pando/internal/tui/components/settings"
	"github.com/digiogithub/pando/internal/tui/util"
)

const (
	modelAutoKeyPrefix   = "modelAutoMode."
	modelAutoNoneOption  = "(none)"
	modelAutoKeyMaskRune = "•"
)

// modelAutoRouterModels caches the decision models discovered by the
// "Discover router models" action so the section can offer them as options
// without doing network I/O while it is being built.
var (
	modelAutoRouterMu     sync.Mutex
	modelAutoRouterModels []string
	// Discovery side state: list status, suggested-but-missing Ollama models
	// and whether the unfiltered catalogue was requested.
	modelAutoListStatus  string
	modelAutoSuggestions []string
	modelAutoShowAll     bool
	modelAutoPullMgr     = ollamasetup.NewManager()
)

// modelAutoPreset is a Custom provider preset (gateway root + default model).
type modelAutoPreset struct {
	Name    string
	BaseURL string
	Model   string
}

// modelAutoPresets mirrors the WebUI presets. The client appends
// /v1/systemone and /v1/models to the base URL, so LiteLLM's root is its
// /typesafe route.
var modelAutoPresets = []modelAutoPreset{
	{Name: "OpenRouter", BaseURL: "https://openrouter.ai/api", Model: "typesafe/jev-1.13"},
	{Name: "LiteLLM", BaseURL: "http://localhost:4000/typesafe"},
	{Name: "Kev", BaseURL: "http://localhost:8009"},
}

func setModelAutoDiscovery(status string, suggestions []string) {
	modelAutoRouterMu.Lock()
	modelAutoListStatus = status
	modelAutoSuggestions = append([]string(nil), suggestions...)
	modelAutoRouterMu.Unlock()
}

func getModelAutoDiscovery() (status string, suggestions []string, showAll bool) {
	modelAutoRouterMu.Lock()
	defer modelAutoRouterMu.Unlock()
	return modelAutoListStatus, append([]string(nil), modelAutoSuggestions...), modelAutoShowAll
}

func setModelAutoRouterModels(ids []string) {
	modelAutoRouterMu.Lock()
	modelAutoRouterModels = append([]string(nil), ids...)
	modelAutoRouterMu.Unlock()
}

func getModelAutoRouterModels() []string {
	modelAutoRouterMu.Lock()
	defer modelAutoRouterMu.Unlock()
	return append([]string(nil), modelAutoRouterModels...)
}

// modelAutoTestResultMsg carries the outcome of a "Test connection" action.
type modelAutoTestResultMsg struct {
	ok       bool
	summary  string
	problems []string
	err      error
}

// modelAutoDiscoverMsg carries the result of a router model discovery.
type modelAutoDiscoverMsg struct {
	ids         []string
	status      string
	suggestions []string
	err         error
}

// modelAutoPullMsg carries the final outcome of a suggested-model pull.
type modelAutoPullMsg struct {
	model string
	err   error
}

func buildModelAutoModeSection(cfg *config.Config) settings.Section {
	m := cfg.ModelAutoMode
	kind := m.Router.EffectiveProvider()
	baseURL := m.Router.BaseURL
	if kind != config.DecisionProviderCustom {
		baseURL = m.Router.EffectiveBaseURL()
	}

	keyValue := ""
	keyHint := "No key stored. Type a key to set it (never displayed afterwards)."
	if strings.TrimSpace(m.Router.APIKey) != "" {
		keyValue = config.MaskAPIKey(m.Router.APIKey)
		keyHint = "Key set. Type a new key to replace it; saving the masked value keeps it."
	}

	providerHint := "Ollama (local), TypeSafe Jev or Custom gateway."
	if kind != config.DecisionProviderOllama {
		if u, err := url.Parse(m.Router.EffectiveBaseURL()); err == nil && u.Host != "" {
			providerHint = fmt.Sprintf("Privacy: prompts and recent history are sent to %s.", u.Host)
		}
	}

	modelField := settings.Field{
		Label: "Router Model",
		Key:   modelAutoKeyPrefix + "model",
		Type:  settings.FieldText,
		Value: m.Router.Model,
		Hint:  "Decision model (e.g. tev1:0.8b). Use \"Discover router models\" to list them. Required to enable.",
	}
	if opts := getModelAutoRouterModels(); len(opts) > 0 {
		modelField.Type = settings.FieldSelect
		modelField.Options = ensureOption(opts, m.Router.Model)
	}

	fields := []settings.Field{
		{Label: "Enabled", Key: modelAutoKeyPrefix + "enabled", Type: settings.FieldToggle, Value: boolString(m.Enabled)},
		{Label: "Default to Auto", Key: modelAutoKeyPrefix + "defaultAuto", Type: settings.FieldToggle, Value: boolString(m.DefaultAuto), Hint: "New sessions start in Auto."},
		{
			Label:   "Decision Provider",
			Key:     modelAutoKeyPrefix + "provider",
			Type:    settings.FieldSelect,
			Value:   string(kind),
			Options: []string{string(config.DecisionProviderOllama), string(config.DecisionProviderTypeSafe), string(config.DecisionProviderCustom)},
			Hint:    providerHint,
		},
		{
			Label:    "Base URL",
			Key:      modelAutoKeyPrefix + "baseURL",
			Type:     settings.FieldText,
			Value:    baseURL,
			Disabled: kind == config.DecisionProviderOllama,
			Hint:     "Ollama: resolved from the Ollama provider. Custom: gateway root (OpenRouter, LiteLLM, Kev).",
		},
		{Label: "API Key", Key: modelAutoKeyPrefix + "apiKey", Type: settings.FieldText, Value: keyValue, Hint: keyHint},
	}
	if kind == config.DecisionProviderCustom {
		names := []string{"(choose)"}
		for _, pr := range modelAutoPresets {
			names = append(names, pr.Name)
		}
		fields = append(fields[:4], append([]settings.Field{{
			Label: "Custom Preset", Key: modelAutoKeyPrefix + "preset", Type: settings.FieldSelect,
			Value: "(choose)", Options: names,
			Hint: "Fills the base URL (and model) for OpenRouter, LiteLLM or Kev; edit the URL afterwards if needed.",
		}}, fields[4:]...)...)
	}
	if strings.TrimSpace(m.Router.APIKey) != "" {
		fields = append(fields, settings.Field{Label: "Clear API Key", Key: "action:model_auto_clear_key", Type: settings.FieldAction, Value: "Remove the stored key"})
	}
	fields = append(fields,
		modelField,
		settings.Field{Label: "Discover router models", Key: "action:model_auto_discover", Type: settings.FieldAction, Value: "List decision models from the provider"},
	)
	listStatus, suggestions, showAll := getModelAutoDiscovery()
	switch listStatus {
	case "filtered":
		val := "Showing decision models only; select to show all"
		if showAll {
			val = "Showing all models; select to show decision models only"
		}
		fields = append(fields, settings.Field{Label: "Show all models", Key: "action:model_auto_toggle_show_all", Type: settings.FieldAction, Value: val})
	case "unsupported":
		fields[len(fields)-1].Hint = "This provider cannot list models; type the router model id."
	}
	if kind == config.DecisionProviderOllama {
		for _, name := range suggestions {
			fields = append(fields, settings.Field{
				Label: "Pull " + name, Key: "action:model_auto_pull:" + name, Type: settings.FieldAction,
				Value: "Download this suggested decision model into Ollama",
			})
		}
	}
	fields = append(fields,
		settings.Field{Label: "Match Threshold", Key: modelAutoKeyPrefix + "threshold", Type: settings.FieldText, Value: strconv.FormatFloat(m.EffectiveThreshold(), 'f', -1, 64), Hint: "Minimum route probability (0-1, default 0.60)."},
		settings.Field{Label: "Router Timeout (ms)", Key: modelAutoKeyPrefix + "timeoutMs", Type: settings.FieldText, Value: strconv.Itoa(m.TimeoutMs), Hint: "0 = default."},
		settings.Field{Label: "History Prompts", Key: modelAutoKeyPrefix + "historyPrompts", Type: settings.FieldText, Value: strconv.Itoa(m.HistoryPrompts), Hint: "Previous user prompts sent to the router as context."},
		settings.Field{Label: "Test connection", Key: "action:model_auto_test", Type: settings.FieldAction, Value: "Check the router health"},
	)

	modelOptions := supportedModelOptions(cfg)
	fallbackOptions := append([]string{modelAutoNoneOption}, modelOptions...)
	for i, r := range m.Routes {
		prefix := fmt.Sprintf("%sroutes.%d.", modelAutoKeyPrefix, i)
		label := fmt.Sprintf("[Route %d] ", i+1)
		fb := func(n int) string {
			if n < len(r.Fallbacks) && r.Fallbacks[n] != "" {
				return string(r.Fallbacks[n])
			}
			return modelAutoNoneOption
		}
		fields = append(fields,
			settings.Field{Label: label + "ID", Key: prefix + "id", Type: settings.FieldText, Value: r.ID},
			settings.Field{Label: label + "Description", Key: prefix + "description", Type: settings.FieldText, Value: r.Description, Hint: "Tells the router when this route applies."},
			settings.Field{Label: label + "Model", Key: prefix + "model", Type: settings.FieldSelect, Value: string(r.Model), Options: ensureOption(modelOptions, string(r.Model)), UseModelDialog: true, ModelDialogTitle: "Select Route Model"},
			settings.Field{Label: label + "Fallback 1", Key: prefix + "fallback1", Type: settings.FieldSelect, Value: fb(0), Options: ensureOption(fallbackOptions, fb(0))},
			settings.Field{Label: label + "Fallback 2", Key: prefix + "fallback2", Type: settings.FieldSelect, Value: fb(1), Options: ensureOption(fallbackOptions, fb(1))},
			settings.Field{Label: label + "Enabled", Key: prefix + "enabled", Type: settings.FieldToggle, Value: boolString(!r.Disabled)},
			settings.Field{Label: label + "Move up", Key: fmt.Sprintf("action:model_auto_move_route:%d:-1", i), Type: settings.FieldAction, Value: "Move this route earlier", Disabled: i == 0},
			settings.Field{Label: label + "Move down", Key: fmt.Sprintf("action:model_auto_move_route:%d:1", i), Type: settings.FieldAction, Value: "Move this route later", Disabled: i == len(m.Routes)-1},
			settings.Field{Label: label + "Delete", Key: fmt.Sprintf("action:model_auto_delete_route:%d", i), Type: settings.FieldAction, Value: "Delete this route"},
		)
	}

	addField := settings.Field{Label: "Add route", Key: "action:model_auto_add_route", Type: settings.FieldAction, Value: "Add a new route"}
	if len(m.Routes) >= config.ModelAutoModeMaxRoutes {
		addField.Disabled = true
		addField.Value = fmt.Sprintf("Route limit reached (%d)", config.ModelAutoModeMaxRoutes)
	}
	fields = append(fields, addField)

	return settings.Section{Title: "Auto mode", Fields: fields}
}

func cloneModelAutoMode(m config.ModelAutoModeConfig) config.ModelAutoModeConfig {
	out := m
	out.Routes = make([]config.ModelAutoRoute, len(m.Routes))
	for i, r := range m.Routes {
		r.Fallbacks = append([]models.ModelID(nil), r.Fallbacks...)
		out.Routes[i] = r
	}
	return out
}

// persistModelAutoMode validates the draft and stores it, returning a readable
// error listing every field problem.
func persistModelAutoMode(m config.ModelAutoModeConfig) error {
	if errs, _ := config.ValidateModelAutoMode(m); len(errs) > 0 {
		parts := make([]string, len(errs))
		for i, e := range errs {
			parts[i] = e.Error()
		}
		return fmt.Errorf("invalid auto mode settings: %s", strings.Join(parts, "; "))
	}
	return config.UpdateModelAutoMode(m)
}

func saveModelAutoMode(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	m := cloneModelAutoMode(cfg.ModelAutoMode)
	sub := strings.TrimPrefix(field.Key, modelAutoKeyPrefix)
	value := strings.TrimSpace(field.Value)

	switch {
	case sub == "enabled" || sub == "defaultAuto":
		v, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid value for %s: %w", field.Label, err)
		}
		if sub == "enabled" {
			m.Enabled = v
		} else {
			m.DefaultAuto = v
		}
	case sub == "provider":
		kind := config.DecisionProviderKind(value)
		if kind != m.Router.Provider {
			// Switching provider invalidates the model and the discovered list.
			m.Router.Model = ""
			m.Router.BaseURL = ""
			setModelAutoRouterModels(nil)
			setModelAutoDiscovery("", nil)
			if kind == config.DecisionProviderCustom {
				// Custom requires a base URL: start from the first preset so the
				// switch validates; the user picks another preset or edits it.
				m.Router.BaseURL = modelAutoPresets[0].BaseURL
			}
		}
		m.Router.Provider = kind
	case sub == "preset":
		for _, pr := range modelAutoPresets {
			if pr.Name == value {
				m.Router.BaseURL = pr.BaseURL
				if pr.Model != "" {
					m.Router.Model = pr.Model
				}
			}
		}
	case sub == "baseURL":
		m.Router.BaseURL = value
	case sub == "apiKey":
		if value == "" || strings.HasPrefix(value, modelAutoKeyMaskRune) {
			return nil // unchanged / masked: keep the stored key
		}
		m.Router.APIKey = value
	case sub == "model":
		m.Router.Model = value
	case sub == "threshold":
		v, err := parseFloatValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid threshold: %w", err)
		}
		m.Threshold = v
	case sub == "timeoutMs" || sub == "historyPrompts":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid value for %s: %w", field.Label, err)
		}
		if sub == "timeoutMs" {
			m.TimeoutMs = v
		} else {
			m.HistoryPrompts = v
		}
	case strings.HasPrefix(sub, "routes."):
		parts := strings.Split(strings.TrimPrefix(sub, "routes."), ".")
		if len(parts) != 2 {
			return fmt.Errorf("unsupported auto mode setting %q", field.Key)
		}
		idx, err := strconv.Atoi(parts[0])
		if err != nil || idx < 0 || idx >= len(m.Routes) {
			return fmt.Errorf("unknown route in %q", field.Key)
		}
		r := &m.Routes[idx]
		setFallback := func(n int) {
			for len(r.Fallbacks) < 2 {
				r.Fallbacks = append(r.Fallbacks, "")
			}
			if value == modelAutoNoneOption {
				value = ""
			}
			r.Fallbacks[n] = models.ModelID(value)
			// Drop trailing blanks; interior blanks are invalid and surface as errors.
			for len(r.Fallbacks) > 0 && r.Fallbacks[len(r.Fallbacks)-1] == "" {
				r.Fallbacks = r.Fallbacks[:len(r.Fallbacks)-1]
			}
		}
		switch parts[1] {
		case "id":
			r.ID = value
		case "description":
			r.Description = value
		case "model":
			r.Model = models.ModelID(value)
		case "fallback1":
			setFallback(0)
		case "fallback2":
			setFallback(1)
		case "enabled":
			v, err := parseBoolValue(field.Value)
			if err != nil {
				return fmt.Errorf("invalid value for %s: %w", field.Label, err)
			}
			r.Disabled = !v
		default:
			return fmt.Errorf("unsupported auto mode setting %q", field.Key)
		}
	default:
		return fmt.Errorf("unsupported auto mode setting %q", field.Key)
	}
	return persistModelAutoMode(m)
}

// addModelAutoRoute appends a valid placeholder route the user then edits.
func addModelAutoRoute() error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	m := cloneModelAutoMode(cfg.ModelAutoMode)
	if len(m.Routes) >= config.ModelAutoModeMaxRoutes {
		return fmt.Errorf("at most %d routes are allowed", config.ModelAutoModeMaxRoutes)
	}
	used := map[string]bool{}
	for _, r := range m.Routes {
		used[strings.ToLower(r.ID)] = true
	}
	id := ""
	for n := len(m.Routes) + 1; ; n++ {
		id = fmt.Sprintf("route-%d", n)
		if !used[id] {
			break
		}
	}
	model := cfg.Agents[config.AgentCoder].Model
	m.Routes = append(m.Routes, config.ModelAutoRoute{ID: id, Description: "Describe when this route should be used", Model: model})
	return persistModelAutoMode(m)
}

// moveModelAutoRoute swaps route idx with its neighbour (delta -1 or +1).
func moveModelAutoRoute(idx, delta int) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	m := cloneModelAutoMode(cfg.ModelAutoMode)
	to := idx + delta
	if idx < 0 || idx >= len(m.Routes) || to < 0 || to >= len(m.Routes) {
		return fmt.Errorf("cannot move route %d", idx+1)
	}
	m.Routes[idx], m.Routes[to] = m.Routes[to], m.Routes[idx]
	return persistModelAutoMode(m)
}

// pullModelAutoModel pulls a suggested Ollama decision model on the router's
// base URL and reports when the download ends. The TUI cannot stream, so the
// command polls the job and returns once it finishes.
func pullModelAutoModel(model string) tea.Cmd {
	cfg := config.Get()
	if cfg == nil {
		return util.ReportError(fmt.Errorf("config not loaded"))
	}
	router := cfg.ModelAutoMode.Router
	if router.EffectiveProvider() != config.DecisionProviderOllama {
		return util.ReportError(fmt.Errorf("pull is only available for the Ollama decision provider"))
	}
	if !systemone.IsSuggestedOllamaModel(model) {
		return util.ReportError(fmt.Errorf("%q is not a suggested decision model", model))
	}
	job, err := modelAutoPullMgr.Pull(router.EffectiveBaseURL(), model)
	if err != nil {
		return util.ReportError(err)
	}
	return tea.Batch(util.ReportInfo("Pulling "+model+"..."), func() tea.Msg {
		for {
			time.Sleep(time.Second)
			j, ok := modelAutoPullMgr.Get(job.ID)
			if !ok {
				return modelAutoPullMsg{model: model, err: fmt.Errorf("pull job vanished")}
			}
			switch j.State {
			case "done":
				return modelAutoPullMsg{model: model}
			case "error":
				return modelAutoPullMsg{model: model, err: fmt.Errorf("%s", j.Error)}
			}
		}
	})
}

func deleteModelAutoRoute(idx int) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	m := cloneModelAutoMode(cfg.ModelAutoMode)
	if idx < 0 || idx >= len(m.Routes) {
		return fmt.Errorf("unknown route %d", idx+1)
	}
	m.Routes = append(m.Routes[:idx], m.Routes[idx+1:]...)
	return persistModelAutoMode(m)
}

// testModelAutoConnection probes the router (bypassing the health cache).
func testModelAutoConnection() tea.Cmd {
	cfg := config.Get()
	if cfg == nil {
		return util.ReportError(fmt.Errorf("config not loaded"))
	}
	m := cfg.ModelAutoMode
	return func() tea.Msg {
		p, err := modelrouter.ProviderFor(m.Router, m.EffectiveTimeout())
		if err != nil {
			return modelAutoTestResultMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		report := p.Health(ctx, m.Router.Model)
		return modelAutoTestResultMsg{
			ok:       report.OK,
			problems: report.Problems,
			summary:  fmt.Sprintf("router %s/%s reachable=%t authorized=%t latency=%dms", report.Kind, report.Model, report.Reachable, report.Authorized, report.LatencyMs),
		}
	}
}

// discoverModelAutoModels lists decision models from the configured provider.
func discoverModelAutoModels() tea.Cmd {
	cfg := config.Get()
	if cfg == nil {
		return util.ReportError(fmt.Errorf("config not loaded"))
	}
	m := cfg.ModelAutoMode
	return func() tea.Msg {
		p, err := modelrouter.ProviderFor(m.Router, m.EffectiveTimeout())
		if err != nil {
			return modelAutoDiscoverMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _, showAll := getModelAutoDiscovery()
		list, status, err := p.ListDecisionModels(ctx, showAll)
		if err != nil {
			return modelAutoDiscoverMsg{err: err}
		}
		ids := make([]string, 0, len(list))
		for _, dm := range list {
			ids = append(ids, dm.ID)
		}
		var suggestions []string
		if p.Kind() == systemone.KindOllama {
			installed := ids
			if !showAll {
				if all, _, aerr := p.ListDecisionModels(ctx, true); aerr == nil {
					installed = installed[:0:0]
					for _, dm := range all {
						installed = append(installed, dm.ID)
					}
				}
			}
			suggestions = systemone.MissingSuggestedOllamaModels(installed)
		}
		return modelAutoDiscoverMsg{ids: ids, status: fmt.Sprint(status), suggestions: suggestions}
	}
}
