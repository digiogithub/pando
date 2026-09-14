package agui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/userinput"
)

// Runtime is the AG-UI adapter. It owns everything the protocol needs that
// Pando does not already provide: its own agent instances, its own permission
// and user-input services, and the thread bookkeeping AG-UI clients expect.
//
// Nothing outside this package holds a reference to those objects, which is
// what keeps the adapter isolated from the TUI, Web-UI and ACP surfaces.
type Runtime struct {
	deps Deps
	cfg  Config

	// perms and userInput are created here, never injected: an approval raised
	// by a browser run must not reach a desktop user (invariant I3).
	perms     permission.Service
	userInput userinput.Service

	pool    *agentPool
	threads *threadStore
	// states holds each thread's shared-state document across its turns.
	states *stateStore
	// pending and runs exist because a frontend-tool handoff outlives the request
	// that reported it (see run.go).
	pending *pendingRegistry
	runs    *runStore

	// baseCtx is the parent of every agent run. Runs are deliberately NOT bound
	// to the HTTP request: an interrupted run must stay alive between the
	// response that announced the interrupt and the request that resolves it.
	baseCtx context.Context
	cancel  context.CancelFunc
	once    sync.Once
}

// New builds the adapter. It does not start any listener; call Register to
// mount it on a mux.
func New(deps Deps, cfg Config) (*Runtime, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	if err := validatePersona(cfg.Persona); err != nil {
		return nil, err
	}
	for name, profile := range cfg.Profiles {
		if err := validatePersona(profile.Persona); err != nil {
			return nil, fmt.Errorf("agui profile %q: %w", name, err)
		}
	}
	perms := permission.NewPermissionService()
	ui := userinput.NewService()
	pending := newPendingRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{
		deps:      deps,
		cfg:       cfg,
		perms:     perms,
		userInput: ui,
		pool:      newAgentPool(deps, cfg, perms, ui, pending),
		threads:   newThreadStore(deps.DB),
		states:    newStateStore(),
		pending:   pending,
		runs:      newRunStore(),
		baseCtx:   ctx,
		cancel:    cancel,
	}
	go r.watchQuestions(ctx)

	logging.Info("AG-UI adapter ready",
		"path", cfg.Path,
		"agents", cfg.Agents,
		"profiles", len(cfg.Profiles),
		"requireToken", cfg.RequireToken,
		"allowedOrigins", cfg.AllowedOrigins,
		"autoApprove", cfg.AutoApprove,
		"humanInTheLoop", cfg.HumanInTheLoop,
		"persona", cfg.Persona,
	)
	return r, nil
}

// Close stops the adapter's background work and cancels every detached run.
// Pooled agents are left to the garbage collector.
func (r *Runtime) Close() {
	r.once.Do(func() {
		for _, run := range r.runs.all() {
			r.finishRun(run)
		}
		r.cancel()
		if r.pool.busy() {
			logging.Warn("AG-UI adapter closing with runs still in flight")
		}
	})
}

// resolveAgent maps the agent name/profile from the request path to a
// configured Base agent, plus the declared profile that named it, if any.
//
// A declared profile always takes precedence over a same-named built-in
// agent, though config validation already refuses a profile name that
// collides with a KnownAgentNames entry (PANDO-US-0012), so in practice the
// two namespaces never actually overlap. The 404 for an undeclared name is
// unchanged from before profiles existed.
func (r *Runtime) resolveAgent(name string) (config.AgentName, *Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return r.cfg.Agents[0], nil, nil
	}
	if profile, ok := r.cfg.resolveProfile(name); ok {
		return profile.Base, &profile, nil
	}
	agentName := config.AgentName(name)
	if !config.IsKnownAgent(agentName) {
		return "", nil, fmt.Errorf("unknown agent %q", name)
	}
	if !r.cfg.allowsAgent(agentName) {
		return "", nil, fmt.Errorf("agent %q is not exposed over AG-UI", name)
	}
	return agentName, nil, nil
}

