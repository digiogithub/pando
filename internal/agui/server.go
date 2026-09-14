package agui

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/version"
)

// Register mounts the adapter's routes on mux:
//
//	GET    {path}/healthz               unauthenticated liveness probe (PANDO-US-0020)
//	POST   {path}/{agent}               run an agent, streaming AG-UI events over SSE
//	GET    {path}/info                  agent discovery, consumed by CopilotKit's runtime
//	GET    {path}/threads               list this adapter's threads, paginated newest-first
//	GET    {path}/threads/{id}/messages read a thread's transcript, AG-UI Message[] shaped
//	GET    {path}/threads/{id}/stream   reattach to a thread's live run (PANDO-US-0018)
//	DELETE {path}/threads/{id}          delete a thread: its messages, session and binding
//	POST   {path}/runs/{id}/cancel      cancel a thread's live or parked run (PANDO-US-0019)
//
// It is the only place this package touches the outside world's routing, and it
// is called only when the feature is enabled. The thread routes (PANDO-US-0015)
// let a browser client rebuild a conversation without co-mounting the Web-UI
// REST API: they work on the dedicated `agui-serve` listener, which carries no
// REST API by design (see cmd/agui_serve.go).
//
// /healthz is the one route that does not go through authorize(): it is
// registered directly against handleHealthz, never wrapped by the bearer-token
// check every other handler below performs on its own first line. That is a
// deliberate, narrow exception -- see handleHealthz's doc comment for what it
// is and is not allowed to expose -- and nothing else may bypass authorize()
// this way.
func (r *Runtime) Register(mux *http.ServeMux) {
	path := strings.TrimSuffix(r.cfg.Path, "/")
	mux.HandleFunc("GET "+path+"/healthz", r.handleHealthz)
	mux.HandleFunc("GET "+path+"/info", r.handleInfo)
	mux.HandleFunc("OPTIONS "+path+"/", r.handlePreflight)
	mux.HandleFunc("GET "+path+"/threads", r.handleListThreads)
	mux.HandleFunc("GET "+path+"/threads/{id}/messages", r.handleThreadMessages)
	mux.HandleFunc("GET "+path+"/threads/{id}/stream", r.handleStream)
	mux.HandleFunc("DELETE "+path+"/threads/{id}", r.handleDeleteThread)
	mux.HandleFunc("POST "+path+"/runs/{id}/cancel", r.handleCancelRun)
	mux.HandleFunc("POST "+path+"/{agent}", r.handleRun)
	mux.HandleFunc("POST "+path, r.handleRun)
	logging.Info("AG-UI routes registered", "path", path)
}

// Handler returns a standalone mux carrying only the AG-UI routes, for serving
// the adapter on its own listener (Config.Port > 0).
func (r *Runtime) Handler() http.Handler {
	mux := http.NewServeMux()
	r.Register(mux)
	return mux
}

// ---------------------------------------------------------------- middleware

// authorize enforces the bearer token and the CORS allow-list. It writes the
// error response itself and reports whether the request may proceed.
func (r *Runtime) authorize(w http.ResponseWriter, req *http.Request) bool {
	origin := req.Header.Get("Origin")
	if origin != "" && !r.originAllowed(origin) {
		logging.Warn("agui: rejected cross-origin request", "origin", origin)
		writeJSONError(w, http.StatusForbidden, "origin not allowed")
		return false
	}
	r.setCORSHeaders(w, origin)

	if !r.cfg.RequireToken {
		return true
	}
	if r.deps.Token == "" {
		// Fail closed: a required token that was never provisioned must not
		// degrade into an open endpoint.
		writeJSONError(w, http.StatusInternalServerError, "agui: no API token configured")
		return false
	}
	supplied := bearerToken(req)
	if subtle.ConstantTimeCompare([]byte(supplied), []byte(r.deps.Token)) != 1 {
		writeJSONError(w, http.StatusUnauthorized, "invalid or missing token")
		return false
	}
	return true
}

func bearerToken(req *http.Request) string {
	auth := req.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return strings.TrimSpace(req.URL.Query().Get("token"))
}

