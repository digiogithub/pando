package agui

import (
	"database/sql"
	"errors"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/mcpgateway"
	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/skills"
)

// Deps are the collaborators the adapter needs to build its own agents. It
// mirrors the arguments internal/app/app.go already passes to
// agent.CoderAgentToolsWithMesnada, minus the two services the adapter creates
// itself (permissions and user input, see invariant I3 in doc.go) and minus the
// agent itself (invariant I2).
//
// The struct exists so this package never imports *app.App: that would be an
// import cycle and would also make the adapter reachable from the rest of the
// application, which is exactly what the isolation invariants forbid.
type Deps struct {
	Sessions     session.Service
	Messages     message.Service
	History      history.Service
	Skills       *skills.SkillManager
	Gateway      *mcpgateway.Gateway
	Orchestrator *orchestrator.Orchestrator
	Remembrances *rag.RemembrancesService
	// LSP is the language-server provider used by the edit/view tools.
	// *app.App satisfies it.
	LSP tools.LSPProvider
	// DB is the shared SQLite connection, used only for the adapter's own
	// agui_threads table (thread -> session bindings). It is optional: with a nil
	// DB the mapping is in-memory and does not survive a restart.
	DB *sql.DB
	// Token is the API bearer token clients must present when
	// Config.RequireToken is set. It is supplied by the caller so the adapter
	// shares the API server's token instead of minting a second one.
	Token string
}

func (d Deps) validate() error {
	if d.Sessions == nil {
		return errors.New("agui: Deps.Sessions is required")
	}
	if d.Messages == nil {
		return errors.New("agui: Deps.Messages is required")
	}
	return nil
}

// Config is the adapter's resolved configuration.
type Config struct {
	Path string
	// Port serves the adapter on its own listener when > 0. Zero means the
	// adapter is mounted on the API server's mux.
	Port           int
	Agents         []config.AgentName
	AllowedOrigins []string
	RequireToken   bool
	FrontendTools  bool
	AgentPoolSize  int
	AgentPoolTTL   time.Duration
	AutoApprove    bool
	// HumanInTheLoop surfaces permission prompts and questions to the AG-UI
	// client. With it off (and AutoApprove off) a run that needs approval is
	// denied instead of asking, which is the pre-P4 behaviour.
	HumanInTheLoop bool
	// Persona names a persona injected into the system prompt of every run, via
	// a per-session persona override. Empty means no override: the
	// process-wide active persona (or auto-selection) applies as usual.
	Persona string
}

const (
	defaultPath         = "/api/v1/agui"
	defaultPoolSize     = 4
	defaultPoolTTL      = 30 * time.Minute
	defaultHeartbeat    = 15 * time.Second
	defaultAgentTimeout = 0 // no adapter-side cap; the client closing the stream cancels
)

// ConfigFromApp resolves an AGUIConfig into the adapter's own Config, applying
// the defaults the adapter guarantees even when the config file predates this
// feature.
func ConfigFromApp(c config.AGUIConfig) Config {
	out := Config{
		Path:           c.Path,
		Port:           c.Port,
		AllowedOrigins: c.AllowedOrigins,
		RequireToken:   c.RequireToken,
		FrontendTools:  c.FrontendTools,
		AgentPoolSize:  c.AgentPoolSize,
		AutoApprove:    c.AutoApprove,
		HumanInTheLoop: c.HumanInTheLoop,
		Persona:        c.Persona,
	}
	if out.Path == "" {
		out.Path = defaultPath
	}
	if out.AgentPoolSize <= 0 {
		out.AgentPoolSize = defaultPoolSize
	}
	out.AgentPoolTTL = defaultPoolTTL
	if c.AgentPoolTTL != "" {
		if d, err := time.ParseDuration(c.AgentPoolTTL); err == nil && d > 0 {
			out.AgentPoolTTL = d
		}
	}
	for _, name := range c.Agents {
		agentName := config.AgentName(name)
		if config.IsKnownAgent(agentName) {
			out.Agents = append(out.Agents, agentName)
		}
	}
	if len(out.Agents) == 0 {
		out.Agents = []config.AgentName{config.AgentCoder}
	}
	return out
}

// allowsAgent reports whether the named agent may be driven over AG-UI.
func (c Config) allowsAgent(name config.AgentName) bool {
	for _, a := range c.Agents {
		if a == name {
			return true
		}
	}
	return false
}
