package acp

import (
	"context"
	"fmt"
	"log"
	"sync"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/digiogithub/pando/internal/message"
	"github.com/google/uuid"
)

// AgentEventType represents the type of agent event
type AgentEventType string

const (
	AgentEventTypeError         AgentEventType = "error"
	AgentEventTypeResponse      AgentEventType = "response"
	AgentEventTypeSummarize     AgentEventType = "summarize"
	AgentEventTypeContentDelta  AgentEventType = "content_delta"
	AgentEventTypeThinkingDelta AgentEventType = "thinking_delta"
	AgentEventTypeToolCall      AgentEventType = "tool_call"
	AgentEventTypeToolResult    AgentEventType = "tool_result"
)

// AgentEvent represents an event from the agent service
type AgentEvent struct {
	Type       AgentEventType
	Message    message.Message
	Error      error
	Delta      string
	ToolCall   *message.ToolCall
	ToolResult *message.ToolResult
}

// ACPModelInfo holds minimal model metadata for ACP responses.
// Defined here to avoid importing internal/llm/models from this package.
type ACPModelInfo struct {
	ID   string
	Name string
}

// AgentService defines the interface for interacting with Pando's LLM agent.
// This is intentionally minimal to avoid import cycles.
type AgentService interface {
	Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error)
	Cancel(sessionID string)
	// CurrentModelID returns the ID of the currently active model.
	CurrentModelID() string
	// AvailableModels returns the list of available models with name metadata.
	AvailableModels() []ACPModelInfo
	// SetModelOverride temporarily changes the active model (in-memory only).
	// Pass empty string to clear any previous override.
	SetModelOverride(modelID string) error
}

// ACPSessionInfo is a minimal session descriptor used by the ACP layer.
// Using a local struct avoids importing the session package and breaking the
// session→llm/tools→acp import cycle.
type ACPSessionInfo struct {
	ID        string
	Title     string
	UpdatedAt int64
}

// SessionService defines the minimal interface needed by the ACP agent.
// Using a narrow interface avoids the session→llm/tools→acp import cycle.
type SessionService interface {
	CreateSession(ctx context.Context, title string) (string, error)
	GetSession(ctx context.Context, id string) (ACPSessionInfo, error)
	ListSessions(ctx context.Context) ([]ACPSessionInfo, error)
}

// PandoACPAgent implements the ACP Agent interface.
// It allows external ACP clients to connect to Pando and use its capabilities.
type PandoACPAgent struct {
	// version is the agent version string
	version string

	// capabilities defines what this agent offers to clients
	capabilities acpsdk.AgentCapabilities

	// logger for agent events
	logger *log.Logger

	// workDir is the base working directory for file operations
	workDir string

	// sessions maps ACP session IDs to session objects
	sessions map[acpsdk.SessionId]*ACPServerSession

	// sessionsMu protects concurrent access to sessions map
	sessionsMu sync.RWMutex

	// agentService is the Pando LLM agent service
	agentService AgentService

	// sessionService is the Pando session service (minimal interface)
	sessionService SessionService

	// conn is the AgentSideConnection used to stream updates to the client.
	// Set by SetConnection() after the transport creates it.
	conn *acpsdk.AgentSideConnection
}

// NewPandoACPAgent creates a new ACP agent instance.
func NewPandoACPAgent(
	version string,
	workDir string,
	logger *log.Logger,
	agentService AgentService,
	sessionService SessionService,
) *PandoACPAgent {
	if logger == nil {
		logger = log.Default()
	}

	return &PandoACPAgent{
		version:        version,
		workDir:        workDir,
		logger:         logger,
		sessions:       make(map[acpsdk.SessionId]*ACPServerSession),
		agentService:   agentService,
		sessionService: sessionService,
		capabilities: acpsdk.AgentCapabilities{
			LoadSession: true,
			McpCapabilities: acpsdk.McpCapabilities{
				Http: false,
				Sse:  false,
			},
			PromptCapabilities: acpsdk.PromptCapabilities{
				Audio:           false,
				EmbeddedContext: false,
				Image:           false,
			},
		},
	}
}

// Initialize handles the initialization handshake from an ACP client.
// This is the first method called when a client connects.
func (a *PandoACPAgent) Initialize(ctx context.Context, req acpsdk.InitializeRequest) (acpsdk.InitializeResponse, error) {
	a.logger.Printf("[ACP AGENT] Initialize request from client: %+v", req.ClientInfo)
	a.logger.Printf("[ACP AGENT] Client protocol version: %v", req.ProtocolVersion)

	agentInfo := &acpsdk.Implementation{
		Name:    "pando",
		Version: a.version,
	}

	result := acpsdk.InitializeResponse{
		ProtocolVersion:   1,
		AgentInfo:         agentInfo,
		AgentCapabilities: a.capabilities,
		AuthMethods:       []acpsdk.AuthMethod{},
	}

	a.logger.Printf("[ACP AGENT] Initialize successful: %+v", result)
	return result, nil
}

