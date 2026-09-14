package agui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	// admission enforces Config.MaxConcurrentRuns (PANDO-US-0021). Never nil
	// on a Runtime built through New; a nil-receiver-safe zero value on one
	// built directly by a test.
	admission *runAdmission
	// draining is set once shutdown has begun (PANDO-US-0022): handleRun
	// stops admitting new runs, existing streams are left to finish. See
	// Runtime.Close and isDraining/StartDraining.
	draining atomic.Bool

	// baseCtx is the parent of every agent run. Runs are deliberately NOT bound
	// to the HTTP request: an interrupted run must stay alive between the
	// response that announced the interrupt and the request that resolves it.
	baseCtx context.Context
	cancel  context.CancelFunc
	once    sync.Once

	// startedAt is when this adapter instance came up, used to compute the
	// uptime reported by GET {path}/healthz (PANDO-US-0020).
	startedAt time.Time
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
		admission: newRunAdmission(cfg.MaxConcurrentRuns),
		baseCtx:   ctx,
		cancel:    cancel,
		startedAt: time.Now(),
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

// StartDraining stops the adapter from admitting new runs (PANDO-US-0022):
// handleRun starts answering every new-run POST with 503 + Retry-After,
// reusing the PANDO-US-0021 rejection path, while runs already in flight are
// left to keep streaming. It is idempotent and safe to call before Close --
// Close calls it itself, first thing, but a caller that wants the /healthz
// draining flag to flip before the rest of teardown begins (e.g. so a load
// balancer notices sooner) may call it directly.
func (r *Runtime) StartDraining() {
	r.draining.Store(true)
}

// isDraining reports whether the adapter has begun shutting down (see
// StartDraining), consulted by handleRun and reported in GET {path}/healthz.
func (r *Runtime) isDraining() bool {
	return r.draining.Load()
}

// Close begins draining (see StartDraining) so no new run is admitted, then
// gives every already-admitted, non-suspended run up to Config.ShutdownGrace
// to finish naturally -- letting the normal per-chunk message-store writes
// internal/llm/agent already performs while streaming catch up -- before
// falling back to the hard cancel this method always did for whatever is
// still running. A suspended run (parked on a permission prompt) is never
// waited on: nobody is going to answer a human-in-the-loop prompt inside a
// shutdown window, so it is checkpointed and released immediately regardless
// of grace -- its accumulated messages are already durably persisted by the
// same per-chunk writes, so releasing it early loses nothing that waiting
// would have preserved. Pooled agents are left to the garbage collector.
//
// ShutdownGrace <= 0 reproduces the pre-PANDO-US-0022 behaviour: every run is
// cancelled immediately, with no wait.
func (r *Runtime) Close() {
	r.once.Do(func() {
		r.StartDraining()

		var waiting, suspended []*activeRun
		for _, run := range r.runs.all() {
			if run.isSuspended() {
				suspended = append(suspended, run)
				continue
			}
			waiting = append(waiting, run)
		}
		for _, run := range suspended {
			// cancelAll first: a human-in-the-loop wait (hitl.go) selects on
			// the adapter's own base context, not the run's (see
			// pendingRegistry.cancelAll's doc comment), so cancelling the
			// run's context alone would not reach it -- exactly the same
			// two-step handleCancelRun already uses.
			r.pending.cancelAll(run.sessionID)
			r.finishRun(run)
		}

		grace := r.cfg.ShutdownGrace
		if grace > 0 && len(waiting) > 0 {
			logging.Info("agui: draining, waiting for in-flight runs to finish",
				"count", len(waiting), "grace", grace)
			deadline := time.NewTimer(grace)
		waitLoop:
			for _, run := range waiting {
				select {
				case <-run.done:
				case <-deadline.C:
					break waitLoop
				}
			}
			deadline.Stop()
		}

		var cut int
		for _, run := range waiting {
			select {
			case <-run.done:
			default:
				cut++
				r.finishRun(run)
			}
		}
		if cut > 0 {
			logging.Warn("agui: shutdown deadline reached, cancelling runs still in flight", "count", cut)
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
