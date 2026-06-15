package permission

import (
	"errors"
	"path/filepath"
	"slices"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/google/uuid"
)

var ErrorPermissionDenied = errors.New("permission denied")

type CreatePermissionRequest struct {
	SessionID               string `json:"session_id"`
	ToolName                string `json:"tool_name"`
	Description             string `json:"description"`
	Action                  string `json:"action"`
	Params                  any    `json:"params"`
	Path                    string `json:"path"`
	RequireExplicitApproval bool   `json:"require_explicit_approval,omitempty"`
}

type PermissionRequest struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
}

type Service interface {
	pubsub.Suscriber[PermissionRequest]
	GrantPersistant(permission PermissionRequest)
	Grant(permission PermissionRequest)
	Deny(permission PermissionRequest)
	Request(opts CreatePermissionRequest) bool
	AutoApproveSession(sessionID string)
	RemoveAutoApproveSession(sessionID string)
	RequireExplicitApprovalSession(sessionID string)
	RemoveExplicitApprovalSession(sessionID string)
	SetGlobalAutoApprove(enabled bool)
	// RegisterSessionHandler installs a custom approval function for a specific session.
	// When set, this handler is called instead of the TUI dialog for that session.
	// The handler receives the full CreatePermissionRequest and returns true to approve.
	RegisterSessionHandler(sessionID string, handler func(req CreatePermissionRequest) bool)
	// UnregisterSessionHandler removes the custom handler for a session.
	UnregisterSessionHandler(sessionID string)
	// IsAutoApproveSession reports whether the given session is currently in
	// auto-approve ("auto mode") state.
	IsAutoApproveSession(sessionID string) bool
	// PendingRequests returns the permission requests that have been published
	// for a session and are still awaiting a response. Used to replay prompts to
	// clients (e.g. the Web UI) that reconnect to a session stream.
	PendingRequests(sessionID string) []PermissionRequest
}

type permissionService struct {
	*pubsub.Broker[PermissionRequest]

	mu                  sync.RWMutex
	sessionPermissions  []PermissionRequest
	pendingRequests     sync.Map
	pendingBySession    map[string]map[string]PermissionRequest
	autoApproveSessions []string
	explicitApproval    []string
	globalAutoApprove   bool
	sessionHandlers     map[string]func(req CreatePermissionRequest) bool
	sessionHandlersMu   sync.RWMutex
}

func (s *permissionService) GrantPersistant(permission PermissionRequest) {
	respCh, ok := s.pendingRequests.Load(permission.ID)
	if ok {
		respCh.(chan bool) <- true
	}
	s.removePending(permission.SessionID, permission.ID)
	s.mu.Lock()
	s.sessionPermissions = append(s.sessionPermissions, permission)
	s.mu.Unlock()
}

func (s *permissionService) Grant(permission PermissionRequest) {
	respCh, ok := s.pendingRequests.Load(permission.ID)
	if ok {
		respCh.(chan bool) <- true
	}
	s.removePending(permission.SessionID, permission.ID)
}

func (s *permissionService) Deny(permission PermissionRequest) {
	respCh, ok := s.pendingRequests.Load(permission.ID)
	if ok {
		respCh.(chan bool) <- false
	}
	s.removePending(permission.SessionID, permission.ID)
}

// trackPending records a published request so it can be replayed to clients
// that connect after it was emitted (e.g. a reconnecting Web UI stream).
func (s *permissionService) trackPending(p PermissionRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bySession, ok := s.pendingBySession[p.SessionID]
	if !ok {
		bySession = make(map[string]PermissionRequest)
		s.pendingBySession[p.SessionID] = bySession
	}
	bySession[p.ID] = p
}

// removePending clears a tracked request once it has been answered.
func (s *permissionService) removePending(sessionID, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if bySession, ok := s.pendingBySession[sessionID]; ok {
		delete(bySession, id)
		if len(bySession) == 0 {
			delete(s.pendingBySession, sessionID)
		}
	}
}