// originAllowed reports whether a browser origin may talk to the adapter.
// An empty allow-list means no cross-origin access at all: unlike the Web-UI
// chat stream, this endpoint never answers `Access-Control-Allow-Origin: *`
// implicitly, because it drives an agent that executes code.
func (r *Runtime) originAllowed(origin string) bool {
	for _, allowed := range r.cfg.AllowedOrigins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	return false
}

func (r *Runtime) setCORSHeaders(w http.ResponseWriter, origin string) {
	if origin == "" || !r.originAllowed(origin) {
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Vary", "Origin")
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	h.Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
}

func (r *Runtime) handlePreflight(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")
	if origin == "" || !r.originAllowed(origin) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	r.setCORSHeaders(w, origin)
	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------------------ handlers

// HealthResponse is the GET {path}/healthz payload (PANDO-US-0020). It is
// deliberately minimal and carries nothing an unauthenticated caller
// shouldn't see: no agent/profile names, no configured path, no allowed
// origins, no token or anything token-derived, and no session or thread
// identifier. ActiveRuns/MaxConcurrentRuns (PANDO-US-0021) and Draining
// (PANDO-US-0022) are the one aggregate signal about user activity this
// payload carries -- a concurrency gauge and a drain flag, never a count or
// list that could identify a particular user or conversation.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	// UptimeSeconds is how long this adapter instance has been running.
	UptimeSeconds float64 `json:"uptimeSeconds"`
	// ActiveRuns is the number of runs currently holding an admission slot
	// (PANDO-US-0021): running, or suspended waiting on a client -- a parked
	// run still occupies its slot, so it is included here too.
	ActiveRuns int `json:"activeRuns"`
	// MaxConcurrentRuns is the configured cap (Config.MaxConcurrentRuns); 0
	// means unlimited, matching the config's own default semantics.
	MaxConcurrentRuns int `json:"maxConcurrentRuns"`
	// Draining reports whether the adapter is shutting down and no longer
	// admitting new runs (PANDO-US-0022), so a load balancer can take this
	// instance out of rotation.
	Draining bool `json:"draining"`
}

// handleHealthz answers GET {path}/healthz: an unauthenticated liveness probe
// for a load balancer, systemd unit or container orchestrator (PANDO-US-0020).
// It is the only route Register mounts outside authorize() -- see Register's
// doc comment -- and it is never routed through setCORSHeaders/the Origin
// allow-list either: an absent Origin (what every such probe sends) already
// bypasses that check in authorize(), and the payload here contains nothing
// sensitive, so a browser hitting it directly is harmless.
func (r *Runtime) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	current, max := r.admission.snapshot()
	resp := HealthResponse{
		Status:            "ok",
		Version:           version.Normalize(),
		UptimeSeconds:     time.Since(r.startedAt).Seconds(),
		ActiveRuns:        current,
		MaxConcurrentRuns: max,
		Draining:          r.isDraining(),
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logging.Debug("agui: encode healthz response", "error", err)
	}
}

// AgentDescriptor is one entry of the /info response.
type AgentDescriptor struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	URL         string           `json:"url"`
	Model       *ModelDescriptor `json:"model,omitempty"`
}

// ModelDescriptor tells the client what it is talking to, so a dashboard can
// show the model and size its own context budget. It carries no credentials and
// no provider endpoint: the provider is named, never located.
type ModelDescriptor struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Provider      string `json:"provider,omitempty"`
	ContextWindow int64  `json:"contextWindow,omitempty"`
}

// Capabilities advertises the optional halves of the protocol this adapter
// implements, so a client can tell "not supported" from "nothing happened"
// without probing.
type Capabilities struct {
	// FrontendTools reports whether RunAgentInput.tools are proxied.
	FrontendTools bool `json:"frontendTools"`
	// HumanInTheLoop reports whether permission prompts and agent questions
	// reach the client as tool calls.
	HumanInTheLoop bool `json:"humanInTheLoop"`
	// SharedState reports STATE_SNAPSHOT / STATE_DELTA support.
	SharedState bool `json:"sharedState"`
	// Interrupts reports RUN_FINISHED{outcome:"interrupt"} and resumption.
	Interrupts bool `json:"interrupts"`
}