// sessionForThread resolves the Pando session backing an AG-UI thread, creating
// it on first contact. profile is the declared profile serving this request,
// if any (nil for a plain built-in agent name) -- see resolveAgent.
//
// Since P5 the mapping survives a restart (see threads.go). A binding whose
// session has since been deleted is dropped rather than returned: the run would
// otherwise fail on every message of a thread the browser still considers open.
//
// The second return value, existed, reports whether the session was already
// there before this call (a known thread binding, or a client reusing an
// existing Pando session id as its thread id) as opposed to being created by
// this very call. PANDO-US-0016's MESSAGES_SNAPSHOT resync is gated on it: a
// thread created by this call has no history to resync yet.
func (r *Runtime) sessionForThread(ctx context.Context, threadID, agentName, title string, profile *Profile) (sessionID string, existed bool, err error) {
	if boundID, ok := r.threads.get(ctx, threadID); ok {
		if sess, err := r.deps.Sessions.Get(ctx, boundID); err == nil && sess.ID != "" {
			// The handler is per session and the mapping outlives the process, so
			// it must be (re)installed here, not only where the session is created.
			r.installPermissionPolicy(boundID)
			r.applySessionOverrides(boundID, profile)
			return boundID, true, nil
		}
		logging.Warn("agui: thread pointed at a missing session, rebinding",
			"thread", threadID, "session", boundID)
		r.threads.forget(ctx, threadID)
	}
	// A client may reuse a Pando session id as its thread id; honour it.
	if sess, err := r.deps.Sessions.Get(ctx, threadID); err == nil && sess.ID != "" {
		r.threads.put(ctx, threadID, sess.ID, agentName)
		r.installPermissionPolicy(sess.ID)
		r.applySessionOverrides(sess.ID, profile)
		return sess.ID, true, nil
	}

	sess, err := r.deps.Sessions.Create(ctx, sessionTitle(title))
	if err != nil {
		return "", false, fmt.Errorf("agui: create session for thread %q: %w", threadID, err)
	}
	r.threads.put(ctx, threadID, sess.ID, agentName)
	r.installPermissionPolicy(sess.ID)
	r.applySessionOverrides(sess.ID, profile)
	logging.Debug("agui: thread bound to session", "thread", threadID, "session", sess.ID)
	return sess.ID, false, nil
}

// applySessionOverrides scopes a profile's Persona/Prompt/Model -- or, absent
// a profile, just the adapter-wide Persona -- to one session through the same
// per-session override the ACP server uses (agent.SetSessionLLMOverrides), so
// every run of the thread resolves them into its system prompt/provider
// without touching the process-wide active persona (the TUI/desktop sharing
// this process keep theirs) or any other session's overrides. Because the
// overrides are per session and a thread is bound to one session, two threads
// on two profiles in one process never see each other's values
// (PANDO-US-0014). Merging keeps any other override field (reasoning effort,
// thinking mode) already installed for the session.
//
// profile's fields are already fully resolved against the adapter-wide
// fallback (see ConfigFromApp / Profile's field docs): profile.Persona is
// only empty here when neither the profile nor [AGUI] Persona declared one,
// so there is no second fallback to apply.
func (r *Runtime) applySessionOverrides(sessionID string, profile *Profile) {
	persona, prompt, model := r.cfg.Persona, "", models.ModelID("")
	if profile != nil {
		persona, prompt, model = profile.Persona, profile.Prompt, profile.Model
	}
	if persona == "" && prompt == "" && model == "" {
		return
	}

	ov := agent.SessionLLMOverridesFor(sessionID)
	if persona != "" || prompt != "" {
		ov.Persona = persona
		ov.PersonaScoped = true
		ov.Prompt = prompt
	}
	if model != "" {
		ov.Model = model
	}
	agent.SetSessionLLMOverrides(sessionID, ov)
}

// validatePersona fails fast rather than serving every run without the persona
// the deployment declared. Built-ins are always loaded, so the manager is
// expected to exist; a nil manager means personas are unavailable.
func validatePersona(name string) error {
	if name == "" {
		return nil
	}
	if mgr := agent.GetPersonaManager(); mgr == nil || !mgr.HasPersona(name) {
		return fmt.Errorf("agui: persona %q not found", name)
	}
	return nil
}

// sessionTitle builds a recognizable title so AG-UI threads are distinguishable
// from TUI/Web-UI sessions in the session list.
func sessionTitle(firstPrompt string) string {
	const maxLen = 60
	prompt := strings.TrimSpace(strings.ReplaceAll(firstPrompt, "\n", " "))
	if prompt == "" {
		return "agui: new thread"
	}
	if len(prompt) > maxLen {
		prompt = prompt[:maxLen] + "…"
	}
	return "agui: " + prompt
}
