package api

import (
	"encoding/json"
	"net/http"

	"github.com/digiogithub/pando/internal/userinput"
)

// questionAnswer mirrors userinput.Answer in the request body.
type questionAnswer struct {
	QuestionID string   `json:"questionId"`
	Selected   []string `json:"selected"`
	OtherText  string   `json:"otherText"`
}

// questionRespondRequest is the body of POST /api/v1/questions/respond.
type questionRespondRequest struct {
	ID        string           `json:"id"`
	SessionID string           `json:"sessionId"`
	Answers   []questionAnswer `json:"answers"`
	Cancelled bool             `json:"cancelled"`
}

// handleQuestionRespond resolves a pending AskUserQuestion request that was
// surfaced to the Web UI via a question_request SSE event.
func (s *Server) handleQuestionRespond(w http.ResponseWriter, r *http.Request) {
	var req questionRespondRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	if req.Cancelled {
		s.app.UserInput.Cancel(req.ID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	answers := make([]userinput.Answer, 0, len(req.Answers))
	for _, a := range req.Answers {
		answers = append(answers, userinput.Answer{
			QuestionID: a.QuestionID,
			Selected:   a.Selected,
			OtherText:  a.OtherText,
		})
	}

	s.app.UserInput.Respond(req.ID, userinput.AskResponse{Answers: answers})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleSessionPending returns the permission and AskUserQuestion requests that
// are currently blocking a session.
//
// GET /api/v1/sessions/{id}/pending
//
// Pending requests are normally pushed over the chat SSE stream, but a client
// that is not attached to that stream (page reloaded, connection dropped, or a
// run that continued in the background after the initial stream ended) never
// sees them. The agent stays blocked inside the tool while the UI renders
// nothing, which also makes every later action look stuck — a model switch, for
// example, fails with "cannot change model while processing requests". This
// endpoint lets the UI poll for that state and render the dialog regardless of
// the stream.
func (s *Server) handleSessionPending(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}

	permissions := []map[string]interface{}{}
	for _, p := range s.app.Permissions.PendingRequests(sessionID) {
		permissions = append(permissions, map[string]interface{}{
			"id":          p.ID,
			"session_id":  p.SessionID,
			"tool_name":   p.ToolName,
			"description": p.Description,
			"action":      p.Action,
			"path":        p.Path,
			"params":      p.Params,
		})
	}

	questions := []map[string]interface{}{}
	for _, q := range s.app.UserInput.PendingRequests(sessionID) {
		questions = append(questions, map[string]interface{}{
			"id":         q.ID,
			"session_id": q.SessionID,
			"questions":  q.Questions,
		})
	}

	// "running" is the server's truth about the session: the Web UI polls this
	// endpoint anyway, so it doubles as the heartbeat that tells the client a run
	// is still alive after its SSE stream dropped (without polling the whole
	// session list).
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"permissions": permissions,
		"questions":   questions,
		"running":     s.bgRunner.IsBusy(sessionID),
	})
}
