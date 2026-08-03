package agui

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
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
//	POST {path}/{agent}  run an agent, streaming AG-UI events over SSE
//	GET  {path}/info     agent discovery, consumed by CopilotKit's runtime
//
// It is the only place this package touches the outside world's routing, and it
// is called only when the feature is enabled.
func (r *Runtime) Register(mux *http.ServeMux) {
	path := strings.TrimSuffix(r.cfg.Path, "/")
	mux.HandleFunc("GET "+path+"/info", r.handleInfo)
	mux.HandleFunc("OPTIONS "+path+"/", r.handlePreflight)
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
// One request is either a new turn or the resolution of an interrupted one. The
// two are told apart by content, not by a flag: a payload whose trailing tool
// messages resolve calls the thread is currently blocked on is a resumption.
func (r *Runtime) handleRun(w http.ResponseWriter, req *http.Request) {
	if !r.authorize(w, req) {
		return
	}

	agentName, err := r.resolveAgent(req.PathValue("agent"))
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	in, err := DecodeRunAgentInput(req.Body, defaultMaxRequestBytes)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if run, ok := r.runs.get(in.ThreadID); ok {
		if resolved := r.deliverToolResults(run, in); len(resolved) > 0 {
			r.resumeRun(w, req, run, in, resolved)
			return
		}
		// The client sent a new turn instead of the awaited result. Its previous
		// run is now unreachable, so it is torn down before the new one starts.
		r.abandonRun(run)
	}

	userMsg, ok := in.LastUserMessage()
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "no user message to run")
		return
	}
	prompt := userMsg.Content.String()
	if strings.TrimSpace(prompt) == "" {
		writeJSONError(w, http.StatusBadRequest, "the trailing user message has no text content")
		return
	}
	if ctxBlock := in.ContextBlock(); ctxBlock != "" {
		prompt = ctxBlock + "\n\n" + prompt
	}

	sessionID, err := r.sessionForThread(req.Context(), in.ThreadID, string(agentName), prompt)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	svc, err := r.pool.get(agentName, in.Tools)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Everything below this point streams, so failures are reported as RUN_ERROR
	// events rather than HTTP status codes.
	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer sse.Close()

	// The document belongs to the thread, not to this run: files, todos and
	// sub-agents accumulate over the conversation instead of resetting each turn.
	state := r.states.get(in.ThreadID, sessionID, agentName, svc.Model(), in.State)
	t := newTranslator(in.ThreadID, in.RunID).withState(state)
	if err := sse.WriteAll(t.Start()); err != nil {
		return
	}
	if err := sse.Write(state.Snapshot()); err != nil {
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
	// call must survive the response that announces the interrupt.
	runCtx, cancel := context.WithCancel(r.baseCtx)
	suspend := r.pending.watch(sessionID)

	events, err := svc.Run(runCtx, sessionID, prompt)
	if err != nil {
		cancel()
		_ = sse.WriteAll(t.Fail(err.Error(), runErrorCode(err)))
		return
	}

	run := &activeRun{
		threadID:  in.ThreadID,
		sessionID: sessionID,
		events:    events,
		cancel:    cancel,
		state:     state,
		suspend:   suspend,
	}
	r.runs.put(run)

	r.stream(req.Context(), sse, t, run)
}

// resumeRun re-attaches to a run that was suspended waiting for the browser. It
// does not start a new agent run: the same agent goroutine is still blocked
// inside the frontend tool and simply continues once the result is delivered.
func (r *Runtime) resumeRun(w http.ResponseWriter, req *http.Request, run *activeRun, in *RunAgentInput, resolved []string) {
	run.unpark()

	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer sse.Close()

	// A resumption is a new AG-UI run over the same agent run, so it gets a fresh
	// message id. The calls the client just answered are marked as already
	// reported: it owns those results and must not receive them back.
	t := newTranslator(in.ThreadID, in.RunID).withState(run.state).inheritEnded(run.endedCalls())
	for _, callID := range resolved {
		t.suppressToolCall(callID)
	}
	if err := sse.WriteAll(t.Start()); err != nil {
		return
	}
	if err := sse.Write(run.state.Snapshot()); err != nil {
		return
	}

	logging.Debug("agui: resuming suspended run",
		"thread", run.threadID, "session", run.sessionID, "calls", len(resolved))
	r.stream(req.Context(), sse, t, run)
}

