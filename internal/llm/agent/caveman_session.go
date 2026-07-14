package agent

import (
	"context"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/caveman"
	"github.com/digiogithub/pando/internal/config"
)

// cavemanModes holds the per-session caveman output-brevity level, keyed by
// sessionID. It mirrors the per-session, ctx-threaded design of ponytailModes so
// concurrent sessions never clobber each other's mode.
//
// Presence semantics: an entry is present when the session has explicitly chosen
// a mode (including an explicit "off" that overrides a non-off configured
// default). When absent, the session inherits config.CavemanDefaultMode.
var cavemanModes sync.Map // map[string]caveman.Mode

// SetCavemanMode sets the caveman mode for a session. It is safe for concurrent
// use and is the single mutation point used by every surface that exposes the
// /caveman command (ACP, Web UI, TUI). Turning it off clears the override unless
// a non-off default is configured, in which case an explicit "off" is recorded so
// the session stays disabled despite the default.
func SetCavemanMode(sessionID string, mode caveman.Mode) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if mode.IsActive() {
		cavemanModes.Store(sessionID, mode)
		return
	}
	if configuredCavemanMode().IsActive() {
		cavemanModes.Store(sessionID, caveman.ModeOff)
		return
	}
	cavemanModes.Delete(sessionID)
}

// CavemanMode returns the effective caveman mode for a session: the session's
// explicit choice when set, otherwise the configured default (off when none).
func CavemanMode(sessionID string) caveman.Mode {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return caveman.ModeOff
	}
	if v, ok := cavemanModes.Load(sessionID); ok {
		if m, ok := v.(caveman.Mode); ok {
			return m
		}
	}
	return configuredCavemanMode()
}

// configuredCavemanMode reads the default caveman mode from active config.
func configuredCavemanMode() caveman.Mode {
	cfg := config.Get()
	if cfg == nil {
		return caveman.ModeOff
	}
	m, _ := caveman.ParseMode(cfg.CavemanDefaultMode())
	return m
}

// cavemanModeForContext resolves the caveman mode from the session id carried by
// the request context (same keys used by sessionLLMOverridesForContext).
func cavemanModeForContext(ctx context.Context) caveman.Mode {
	return CavemanMode(sessionIDFromContext(ctx))
}
