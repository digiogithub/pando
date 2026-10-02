package page

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/modelrouter"
	"github.com/digiogithub/pando/internal/llm/systemone"
	"github.com/digiogithub/pando/internal/ollamasetup"
	"github.com/digiogithub/pando/internal/tui/components/settings"
	"github.com/digiogithub/pando/internal/tui/util"
)

const (
	decisionKeyPrefix   = "decisionModel."
	decisionKeyMaskRune = "•"
	// decisionInfoHint points the read-only info rows of the other sections at
	// the section that owns the settings.
	decisionInfoHint = "Configure in the Decision model section."
)

// Discovered decision models are cached so the section can offer them as
// options without doing network I/O while it is being built.
var (
	decisionMu     sync.Mutex
	decisionModels []string
	// Discovery side state: list status, suggested-but-missing Ollama models
	// and whether the unfiltered catalogue was requested.
	decisionListStatus  string
	decisionSuggestions []string
	decisionShowAll     bool
	decisionPullMgr     = ollamasetup.NewManager()
)

// decisionPreset is a Custom provider preset (gateway root + default model).
type decisionPreset struct {
	Name    string
	BaseURL string
	Model   string
}

// decisionPresets mirrors the WebUI presets. The client appends /v1/systemone
// and /v1/models to the base URL, so LiteLLM's root is its /typesafe route.
var decisionPresets = []decisionPreset{
	{Name: "OpenRouter", BaseURL: "https://openrouter.ai/api", Model: "typesafe/jev-1.13"},
	{Name: "LiteLLM", BaseURL: "http://localhost:4000/typesafe"},
	{Name: "Kev", BaseURL: "http://localhost:8009"},
}

func setDecisionDiscovery(status string, suggestions []string) {
	decisionMu.Lock()
	decisionListStatus = status
	decisionSuggestions = append([]string(nil), suggestions...)
	decisionMu.Unlock()
}

func getDecisionDiscovery() (status string, suggestions []string, showAll bool) {
	decisionMu.Lock()
	defer decisionMu.Unlock()
	return decisionListStatus, append([]string(nil), decisionSuggestions...), decisionShowAll
}

func setDecisionModels(ids []string) {
	decisionMu.Lock()
	decisionModels = append([]string(nil), ids...)
	decisionMu.Unlock()
}

func getDecisionModels() []string {
	decisionMu.Lock()
	defer decisionMu.Unlock()
	return append([]string(nil), decisionModels...)
}

// decisionTestResultMsg carries the outcome of a "Test connection" action.
type decisionTestResultMsg struct {
	ok       bool
	summary  string
	problems []string
	err      error
}

// decisionDiscoverMsg carries the result of a decision model discovery.
type decisionDiscoverMsg struct {
	ids         []string
	status      string
	suggestions []string
	err         error
}

// decisionPullMsg carries the final outcome of a suggested-model pull.
type decisionPullMsg struct {
	model string
	err   error
}

// decisionProviderPrivacy returns the privacy note for a hosted decision
// provider, or "" for the local Ollama provider.
func decisionProviderPrivacy(r config.DecisionRouterConfig) string {
	if r.EffectiveProvider() == config.DecisionProviderOllama {
		return ""
	}
	if u, err := url.Parse(r.EffectiveBaseURL()); err == nil && u.Host != "" {
		return fmt.Sprintf("Privacy: prompts and recent history are sent to %s.", u.Host)
	}
	return "Privacy: this provider is remote; prompts leave your machine to be classified."
}

// decisionModelInfoRows returns the read-only rows other sections show about
// the shared decision model: the provider/model, its health, a privacy note for
// hosted providers and a pointer to the Decision model section. keyPrefix
// namespaces the (disabled) field keys inside the host section.
func decisionModelInfoRows(cfg *config.Config, keyPrefix string) []settings.Field {
	info := func(suffix, label, value string) settings.Field {
		return settings.Field{Label: label, Key: keyPrefix + suffix, Value: value, Type: settings.FieldText, Disabled: true}
	}
	router := cfg.DecisionModel.Router
	model := strings.TrimSpace(router.Model)
	if model == "" {
		return []settings.Field{
			warningNote(keyPrefix+"routerWarning", "Decision model", "Not configured. "+decisionInfoHint),
		}
	}
	rows := []settings.Field{
		info("routerInfo", "Decision model", string(router.EffectiveProvider())+"/"+model),
		info("routerHealth", "Health", personaRouterHealthStatus(cfg.DecisionModel)),
	}
	if router.EffectiveProvider() != config.DecisionProviderOllama {
		rows = append(rows, infoNote(keyPrefix+"routerPrivacy", "Privacy", "This provider is remote: your prompts leave your machine to be classified."))
	}
	return append(rows, infoNote(keyPrefix+"routerHint", "Note", decisionInfoHint))
}

