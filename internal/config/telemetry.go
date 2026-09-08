package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/telemetry"
)

// TelemetryConfig controls the opt-in remote logs/telemetry pipeline that
// ships Pando's logs to a Better Stack source so a user can share diagnostics
// with the maintainers. Off by default.
//
// Telemetry is a GLOBAL-only setting (see updateGlobalCfgFile): Load always
// pins it to the value found in the profile-level config file, never a
// project-local or overlay config, so opening an untrusted project can never
// silently turn on remote logging or swap the debug id.
type TelemetryConfig struct {
	// Enabled turns remote shipping on. Has no effect when the build carries
	// no Better Stack token (internal/telemetry.Available()).
	Enabled bool `json:"enabled" toml:"Enabled"`
	// DebugID is the anonymous 16-digit identifier attached to every shipped
	// record. Generated on first enable and kept across disable/enable so it
	// can be quoted in a support issue. Empty until first enabled.
	DebugID string `json:"debugId,omitempty" toml:"DebugID"`
	// MinLevel is the lowest slog level shipped: "debug", "info", "warn" or
	// "error". Defaults to "info". A "debug" record still requires cfg.Debug
	// to also be on, since that is what makes the process emit it at all.
	MinLevel string `json:"minLevel" toml:"MinLevel"`
}

// Telemetry levels accepted by Config.Telemetry.MinLevel, from most to least
// verbose.
const (
	TelemetryLevelDebug = "debug"
	TelemetryLevelInfo  = "info"
	TelemetryLevelWarn  = "warn"
	TelemetryLevelError = "error"
)

// normalizeTelemetryDefaults repairs and defaults cfg.Telemetry so a
// malformed telemetry.* value in the config file can never fail Load (see
// code review: Validate() used to hard-fail Load over a bad debug id or
// level, which meant a corrupted telemetry section — e.g. hand-edited,
// or a debug id pasted back in its dashed display form — stopped Pando
// from starting at all, with no easy way to fix a file the user cannot
// even get the app open to edit). Called from applyDefaultValues on every
// load, after Load has already pinned cfg.Telemetry to the global-only
// snapshot, so it normalizes exactly the value the global config file
// holds — never a project-local or overlay one.
//
//   - DebugID: dashes/spaces are stripped first (so the grouped display
//     form "1234-5678-9012-3456" a user might paste back into a config
//     file by hand still normalizes cleanly); if the result is still not
//     exactly 16 digits, it is cleared with a warning rather than
//     rejected — UpdateTelemetry regenerates one automatically the next
//     time telemetry is enabled, so losing a malformed one costs nothing.
//     If telemetry is Enabled with no valid id at all (cleared just now,
//     or simply never set despite Enabled=true), a fresh one is generated
//     in memory for this run so shipped records are never tagged with an
//     empty debug_id — it is not written back to the file here (simplest
//     safe behavior per review); it becomes durable the next time the
//     user toggles or regenerates telemetry from Settings.
//   - MinLevel: lowercased/trimmed; an unrecognized value is defaulted to
//     "info" with a warning instead of failing.
func normalizeTelemetryDefaults() {
	t := cfg.Telemetry

	if t.DebugID != "" {
		if cleaned := stripDebugIDFormatting(t.DebugID); telemetry.ValidDebugID(cleaned) {
			t.DebugID = cleaned
		} else {
			logging.Warn("Invalid telemetry debug id in config, clearing it — a new one will be generated on next enable",
				"value", t.DebugID)
			t.DebugID = ""
		}
	}

	t.MinLevel = strings.ToLower(strings.TrimSpace(t.MinLevel))
	switch t.MinLevel {
	case "", TelemetryLevelDebug, TelemetryLevelInfo, TelemetryLevelWarn, TelemetryLevelError:
	default:
		logging.Warn("Invalid telemetry minLevel in config, defaulting to info",
			"value", t.MinLevel)
		t.MinLevel = ""
	}
	if t.MinLevel == "" {
		t.MinLevel = TelemetryLevelInfo
	}

	if t.Enabled && t.DebugID == "" {
		if id, err := telemetry.NewDebugID(); err == nil {
			t.DebugID = id
			logging.Warn("Telemetry was enabled with no valid debug id; generated one in memory for this run (re-enable or regenerate in Settings to persist it)")
		}
	}

	cfg.Telemetry = t
}

