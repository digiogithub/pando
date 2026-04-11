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

	// mode is the current session mode (set via SetSessionMode)
	mode string

	// model is the model ID requested by the client (set via SetSessionModel)
	model string

	// persona is the persona name requested by the client (set via SetSessionPersona)
	persona string

	// variant is the session variant (reserved for future use)
	variant string

	// mu protects concurrent access to session state
	mu sync.Mutex
}

// SetAgentConnection updates the agent-side connection used to stream updates.
func (s *ACPServerSession) SetAgentConnection(conn *acpsdk.AgentSideConnection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentConn = conn
}

// SetWorkDir updates the working directory associated with this session.
func (s *ACPServerSession) SetWorkDir(workDir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.WorkDir = workDir
}

// HasAgentConnection reports whether the session has an attached agent connection.
func (s *ACPServerSession) HasAgentConnection() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentConn != nil
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

// SetMode stores the session mode.
func (s *ACPServerSession) SetMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
}

// Mode returns the current session mode.
func (s *ACPServerSession) Mode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

// SetModel stores the requested model ID.
func (s *ACPServerSession) SetModel(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = model
}

// Model returns the current model ID.
func (s *ACPServerSession) Model() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.model
}

// SetPersona stores the requested persona name.
func (s *ACPServerSession) SetPersona(persona string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persona = persona
}

// Persona returns the current persona name.
func (s *ACPServerSession) Persona() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persona
}

// SetVariant stores the session variant.
func (s *ACPServerSession) SetVariant(variant string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.variant = variant
}

// Variant returns the session variant.
func (s *ACPServerSession) Variant() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.variant
}
