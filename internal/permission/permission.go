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
	SessionID   string `json:"session_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
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
	SetGlobalAutoApprove(enabled bool)
	// RegisterSessionHandler installs a custom approval function for a specific session.
	// When set, this handler is called instead of the TUI dialog for that session.
	// The handler receives (sessionID, toolName, description string) and returns true to approve.
	RegisterSessionHandler(sessionID string, handler func(sessionID, toolName, description string) bool)
	// UnregisterSessionHandler removes the custom handler for a session.
	UnregisterSessionHandler(sessionID string)
}

type permissionService struct {
	*pubsub.Broker[PermissionRequest]

	sessionPermissions  []PermissionRequest
	pendingRequests     sync.Map
	autoApproveSessions []string
	globalAutoApprove   bool
	sessionHandlers     map[string]func(sessionID, toolName, description string) bool
	sessionHandlersMu   sync.RWMutex
}

func (s *permissionService) GrantPersistant(permission PermissionRequest) {
	respCh, ok := s.pendingRequests.Load(permission.ID)
	if ok {
		respCh.(chan bool) <- true
	}
	s.sessionPermissions = append(s.sessionPermissions, permission)
}

func (s *permissionService) Grant(permission PermissionRequest) {
	respCh, ok := s.pendingRequests.Load(permission.ID)
	if ok {
		respCh.(chan bool) <- true
	}
}

func (s *permissionService) Deny(permission PermissionRequest) {
	respCh, ok := s.pendingRequests.Load(permission.ID)
	if ok {
		respCh.(chan bool) <- false
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
		resp := handler(opts.SessionID, opts.ToolName, opts.Description)
		logging.Debug("Permission result via session handler", "sessionID", opts.SessionID, "toolName", opts.ToolName, "approved", resp)
		return resp
	}

	if slices.Contains(s.autoApproveSessions, opts.SessionID) {
		logging.Debug("Permission result via auto-approve session", "sessionID", opts.SessionID, "toolName", opts.ToolName, "approved", true)
		return true
	}
	if s.globalAutoApprove {
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

	for _, p := range s.sessionPermissions {
		if p.ToolName == permission.ToolName && p.Action == permission.Action && p.SessionID == permission.SessionID && p.Path == permission.Path {
			return true
		}
	}

	respCh := make(chan bool, 1)

	s.pendingRequests.Store(permission.ID, respCh)
	defer s.pendingRequests.Delete(permission.ID)

	s.Publish(pubsub.CreatedEvent, permission)

	// Wait for the response with a timeout
	resp := <-respCh
	logging.Debug("Permission result", "sessionID", opts.SessionID, "toolName", opts.ToolName, "approved", resp)
	return resp
}

func (s *permissionService) AutoApproveSession(sessionID string) {
	s.autoApproveSessions = append(s.autoApproveSessions, sessionID)
}

func (s *permissionService) RemoveAutoApproveSession(sessionID string) {
	s.autoApproveSessions = slices.DeleteFunc(s.autoApproveSessions, func(id string) bool {
		return id == sessionID
	})
}

func (s *permissionService) SetGlobalAutoApprove(enabled bool) {
	s.globalAutoApprove = enabled
}

func (s *permissionService) RegisterSessionHandler(sessionID string, handler func(sessionID, toolName, description string) bool) {
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
		sessionHandlers:    make(map[string]func(sessionID, toolName, description string) bool),
	}
}
