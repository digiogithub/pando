package page

import (
	"context"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/modelrouter"
)

// personaRouterHealthTimeout bounds the health probe behind the router line of
// the persona-selector agent.
const personaRouterHealthTimeout = 3 * time.Second

// personaRouterHealthMsg carries the result of a router health probe.
type personaRouterHealthMsg struct {
	key    string
	status string
}

var (
	personaHealthMu  sync.Mutex
	personaHealthKey string
	personaHealthVal string
)

// personaRouterKey identifies the router a health status belongs to. It never
// includes the API key.
func personaRouterHealthKey(m config.DecisionModelConfig) string {
	return string(m.Router.EffectiveProvider()) + "|" + m.Router.EffectiveBaseURL() + "|" + m.Router.Model
}

// personaRouterHealthStatus returns the last known status of the router in
// cfg, or "checking..." while no probe finished for it.
func personaRouterHealthStatus(m config.DecisionModelConfig) string {
	personaHealthMu.Lock()
	defer personaHealthMu.Unlock()
	if personaHealthKey == personaRouterHealthKey(m) && personaHealthVal != "" {
		return personaHealthVal
	}
	return "checking..."
}

func setPersonaRouterHealth(key, status string) {
	personaHealthMu.Lock()
	defer personaHealthMu.Unlock()
	personaHealthKey, personaHealthVal = key, status
}

// checkPersonaRouterHealth probes the shared decision model off the UI thread
// (cached health, short timeout). It returns nil when no model is configured.
func checkPersonaRouterHealth() tea.Cmd {
	cfg := config.Get()
	if cfg == nil || strings.TrimSpace(cfg.DecisionModel.Router.Model) == "" {
		return nil
	}
	m := cfg.DecisionModel
	return func() tea.Msg {
		key := personaRouterHealthKey(m)
		ctx, cancel := context.WithTimeout(context.Background(), personaRouterHealthTimeout)
		defer cancel()
		report, err := modelrouter.RouterHealth(ctx, m)
		switch {
		case err != nil:
			return personaRouterHealthMsg{key: key, status: "Unhealthy: " + err.Error()}
		case report.OK:
			return personaRouterHealthMsg{key: key, status: "Healthy"}
		case len(report.Problems) > 0:
			return personaRouterHealthMsg{key: key, status: "Unhealthy: " + report.Problems[0]}
		default:
			return personaRouterHealthMsg{key: key, status: "Unhealthy"}
		}
	}
}
