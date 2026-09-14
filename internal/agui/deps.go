package agui

import (
	"database/sql"
	"errors"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/llm/models"
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
	// Tools is the adapter-wide glob allow-list applied to every agent's tool
	// set (see config.AGUIConfig.Tools). Empty means no restriction.
	Tools []string
	// Mesnada gates mesnada_* delegation tools independent of Tools. Resolved
	// from config.AGUIConfig.Mesnada (a *bool) to its documented default of
	// true here, so every other consumer of this already-resolved Config can
	// treat it as a plain switch.
	Mesnada bool
	// Profiles holds every declared [AGUI.Profiles.<name>] entry, resolved
	// against the adapter-wide fields above exactly the way this Config is
	// resolved from config.AGUIConfig: every fallback (Tools, Mesnada,
	// Persona) is already applied, so the pool and the /info handler treat
	// each Profile's fields as already effective. Keyed by profile name
	// (the route segment a client's POST {path}/<name> resolves against).
	Profiles map[string]Profile
	// MessagesSnapshotMaxMessages caps how many of a thread's most recent
	// messages MESSAGES_SNAPSHOT carries (PANDO-US-0016). ConfigFromApp
	// always resolves this to defaultMessagesSnapshotMaxMessages; <= 0 means
	// no count limit (only MessagesSnapshotMaxBytes applies).
	MessagesSnapshotMaxMessages int
	// MessagesSnapshotMaxBytes caps the snapshot's total JSON-encoded size,
	// in addition to MessagesSnapshotMaxMessages. ConfigFromApp always
	// resolves this to defaultMessagesSnapshotMaxBytes; <= 0 means no byte
	// limit (only MessagesSnapshotMaxMessages applies).
	MessagesSnapshotMaxBytes int
}

// Profile is one resolved AG-UI profile: a Base built-in agent plus the
// overrides (persona, prompt, model, tool allow/deny lists, mesnada switch)
// that let one process serve several restricted assistants without running
// one process per profile. Resolved from config.AGUIProfile in
// ConfigFromApp, the same way Config is resolved from config.AGUIConfig.
type Profile struct {
	// Name is the profile's route name: the map key in
	// config.AGUIConfig.Profiles and in Config.Profiles.
	Name string
	// Base is the built-in agent this profile's model/token configuration is
	// inherited from.
	Base config.AgentName
	// Model overrides Base's configured model for runs served under this
	// profile. Empty means inherit Base's own configured model.
	Model models.ModelID
	// Persona overrides the adapter-wide Config.Persona. Already resolved:
	// empty here means neither the profile nor the adapter declared one, so
	// callers must not fall back to Config.Persona a second time.
	Persona string
	// Prompt is extra system-prompt text for runs served under this profile.
	// There is no adapter-wide equivalent to fall back to.
	Prompt string
	// Tools is this profile's resolved glob allow-list: the profile's own
	// declared list (even an explicit empty one) when it set one, otherwise
	// the adapter-wide Config.Tools. Already resolved: pass it straight to
	// filterAGUITools, the fallback has already been applied.
	Tools []string
	// DenyTools is this profile's glob deny-list. There is no adapter-wide
	// equivalent; nil/empty both mean no deny-list.
	DenyTools []string
	// Mesnada is this profile's resolved Mesnada switch: the profile's own
	// value when it set one, otherwise the adapter-wide Config.Mesnada.
	Mesnada bool
}

const (
	defaultPath         = "/api/v1/agui"
	defaultPoolSize     = 4
	defaultPoolTTL      = 30 * time.Minute
	defaultHeartbeat    = 15 * time.Second
	defaultAgentTimeout = 0 // no adapter-side cap; the client closing the stream cancels

	// defaultMessagesSnapshotMaxMessages and defaultMessagesSnapshotMaxBytes
	// bound MESSAGES_SNAPSHOT (PANDO-US-0016): a thread's history can grow
	// without limit, but the resync payload sent on the first run of a
	// pre-existing thread's attach must not.
	defaultMessagesSnapshotMaxMessages = 200
	defaultMessagesSnapshotMaxBytes    = 256 << 10 // 256 KiB
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
		Tools:          c.Tools,
		Mesnada:        true,
	}
	if c.Mesnada != nil {
		out.Mesnada = *c.Mesnada
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
	out.MessagesSnapshotMaxMessages = defaultMessagesSnapshotMaxMessages
	out.MessagesSnapshotMaxBytes = defaultMessagesSnapshotMaxBytes
	for _, name := range c.Agents {
		agentName := config.AgentName(name)
		if config.IsKnownAgent(agentName) {
			out.Agents = append(out.Agents, agentName)
		}
	}
	if len(out.Agents) == 0 {
		out.Agents = []config.AgentName{config.AgentCoder}
	}

	// Profiles absorb the adapter-wide Tools/Mesnada/Persona knobs as
	// per-profile fields with no migration (PANDO-US-0011's decision,
	// carried out here): a profile that declares none of its own inherits
	// out.Tools/out.Mesnada/out.Persona exactly as resolved above, and an
	// explicit profile value overrides it. See Profile's field docs for the
	// nil-vs-empty-Tools distinction this preserves.
	for name, p := range c.Profiles {
		if config.IsKnownAgent(config.AgentName(name)) {
			// config.Validate already refuses this at load time; skip
			// defensively in case ConfigFromApp is ever called on a config
			// that bypassed Validate (e.g. directly in a test), mirroring
			// the Agents filtering above.
			continue
		}
		if !config.IsKnownAgent(p.Base) {
			continue
		}
		resolved := Profile{
			Name:    name,
			Base:    p.Base,
			Model:   p.Model,
			Persona: p.Persona,
			Prompt:  p.Prompt,
			Tools:   out.Tools,
			Mesnada: out.Mesnada,
		}
		if resolved.Persona == "" {
			resolved.Persona = out.Persona
		}
		if p.Tools != nil {
			resolved.Tools = *p.Tools
		}
		if p.DenyTools != nil {
			resolved.DenyTools = *p.DenyTools
		}
		if p.Mesnada != nil {
			resolved.Mesnada = *p.Mesnada
		}
		if out.Profiles == nil {
			out.Profiles = make(map[string]Profile, len(c.Profiles))
		}
		out.Profiles[name] = resolved
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

// resolveProfile returns the named profile, if one was declared.
func (c Config) resolveProfile(name string) (Profile, bool) {
	p, ok := c.Profiles[name]
	return p, ok
}