// stripDebugIDFormatting removes dashes and spaces from id, so a value
// copy-pasted in its grouped display form ("1234-5678-9012-3456") still
// normalizes to the raw 16-digit storage form.
func stripDebugIDFormatting(id string) string {
	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		if r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// validateTelemetryConfig checks a candidate TelemetryConfig strictly,
// rejecting a malformed debug id or an unrecognized level rather than
// silently coercing it. Used ONLY by the interactive write paths
// (UpdateTelemetryMinLevel; UpdateTelemetry's generated id is always valid
// by construction) — where rejecting genuinely bad input is the right
// behavior — never by Load/Validate, which relies on
// normalizeTelemetryDefaults to self-heal instead (see its doc for why: a
// bad value in the file must never stop Pando from starting).
func validateTelemetryConfig(t TelemetryConfig) error {
	if t.DebugID != "" && !telemetry.ValidDebugID(t.DebugID) {
		return fmt.Errorf("telemetry.debugId must be empty or exactly 16 digits, got %q", t.DebugID)
	}
	switch t.MinLevel {
	case "", TelemetryLevelDebug, TelemetryLevelInfo, TelemetryLevelWarn, TelemetryLevelError:
	default:
		return fmt.Errorf("telemetry.minLevel %q is invalid: must be one of debug, info, warn, error", t.MinLevel)
	}
	return nil
}

// TelemetryDebugIDDisplay returns the current debug id grouped for display
// and copy-to-clipboard ("1234-5678-9012-3456"), or "" when none has been
// generated yet (telemetry was never enabled).
func TelemetryDebugIDDisplay() string {
	if cfg == nil {
		return ""
	}
	return telemetry.FormatDebugID(cfg.Telemetry.DebugID)
}

// UpdateTelemetry enables or disables remote telemetry and persists the
// change to the GLOBAL config file only (see updateGlobalCfgFile), regardless
// of whether a project-local config is active for the current working
// directory. When enabling for the first time (no DebugID yet), a new one is
// generated; re-enabling after a disable reuses the existing id. Returns the
// debug id in effect after the call (unchanged on a failed update, alongside
// the error).
func UpdateTelemetry(enabled bool) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config not loaded")
	}

	old := cfg.Telemetry
	next := old
	next.Enabled = enabled
	if enabled && next.DebugID == "" {
		id, err := telemetry.NewDebugID()
		if err != nil {
			return old.DebugID, fmt.Errorf("failed to generate telemetry debug id: %w", err)
		}
		next.DebugID = id
	}
	cfg.Telemetry = next

	if err := updateGlobalCfgFile(func(config *Config) {
		config.Telemetry = next
	}); err != nil {
		cfg.Telemetry = old
		return old.DebugID, err
	}

	// UpdateTelemetry only writes the GLOBAL config file, which the running
	// process's file watcher may not even be watching (it watches whichever
	// path ResolveConfigFilePath picked, local-preferring — see
	// cmd/root.go's WatchConfigFile call). Publish directly so an in-process
	// caller (TUI/WebUI settings) starts or stops the shipper live without
	// waiting on — or depending on — that watcher.
	Bus.Publish(ConfigChangeEvent{
		Section:     "telemetry",
		Timestamp:   time.Now(),
		Source:      "config",
		ChangedKeys: []string{"telemetry.enabled", "telemetry.debugId"},
	})

	return next.DebugID, nil
}

// RegenerateTelemetryID replaces the current debug id with a freshly
// generated one and persists it to the GLOBAL config file, independent of
// whether telemetry is currently enabled (a user may want a fresh id ready
// before re-enabling).
func RegenerateTelemetryID() (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config not loaded")
	}

	id, err := telemetry.NewDebugID()
	if err != nil {
		return "", fmt.Errorf("failed to generate telemetry debug id: %w", err)
	}

	old := cfg.Telemetry
	next := old
	next.DebugID = id
	cfg.Telemetry = next

	if err := updateGlobalCfgFile(func(config *Config) {
		config.Telemetry = next
	}); err != nil {
		cfg.Telemetry = old
		return old.DebugID, err
	}

	// See the matching comment in UpdateTelemetry: the global-only write
	// above is not guaranteed to be seen by the process's config file
	// watcher, so publish directly for in-process subscribers (e.g. the
	// telemetry runtime updating the shipper's live debug id).
	Bus.Publish(ConfigChangeEvent{
		Section:     "telemetry",
		Timestamp:   time.Now(),
		Source:      "config",
		ChangedKeys: []string{"telemetry.debugId"},
	})

	return id, nil
}

// UpdateTelemetryMinLevel changes the minimum level shipped and persists it
// to the GLOBAL config file. level is validated against the same allowed set
// as Validate() (case-insensitive; surrounding whitespace is trimmed).
func UpdateTelemetryMinLevel(level string) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	normalized := strings.ToLower(strings.TrimSpace(level))
	if normalized == "" {
		return fmt.Errorf("telemetry.minLevel must be one of debug, info, warn, error, got empty")
	}
	if err := validateTelemetryConfig(TelemetryConfig{MinLevel: normalized}); err != nil {
		return err
	}

	old := cfg.Telemetry
	next := old
	next.MinLevel = normalized
	cfg.Telemetry = next

	if err := updateGlobalCfgFile(func(config *Config) {
		config.Telemetry = next
	}); err != nil {
		cfg.Telemetry = old
		return err
	}

	// See the matching comment in UpdateTelemetry.
	Bus.Publish(ConfigChangeEvent{
		Section:     "telemetry",
		Timestamp:   time.Now(),
		Source:      "config",
		ChangedKeys: []string{"telemetry.minLevel"},
	})

	return nil
}
