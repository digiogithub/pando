package acp

import (
	"context"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/design"
	acpsdk "github.com/madeindigio/acp-go-sdk"
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

	// runCtx is the context for in-flight prompt/goal execution cancellation.
	runCtx context.Context

	// runCancel cancels in-flight prompt/goal execution.
	runCancel context.CancelFunc

	// pandoSessionID is the internal Pando session ID
	pandoSessionID string

	// updateMu serializes outbound session/update notifications so tool starts
	// are observed by the client before follow-up updates for the same tool call.
	updateMu sync.Mutex

	// mode is the current session mode (set via SetSessionMode)
	mode string

	// model is the model ID requested by the client (set via SetSessionModel)
	model string

	// persona is the persona name requested by the client (set via SetSessionPersona)
	persona string

	// cleanMode disables all extra system/prompt-builder instructions for the session.
	cleanMode bool

	// askPermission controls whether tool calls require ACP approval prompts.
	askPermission bool

	// thinkingStreamMode controls how reasoning/thinking is streamed to ACP clients.
	thinkingStreamMode string

	// reasoningEffort stores the session-scoped ACP reasoning_effort override.
	reasoningEffort string

	// thinkingMode stores the session-scoped ACP thinking_mode override.
	thinkingMode string

	// permissionConfigured reports whether askPermission was explicitly chosen
	// through config options instead of being inherited from legacy mode defaults.
	permissionConfigured bool

	// variant is the session variant (reserved for future use)
	variant string

	// goal stores the latest known goal status for the session.
	goal *GoalStateUpdate

	// goalCancel cancels the currently executing goal iteration, if any.
	goalCancel context.CancelFunc

	// designCancel stops the session-scoped design event subscription.
	designCancel context.CancelFunc

	// designAutoOpen deduplicates best-effort browser launches for this session's
	// design-created events.
	designAutoOpen *design.BrowserAutoOpener

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
	runCtx, runCancel := context.WithCancel(context.Background())

	return &ACPServerSession{
		ID:                 sessionID,
		WorkDir:            workDir,
		CreatedAt:          time.Now(),
		agentConn:          agentConn,
		runCtx:             runCtx,
		runCancel:          runCancel,
		pandoSessionID:     pandoSessionID,
		thinkingStreamMode: defaultACPThinkingStreamMode,
	}
}

// Context returns the session's execution context.
func (s *ACPServerSession) Context() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runCtx
}

// Cancel cancels in-flight session work while keeping ACP transport available
// for any final updates or future prompts on the same session.
func (s *ACPServerSession) Cancel() {
	s.mu.Lock()
	goalCancel := s.goalCancel
	s.goalCancel = nil
	runCancel := s.runCancel
	s.runCtx, s.runCancel = context.WithCancel(context.Background())
	s.mu.Unlock()
	if goalCancel != nil {
		goalCancel()
	}
	if runCancel != nil {
		runCancel()
	}
}

// SendUpdate sends a SessionUpdate notification to the client via the AgentSideConnection.
func (s *ACPServerSession) SendUpdate(update acpsdk.SessionUpdate) (err error) {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			err = nil
		}
	}()

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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return agentConn.SessionUpdate(ctx, notification)
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

// SetCleanMode stores whether the session should run in clean mode.
func (s *ACPServerSession) SetCleanMode(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanMode = enabled
}

// CleanMode reports whether the session should run in clean mode.
func (s *ACPServerSession) CleanMode() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cleanMode
}

// SetAskPermission stores whether tool calls require explicit approval.
func (s *ACPServerSession) SetAskPermission(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.askPermission = enabled
}

// AskPermission returns whether tool calls require explicit approval.
func (s *ACPServerSession) AskPermission() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.askPermission
}

// SetThinkingStreamMode stores how reasoning/thinking should be streamed to ACP clients.
func (s *ACPServerSession) SetThinkingStreamMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.thinkingStreamMode = normalizeThinkingStreamMode(mode)
}

// ThinkingStreamMode returns the configured ACP thinking visibility mode.
func (s *ACPServerSession) ThinkingStreamMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return normalizeThinkingStreamMode(s.thinkingStreamMode)
}

// SetReasoningEffort stores the session-scoped ACP reasoning_effort override.
func (s *ACPServerSession) SetReasoningEffort(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reasoningEffort = normalizeReasoningEffortValue(value)
}

// ReasoningEffort returns the stored ACP reasoning_effort override, if any.
func (s *ACPServerSession) ReasoningEffort() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return normalizeReasoningEffortValue(s.reasoningEffort)
}

// SetThinkingMode stores the session-scoped ACP thinking_mode override.
func (s *ACPServerSession) SetThinkingMode(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.thinkingMode = normalizeThinkingModeValue(value)
}

// ThinkingMode returns the stored ACP thinking_mode override, if any.
func (s *ACPServerSession) ThinkingMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return normalizeThinkingModeValue(s.thinkingMode)
}

// SetPermissionConfigured records whether askPermission was explicitly selected.
func (s *ACPServerSession) SetPermissionConfigured(configured bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.permissionConfigured = configured
}

// PermissionConfigured reports whether askPermission was explicitly selected.
func (s *ACPServerSession) PermissionConfigured() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.permissionConfigured
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

// SetGoal stores the latest goal state snapshot.
func (s *ACPServerSession) SetGoal(goal *GoalStateUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.goal = goal
}

// Goal returns the latest goal state snapshot.
func (s *ACPServerSession) Goal() *GoalStateUpdate {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.goal == nil {
		return nil
	}
	goal := *s.goal
	return &goal
}

// SetGoalCancel stores the cancel function for the running goal iteration.
func (s *ACPServerSession) SetGoalCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.goalCancel = cancel
}

// CancelGoalExecution cancels the running goal iteration if one exists.
func (s *ACPServerSession) CancelGoalExecution() {
	s.mu.Lock()
	cancel := s.goalCancel
	s.goalCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ClearGoalCancel removes the stored goal cancel function without cancelling it.
func (s *ACPServerSession) ClearGoalCancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.goalCancel = nil
}

// DesignAutoOpener returns the session-scoped browser auto-opener, creating it
// on first use.
func (s *ACPServerSession) DesignAutoOpener() *design.BrowserAutoOpener {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.designAutoOpen == nil {
		s.designAutoOpen = design.NewBrowserAutoOpener()
	}
	return s.designAutoOpen
}

// SetDesignCancel replaces the session-scoped design subscription cancel
// function, stopping any earlier subscription first.
func (s *ACPServerSession) SetDesignCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	previous := s.designCancel
	s.designCancel = cancel
	s.mu.Unlock()
	if previous != nil {
		previous()
	}
}

// CancelDesignSubscription stops the design event stream for this session.
func (s *ACPServerSession) CancelDesignSubscription() {
	s.mu.Lock()
	cancel := s.designCancel
	s.designCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
