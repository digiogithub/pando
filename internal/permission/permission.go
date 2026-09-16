package permission

import (
	"context"
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
	// RequestWithContext is Request bound to the caller's context. When ctx
	// ends before an answer arrives, the request fails closed (returns false)
	// instead of blocking the calling tool's goroutine forever. Tools should
	// prefer it and pass their own ctx; Request is the context.Background()
	// shorthand kept for the call sites that have none.
	RequestWithContext(ctx context.Context, opts CreatePermissionRequest) bool
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

// Request is RequestWithContext with a background context: it never gives up
// on its own. Kept so the existing call sites that have no context of their
// own are untouched.
func (s *permissionService) Request(opts CreatePermissionRequest) bool {
	return s.RequestWithContext(context.Background(), opts)
}

func (s *permissionService) RequestWithContext(ctx context.Context, opts CreatePermissionRequest) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	logging.Debug("Permission requested", "sessionID", opts.SessionID, "toolName", opts.ToolName, "action", opts.Action, "path", opts.Path)

	// Session handlers are checked first — they represent explicit per-session
	// overrides (e.g. ACP "ask" mode) and must take precedence over global flags.
	s.sessionHandlersMu.RLock()
	handler, hasHandler := s.sessionHandlers[opts.SessionID]
	s.sessionHandlersMu.RUnlock()
	if hasHandler {
		// The handler blocks (the AG-UI adapter suspends the run on a
		// synthetic tool call and waits for the browser), so it runs on its
		// own goroutine: that is what lets an ended context fail closed here
		// instead of stranding the calling tool. The goroutine is not leaked
		// -- every handler has its own timeout and returns into the buffered
		// channel, whether or not anyone is still listening.
		handlerCh := make(chan bool, 1)
		go func() { handlerCh <- handler(opts) }()
		select {
		case resp := <-handlerCh:
			logging.Debug("Permission result via session handler", "sessionID", opts.SessionID, "toolName", opts.ToolName, "approved", resp)
			return resp
		case <-ctx.Done():
			logging.Warn("Permission denied: the caller's context ended before the session handler answered",
				"sessionID", opts.SessionID, "toolName", opts.ToolName, "action", opts.Action, "error", ctx.Err())
			return false
		}
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

	// Wait for the answer, or for the caller to give up. There is deliberately
	// no built-in deadline: a TUI prompt may legitimately sit unanswered for as
	// long as the user needs. What must never happen is an unanswerable prompt
	// outliving the run that raised it, which is what ctx closes off.
	select {
	case resp := <-respCh:
		logging.Debug("Permission result", "sessionID", opts.SessionID, "toolName", opts.ToolName, "approved", resp)
		return resp
	case <-ctx.Done():
		logging.Warn("Permission denied: the caller's context ended before the request was answered",
			"sessionID", opts.SessionID, "toolName", opts.ToolName, "action", opts.Action, "error", ctx.Err())
		return false
	}
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
