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