// Authenticate handles authentication requests (not implemented yet).
func (a *PandoACPAgent) Authenticate(ctx context.Context, req acpsdk.AuthenticateRequest) (acpsdk.AuthenticateResponse, error) {
	a.logger.Printf("[ACP AGENT] Authenticate called (not implemented)")
	return acpsdk.AuthenticateResponse{}, fmt.Errorf("authentication not implemented")
}

// Cancel handles cancellation notifications.
func (a *PandoACPAgent) Cancel(ctx context.Context, params acpsdk.CancelNotification) error {
	a.logger.Printf("[ACP AGENT] Cancel notification received for session: %s", params.SessionId)

	a.sessionsMu.RLock()
	acpSession, exists := a.sessions[params.SessionId]
	a.sessionsMu.RUnlock()

	if !exists {
		a.logger.Printf("[ACP AGENT] Session not found for cancellation: %s", params.SessionId)
		return nil
	}

	acpSession.Cancel()
	a.agentService.Cancel(acpSession.PandoSessionID())

	a.logger.Printf("[ACP AGENT] Session cancelled: %s", params.SessionId)
	return nil
}

// NewSession handles new session creation requests.
func (a *PandoACPAgent) NewSession(ctx context.Context, req acpsdk.NewSessionRequest) (acpsdk.NewSessionResponse, error) {
	a.logger.Printf("[ACP AGENT] NewSession request: WorkDir=%v", req.Cwd)

	sessionID := acpsdk.SessionId(uuid.New().String())

	workDir := a.workDir
	if req.Cwd != "" {
		workDir = req.Cwd
	}

	// Create internal Pando session via the minimal SessionService interface
	pandoSessionID, err := a.sessionService.CreateSession(ctx, fmt.Sprintf("ACP Session %s", sessionID))
	if err != nil {
		a.logger.Printf("[ACP AGENT] Failed to create Pando session: %v", err)
		return acpsdk.NewSessionResponse{}, fmt.Errorf("failed to create session: %w", err)
	}

	acpSession := NewACPServerSession(
		sessionID,
		workDir,
		a.conn, // AgentSideConnection for streaming updates
		pandoSessionID,
	)

	a.sessionsMu.Lock()
	a.sessions[sessionID] = acpSession
	a.sessionsMu.Unlock()

	a.logger.Printf("[ACP AGENT] NewSession created: SessionID=%s, PandoSessionID=%s, WorkDir=%s",
		sessionID, pandoSessionID, workDir)

	return acpsdk.NewSessionResponse{
		SessionId: sessionID,
		Modes:     buildSessionModeState("code"),
		Models:    buildSessionModelState(a.agentService),
	}, nil
}

// Prompt handles prompt requests from the client.
func (a *PandoACPAgent) Prompt(ctx context.Context, req acpsdk.PromptRequest) (acpsdk.PromptResponse, error) {
	a.logger.Printf("[ACP AGENT] Prompt request: SessionID=%s", req.SessionId)

	a.sessionsMu.RLock()
	acpSession, exists := a.sessions[req.SessionId]
	a.sessionsMu.RUnlock()

	if !exists {
		a.logger.Printf("[ACP AGENT] Session not found: %s", req.SessionId)
		return acpsdk.PromptResponse{}, fmt.Errorf("session not found: %s", req.SessionId)
	}

	promptText, err := a.extractPromptText(req.Prompt)
	if err != nil {
		a.logger.Printf("[ACP AGENT] Failed to extract prompt text: %v", err)
		return acpsdk.PromptResponse{}, fmt.Errorf("invalid prompt: %w", err)
	}

	a.logger.Printf("[ACP AGENT] Processing prompt (length=%d) for session %s",
		len(promptText), req.SessionId)

	stopReason, err := a.processPromptWithAgent(ctx, acpSession, promptText)
	if err != nil {
		a.logger.Printf("[ACP AGENT] Prompt processing failed: %v", err)
		return acpsdk.PromptResponse{}, fmt.Errorf("prompt processing failed: %w", err)
	}

	a.logger.Printf("[ACP AGENT] Prompt completed: SessionID=%s, StopReason=%s",
		req.SessionId, stopReason)

	return acpsdk.PromptResponse{
		StopReason: stopReason,
	}, nil
}

// extractPromptText extracts the text content from a Prompt (slice of ContentBlocks).
func (a *PandoACPAgent) extractPromptText(prompt []acpsdk.ContentBlock) (string, error) {
	if len(prompt) == 0 {
		return "", fmt.Errorf("empty prompt content")
	}

	var textParts []string
	for _, block := range prompt {
		if block.Text != nil {
			textParts = append(textParts, block.Text.Text)
		}
	}

	if len(textParts) == 0 {
		return "", fmt.Errorf("no text content in prompt")
	}

	return joinTextParts(textParts), nil
}

