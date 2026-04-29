package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/toolmeta"
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

	fmt.Fprintf(w, "event: session\ndata: {\"sessionId\":\"%s\"}\n\n", sess.ID)
	flusher.Flush()

	eventChan, err := s.app.CoderAgent.Run(r.Context(), sess.ID, req.Prompt)
	if err != nil {
		writeSSEEvent(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	workDir := s.config.CWD

	// Track tool call state to guarantee start events precede results
	// and to carry input data across events (mirrors ACP prompt_handler).
	var mu sync.Mutex
	startedToolCalls := map[string]bool{}
	pendingInputs := map[string]string{}

	for event := range eventChan {
		switch event.Type {
		case agent.AgentEventTypeThinkingDelta:
			writeSSEEvent(w, flusher, "thinking_delta", map[string]string{"text": event.Delta})

		case agent.AgentEventTypeContentDelta:
			writeSSEEvent(w, flusher, "content_delta", map[string]string{"text": event.Delta})

		case agent.AgentEventTypeToolCall:
			if event.ToolCall == nil {
				continue
			}
			tc := event.ToolCall

			// TodoWrite → plan: emit plan_update instead of tool_call.
			if toolmeta.IsTodoWriteTool(tc.Name) {
				mu.Lock()
				pendingInputs[tc.ID] = tc.Input
				startedToolCalls[tc.ID] = true
				mu.Unlock()
				// Only emit once the full input is assembled.
				if tc.Finished {
					writeSSETodoWritePlan(w, flusher, tc.Input)
				}
				continue
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
				// Streaming in progress — send start if not yet sent.
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
				// Finished assembling — send start if needed, then in_progress.
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
					// Already started — send update to in_progress with final input.
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
				continue
			}
			tr := event.ToolResult

			// TodoWrite results are suppressed — plan_update already sent.
			if toolmeta.IsTodoWriteTool(tr.Name) {
				mu.Lock()
				delete(pendingInputs, tr.ToolCallID)
				delete(startedToolCalls, tr.ToolCallID)
				mu.Unlock()
				continue
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

			rawInput := toolmeta.ParseJSONInput(storedInput)
			kind := toolmeta.MapToolKind(tr.Name)
			title := toolmeta.DisplayTitle(tr.Name, rawInput, workDir)
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

			// Build rawOutput with structured metadata.
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

			// For bash tools, include terminal metadata.
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

			// For edit tools, include diff metadata.
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

			writeSSEEvent(w, flusher, "tool_result", resultPayload)

		case agent.AgentEventTypeTodosUpdated:
			if len(event.Todos) > 0 {
				writeSSEEvent(w, flusher, "todos_update", map[string]interface{}{
					"session_id": event.SessionID,
					"todos":      event.Todos,
				})
			}

		case agent.AgentEventTypeResponse:
			// Final response — content already streamed via content_delta events.

		case agent.AgentEventTypeError:
			writeSSEEvent(w, flusher, "error", map[string]string{"error": event.Error.Error()})
			return
		}
	}

	writeSSEEvent(w, flusher, "done", map[string]string{})
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

func (s *Server) getOrCreateSession(ctx context.Context, sessionID string) (*session.Session, error) {
	if sessionID != "" {
		sess, err := s.app.Sessions.Get(ctx, sessionID)
		if err == nil {
			return &sess, nil
		}
	}

	sess, err := s.app.Sessions.Create(ctx, "New Chat")
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &sess, nil
}
