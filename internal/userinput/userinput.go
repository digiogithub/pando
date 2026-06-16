// Package userinput provides a blocking question/answer service that lets the
// agent ask the user structured, selectable questions mid-task and wait for the
// response. It mirrors the architecture of internal/permission: a tool blocks on
// a channel, a pubsub event is published, a frontend renders an overlay and
// responds, unblocking the tool. The difference is that userinput returns
// structured data (selected options) instead of a boolean.
package userinput

import (
	"sync"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/google/uuid"
)

// Option is a single selectable choice within a Question.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Question is a single question presented to the user, with selectable options.
type Question struct {
	ID          string   `json:"id"`
	Header      string   `json:"header"`
	Question    string   `json:"question"`
	MultiSelect bool     `json:"multi_select"`
	Options     []Option `json:"options"`
}

// QuestionRequest is the pubsub event payload broadcast to frontends.
type QuestionRequest struct {
	ID        string     `json:"id"`
	SessionID string     `json:"session_id"`
	Questions []Question `json:"questions"`
}

// Answer holds the user's response to a single question.
type Answer struct {
	QuestionID string   `json:"question_id"`
	Selected   []string `json:"selected"`
	OtherText  string   `json:"other_text,omitempty"`
}

// AskResponse is the result returned to the blocking caller.
type AskResponse struct {
	Answers   []Answer `json:"answers"`
	Cancelled bool     `json:"cancelled"`
}

// CreateAskRequest is the input to Ask.
type CreateAskRequest struct {
	SessionID string     `json:"session_id"`
	Questions []Question `json:"questions"`
}

// Service is the userinput service interface, parallel to permission.Service.
type Service interface {
	pubsub.Suscriber[QuestionRequest]
	// Ask publishes a question request and blocks until the user responds or
	// the request is cancelled.
	Ask(req CreateAskRequest) AskResponse
	// Respond delivers a response to a blocked Ask call.
	Respond(id string, resp AskResponse)
	// Cancel cancels a pending request, unblocking Ask with Cancelled=true.
	Cancel(id string)
	// PendingRequests returns the published requests for a session that are
	// still awaiting a response. Used to replay prompts to reconnecting clients.
	PendingRequests(sessionID string) []QuestionRequest
}

type userInputService struct {
	*pubsub.Broker[QuestionRequest]

	mu               sync.RWMutex
	pendingRequests  sync.Map // id -> chan AskResponse
	pendingBySession map[string]map[string]QuestionRequest
}

// trackPending records a published request so it can be replayed to clients
// that connect after it was emitted (e.g. a reconnecting Web UI stream).
func (s *userInputService) trackPending(r QuestionRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bySession, ok := s.pendingBySession[r.SessionID]
	if !ok {
		bySession = make(map[string]QuestionRequest)
		s.pendingBySession[r.SessionID] = bySession
	}
	bySession[r.ID] = r
}

// removePending clears a tracked request once it has been answered.
func (s *userInputService) removePending(sessionID, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if bySession, ok := s.pendingBySession[sessionID]; ok {
		delete(bySession, id)
		if len(bySession) == 0 {
			delete(s.pendingBySession, sessionID)
		}
	}
}

func (s *userInputService) Ask(req CreateAskRequest) AskResponse {
	logging.Debug("UserInput ask", "sessionID", req.SessionID, "questions", len(req.Questions))

	request := QuestionRequest{
		ID:        uuid.New().String(),
		SessionID: req.SessionID,
		Questions: req.Questions,
	}

	respCh := make(chan AskResponse, 1)
	s.pendingRequests.Store(request.ID, respCh)
	defer s.pendingRequests.Delete(request.ID)

	s.trackPending(request)
	defer s.removePending(request.SessionID, request.ID)

	s.Publish(pubsub.CreatedEvent, request)

	resp := <-respCh
	logging.Debug("UserInput response", "sessionID", req.SessionID, "cancelled", resp.Cancelled, "answers", len(resp.Answers))
	return resp
}

func (s *userInputService) Respond(id string, resp AskResponse) {
	if respCh, ok := s.pendingRequests.Load(id); ok {
		respCh.(chan AskResponse) <- resp
	}
}

func (s *userInputService) Cancel(id string) {
	s.Respond(id, AskResponse{Cancelled: true})
}

func (s *userInputService) PendingRequests(sessionID string) []QuestionRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bySession, ok := s.pendingBySession[sessionID]
	if !ok || len(bySession) == 0 {
		return nil
	}
	out := make([]QuestionRequest, 0, len(bySession))
	for _, r := range bySession {
		out = append(out, r)
	}
	return out
}

// NewService creates a new userinput service.
func NewService() Service {
	return &userInputService{
		Broker:           pubsub.NewBroker[QuestionRequest](),
		pendingBySession: make(map[string]map[string]QuestionRequest),
	}
}
