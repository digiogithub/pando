package agent

import (
	"context"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/ponytail"
)

// ponytailModes holds the per-session ponytail intensity, keyed by sessionID. It
// mirrors the per-session, ctx-threaded design of sessionLLMOverrides so
// concurrent sessions never clobber each other's mode.
//
// Presence semantics: an entry is present when the session has explicitly chosen
// a mode (including an explicit "off" that overrides a non-off configured
// default). When absent, the session inherits the configured default mode
// (config.PonytailDefaultMode / PANDO_PONYTAIL_DEFAULT_MODE).
var ponytailModes sync.Map // map[string]ponytail.Mode

// SetPonytailMode sets the ponytail mode for a session. It is safe for concurrent
// use and is the single mutation point used by every surface that exposes the
// /ponytail command (ACP, Web UI, TUI). Turning it off clears the override unless
// a non-off default is configured, in which case an explicit "off" is recorded so
// the session stays disabled despite the default.
func SetPonytailMode(sessionID string, mode ponytail.Mode) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if mode.IsActive() {
		ponytailModes.Store(sessionID, mode)
		return
	}
	if configuredDefaultMode().IsActive() {
		ponytailModes.Store(sessionID, ponytail.ModeOff)
		return
	}
	ponytailModes.Delete(sessionID)
}

// PonytailMode returns the effective ponytail mode for a session: the session's
// explicit choice when set, otherwise the configured default (off when none).
func PonytailMode(sessionID string) ponytail.Mode {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ponytail.ModeOff
	}
	if v, ok := ponytailModes.Load(sessionID); ok {
		if m, ok := v.(ponytail.Mode); ok {
			return m
		}
	}
	return configuredDefaultMode()
}

// configuredDefaultMode reads the default ponytail mode from active config.
func configuredDefaultMode() ponytail.Mode {
	cfg := config.Get()
	if cfg == nil {
		return ponytail.ModeOff
	}
	m, _ := ponytail.ParseMode(cfg.PonytailDefaultMode())
	return m
}

// ponytailModeForContext resolves the ponytail mode from the session id carried
// by the request context (same keys used by sessionLLMOverridesForContext).
func ponytailModeForContext(ctx context.Context) ponytail.Mode {
	return PonytailMode(sessionIDFromContext(ctx))
}
