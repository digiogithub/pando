package config

import (
	"fmt"
	"strings"
)

// PersonaAutoValue is the sentinel stored in [Persona] Active to record an
// explicit "Auto" choice (no manual persona; the auto-selector decides). It is
// distinct from an empty Active, which means "never chosen": the value is then
// inherited from the global config and finally defaults to DefaultPersona.
const PersonaAutoValue = "auto"

// DefaultPersona is the persona used when none has ever been chosen.
const DefaultPersona = "assistant"

// PersonaConfig holds the persisted active-persona choice.
type PersonaConfig struct {
	// Active is the persona name, PersonaAutoValue for explicit Auto, or ""
	// when unset. A project .pando.toml value overrides the global one.
	Active string `json:"active,omitempty" toml:"Active"`
}

// ActivePersonaChoice returns the persisted active persona from the merged
// configuration (project overrides global). auto is true for an explicit Auto
// choice (name is then empty); set is false when nothing was ever persisted.
func ActivePersonaChoice() (name string, auto bool, set bool) {
	if cfg == nil {
		return "", false, false
	}
	v := strings.TrimSpace(cfg.Persona.Active)
	switch {
	case v == "":
		return "", false, false
	case strings.EqualFold(v, PersonaAutoValue):
		return "", true, true
	default:
		return v, false, true
	}
}

// EffectiveActivePersona resolves the persona to activate at startup: the
// persisted choice (project > global), else DefaultPersona. An explicit Auto
// choice resolves to "" (no manual persona).
func EffectiveActivePersona() string {
	name, auto, set := ActivePersonaChoice()
	switch {
	case !set:
		return DefaultPersona
	case auto:
		return ""
	default:
		return name
	}
}

// UpdateActivePersona persists the active persona choice through the usual
// config write path (the project .pando.toml when present, else the global
// file). An empty name is stored as PersonaAutoValue so Auto survives restart.
func UpdateActivePersona(name string) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	value := strings.TrimSpace(name)
	if value == "" {
		value = PersonaAutoValue
	}
	old := cfg.Persona.Active
	cfg.Persona.Active = value
	if err := updateCfgFile(func(config *Config) {
		config.Persona.Active = value
	}); err != nil {
		cfg.Persona.Active = old
		return err
	}
	return nil
}
