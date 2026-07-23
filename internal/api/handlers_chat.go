package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/agentsmd"
	"github.com/digiogithub/pando/internal/caveman"
	"github.com/digiogithub/pando/internal/commands"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/imageopt"
	"github.com/digiogithub/pando/internal/learning"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/ponytail"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/superpowers"
	"github.com/digiogithub/pando/internal/toolmeta"
	"github.com/digiogithub/pando/internal/userinput"
	"github.com/digiogithub/pando/internal/vulnhunter"
)

type ChatRequest struct {
	SessionID string `json:"sessionId"`
	Prompt    string `json:"prompt"`
	Model     string `json:"model,omitempty"`
}

type ChatResponse struct {
	SessionID string `json:"sessionId"`
	MessageID string `json:"messageId"`
	Response  string `json:"response"`
}

type goalStatusResponse struct {
	Goal *goalSSEPayload `json:"goal"`
}

type goalSSEPayload struct {
	Objective     string `json:"objective"`
	Status        string `json:"status"`
	Iteration     int64  `json:"iteration"`
	MaxIterations int64  `json:"maxIterations"`
	Progress      string `json:"progress,omitempty"`
	NextStep      string `json:"nextStep,omitempty"`
	StartedAt     int64  `json:"startedAt"`
	CompletedAt   *int64 `json:"completedAt,omitempty"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	sess, err := s.getOrCreateSession(r.Context(), req.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	eventChan, err := s.app.CoderAgent.Run(r.Context(), sess.ID, req.Prompt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("agent error: %v", err))
		return
	}

	var response string
	for event := range eventChan {
		if event.Type == agent.AgentEventTypeResponse {
			for _, part := range event.Message.Parts {
				if text, ok := part.(message.TextContent); ok {
					response += text.Text
				}
			}
		}
		if event.Type == agent.AgentEventTypeError {
			writeError(w, http.StatusInternalServerError, event.Error.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, ChatResponse{
		SessionID: sess.ID,
		Response:  response,
	})
}

// handleChatStream starts an agent run in the background (decoupled from the
// HTTP connection lifetime) and streams events to the client via SSE.
// If the client disconnects mid-run the agent keeps running; a reconnect via
// GET /api/v1/sessions/{id}/stream will replay buffered events and resume live.
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ChatRequest
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	} else {
		req.SessionID = r.URL.Query().Get("sessionId")
		req.Prompt = r.URL.Query().Get("prompt")
	}

	if req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	sess, err := s.getOrCreateSession(r.Context(), req.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	fmt.Fprintf(w, "event: session\ndata: {\"sessionId\":%q,\"running\":true}\n\n", sess.ID)
	flusher.Flush()

	// Intercept slash commands before sending to agent.
	if cmdName, cmdArgs, isCmd := commands.Parse(req.Prompt); isCmd {
		s.handleSlashCommandStream(w, flusher, r.Context(), sess.ID, cmdName, cmdArgs)
		return
	}

	// Submit the run to the background manager. The agent goroutine uses a
	// context.Background()-derived context so it survives HTTP disconnects.
	agentSvc := s.app.CoderAgent
	sessionID := sess.ID
	submitErr := s.bgRunner.Submit(sessionID, func(ctx context.Context) (<-chan agent.AgentEvent, error) {
		return agentSvc.Run(ctx, sessionID, req.Prompt)
	})
	if submitErr != nil {
		writeSSEEvent(w, flusher, "error", map[string]string{"error": submitErr.Error()})
		return
	}

	// Subscribe to receive buffered + live events from the background run.
	eventChan, unsubFn, _ := s.bgRunner.Subscribe(sessionID)
	s.streamSessionEvents(w, flusher, r.Context(), sessionID, unsubFn, eventChan)
}

// SteerRequest is the body accepted by POST /api/v1/sessions/{id}/steer.
type SteerRequest struct {
	Prompt string `json:"prompt"`
}

// handleSteer queues a mid-run feedback message for an active session. The
// message is injected into the agent loop at the next safe boundary without
// cancelling the run; injection events flow over the existing SSE stream.
// POST /api/v1/sessions/{id}/steer
func (s *Server) handleSteer(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}

	var req SteerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	err := s.app.CoderAgent.Steer(sessionID, req.Prompt)
	if err != nil {
		if errors.Is(err, agent.ErrSessionNotBusy) {
			// No active run to steer; the client should start a normal chat instead.
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "not_busy",
				"hint":  "no active run for this session; send a normal chat message instead",
			})
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("steer error: %v", err))
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"sessionId": sessionID,
		"queued":    s.app.CoderAgent.PendingSteering(sessionID),
	})
}

// handleSessionStream lets clients reconnect to an in-progress (or recently
// completed) background session to observe its events with replay.
// GET /api/v1/sessions/{id}/stream
func (s *Server) handleSessionStream(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	running := s.bgRunner.IsBusy(sessionID)
	fmt.Fprintf(w, "event: session\ndata: {\"sessionId\":%q,\"running\":%v}\n\n", sessionID, running)
	flusher.Flush()
	if err := s.writeCurrentGoalState(w, flusher, sessionID); err != nil {
		writeSSEEvent(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	eventChan, unsubFn, knownSession := s.bgRunner.Subscribe(sessionID)
	if !knownSession {
		// Session not in runner — already completed long ago or never started here.
		// Return a done marker so the UI knows there is nothing to stream.
		writeSSEEvent(w, flusher, "done", map[string]string{})
		return
	}

	s.streamSessionEvents(w, flusher, r.Context(), sessionID, unsubFn, eventChan)
}

// streamSessionEvents reads events from eventChan and writes them as SSE to w.
// It returns when eventChan is closed (agent done) or clientCtx is cancelled
// (HTTP disconnect). On disconnect unsubFn is called to clean up the
// subscription, but the background session keeps running.
func (s *Server) streamSessionEvents(
	w http.ResponseWriter,
	flusher http.Flusher,
	clientCtx context.Context,
	sessionID string,
	unsubFn func(),
	eventChan <-chan agent.AgentEvent,
) {
	workDir := s.config.CWD
	var mu sync.Mutex
	startedToolCalls := map[string]bool{}
	pendingInputs := map[string]string{}

	// Subscribe to permission requests so the UI can prompt when a session is not
	// in auto-approve mode. The agent goroutine blocks inside Permissions.Request
	// until the client responds via POST /api/v1/permissions/respond.
	permCh := s.app.Permissions.Subscribe(clientCtx)

	// Subscribe to AskUserQuestion requests so the UI can prompt the user. The
	// agent goroutine blocks inside UserInput.Ask until the client responds via
	// POST /api/v1/questions/respond.
	questionCh := s.app.UserInput.Subscribe(clientCtx)

	// Replay any requests already pending for this session (covers reconnects).
	for _, p := range s.app.Permissions.PendingRequests(sessionID) {
		writePermissionRequest(w, flusher, p)
	}
	for _, q := range s.app.UserInput.PendingRequests(sessionID) {
		writeQuestionRequest(w, flusher, q)
	}

	cleanup := func() {
		unsubFn()
		// Drain remaining buffered events to prevent the pump goroutine from
		// blocking on sends to this (now-abandoned) subscriber channel.
		for range eventChan {
		}
	}

	for {
		select {
		case <-clientCtx.Done():
			// Client disconnected — leave the agent running.
			go cleanup()
			return
		case ev, open := <-permCh:
			if !open {
				permCh = nil
				continue
			}
			if ev.Payload.SessionID == sessionID {
				writePermissionRequest(w, flusher, ev.Payload)
			}
		case ev, open := <-questionCh:
			if !open {
				questionCh = nil
				continue
			}
			if ev.Payload.SessionID == sessionID {
				writeQuestionRequest(w, flusher, ev.Payload)
			}
		case event, open := <-eventChan:
			if !open {
				if err := s.writeCurrentGoalState(w, flusher, sessionID); err != nil {
					writeSSEEvent(w, flusher, "error", map[string]string{"error": err.Error()})
					return
				}
				writeSSEEvent(w, flusher, "done", map[string]string{})
				return
			}
			s.dispatchSSEEvent(w, flusher, &mu, startedToolCalls, pendingInputs, workDir, event)
		}
	}
}

// writePermissionRequest emits a permission_request SSE event for the Web UI.
func writePermissionRequest(w http.ResponseWriter, flusher http.Flusher, p permission.PermissionRequest) {
	writeSSEEvent(w, flusher, "permission_request", map[string]interface{}{
		"id":          p.ID,
		"session_id":  p.SessionID,
		"tool_name":   p.ToolName,
		"description": p.Description,
		"action":      p.Action,
		"path":        p.Path,
		"params":      p.Params,
	})
}

// writeQuestionRequest emits an AskUserQuestion question_request SSE event for
// the Web UI.
func writeQuestionRequest(w http.ResponseWriter, flusher http.Flusher, q userinput.QuestionRequest) {
	writeSSEEvent(w, flusher, "question_request", map[string]interface{}{
		"id":         q.ID,
		"session_id": q.SessionID,
		"questions":  q.Questions,
	})
}

// dispatchSSEEvent translates a single AgentEvent into SSE writes.
func (s *Server) dispatchSSEEvent(
	w http.ResponseWriter,
	flusher http.Flusher,
	mu *sync.Mutex,
	startedToolCalls map[string]bool,
	pendingInputs map[string]string,
	workDir string,
	event agent.AgentEvent,
) {
	switch event.Type {
	case agent.AgentEventTypeThinkingDelta:
		writeSSEEvent(w, flusher, "thinking_delta", map[string]string{"text": event.Delta})

	case agent.AgentEventTypeContentDelta:
		writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": event.Delta})

	case agent.AgentEventTypeToolCall:
		if event.ToolCall == nil {
			return
		}
		tc := event.ToolCall

		// TodoWrite → plan: emit plan_update instead of tool_call.
		if toolmeta.IsTodoWriteTool(tc.Name) {
			mu.Lock()
			pendingInputs[tc.ID] = tc.Input
			startedToolCalls[tc.ID] = true
			mu.Unlock()
			if tc.Finished {
				writeSSETodoWritePlan(w, flusher, tc.Input)
			}
			return
		}

		kind := toolmeta.MapToolKind(tc.Name)
		rawInput := toolmeta.ParseJSONInput(tc.Input)
		title := toolmeta.DisplayTitle(tc.Name, rawInput, workDir)
		locations := toolmeta.ToLocations(tc.Name, tc.Input)

		mu.Lock()
		pendingInputs[tc.ID] = tc.Input
		started := startedToolCalls[tc.ID]
		mu.Unlock()

		if !tc.Finished {
			if !started {
				writeSSEEvent(w, flusher, "tool_call", map[string]interface{}{
					"id":        tc.ID,
					"name":      tc.Name,
					"kind":      kind,
					"title":     title,
					"status":    toolmeta.StatusPending,
					"input":     tc.Input,
					"locations": locations,
				})
				mu.Lock()
				startedToolCalls[tc.ID] = true
				mu.Unlock()
			}
		} else {
			if !started {
				writeSSEEvent(w, flusher, "tool_call", map[string]interface{}{
					"id":        tc.ID,
					"name":      tc.Name,
					"kind":      kind,
					"title":     title,
					"status":    toolmeta.StatusInProgress,
					"input":     tc.Input,
					"locations": locations,
				})
				mu.Lock()
				startedToolCalls[tc.ID] = true
				mu.Unlock()
			} else {
				writeSSEEvent(w, flusher, "tool_call_update", map[string]interface{}{
					"id":        tc.ID,
					"status":    toolmeta.StatusInProgress,
					"kind":      kind,
					"title":     title,
					"input":     tc.Input,
					"locations": locations,
				})
			}
		}

	case agent.AgentEventTypeToolResult:
		if event.ToolResult == nil {
			return
		}
		tr := event.ToolResult

		// TodoWrite results are suppressed — plan_update already sent.
		if toolmeta.IsTodoWriteTool(tr.Name) {
			mu.Lock()
			delete(pendingInputs, tr.ToolCallID)
			delete(startedToolCalls, tr.ToolCallID)
			mu.Unlock()
			return
		}

		status := toolmeta.StatusCompleted
		if tr.IsError {
			status = toolmeta.StatusFailed
		}

		mu.Lock()
		storedInput := pendingInputs[tr.ToolCallID]
		wasStarted := startedToolCalls[tr.ToolCallID]
		delete(startedToolCalls, tr.ToolCallID)
		delete(pendingInputs, tr.ToolCallID)
		mu.Unlock()

		rawInputParsed := toolmeta.ParseJSONInput(storedInput)
		kind := toolmeta.MapToolKind(tr.Name)
		title := toolmeta.DisplayTitle(tr.Name, rawInputParsed, workDir)
		locations := toolmeta.ToLocations(tr.Name, storedInput)

		// Guarantee a tool_call start event precedes the result.
		if !wasStarted {
			writeSSEEvent(w, flusher, "tool_call", map[string]interface{}{
				"id":        tr.ToolCallID,
				"name":      tr.Name,
				"kind":      kind,
				"title":     title,
				"status":    toolmeta.StatusInProgress,
				"input":     storedInput,
				"locations": locations,
			})
		}

		rawOutput := map[string]interface{}{
			"output": tr.Content,
		}
		if tr.Metadata != "" {
			var meta interface{}
			if jerr := json.Unmarshal([]byte(tr.Metadata), &meta); jerr == nil {
				rawOutput["metadata"] = meta
			} else {
				rawOutput["metadata"] = tr.Metadata
			}
		}

		var terminalMeta map[string]interface{}
		if toolmeta.IsBashTool(tr.Name) {
			exitCode := 0
			if tr.IsError {
				exitCode = 1
			}
			terminalMeta = map[string]interface{}{
				"terminal_id": tr.ToolCallID,
				"exit_code":   exitCode,
			}
		}

		var diffMeta map[string]interface{}
		if toolmeta.IsEditTool(tr.Name) && !tr.IsError && storedInput != "" {
			var ep struct {
				FilePath  string `json:"file_path"`
				OldString string `json:"old_string"`
				NewString string `json:"new_string"`
				Content   string `json:"content"`
			}
			if jerr := json.Unmarshal([]byte(storedInput), &ep); jerr == nil && ep.FilePath != "" {
				diffMeta = map[string]interface{}{
					"file_path": ep.FilePath,
				}
				if tr.Name == "write" {
					diffMeta["new_content"] = ep.Content
				} else {
					diffMeta["old_string"] = ep.OldString
					diffMeta["new_string"] = ep.NewString
				}
			}
		}

		resultPayload := map[string]interface{}{
			"tool_call_id": tr.ToolCallID,
			"name":         tr.Name,
			"kind":         kind,
			"title":        title,
			"status":       status,
			"content":      tr.Content,
			"is_error":     tr.IsError,
			"locations":    locations,
			"raw_output":   rawOutput,
		}
		if terminalMeta != nil {
			resultPayload["terminal"] = terminalMeta
		}
		if diffMeta != nil {
			resultPayload["diff"] = diffMeta
		}
		if len(tr.Images) > 0 {
			images := make([]string, 0, len(tr.Images))
			for _, img := range tr.Images {
				images = append(images, imageopt.ToDataURI(img.Data, img.MIMEType))
			}
			resultPayload["images"] = images
		}
		writeSSEEvent(w, flusher, "tool_result", resultPayload)

	case agent.AgentEventTypeTodosUpdated:
		if len(event.Todos) > 0 {
			writeSSEEvent(w, flusher, "todos_update", map[string]interface{}{
				"session_id": event.SessionID,
				"todos":      event.Todos,
			})
		}

	case agent.AgentEventTypeTokenUsage:
		if event.TokenUsage != nil {
			writeSSEEvent(w, flusher, "token_usage", map[string]interface{}{
				"session_id":            event.SessionID,
				"prompt_tokens":         event.TokenUsage.PromptTokens,
				"completion_tokens":     event.TokenUsage.CompletionTokens,
				"context_window":        event.TokenUsage.ContextWindow,
				"estimated":             event.TokenUsage.Estimated,
				"cache_read_tokens":     event.TokenUsage.CacheReadTokens,
				"cache_creation_tokens": event.TokenUsage.CacheCreationTokens,
				"reasoning_tokens":      event.TokenUsage.ReasoningTokens,
				"cost":                  event.TokenUsage.Cost,
			})
		}

	case agent.AgentEventTypeSteeringQueued:
		writeSSEEvent(w, flusher, "steering_queued", map[string]interface{}{
			"session_id": event.SessionID,
			"message":    event.SystemMessage,
		})

	case agent.AgentEventTypeSteeringInjected:
		writeSSEEvent(w, flusher, "steering_injected", map[string]interface{}{
			"session_id": event.SessionID,
			"message":    event.SystemMessage,
		})

	case agent.AgentEventTypeResponse:
		// Final response — content already streamed via content_delta events.

	case agent.AgentEventTypeError:
		if event.Error != nil {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": event.Error.Error()})
		}
	}
}

// writeSSETodoWritePlan emits a plan_update SSE event from a TodoWrite input.
func writeSSETodoWritePlan(w http.ResponseWriter, flusher http.Flusher, inputJSON string) {
	if inputJSON == "" {
		return
	}
	var raw struct {
		Todos []struct {
			Content    string `json:"content"`
			Status     string `json:"status"`
			ActiveForm string `json:"activeForm"`
		} `json:"todos"`
	}
	if err := json.Unmarshal([]byte(inputJSON), &raw); err != nil || len(raw.Todos) == 0 {
		return
	}

	entries := make([]map[string]string, 0, len(raw.Todos))
	for _, t := range raw.Todos {
		content := strings.TrimSpace(t.Content)
		if content == "" {
			continue
		}
		entry := map[string]string{
			"title":  content,
			"status": t.Status,
		}
		if t.ActiveForm != "" {
			entry["active_form"] = t.ActiveForm
		}
		entries = append(entries, entry)
	}

	if len(entries) > 0 {
		writeSSEEvent(w, flusher, "plan_update", map[string]interface{}{
			"entries": entries,
		})
	}
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
	flusher.Flush()
}

func (s *Server) handleGoalStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}

	goal, err := s.getGoalBySession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, goalStatusResponse{Goal: goal})
}

func (s *Server) handleCancelGoal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}

	goal, err := s.cancelPersistedGoal(r.Context(), sessionID)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "no active goal")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.bgRunner.Cancel(sessionID)
	writeJSON(w, http.StatusOK, goalStatusResponse{Goal: goalStateFromDB(goal)})
}

func (s *Server) writeCurrentGoalState(w http.ResponseWriter, flusher http.Flusher, sessionID string) error {
	goal, err := s.getGoalBySession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	writeSSEEvent(w, flusher, "goal_status", map[string]any{"goal": goal})
	return nil
}

func (s *Server) getGoalBySession(ctx context.Context, sessionID string) (*goalSSEPayload, error) {
	goal, err := s.app.DBQuerier.GetGoalBySession(ctx, sessionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return goalStateFromDB(goal), nil
}

func (s *Server) cancelPersistedGoal(ctx context.Context, sessionID string) (db.SessionGoal, error) {
	goal, err := s.app.DBQuerier.GetActiveGoal(ctx, sessionID)
	if err != nil {
		return db.SessionGoal{}, err
	}

	return s.app.DBQuerier.UpdateGoalStatus(ctx, db.UpdateGoalStatusParams{
		Status:        agent.GoalStatusCancelled,
		Iteration:     goal.Iteration,
		LastProgress:  sql.NullString{String: "GOAL CANCELLED", Valid: true},
		NextStep:      sql.NullString{},
		BlockedReason: sql.NullString{},
		CompletedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
		ID:            goal.ID,
	})
}

func goalStateFromDB(goal db.SessionGoal) *goalSSEPayload {
	payload := &goalSSEPayload{
		Objective:     goal.Objective,
		Status:        goal.Status,
		Iteration:     goal.Iteration,
		MaxIterations: goal.MaxIterations,
		StartedAt:     goal.StartedAt,
	}
	if goal.CompletedAt.Valid {
		completedAt := goal.CompletedAt.Int64
		payload.CompletedAt = &completedAt
	}
	if goal.LastProgress.Valid {
		payload.Progress = goal.LastProgress.String
	}
	if goal.NextStep.Valid {
		payload.NextStep = goal.NextStep.String
	}
	if payload.Progress == "" && goal.BlockedReason.Valid {
		payload.Progress = goal.BlockedReason.String
	}
	return payload
}

func (s *Server) getOrCreateSession(ctx context.Context, sessionID string) (*session.Session, error) {
	if sessionID != "" {
		sess, err := s.app.Sessions.Get(ctx, sessionID)
		if err == nil {
			s.seedAutoApprove(sess.ID)
			return &sess, nil
		}
	}

	sess, err := s.app.Sessions.Create(ctx, "New Chat")
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	s.seedAutoApprove(sess.ID)
	return &sess, nil
}

// seedAutoApprove applies the Permissions.AutoApproveTools config default to a
// session the first time it is used. It runs once per session (tracked in
// seededSessions) so that a later user toggle is never overridden by the default.
func (s *Server) seedAutoApprove(sessionID string) {
	if _, seeded := s.seededSessions.LoadOrStore(sessionID, true); seeded {
		return
	}
	if cfg := config.Get(); cfg != nil && cfg.Permissions.AutoApproveTools {
		s.app.Permissions.AutoApproveSession(sessionID)
	}
}

// humanizeBytes renders a byte count in human-readable units (e.g. "12.3 MB").
func humanizeBytes(n int64) string {
	neg := ""
	if n < 0 {
		neg = "-"
		n = -n
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%s%d B", neg, n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%s%.1f %cB", neg, float64(n)/float64(div), "KMGTPE"[exp])
}

// handleSlashCommandStream processes a slash command and writes results as SSE events.
func (s *Server) handleSlashCommandStream(w http.ResponseWriter, flusher http.Flusher, ctx context.Context, sessionID, cmdName, cmdArgs string) {
	switch cmdName {
	case "compact", "summarize":
		// SummarizeStream returns a channel that stays open until the (asynchronous)
		// summary actually finishes. Draining it lets us stream real progress and only
		// report success once the summary model has completed — the plain Summarize
		// returns immediately and would falsely report instant completion.
		events, err := s.app.CoderAgent.SummarizeStream(ctx, sessionID)
		if err != nil {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": "compaction failed: " + err.Error()})
			break
		}
		var summarizeErr error
		for ev := range events {
			switch ev.Type {
			case agent.AgentEventTypeError:
				summarizeErr = ev.Error
			case agent.AgentEventTypeSummarize:
				if ev.Progress != "" {
					writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": ev.Progress + "\n"})
				}
			}
		}
		if summarizeErr != nil {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": "compaction failed: " + summarizeErr.Error()})
		} else {
			writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": "Session compacted successfully."})
		}
	case "db-compact":
		writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": "Compacting database (VACUUM)..."})
		res, err := s.app.CompactDatabase(ctx, false, true)
		if err != nil {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": "database compaction failed: " + err.Error()})
		} else {
			writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": fmt.Sprintf(
				"\nDatabase compacted (%s). Freed %s (%s → %s).",
				res.Mode, humanizeBytes(res.Freed), humanizeBytes(res.SizeBefore), humanizeBytes(res.SizeAfter))})
		}
	case "ponytail":
		arg := strings.TrimSpace(cmdArgs)
		if arg == "" {
			arg = "full"
		}
		mode, ok := ponytail.ParseMode(arg)
		if !ok {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": "unknown ponytail mode: " + arg})
		} else {
			agent.SetPonytailMode(sessionID, mode)
			if mode.IsActive() {
				writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": "Ponytail mode: " + mode.String() + ".\n" + ponytail.Description(mode)})
			} else {
				writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": "Ponytail mode disabled. Back to normal."})
			}
		}
	case "caveman":
		arg := strings.TrimSpace(cmdArgs)
		if arg == "" {
			arg = "full"
		}
		mode, ok := caveman.ParseMode(arg)
		if !ok {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": "unknown caveman level: " + arg + "\n" + caveman.Usage})
		} else {
			agent.SetCavemanMode(sessionID, mode)
			writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": caveman.ActivationMessage(mode)})
		}
	case "caveman-finish":
		// The command takes no level: report a stray argument instead of letting
		// "/caveman-finish ultra" look like it switched levels.
		if strings.TrimSpace(cmdArgs) != "" {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": caveman.FinishUsage})
			return
		}
		agent.SetCavemanMode(sessionID, caveman.ModeOff)
		writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": caveman.DisabledMessage})
	case "superpowers":
		if agent.SuperpowersMode(sessionID) {
			writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": superpowers.AlreadyActiveMessage})
		} else {
			agent.SetSuperpowersMode(sessionID, true)
			writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": superpowers.ActivationMessage(cmdArgs)})
		}
	case "superpowers-finish":
		if !agent.SuperpowersMode(sessionID) {
			writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": superpowers.NotActiveMessage})
			return
		}
		writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": "Closing the Superpowers workflow: verifying and reporting..."})
		finishSvc := s.app.CoderAgent
		submitErr := s.bgRunner.Submit(sessionID, func(bgCtx context.Context) (<-chan agent.AgentEvent, error) {
			return agent.RunSuperpowersFinish(bgCtx, finishSvc, sessionID)
		})
		if submitErr != nil {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": submitErr.Error()})
			return
		}
		eventChan, unsubFn, _ := s.bgRunner.Subscribe(sessionID)
		s.streamSessionEvents(w, flusher, ctx, sessionID, unsubFn, eventChan)
		return
	case "learning":
		if agent.LearningMode(sessionID) {
			writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": learning.AlreadyActiveMessage})
		} else {
			agent.SetLearningMode(sessionID, true)
			writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": learning.ActivationMessage(cmdArgs)})
		}
	case "learning-finish":
		if !agent.LearningMode(sessionID) {
			writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": learning.NotActiveMessage})
			return
		}
		writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": "Closing Learning mode: consolidating what was learned..."})
		learningSvc := s.app.CoderAgent
		submitErr := s.bgRunner.Submit(sessionID, func(bgCtx context.Context) (<-chan agent.AgentEvent, error) {
			return agent.RunLearningFinish(bgCtx, learningSvc, sessionID)
		})
		if submitErr != nil {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": submitErr.Error()})
			return
		}
		eventChan, unsubFn, _ := s.bgRunner.Subscribe(sessionID)
		s.streamSessionEvents(w, flusher, ctx, sessionID, unsubFn, eventChan)
		return
	case "improve-agents-md":
		writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": "Improving AGENTS.md..."})
		agentSvc := s.app.CoderAgent
		submitErr := s.bgRunner.Submit(sessionID, func(bgCtx context.Context) (<-chan agent.AgentEvent, error) {
			return agentSvc.Run(bgCtx, sessionID, agentsmd.Prompt(cmdArgs))
		})
		if submitErr != nil {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": submitErr.Error()})
			return
		}
		eventChan, unsubFn, _ := s.bgRunner.Subscribe(sessionID)
		s.streamSessionEvents(w, flusher, ctx, sessionID, unsubFn, eventChan)
		return
	case "vulnhunt", "vulnhunter-fix", "vulnhunt-fix-verify":
		var prompt, intro string
		switch cmdName {
		case "vulnhunt":
			prompt, intro = vulnhunter.HuntPrompt(cmdArgs), "Starting security audit (vulnhunt)..."
		case "vulnhunter-fix":
			prompt, intro = vulnhunter.FixPrompt(cmdArgs), "Starting test-driven remediation (vulnhunter-fix)..."
		case "vulnhunt-fix-verify":
			prompt, intro = vulnhunter.VerifyPrompt(cmdArgs), "Verifying claimed security fixes (vulnhunt-fix-verify)..."
		}
		writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": intro})
		vhSvc := s.app.CoderAgent
		submitErr := s.bgRunner.Submit(sessionID, func(bgCtx context.Context) (<-chan agent.AgentEvent, error) {
			return vhSvc.Run(bgCtx, sessionID, prompt)
		})
		if submitErr != nil {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": submitErr.Error()})
			return
		}
		eventChan, unsubFn, _ := s.bgRunner.Subscribe(sessionID)
		s.streamSessionEvents(w, flusher, ctx, sessionID, unsubFn, eventChan)
		return
	case "goal", "autopilot":
		if strings.TrimSpace(cmdArgs) == "" {
			// No objective: show status
			goal, err := s.getGoalBySession(ctx, sessionID)
			if err != nil {
				writeSSEEvent(w, flusher, "error", map[string]string{"error": err.Error()})
			} else {
				writeSSEEvent(w, flusher, "goal_status", map[string]any{"goal": goal})
			}
		} else {
			// Start goal: submit as regular prompt with goal prefix
			agentSvc := s.app.CoderAgent
			submitErr := s.bgRunner.Submit(sessionID, func(bgCtx context.Context) (<-chan agent.AgentEvent, error) {
				return agentSvc.Run(bgCtx, sessionID, "/goal "+cmdArgs)
			})
			if submitErr != nil {
				writeSSEEvent(w, flusher, "error", map[string]string{"error": submitErr.Error()})
				return
			}
			eventChan, unsubFn, _ := s.bgRunner.Subscribe(sessionID)
			s.streamSessionEvents(w, flusher, ctx, sessionID, unsubFn, eventChan)
			return
		}
	case "goal-status":
		goal, err := s.getGoalBySession(ctx, sessionID)
		if err != nil {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": err.Error()})
		} else {
			writeSSEEvent(w, flusher, "goal_status", map[string]any{"goal": goal})
		}
	case "goal-cancel":
		goal, err := s.cancelPersistedGoal(ctx, sessionID)
		if err != nil {
			if err == sql.ErrNoRows {
				writeSSEEvent(w, flusher, "error", map[string]string{"error": "no active goal"})
			} else {
				writeSSEEvent(w, flusher, "error", map[string]string{"error": err.Error()})
			}
		} else {
			s.bgRunner.Cancel(sessionID)
			writeSSEEvent(w, flusher, "goal_status", map[string]any{"goal": goalStateFromDB(goal)})
		}
	default:
		// Unknown or custom command: pass through to agent as regular prompt
		agentSvc := s.app.CoderAgent
		submitErr := s.bgRunner.Submit(sessionID, func(bgCtx context.Context) (<-chan agent.AgentEvent, error) {
			return agentSvc.Run(bgCtx, sessionID, "/"+cmdName+" "+cmdArgs)
		})
		if submitErr != nil {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": submitErr.Error()})
			return
		}
		eventChan, unsubFn, _ := s.bgRunner.Subscribe(sessionID)
		s.streamSessionEvents(w, flusher, ctx, sessionID, unsubFn, eventChan)
		return
	}

	// For non-streaming commands, send completion event
	writeSSEEvent(w, flusher, "session", map[string]any{"sessionId": sessionID, "running": false})
}