// joinTextParts joins multiple text parts with newlines.
func joinTextParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "\n"
		}
		result += part
	}
	return result
}

// processPromptWithAgent processes a prompt using the Pando LLM agent.
func (a *PandoACPAgent) processPromptWithAgent(
	ctx context.Context,
	acpSession *ACPServerSession,
	promptText string,
) (acpsdk.StopReason, error) {
	pandoSessionID := acpSession.PandoSessionID()

	eventChan, err := a.agentService.Run(ctx, pandoSessionID, promptText)
	if err != nil {
		return "", fmt.Errorf("failed to start agent: %w", err)
	}

	var finalStopReason acpsdk.StopReason
	for event := range eventChan {
		switch event.Type {
		case AgentEventTypeError:
			if event.Error != nil {
				a.logger.Printf("[ACP AGENT] Agent error: %v", event.Error)
				return acpsdk.StopReasonRefusal, event.Error
			}

		case AgentEventTypeResponse:
			err := a.processAgentResponse(acpSession, event.Message)
			if err != nil {
				a.logger.Printf("[ACP AGENT] Failed to process response: %v", err)
				return acpsdk.StopReasonRefusal, err
			}
			finalStopReason = a.mapFinishReasonToStopReason(event.Message.FinishReason())

		case AgentEventTypeContentDelta:
			if event.Delta != "" {
				if err := acpSession.SendUpdate(acpsdk.UpdateAgentMessageText(event.Delta)); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send content delta: %v", err)
				}
			}

		case AgentEventTypeThinkingDelta:
			if event.Delta != "" {
				if err := acpSession.SendUpdate(acpsdk.UpdateAgentThoughtText(event.Delta)); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send thinking delta: %v", err)
				}
			}

		case AgentEventTypeToolCall:
			if event.ToolCall != nil {
				tc := event.ToolCall
				update := acpsdk.StartToolCall(
					acpsdk.ToolCallId(tc.ID),
					tc.Name,
					acpsdk.WithStartKind(mapToolKind(tc.Name)),
					acpsdk.WithStartStatus(acpsdk.ToolCallStatusInProgress),
					acpsdk.WithStartRawInput(tc.Input),
				)
				if err := acpSession.SendUpdate(update); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send tool call: %v", err)
				}
			}

		case AgentEventTypeToolResult:
			if event.ToolResult != nil {
				tr := event.ToolResult
				status := acpsdk.ToolCallStatusCompleted
				if tr.IsError {
					status = acpsdk.ToolCallStatusFailed
				}
				update := acpsdk.UpdateToolCall(
					acpsdk.ToolCallId(tr.ToolCallID),
					acpsdk.WithUpdateStatus(status),
					acpsdk.WithUpdateRawOutput(tr.Content),
				)
				if err := acpSession.SendUpdate(update); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send tool result: %v", err)
				}
			}

		case AgentEventTypeSummarize:
			a.logger.Printf("[ACP AGENT] Summarize event")
		}
	}

	if finalStopReason == "" {
		finalStopReason = acpsdk.StopReasonEndTurn
	}

	return finalStopReason, nil
}

// processAgentResponse processes an agent response message and sends updates to the client.
func (a *PandoACPAgent) processAgentResponse(
	acpSession *ACPServerSession,
	msg message.Message,
) error {
	if content := msg.Content(); content.String() != "" {
		update := acpsdk.UpdateAgentMessageText(content.String())
		if err := acpSession.SendUpdate(update); err != nil {
			a.logger.Printf("[ACP AGENT] Failed to send message update: %v", err)
		}
	}

	if reasoning := msg.ReasoningContent(); reasoning.String() != "" {
		update := acpsdk.UpdateAgentThoughtText(reasoning.String())
		if err := acpSession.SendUpdate(update); err != nil {
			a.logger.Printf("[ACP AGENT] Failed to send thought update: %v", err)
		}
	}

	for _, toolCall := range msg.ToolCalls() {
		update := acpsdk.StartToolCall(
			acpsdk.ToolCallId(toolCall.ID),
			toolCall.Name,
		)
		if err := acpSession.SendUpdate(update); err != nil {
			a.logger.Printf("[ACP AGENT] Failed to send tool call update: %v", err)
		}
	}

	return nil
}

