package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
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

	_, err = s.app.Messages.Create(r.Context(), sess.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: req.Prompt}},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create message")
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

	_, err = s.app.Messages.Create(r.Context(), sess.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: req.Prompt}},
	})
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: {\"error\":\"failed to create message\"}\n\n")
		flusher.Flush()
		return
	}

	eventChan, err := s.app.CoderAgent.Run(r.Context(), sess.ID, req.Prompt)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: {\"error\":\"%s\"}\n\n", strings.ReplaceAll(err.Error(), "\"", "\\\""))
		flusher.Flush()
		return
	}

	for event := range eventChan {
		switch event.Type {
		case agent.AgentEventTypeResponse:
			for _, part := range event.Message.Parts {
				if text, ok := part.(message.TextContent); ok {
					escaped := strings.ReplaceAll(text.Text, "\n", "\\n")
					escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
					fmt.Fprintf(w, "event: content\ndata: {\"text\":\"%s\"}\n\n", escaped)
					flusher.Flush()
				}
			}
		case agent.AgentEventTypeError:
			fmt.Fprintf(w, "event: error\ndata: {\"error\":\"%s\"}\n\n", strings.ReplaceAll(event.Error.Error(), "\"", "\\\""))
			flusher.Flush()
			return
		}
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
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