// formatDecisionHeaders renders the headers as "Name: ••••; Name2: ••••" so
// secrets never reach the screen; saving the masked value keeps the stored one.
func formatDecisionHeaders(h map[string]string) string {
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, k := range names {
		parts[i] = k + ": " + strings.Repeat(decisionKeyMaskRune, 4)
	}
	return strings.Join(parts, "; ")
}

// parseDecisionHeaders parses "Name: value; Name2: value" and keeps the stored
// value of an entry whose value is still masked.
func parseDecisionHeaders(raw string, stored map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, ":")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid header %q (expected Name: value)", part)
		}
		if strings.HasPrefix(value, decisionKeyMaskRune) {
			if old, found := stored[name]; found {
				value = old
			} else {
				return nil, fmt.Errorf("header %q has no stored value; type its value", name)
			}
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func buildDecisionModelSection(cfg *config.Config) settings.Section {
	d := cfg.DecisionModel
	kind := d.Router.EffectiveProvider()
	baseURL := d.Router.BaseURL
	if kind != config.DecisionProviderCustom {
		baseURL = d.Router.EffectiveBaseURL()
	}

	keyValue := ""
	keyHint := "No key stored. Type a key to set it (never displayed afterwards)."
	if strings.TrimSpace(d.Router.APIKey) != "" {
		keyValue = config.MaskAPIKey(d.Router.APIKey)
		keyHint = "Key set. Type a new key to replace it; saving the masked value keeps it."
	}

	providerHint := "Ollama (local), TypeSafe Jev or Custom gateway."
	if note := decisionProviderPrivacy(d.Router); note != "" {
		providerHint = note
	}

	modelField := settings.Field{
		Label: "Model",
		Key:   decisionKeyPrefix + "router.model",
		Type:  settings.FieldText,
		Value: d.Router.Model,
		Hint:  "Decision model (e.g. tev1:0.8b). Use \"Discover models\" to list them. Required while a feature uses it.",
	}
	if opts := getDecisionModels(); len(opts) > 0 {
		modelField.Type = settings.FieldSelect
		modelField.Options = ensureOption(opts, d.Router.Model)
	}

	fields := []settings.Field{
		headerField(decisionKeyPrefix+"header.connection", "Connection"),
		{
			Label:   "Provider",
			Key:     decisionKeyPrefix + "router.provider",
			Type:    settings.FieldSelect,
			Value:   string(kind),
			Options: []string{string(config.DecisionProviderOllama), string(config.DecisionProviderTypeSafe), string(config.DecisionProviderCustom)},
			Hint:    providerHint,
		},
	}
	if kind == config.DecisionProviderCustom {
		names := []string{"(choose)"}
		for _, pr := range decisionPresets {
			names = append(names, pr.Name)
		}
		fields = append(fields, settings.Field{
			Label: "Custom Preset", Key: decisionKeyPrefix + "router.preset", Type: settings.FieldSelect,
			Value: "(choose)", Options: names,
			Hint: "Fills the base URL (and model) for OpenRouter, LiteLLM or Kev; edit the URL afterwards if needed.",
		})
	}
	fields = append(fields,
		settings.Field{
			Label:    "Base URL",
			Key:      decisionKeyPrefix + "router.baseURL",
			Type:     settings.FieldText,
			Value:    baseURL,
			Disabled: kind == config.DecisionProviderOllama,
			Hint:     "Ollama: resolved from the Ollama provider. Custom: gateway root (OpenRouter, LiteLLM, Kev).",
		},
		settings.Field{Label: "API Key", Key: decisionKeyPrefix + "router.apiKey", Type: settings.FieldText, Value: keyValue, Hint: keyHint},
	)
	if strings.TrimSpace(d.Router.APIKey) != "" {
		fields = append(fields, settings.Field{Label: "Clear API Key", Key: "action:decision_model_clear_key", Type: settings.FieldAction, Value: "Remove the stored key"})
	}
	if kind == config.DecisionProviderCustom {
		fields = append(fields, settings.Field{
			Label: "Headers", Key: decisionKeyPrefix + "router.headers", Type: settings.FieldText,
			Value: formatDecisionHeaders(d.Router.Headers),
			Hint:  "Extra gateway headers as \"Name: value; Name2: value\". Values are never displayed; keep a masked value to retain it.",
		})
	}
	if kind == config.DecisionProviderOllama {
		fields = append(fields, settings.Field{
			Label: "Keep Alive", Key: decisionKeyPrefix + "router.keepAlive", Type: settings.FieldText,
			Value: d.Router.KeepAlive, Hint: "Ollama keep_alive (default 30m).",
		})
	}
	fields = append(fields,
		modelField,
		settings.Field{Label: "Discover models", Key: "action:decision_model_discover", Type: settings.FieldAction, Value: "List decision models from the provider"},
	)
	listStatus, suggestions, showAll := getDecisionDiscovery()
	switch listStatus {
	case "filtered":
		val := "Showing decision models only; select to show all"
		if showAll {
			val = "Showing all models; select to show decision models only"
		}
		fields = append(fields, settings.Field{Label: "Show all models", Key: "action:decision_model_toggle_show_all", Type: settings.FieldAction, Value: val})
	case "unsupported":
		fields[len(fields)-1].Hint = "This provider cannot list models; type the model id."
	}
	if kind == config.DecisionProviderOllama {
		for _, name := range suggestions {
			fields = append(fields, settings.Field{
				Label: "Pull " + name, Key: "action:decision_model_pull:" + name, Type: settings.FieldAction,
				Value: "Download this suggested decision model into Ollama",
			})
		}
	}
	fields = append(fields,
		headerField(decisionKeyPrefix+"header.diagnostics", "Diagnostics"),
		settings.Field{Label: "Timeout (ms)", Key: decisionKeyPrefix + "timeoutMs", Type: settings.FieldText, Value: strconv.Itoa(d.TimeoutMs), Hint: "0 = default (1500 local, 3000 remote)."},
		settings.Field{Label: "Effective URL", Key: decisionKeyPrefix + "info.url", Type: settings.FieldText, Value: d.Router.EffectiveBaseURL(), Disabled: true},
	)
	if strings.TrimSpace(d.Router.Model) != "" {
		fields = append(fields, settings.Field{Label: "Health", Key: decisionKeyPrefix + "info.health", Type: settings.FieldText, Value: personaRouterHealthStatus(d), Disabled: true})
	}
	if note := decisionProviderPrivacy(d.Router); note != "" {
		fields = append(fields, infoNote(decisionKeyPrefix+"info.privacy", "Privacy", note))
	}
	fields = append(fields, settings.Field{Label: "Test connection", Key: "action:decision_model_test", Type: settings.FieldAction, Value: "Check the decision model health"})

	return settings.Section{Title: "Decision model", Fields: fields}
}

// saveDecisionModel stores one decisionModel.* field through
// config.UpdateDecisionModel, which validates the whole block.
func saveDecisionModel(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	d := cfg.DecisionModel
	d.Router.Headers = copyStringMap(d.Router.Headers)
	sub := strings.TrimPrefix(field.Key, decisionKeyPrefix)
	value := strings.TrimSpace(field.Value)

	switch sub {
	case "router.provider":
		kind := config.DecisionProviderKind(value)
		if kind != d.Router.Provider {
			// Switching provider invalidates the model and the discovered list.
			d.Router.Model = ""
			d.Router.BaseURL = ""
			setDecisionModels(nil)
			setDecisionDiscovery("", nil)
			if kind == config.DecisionProviderCustom {
				// Custom requires a base URL: start from the first preset so the
				// switch validates; the user picks another preset or edits it.
				d.Router.BaseURL = decisionPresets[0].BaseURL
			}
		}
		d.Router.Provider = kind
	case "router.preset":
		for _, pr := range decisionPresets {
			if pr.Name == value {
				d.Router.BaseURL = pr.BaseURL
				if pr.Model != "" {
					d.Router.Model = pr.Model
				}
			}
		}
	case "router.baseURL":
		d.Router.BaseURL = value
	case "router.apiKey":
		if value == "" || strings.HasPrefix(value, decisionKeyMaskRune) {
			return nil // unchanged / masked: keep the stored key
		}
		d.Router.APIKey = value
	case "router.model":
		d.Router.Model = value
	case "router.keepAlive":
		d.Router.KeepAlive = value
	case "router.headers":
		h, err := parseDecisionHeaders(field.Value, d.Router.Headers)
		if err != nil {
			return err
		}
		d.Router.Headers = h
	case "timeoutMs":
		v, err := strconv.Atoi(value)
		if err != nil {
			return invalidFieldValueError(field, err)
		}
		d.TimeoutMs = v
	default:
		return fmt.Errorf("unsupported decision model setting %q", field.Key)
	}

	// Refuse a change that would leave an enabled consumer without a model.
	if strings.TrimSpace(d.Router.Model) == "" && cfg.AnyDecisionConsumerEnabled() {
		return fmt.Errorf("a feature that uses the decision model is enabled; set a model first or disable the feature")
	}
	return config.UpdateDecisionModel(d)
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// pullDecisionModel pulls a suggested Ollama decision model on the router's
// base URL and reports when the download ends. The TUI cannot stream, so the
// command polls the job and returns once it finishes.
func pullDecisionModel(model string) tea.Cmd {
	cfg := config.Get()
	if cfg == nil {
		return util.ReportError(fmt.Errorf("config not loaded"))
	}
	router := cfg.DecisionModel.Router
	if router.EffectiveProvider() != config.DecisionProviderOllama {
		return util.ReportError(fmt.Errorf("pull is only available for the Ollama decision provider"))
	}
	if !systemone.IsSuggestedOllamaModel(model) {
		return util.ReportError(fmt.Errorf("%q is not a suggested decision model", model))
	}
	job, err := decisionPullMgr.Pull(router.EffectiveBaseURL(), model)
	if err != nil {
		return util.ReportError(err)
	}
	return tea.Batch(util.ReportInfo("Pulling "+model+"..."), func() tea.Msg {
		for {
			time.Sleep(time.Second)
			j, ok := decisionPullMgr.Get(job.ID)
			if !ok {
				return decisionPullMsg{model: model, err: fmt.Errorf("pull job vanished")}
			}
			switch j.State {
			case "done":
				return decisionPullMsg{model: model}
			case "error":
				return decisionPullMsg{model: model, err: fmt.Errorf("%s", j.Error)}
			}
		}
	})
}

// testDecisionConnection probes the decision model (bypassing the health cache).
func testDecisionConnection() tea.Cmd {
	cfg := config.Get()
	if cfg == nil {
		return util.ReportError(fmt.Errorf("config not loaded"))
	}
	m := cfg.DecisionModel
	return func() tea.Msg {
		p, err := modelrouter.ProviderWithTimeout(m.Router, m.EffectiveTimeout())
		if err != nil {
			return decisionTestResultMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		report := p.Health(ctx, m.Router.Model)
		return decisionTestResultMsg{
			ok:       report.OK,
			problems: report.Problems,
			summary:  fmt.Sprintf("decision model %s/%s reachable=%t authorized=%t latency=%dms", report.Kind, report.Model, report.Reachable, report.Authorized, report.LatencyMs),
		}
	}
}

// discoverDecisionModels lists decision models from the configured provider.
func discoverDecisionModels() tea.Cmd {
	cfg := config.Get()
	if cfg == nil {
		return util.ReportError(fmt.Errorf("config not loaded"))
	}
	m := cfg.DecisionModel
	return func() tea.Msg {
		p, err := modelrouter.ProviderWithTimeout(m.Router, m.EffectiveTimeout())
		if err != nil {
			return decisionDiscoverMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _, showAll := getDecisionDiscovery()
		list, status, err := p.ListDecisionModels(ctx, showAll)
		if err != nil {
			return decisionDiscoverMsg{err: err}
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
		return decisionDiscoverMsg{ids: ids, status: fmt.Sprint(status), suggestions: suggestions}
	}
}