// mapFinishReasonToStopReason maps Pando finish reasons to ACP stop reasons.
func (a *PandoACPAgent) mapFinishReasonToStopReason(finishReason message.FinishReason) acpsdk.StopReason {
	switch finishReason {
	case message.FinishReasonEndTurn:
		return acpsdk.StopReasonEndTurn
	case message.FinishReasonMaxTokens:
		return acpsdk.StopReasonMaxTokens
	case message.FinishReasonCanceled:
		return acpsdk.StopReasonCancelled
	case message.FinishReasonPermissionDenied:
		return acpsdk.StopReasonRefusal
	default:
		return acpsdk.StopReasonEndTurn
	}
}

// SetSessionMode handles session mode changes.
// Stores the mode in session state for use in Phase 5.
func (a *PandoACPAgent) SetSessionMode(ctx context.Context, req acpsdk.SetSessionModeRequest) (acpsdk.SetSessionModeResponse, error) {
	a.logger.Printf("[ACP AGENT] SetSessionMode: SessionID=%s, ModeID=%s", req.SessionId, req.ModeId)

	a.sessionsMu.RLock()
	acpSession, exists := a.sessions[req.SessionId]
	a.sessionsMu.RUnlock()

	if !exists {
		return acpsdk.SetSessionModeResponse{}, fmt.Errorf("session not found: %s", req.SessionId)
	}

	acpSession.SetMode(string(req.ModeId))
	a.logger.Printf("[ACP AGENT] Session mode set: SessionID=%s, Mode=%s", req.SessionId, req.ModeId)

	return acpsdk.SetSessionModeResponse{}, nil
}

// SetConnection stores a reference to the AgentSideConnection so the agent can
// stream session updates back to the client.  Called by transport_stdio.go
// immediately after NewAgentSideConnection() returns.
func (a *PandoACPAgent) SetConnection(conn *acpsdk.AgentSideConnection) {
	a.conn = conn
}

// GetVersion returns the agent version.
func (a *PandoACPAgent) GetVersion() string {
	return a.version
}

// GetCapabilities returns the agent capabilities.
func (a *PandoACPAgent) GetCapabilities() acpsdk.AgentCapabilities {
	return a.capabilities
}

// LoadSession implements AgentLoader.
// It validates the requested session exists in Pando and registers an ACP
// session mapping so subsequent Prompt calls can find it.
func (a *PandoACPAgent) LoadSession(ctx context.Context, req acpsdk.LoadSessionRequest) (acpsdk.LoadSessionResponse, error) {
	a.logger.Printf("[ACP AGENT] LoadSession: SessionID=%s, Cwd=%s", req.SessionId, req.Cwd)

	_, err := a.sessionService.GetSession(ctx, string(req.SessionId))
	if err != nil {
		a.logger.Printf("[ACP AGENT] LoadSession: session not found: %v", err)
		return acpsdk.LoadSessionResponse{}, fmt.Errorf("session not found: %w", err)
	}

	workDir := a.workDir
	if req.Cwd != "" {
		workDir = req.Cwd
	}

	acpSession := NewACPServerSession(req.SessionId, workDir, a.conn, string(req.SessionId))

	a.sessionsMu.Lock()
	a.sessions[req.SessionId] = acpSession
	a.sessionsMu.Unlock()

	a.logger.Printf("[ACP AGENT] LoadSession: registered session %s", req.SessionId)
	return acpsdk.LoadSessionResponse{
		Modes:  buildSessionModeState("code"),
		Models: buildSessionModelState(a.agentService),
	}, nil
}

// SetSessionModel implements AgentExperimental.
// It stores the requested model on the ACP session for use in future prompts.
func (a *PandoACPAgent) SetSessionModel(ctx context.Context, req acpsdk.SetSessionModelRequest) (acpsdk.SetSessionModelResponse, error) {
	a.logger.Printf("[ACP AGENT] SetSessionModel: SessionID=%s, ModelID=%s", req.SessionId, req.ModelId)

	a.sessionsMu.RLock()
	acpSession, exists := a.sessions[req.SessionId]
	a.sessionsMu.RUnlock()

	if !exists {
		return acpsdk.SetSessionModelResponse{}, fmt.Errorf("session not found: %s", req.SessionId)
	}

	acpSession.SetModel(string(req.ModelId))
	a.logger.Printf("[ACP AGENT] SetSessionModel: model set to %s for session %s", req.ModelId, req.SessionId)
	return acpsdk.SetSessionModelResponse{}, nil
}

// mapToolKind maps a Pando tool name to the corresponding ACP ToolKind.
func mapToolKind(toolName string) acpsdk.ToolKind {
	switch toolName {
	case "bash", "execute_command":
		return acpsdk.ToolKindExecute
	case "edit", "write", "multiedit":
		return acpsdk.ToolKindEdit
	case "read":
		return acpsdk.ToolKindRead
	case "glob", "grep":
		return acpsdk.ToolKindSearch
	case "web_search", "web_fetch", "fetch":
		return acpsdk.ToolKindFetch
	default:
		return acpsdk.ToolKindOther
	}
}