// deliverToolResults hands the client's tool messages to the proxies blocked on
// them and returns the call ids that were actually waiting. Unknown ids are
// ignored: a stale retry must not be mistaken for a resumption.
func (r *Runtime) deliverToolResults(run *activeRun, in *RunAgentInput) []string {
	var resolved []string
	for _, msg := range in.TrailingToolMessages() {
		if msg.ToolCallID == "" {
			continue
		}
		if r.pending.resolve(run.sessionID, msg.ToolCallID, msg) {
			resolved = append(resolved, msg.ToolCallID)
		}
	}
	return resolved
}

// stream pumps the agent's events through the translator until the run ends, a
// frontend tool suspends it, the client disconnects, or the stream fails.
//
// ctx is the *request* context: it detects a browser that went away. The run
// itself lives on until finishRun, which is why every terminal branch below
// either calls it or deliberately parks the run.
func (r *Runtime) stream(ctx context.Context, sse *SSEWriter, t *translator, run *activeRun) {
	heartbeat := time.NewTicker(defaultHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			// The browser went away mid-run. Nothing will consume the rest of the
			// events, so the run is cancelled rather than left to fill its buffer.
			r.finishRun(run)
			return

		case <-heartbeat.C:
			if err := sse.Comment("keep-alive"); err != nil {
				r.finishRun(run)
				return
			}

		case s := <-run.suspend:
			r.suspendRun(sse, t, run, s)
			return

		case ev, ok := <-run.events:
			if !ok {
				// Channel closed without a terminal event.
				_ = sse.WriteAll(t.Finish(OutcomeSuccess, nil))
				r.finishRun(run)
				return
			}

			switch ev.Type {
			case agent.AgentEventTypeResponse:
				result := ev.Message.Content().String()
				_ = sse.WriteAll(t.Finish(OutcomeSuccess, result))
				r.finishRun(run)
				return

			case agent.AgentEventTypeError:
				msg := "unknown error"
				if ev.Error != nil {
					msg = ev.Error.Error()
				}
				_ = sse.WriteAll(t.Fail(msg, runErrorCode(ev.Error)))
				r.finishRun(run)
				return
			}

			if err := sse.WriteAll(t.Translate(ev)); err != nil {
				logging.Debug("agui: stream write failed, dropping client", "error", err)
				r.finishRun(run)
				return
			}
		}
	}
}

// suspendRun ends the HTTP response with an interrupt while leaving the agent
// blocked on the client.
//
// Either way the events already queued by the agent are flushed first: a client
// that receives RUN_FINISHED before TOOL_CALL_ARGS has no arguments to execute
// with, and one that sees a permission prompt before the tool call that raised
// it has no context to render.
func (r *Runtime) suspendRun(sse *SSEWriter, t *translator, run *activeRun, s suspension) {
	if len(s.events) > 0 {
		// A synthetic call (a permission prompt): the agent stream will never
		// carry it, so only what it already queued can be drained.
		r.drainQueued(sse, t, run)
		t.adoptToolCall(s.callID)
		if err := sse.WriteAll(s.events); err != nil {
			r.finishRun(run)
			return
		}
	} else {
		// A streaming provider has already queued this call's events by the time
		// its tool runs, so flushing the queue is enough — and unlike waiting for
		// them, it costs nothing when they are never coming.
		r.drainQueued(sse, t, run)
		if err := sse.WriteAll(t.completeToolCall(s.callID, callName(s), callInput(s))); err != nil {
			r.finishRun(run)
			return
		}
	}

	if err := sse.WriteAll(t.Finish(OutcomeInterrupt, nil)); err != nil {
		// The client that must answer this call is gone; nobody else can.
		r.finishRun(run)
		return
	}
	logging.Debug("agui: run suspended waiting for the client",
		"thread", run.threadID, "call", s.callID)
	run.rememberEnded(t.endedSnapshot())
	run.park(func() {
		logging.Warn("agui: suspended run expired without a client result",
			"thread", run.threadID, "call", s.callID)
		r.finishRun(run)
	})
}

// callName and callInput read the suspending call's description, which is absent
// only for synthetic calls (they carry their own events instead).
func callName(s suspension) string {
	if s.call == nil {
		return ""
	}
	return s.call.name
}

func callInput(s suspension) string {
	if s.call == nil {
		return ""
	}
	return s.call.input
}

// drainQueued forwards whatever the agent has already produced, without waiting
// for more. A permission prompt is raised from inside a tool's Run, so the tool
// call that triggered it is guaranteed to be in the channel already.
func (r *Runtime) drainQueued(sse *SSEWriter, t *translator, run *activeRun) {
	for {
		select {
		case ev, ok := <-run.events:
			if !ok {
				return
			}
			if err := sse.WriteAll(t.Translate(ev)); err != nil {
				return
			}
		default:
			return
		}
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

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