func (s *permissionService) Request(opts CreatePermissionRequest) bool {
	logging.Debug("Permission requested", "sessionID", opts.SessionID, "toolName", opts.ToolName, "action", opts.Action, "path", opts.Path)

	// Session handlers are checked first — they represent explicit per-session
	// overrides (e.g. ACP "ask" mode) and must take precedence over global flags.
	s.sessionHandlersMu.RLock()
	handler, hasHandler := s.sessionHandlers[opts.SessionID]
	s.sessionHandlersMu.RUnlock()
	if hasHandler {
		resp := handler(opts)
		logging.Debug("Permission result via session handler", "sessionID", opts.SessionID, "toolName", opts.ToolName, "approved", resp)
		return resp
	}

	s.mu.RLock()
	bypassSessionAutoApprove := opts.RequireExplicitApproval && slices.Contains(s.explicitApproval, opts.SessionID)
	autoApprove := !bypassSessionAutoApprove && slices.Contains(s.autoApproveSessions, opts.SessionID)
	globalAutoApprove := s.globalAutoApprove
	s.mu.RUnlock()
	if autoApprove {
		logging.Debug("Permission result via auto-approve session", "sessionID", opts.SessionID, "toolName", opts.ToolName, "approved", true)
		return true
	}
	if globalAutoApprove {
		logging.Debug("Permission result via global auto-approve", "sessionID", opts.SessionID, "toolName", opts.ToolName, "approved", true)
		return true
	}

	dir := filepath.Dir(opts.Path)
	if dir == "." {
		dir = config.WorkingDirectory()
	}
	permission := PermissionRequest{
		ID:          uuid.New().String(),
		Path:        dir,
		SessionID:   opts.SessionID,
		ToolName:    opts.ToolName,
		Description: opts.Description,
		Action:      opts.Action,
		Params:      opts.Params,
	}

	s.mu.RLock()
	for _, p := range s.sessionPermissions {
		if p.ToolName == permission.ToolName && p.Action == permission.Action && p.SessionID == permission.SessionID && p.Path == permission.Path {
			s.mu.RUnlock()
			return true
		}
	}
	s.mu.RUnlock()

	respCh := make(chan bool, 1)

	s.pendingRequests.Store(permission.ID, respCh)
	defer s.pendingRequests.Delete(permission.ID)

	s.trackPending(permission)
	defer s.removePending(permission.SessionID, permission.ID)

	s.Publish(pubsub.CreatedEvent, permission)

	// Wait for the response with a timeout
	resp := <-respCh
	logging.Debug("Permission result", "sessionID", opts.SessionID, "toolName", opts.ToolName, "approved", resp)
	return resp
}

func (s *permissionService) AutoApproveSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.autoApproveSessions, sessionID) {
		return
	}
	s.autoApproveSessions = append(s.autoApproveSessions, sessionID)
}

func (s *permissionService) RemoveAutoApproveSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoApproveSessions = slices.DeleteFunc(s.autoApproveSessions, func(id string) bool {
		return id == sessionID
	})
}

func (s *permissionService) IsAutoApproveSession(sessionID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Contains(s.autoApproveSessions, sessionID)
}

func (s *permissionService) PendingRequests(sessionID string) []PermissionRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bySession, ok := s.pendingBySession[sessionID]
	if !ok || len(bySession) == 0 {
		return nil
	}
	out := make([]PermissionRequest, 0, len(bySession))
	for _, p := range bySession {
		out = append(out, p)
	}
	return out
}

func (s *permissionService) RequireExplicitApprovalSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.explicitApproval, sessionID) {
		return
	}
	s.explicitApproval = append(s.explicitApproval, sessionID)
}

func (s *permissionService) RemoveExplicitApprovalSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.explicitApproval = slices.DeleteFunc(s.explicitApproval, func(id string) bool {
		return id == sessionID
	})
}

func (s *permissionService) SetGlobalAutoApprove(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.globalAutoApprove = enabled
}

func (s *permissionService) RegisterSessionHandler(sessionID string, handler func(req CreatePermissionRequest) bool) {
	s.sessionHandlersMu.Lock()
	defer s.sessionHandlersMu.Unlock()
	s.sessionHandlers[sessionID] = handler
}

func (s *permissionService) UnregisterSessionHandler(sessionID string) {
	s.sessionHandlersMu.Lock()
	defer s.sessionHandlersMu.Unlock()
	delete(s.sessionHandlers, sessionID)
}

func NewPermissionService() Service {
	return &permissionService{
		Broker:             pubsub.NewBroker[PermissionRequest](),
		sessionPermissions: make([]PermissionRequest, 0),
		pendingBySession:   make(map[string]map[string]PermissionRequest),
		sessionHandlers:    make(map[string]func(req CreatePermissionRequest) bool),
	}
}
