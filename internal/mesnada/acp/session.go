package acp

import (
	"context"
	"sync"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

// ACPServerSession represents an active ACP session on the server side.
// Each session manages a conversation between a client and the Pando LLM agent.
type ACPServerSession struct {
	// ID is the unique session identifier
	ID acpsdk.SessionId

	// WorkDir is the working directory for this session
	WorkDir string

	// CreatedAt is when the session was created
	CreatedAt time.Time

	// agentConn is the agent-side connection for sending notifications to the client
	agentConn *acpsdk.AgentSideConnection

	// ctx is the context for this session (for cancellation)
	ctx context.Context

	// cancel is the cancellation function
	cancel context.CancelFunc

	// pandoSessionID is the internal Pando session ID
	pandoSessionID string

	// mu protects concurrent access to session state
	mu sync.Mutex
}

// NewACPServerSession creates a new ACP server session.
func NewACPServerSession(
	sessionID acpsdk.SessionId,
	workDir string,
	agentConn *acpsdk.AgentSideConnection,
	pandoSessionID string,
) *ACPServerSession {
	ctx, cancel := context.WithCancel(context.Background())

	return &ACPServerSession{
		ID:             sessionID,
		WorkDir:        workDir,
		CreatedAt:      time.Now(),
		agentConn:      agentConn,
		ctx:            ctx,
		cancel:         cancel,
		pandoSessionID: pandoSessionID,
	}
}

// Context returns the session's context.
func (s *ACPServerSession) Context() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx
}

// Cancel cancels the session context.
func (s *ACPServerSession) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

// SendUpdate sends a SessionUpdate notification to the client via the AgentSideConnection.
func (s *ACPServerSession) SendUpdate(update acpsdk.SessionUpdate) error {
	s.mu.Lock()
	agentConn := s.agentConn
	sessionID := s.ID
	s.mu.Unlock()

	if agentConn == nil {
		return nil // No agent connection, skip notification
	}

	notification := acpsdk.SessionNotification{
		SessionId: sessionID,
		Update:    update,
	}

	return agentConn.SessionUpdate(s.ctx, notification)
}

// PandoSessionID returns the internal Pando session ID.
func (s *ACPServerSession) PandoSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pandoSessionID
}
