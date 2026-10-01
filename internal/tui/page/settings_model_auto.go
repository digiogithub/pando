package page

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/tui/components/settings"
)

const (
	modelAutoKeyPrefix  = "modelAutoMode."
	modelAutoNoneOption = "(none)"
)

func buildModelAutoModeSection(cfg *config.Config) settings.Section {
	m := cfg.ModelAutoMode

	fields := []settings.Field{
		{Label: "Enabled", Key: modelAutoKeyPrefix + "enabled", Type: settings.FieldToggle, Value: boolString(m.Enabled)},
		{Label: "Default to Auto", Key: modelAutoKeyPrefix + "defaultAuto", Type: settings.FieldToggle, Value: boolString(m.DefaultAuto), Hint: "New sessions start in Auto."},
	}
	fields = append(fields, decisionModelInfoRows(cfg, modelAutoKeyPrefix+"info.")...)
	fields = append(fields,
		settings.Field{Label: "Match Threshold", Key: modelAutoKeyPrefix + "threshold", Type: settings.FieldText, Value: strconv.FormatFloat(m.EffectiveThreshold(), 'f', -1, 64), Hint: "Minimum route probability (0-1, default 0.60)."},
		settings.Field{Label: "History Prompts", Key: modelAutoKeyPrefix + "historyPrompts", Type: settings.FieldText, Value: strconv.Itoa(m.HistoryPrompts), Hint: "Previous user prompts sent to the router as context."},
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
	var d config.DecisionModelConfig
	if c := config.Get(); c != nil {
		d = c.DecisionModel
	}
	if errs, _ := config.ValidateModelAutoMode(m, d); len(errs) > 0 {
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
	case sub == "threshold":
		v, err := parseFloatValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid threshold: %w", err)
		}
		m.Threshold = v
	case sub == "historyPrompts":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid value for %s: %w", field.Label, err)
		}
		m.HistoryPrompts = v
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