// InfoResponse is the agent-discovery payload.
type InfoResponse struct {
	Protocol     string            `json:"protocol"`
	Version      string            `json:"version,omitempty"`
	Path         string            `json:"path"`
	Agents       []AgentDescriptor `json:"agents"`
	Capabilities Capabilities      `json:"capabilities"`
}

func (r *Runtime) handleInfo(w http.ResponseWriter, req *http.Request) {
	if !r.authorize(w, req) {
		return
	}
	path := strings.TrimSuffix(r.cfg.Path, "/")
	base := requestBaseURL(req)

	resp := InfoResponse{
		Protocol: "ag-ui",
		Version:  version.Normalize(),
		Path:     path,
		Capabilities: Capabilities{
			FrontendTools:  r.cfg.FrontendTools,
			HumanInTheLoop: r.cfg.HumanInTheLoop,
			SharedState:    true,
			Interrupts:     true,
		},
	}
	for _, name := range r.cfg.Agents {
		resp.Agents = append(resp.Agents, AgentDescriptor{
			Name:        string(name),
			Description: "Pando " + string(name) + " agent",
			URL:         base + path + "/" + string(name),
			Model:       modelDescriptor(name),
		})
	}

	// Declared profiles are listed alongside the built-in agents (PANDO-US-0013),
	// sorted by name for a deterministic response. Neither loop instantiates an
	// agent: modelDescriptor/profileModelDescriptor resolve the model from
	// config only, so /info stays cheap enough to poll and never warms the
	// pool for an agent or profile nobody has run yet.
	profileNames := make([]string, 0, len(r.cfg.Profiles))
	for name := range r.cfg.Profiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)
	for _, name := range profileNames {
		profile := r.cfg.Profiles[name]
		resp.Agents = append(resp.Agents, AgentDescriptor{
			Name:        name,
			Description: "Pando profile " + name + " (base " + string(profile.Base) + ")",
			URL:         base + path + "/" + name,
			Model:       profileModelDescriptor(profile),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	// Discovery reflects live configuration (models and agents can change at
	// runtime), and it is served to a browser that may hold a stale token.
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logging.Debug("agui: encode info response", "error", err)
	}
}

// modelDescriptor reports the model an agent is currently configured with,
// without instantiating it: /info must stay cheap enough to be polled and must
// not warm the agent pool for an agent nobody has run yet.
func modelDescriptor(name config.AgentName) *ModelDescriptor {
	cfg := config.Get()
	if cfg == nil {
		return nil
	}
	agentCfg, ok := cfg.Agents[name]
	if !ok || agentCfg.Model == "" {
		return nil
	}
	out := &ModelDescriptor{ID: string(agentCfg.Model)}
	if model, ok := models.SupportedModels()[agentCfg.Model]; ok {
		out.Name = model.Name
		out.Provider = string(model.Provider)
		out.ContextWindow = model.ContextWindow
	}
	if agentCfg.ContextWindowOverride > 0 {
		out.ContextWindow = agentCfg.ContextWindowOverride
	}
	return out
}

// profileModelDescriptor reports the model a profile's runs are actually
// configured to use: the profile's own Model override when it set one
// (PANDO-US-0014), otherwise its Base agent's configured model -- the same
// fallback the session-override wiring applies at run time (see
// Runtime.applySessionOverrides). Like modelDescriptor, it never instantiates
// an agent or warms the pool.
func profileModelDescriptor(profile Profile) *ModelDescriptor {
	if profile.Model == "" {
		return modelDescriptor(profile.Base)
	}
	cfg := config.Get()
	if cfg == nil {
		return nil
	}
	out := &ModelDescriptor{ID: string(profile.Model)}
	if model, ok := models.SupportedModels()[profile.Model]; ok {
		out.Name = model.Name
		out.Provider = string(model.Provider)
		out.ContextWindow = model.ContextWindow
	}
	if agentCfg, ok := cfg.Agents[profile.Base]; ok && agentCfg.ContextWindowOverride > 0 {
		out.ContextWindow = agentCfg.ContextWindowOverride
	}
	return out
}

// requestBaseURL reconstructs the origin the client reached the adapter on, so
// the URLs in /info are usable as-is by CopilotKit's HttpAgent.
//
// Forwarded headers are honoured only for the scheme and only when the request
// did not already arrive over TLS; the host is always the one the client asked
// for. An attacker-controlled X-Forwarded-Host would otherwise let /info hand
// out URLs pointing at somebody else's server.
func requestBaseURL(req *http.Request) string {
	scheme := "http"
	switch {
	case req.TLS != nil:
		scheme = "https"
	case strings.EqualFold(req.Header.Get("X-Forwarded-Proto"), "https"):
		scheme = "https"
	}
	if req.Host == "" {
		return ""
	}
	return scheme + "://" + req.Host
}

// handleRun is the protocol's single execution endpoint: it accepts a
// RunAgentInput and streams the run back as AG-UI events.
//
// A request is one of four things, told apart by content and by whether the
// thread already has a live run: a new turn (no live run, a trailing user
// message), a resumption (a live run, trailing tool messages that resolve
// calls it is blocked on), a reattach (a live run, no new user message and
// nothing to resolve -- PANDO-US-0018, the same handling GET
// {path}/threads/{id}/stream gives), or the loser of a race (a live run, a
// new user message the run has no room for -- rejected with a single
// documented error, never by silently abandoning the run in progress).
func (r *Runtime) handleRun(w http.ResponseWriter, req *http.Request) {
	if !r.authorize(w, req) {
		return
	}

	routeName := strings.TrimSpace(req.PathValue("agent"))
	agentName, profile, err := r.resolveAgent(routeName)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	// The pool is keyed by route (a profile name, or the base agent name
	// when no profile is involved -- PANDO-US-0013), not by Base agent name:
	// two profiles sharing one Base must never collapse onto one instance.
	routeKey := routeName
	if routeKey == "" {
		routeKey = string(agentName)
	}

	in, err := DecodeRunAgentInput(req.Body, defaultMaxRequestBytes)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// The per-thread lock closes the PANDO-US-0018 TOCTOU: without it, two
	// POSTs can both observe no live run for the thread and both race into
	// svc.Run, and the loser surfaces as agent.ErrSessionBusy deep inside the
	// agent service instead of as a clean, documented rejection here. It is
	// held only through the decide-and-register section below, never across
	// the streaming that follows, so it never serializes unrelated threads
	// (or even a resumption/reattach/reject on this same thread) against
	// each other.
	unlock := r.runs.lockThread(in.ThreadID)
	run, hasRun := r.runs.get(in.ThreadID)
	if hasRun {
		unlock()
		r.handleExistingThreadRun(w, req, run, in)
		return
	}

	userMsg, ok := in.LastUserMessage()
	if !ok {
		unlock()
		// Nothing to resolve and no live run to reattach to.
		writeJSONError(w, http.StatusNotFound, "no live run for this thread")
		return
	}
	prompt := userMsg.Content.String()
	if strings.TrimSpace(prompt) == "" {
		unlock()
		writeJSONError(w, http.StatusBadRequest, "the trailing user message has no text content")
		return
	}
	if ctxBlock := in.ContextBlock(); ctxBlock != "" {
		prompt = ctxBlock + "\n\n" + prompt
	}

	// Draining (PANDO-US-0022) and the concurrency cap (PANDO-US-0021) are
	// both checked here, before anything is created for this request: no
	// session, no agent instance, no thread binding. Checked in this order
	// (rather than folded into one condition) only so the two rejection log
	// lines stay distinct; both answer through the same rejectOverCapacity
	// helper. A resumption or reattach never reaches this point -- it took
	// the hasRun branch above and is re-attaching to a run that already
	// holds its slot.
	if r.isDraining() {
		unlock()
		logging.Info("agui: run rejected, adapter draining", "thread", in.ThreadID)
		r.rejectOverCapacity(w, "the server is shutting down; retry against another instance")
		return
	}
	if !r.admission.tryAdmit() {
		unlock()
		current, max := r.admission.snapshot()
		logging.Warn("agui: run rejected, concurrency cap reached",
			"thread", in.ThreadID, "current", current, "max", max)
		r.rejectOverCapacity(w, "too many concurrent runs, try again later")
		return
	}

	sessionID, existed, err := r.sessionForThread(req.Context(), in.ThreadID, routeKey, prompt, profile)
	if err != nil {
		unlock()
		r.admission.release()
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	svc, err := r.pool.get(routeKey, agentName, profile, in.Tools)
	if err != nil {
		unlock()
		r.admission.release()
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Everything below this point streams, so failures are reported as RUN_ERROR
	// events rather than HTTP status codes.
	sse, err := NewSSEWriter(w)
	if err != nil {
		unlock()
		r.admission.release()
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer sse.Close()

	// The document belongs to the thread, not to this run: files, todos and
	// sub-agents accumulate over the conversation instead of resetting each turn.
	// attached reports whether this call built a brand-new document for the
	// thread -- combined with existed, that is "the first run this process has
	// served for a pre-existing thread's attach" (PANDO-US-0016).
	state, attached := r.states.get(in.ThreadID, sessionID, agentName, svc.Model(), in.State)
	t := newTranslator(in.ThreadID, in.RunID).withState(state)
	if err := r.runPrelude(req.Context(), sse, t, state, sessionID, existed && attached); err != nil {
		unlock()
		r.admission.release()
		return
	}

	if len(in.Tools) > 0 && !r.cfg.FrontendTools {
		// Declared tools would otherwise be silently dropped, leaving the page
		// waiting for a call that can never come.
		_ = sse.Write(NewCustom("pando.frontendToolsDisabled", map[string]any{
			"count":  len(in.Tools),
			"reason": "frontend tool proxying is disabled by configuration",
		}))
	}

	// The run is parented to the adapter, not to this request: a frontend tool
	// call, or a disconnect, must survive the response that reports it.
	runCtx, cancel := context.WithCancel(r.baseCtx)
	suspend := r.pending.watch(sessionID)

	events, err := svc.Run(runCtx, sessionID, prompt)
	if err != nil {
		cancel()
		unlock()
		r.admission.release()
		_ = sse.WriteAll(t.Fail(err.Error(), runErrorCode(err)))
		return
	}

	run = newActiveRun(in.ThreadID, sessionID, events, cancel, state, suspend, t)
	if !r.runs.put(run) {
		// Cannot happen while holding the per-thread lock; guarded defensively
		// so a future bug here fails loudly instead of leaking the run.
		logging.Warn("agui: run store rejected a newly created run", "thread", in.ThreadID)
		cancel()
		unlock()
		r.admission.release()
		_ = sse.WriteAll(t.Fail("internal error starting the run", ""))
		return
	}
	unlock()
	// From here on the run is registered and its admission slot is released
	// exactly once, by finishRun, when the run truly ends -- see
	// runAdmission's doc comment for why a suspension must not release it.

	go r.pump(run)
	r.attachRun(req.Context(), sse, run, true)
}

// handleExistingThreadRun decides what a request means for a thread that
// already has a live run: a resumption, a reattach, or the loser of a race
// against the run already in progress (PANDO-US-0018).
func (r *Runtime) handleExistingThreadRun(w http.ResponseWriter, req *http.Request, run *activeRun, in *RunAgentInput) {
	if candidates := r.resumeCandidates(run, in); len(candidates) > 0 {
		r.beginResumeSegment(run, in, candidates)
		r.streamAttach(w, req, run)
		return
	}
	if _, ok := in.LastUserMessage(); ok {
		// A new turn on a thread that is already busy is the loser case, not
		// a reason to abandon the run in progress.
		writeJSONError(w, http.StatusConflict, "a run is already in progress for this thread")
		return
	}
	// No new user message and nothing to resolve: this is a reattach, exactly
	// like GET {path}/threads/{id}/stream.
	r.streamAttach(w, req, run)
}

// resumeCandidates returns the trailing tool messages that resolve a call
// run is actually blocked on, without delivering them yet. Splitting
// detection from delivery is what lets beginResumeSegment install the new
// segment's translator before any result is handed to the blocked tool
// goroutine (see its doc comment); an unrelated or stale tool message is
// ignored, never mistaken for a resumption.
func (r *Runtime) resumeCandidates(run *activeRun, in *RunAgentInput) []Message {
	var out []Message
	for _, msg := range in.TrailingToolMessages() {
		if msg.ToolCallID != "" && r.pending.isPending(run.sessionID, msg.ToolCallID) {
			out = append(out, msg)
		}
	}
	return out
}

// beginResumeSegment starts the next protocol run over an already-suspended
// activeRun: a new translator (inheriting the calls the previous segment
// already closed), a fresh RUN_STARTED broadcast to every attached
// subscriber, then delivery of the resolved tool results.
//
// Ordering matters: the translator is installed and RUN_STARTED broadcast
// BEFORE any result is delivered to pending.resolve, which is what wakes the
// blocked tool goroutine. Without that ordering, the (single) pump goroutine
// could observe an event produced by the resumed agent while still holding
// the old, already-closed-out translator -- reopening a call the client
// already watched close, or emitting a bare result with no run to attribute
// it to. See activeRun.setTranslator.
//
// markSegmentStart is called here too, before the broadcast, for the same
// reason: the resuming request's own streamAttach (handleExistingThreadRun,
// right after this returns) replays from the buffer via replaySnapshot,
// which stops at the first segment boundary it finds. Without recording
// where THIS segment starts, that replay would restart from the run's very
// first buffered event and stop at ITS boundary -- the interrupt this very
// resume is answering -- never reaching the new segment at all. See
// eventBuffer.snapshotFrom's doc comment (PANDO-T-0002).
func (r *Runtime) beginResumeSegment(run *activeRun, in *RunAgentInput, resolved []Message) {
	t := newTranslator(in.ThreadID, in.RunID).withState(run.state).inheritEnded(run.endedCalls())
	for _, msg := range resolved {
		t.suppressToolCall(msg.ToolCallID)
	}
	run.setTranslator(t)
	run.setSuspended(false)
	run.unpark()
	run.markSegmentStart()
	run.broadcast(t.Start())

	logging.Debug("agui: resuming suspended run",
		"thread", run.threadID, "session", run.sessionID, "calls", len(resolved))
	for _, msg := range resolved {
		r.pending.resolve(run.sessionID, msg.ToolCallID, msg)
	}
}

// streamAttach opens an SSE response and attaches it to an already-live run
// (a resumption, a reattach or a read-only follower) — everything handleRun
// does after runPrelude, minus creating the run itself.
func (r *Runtime) streamAttach(w http.ResponseWriter, req *http.Request, run *activeRun) {
	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer sse.Close()
	r.attachRun(req.Context(), sse, run, false)
}

// handleStream answers GET {path}/threads/{id}/stream: reattach to a
// thread's live run (PANDO-US-0018). A thread with no live run answers 404
// rather than starting one — reattaching is never a way to run an agent.
func (r *Runtime) handleStream(w http.ResponseWriter, req *http.Request) {
	if !r.authorize(w, req) {
		return
	}
	threadID := strings.TrimSpace(req.PathValue("id"))
	run, ok := r.runs.get(threadID)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "no live run for this thread")
		return
	}
	r.streamAttach(w, req, run)
}

// handleCancelRun answers POST {path}/runs/{id}/cancel: end a thread's live
// or parked run (PANDO-US-0019). It works whether or not a stream is
// currently attached, and whether the run is actively streaming or suspended
// waiting on a frontend-tool/permission result — for the latter, the wait is
// released explicitly (pending.cancelAll) before the run is stopped, so no
// goroutine is left blocked on an answer that will never come.
//
// It is idempotent: an unknown thread, or one whose run has already ended,
// answers success with no error, exactly like a repeat cancel of the same
// run. It never touches the agui_threads binding or the session -- cancel
// ends the run, not the thread.
func (r *Runtime) handleCancelRun(w http.ResponseWriter, req *http.Request) {
	if !r.authorize(w, req) {
		return
	}
	threadID := strings.TrimSpace(req.PathValue("id"))
	run, ok := r.runs.get(threadID)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	r.pending.cancelAll(run.sessionID)
	run.requestCancel("cancelled")

	// Wait briefly for the pump to finish tearing the run down, so the
	// response can promise "gone from runStore" rather than "asked to be
	// gone eventually". Bounded: a stuck agent goroutine must not hang this
	// handler forever.
	select {
	case <-run.done:
	case <-time.After(cancelTeardownTimeout):
		logging.Warn("agui: cancel did not observe run teardown in time", "thread", threadID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// cancelTeardownTimeout bounds how long handleCancelRun waits for the pump to
// confirm teardown before answering anyway.
const cancelTeardownTimeout = 5 * time.Second

// runPrelude writes the events that open every run, in the order the protocol
// requires: RUN_STARTED, then STATE_SNAPSHOT.
//
// resync additionally requests MESSAGES_SNAPSHOT right after STATE_SNAPSHOT
// (PANDO-US-0016): the caller has already established it is the first run
// this process has served for a pre-existing thread's attach, so the browser
// that lost its local transcript is resynchronised in-band, in the same
// response, without a second round trip to the thread API. The snapshot is
// built here, before the agent run starts, so a large history cannot block
// the event loop once it is running -- and skipped silently (never failing
// the run) if the message store errors, since a resync the client did not
// strictly ask for must not be allowed to abort the turn it rides along with.
func (r *Runtime) runPrelude(ctx context.Context, sse *SSEWriter, t *translator, state *stateTracker, sessionID string, resync bool) error {
	if err := sse.WriteAll(t.Start()); err != nil {
		return err
	}
	if err := sse.Write(state.Snapshot()); err != nil {
		return err
	}
	if !resync {
		return nil
	}
	msgs, err := r.deps.Messages.List(ctx, sessionID)
	if err != nil {
		logging.Debug("agui: could not build MESSAGES_SNAPSHOT, skipping resync",
			"session", sessionID, "error", err)
		return nil
	}
	snap := buildMessagesSnapshot(msgs, r.cfg.MessagesSnapshotMaxMessages, r.cfg.MessagesSnapshotMaxBytes)
	return sse.Write(snap)
}

// attachRun subscribes reqCtx's request to run and streams it: replay of
// whatever was missed (skipped for first, the request that just created the
// run — runPrelude already wrote its opening frames directly, and nothing has
// been broadcast yet to replay), then live events until the request detaches
// or the run reaches a segment boundary.
//
// Any number of requests may be attached to one run at once (PANDO-US-0018):
// the original stream, a reattach after a disconnect, and read-only
// followers (additional tabs) are all exactly this call. Only a POST that
// resolves a pending call may ever progress the run (via
// beginResumeSegment); attaching, by itself, never does.
func (r *Runtime) attachRun(reqCtx context.Context, sse *SSEWriter, run *activeRun, first bool) {
	sub, ok := run.subscribe()
	if !ok {
		// The run finished between the caller's lookup and this attach.
		_ = sse.WriteAll([]Event{NewRunError("run already finished", "not_found")})
		return
	}
	run.unpark()

	if !first {
		// A fresh, current state snapshot orients a (re)attaching client
		// without depending on the bounded replay buffer to still hold the
		// original one from run start.
		if err := sse.Write(run.state.Snapshot()); err != nil {
			run.unsubscribe(sub)
			return
		}
		buffered, lossy := run.replaySnapshot()
		if lossy {
			if err := sse.Write(NewCustom("pando.replayLossy", true)); err != nil {
				run.unsubscribe(sub)
				return
			}
		}
		// Replay stops at the first segment boundary it encounters, exactly
		// like the live path below: one HTTP response is one AG-UI run
		// segment, whether its RUN_FINISHED/RUN_ERROR arrived live or is
		// being caught up on here. A reattach that fell behind by more than
		// one segment boundary needs a second reattach to keep catching up —
		// simple, and consistent regardless of whether the client catches a
		// boundary live or via replay.
		for _, ev := range buffered {
			if err := sse.Write(ev); err != nil {
				run.unsubscribe(sub)
				return
			}
			if isSegmentBoundary(ev) {
				run.unsubscribe(sub)
				return
			}
		}
	}

	r.attachLoop(reqCtx, sse, run, sub)
}

// attachLoop forwards run's live events to sse until reqCtx is done (the
// client disconnected), the subscriber channel closes (the run truly
// finished) or a segment boundary passes through (RUN_FINISHED or
// RUN_ERROR): every attach's HTTP response ends there, exactly as it did
// before reattachment existed, whether the boundary is a true end or an
// interrupt the run will continue past in a future segment. A client that
// wants to keep watching an interrupted run reattaches, the same as after any
// other disconnect.
//
// On detaching while the run is still ongoing and this was the last attached
// subscriber, it arms the run's teardown timer: Config.DisconnectGrace for a
// live segment, or the (longer, dedicated) suspendGrace if the run is
// currently suspended -- see activeRun.suspended's doc comment for why those
// two lifetimes must not be conflated.
func (r *Runtime) attachLoop(reqCtx context.Context, sse *SSEWriter, run *activeRun, sub *subscriber) {
	defer func() {
		remaining, finished := run.unsubscribe(sub)
		if finished || remaining > 0 {
			return
		}
		grace := r.cfg.DisconnectGrace
		if run.isSuspended() {
			grace = suspendGrace
		}
		run.park(grace, func() {
			logging.Warn("agui: parked run expired with nobody reattached",
				"thread", run.threadID, "session", run.sessionID)
			run.requestCancel("expired")
		})
	}()

	heartbeat := time.NewTicker(defaultHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-reqCtx.Done():
			// The browser went away. The pump keeps the run alive on its own
			// (PANDO-US-0017): nothing more to do here than detach, which the
			// deferred unsubscribe above already handles.
			return

		case <-heartbeat.C:
			if err := sse.Comment("keep-alive"); err != nil {
				return
			}

		case ev, ok := <-sub.ch:
			if !ok {
				// The run finished and closed every subscriber.
				return
			}
			if err := sse.Write(ev); err != nil {
				logging.Debug("agui: stream write failed, detaching client", "error", err)
				return
			}
			if isSegmentBoundary(ev) {
				return
			}
		}
	}
}

// isSegmentBoundary reports whether ev ends an attach's HTTP response: a true
// end (RUN_FINISHED{success}, RUN_ERROR) or an interrupt the run will
// continue past in a future segment (RUN_FINISHED{interrupt}) are both
// segment boundaries in this sense — see attachLoop's doc comment.
func isSegmentBoundary(ev Event) bool {
	switch ev.EventType() {
	case EventRunFinished, EventRunError:
		return true
	default:
		return false
	}
}

// runErrorCode maps known agent errors onto stable RUN_ERROR codes.
func runErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, agent.ErrSessionBusy):
		return "session_busy"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return ""
	}
}

// runRejectedRetryAfterSeconds is the Retry-After value sent with a 503 from
// rejectOverCapacity. It is a plain constant rather than derived from
// Config.ShutdownGrace/MaxConcurrentRuns: those bound how long a slot might
// take to free up, but a short, fixed retry hint is simpler for a client to
// implement correctly than one that varies by rejection reason.
const runRejectedRetryAfterSeconds = 5

// rejectOverCapacity answers a POST that handleRun will not admit -- the
// concurrency cap is reached (PANDO-US-0021) or the adapter is draining for
// shutdown (PANDO-US-0022) -- with 503 and a numeric Retry-After header. It is
// called before any session, agent instance or thread binding is created and
// before any SSE stream is opened, so the caller must not have started
// anything yet: no RUN_ERROR event is emitted, because no run exists to
// attribute one to.
func (r *Runtime) rejectOverCapacity(w http.ResponseWriter, reason string) {
	w.Header().Set("Retry-After", strconv.Itoa(runRejectedRetryAfterSeconds))
	writeJSONError(w, http.StatusServiceUnavailable, reason)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
