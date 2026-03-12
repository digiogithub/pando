package acp

import (
	"context"
	"fmt"
	"log"
	"sync"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
	"github.com/google/uuid"
)

// AgentEventType represents the type of agent event
type AgentEventType string

const (
	AgentEventTypeError     AgentEventType = "error"
	AgentEventTypeResponse  AgentEventType = "response"
	AgentEventTypeSummarize AgentEventType = "summarize"
)

// AgentEvent represents an event from the agent service
type AgentEvent struct {
	Type    AgentEventType
	Message message.Message
	Error   error
}

// AgentService defines the interface for interacting with Pando's LLM agent
type AgentService interface {
	Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error)
	Cancel(sessionID string)
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

	// sessionService is the Pando session service
	sessionService session.Service

	// messageService is the Pando message service
	messageService message.Service
}

// NewPandoACPAgent creates a new ACP agent instance.
func NewPandoACPAgent(
	version string,
	workDir string,
	logger *log.Logger,
	agentService AgentService,
	sessionService session.Service,
	messageService message.Service,
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
		messageService: messageService,
		capabilities: acpsdk.AgentCapabilities{
			LoadSession: false,
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

	// Log the protocol version
	a.logger.Printf("[ACP AGENT] Client protocol version: %v", req.ProtocolVersion)

	// Build agent info
	agentInfo := &acpsdk.Implementation{
		Name:    "pando",
		Version: a.version,
	}

	result := acpsdk.InitializeResponse{
		ProtocolVersion:   req.ProtocolVersion,
		AgentInfo:         agentInfo,
		AgentCapabilities: a.capabilities,
		AuthMethods:       []acpsdk.AuthMethod{}, // No auth required for now
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

	// Get and cancel the session
	a.sessionsMu.RLock()
	acpSession, exists := a.sessions[params.SessionId]
	a.sessionsMu.RUnlock()

	if !exists {
		a.logger.Printf("[ACP AGENT] Session not found for cancellation: %s", params.SessionId)
		return nil // Not an error - session might already be cleaned up
	}

	// Cancel the session context
	acpSession.Cancel()

	// Also cancel the Pando agent processing
	a.agentService.Cancel(acpSession.PandoSessionID())

	a.logger.Printf("[ACP AGENT] Session cancelled: %s", params.SessionId)
	return nil
}

// NewSession handles new session creation requests.
func (a *PandoACPAgent) NewSession(ctx context.Context, req acpsdk.NewSessionRequest) (acpsdk.NewSessionResponse, error) {
	a.logger.Printf("[ACP AGENT] NewSession request: WorkDir=%v", req.Cwd)

	// Generate unique session ID
	sessionID := acpsdk.SessionId(uuid.New().String())

	// Determine working directory
	workDir := a.workDir
	if req.Cwd != "" {
		workDir = req.Cwd
	}

	// Create internal Pando session
	pandoSession, err := a.sessionService.Create(ctx, fmt.Sprintf("ACP Session %s", sessionID))
	if err != nil {
		a.logger.Printf("[ACP AGENT] Failed to create Pando session: %v", err)
		return acpsdk.NewSessionResponse{}, fmt.Errorf("failed to create session: %w", err)
	}

	// Create ACP server session
	acpSession := NewACPServerSession(
		sessionID,
		workDir,
		nil, // clientConn will be set when we have it
		pandoSession.ID,
	)

	// Store session
	a.sessionsMu.Lock()
	a.sessions[sessionID] = acpSession
	a.sessionsMu.Unlock()

	a.logger.Printf("[ACP AGENT] NewSession created: SessionID=%s, PandoSessionID=%s, WorkDir=%s",
		sessionID, pandoSession.ID, workDir)

	return acpsdk.NewSessionResponse{
		SessionId: sessionID,
	}, nil
}

// Prompt handles prompt requests from the client.
func (a *PandoACPAgent) Prompt(ctx context.Context, req acpsdk.PromptRequest) (acpsdk.PromptResponse, error) {
	a.logger.Printf("[ACP AGENT] Prompt request: SessionID=%s", req.SessionId)

	// Get session
	a.sessionsMu.RLock()
	acpSession, exists := a.sessions[req.SessionId]
	a.sessionsMu.RUnlock()

	if !exists {
		a.logger.Printf("[ACP AGENT] Session not found: %s", req.SessionId)
		return acpsdk.PromptResponse{}, fmt.Errorf("session not found: %s", req.SessionId)
	}

	// Extract prompt text from the request
	promptText, err := a.extractPromptText(req.Prompt)
	if err != nil {
		a.logger.Printf("[ACP AGENT] Failed to extract prompt text: %v", err)
		return acpsdk.PromptResponse{}, fmt.Errorf("invalid prompt: %w", err)
	}

	a.logger.Printf("[ACP AGENT] Processing prompt (length=%d) for session %s",
		len(promptText), req.SessionId)

	// Process the prompt with Pando LLM agent
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

	// Extract text from all content blocks
	var textParts []string
	for _, block := range prompt {
		if block.Text != nil {
			textParts = append(textParts, block.Text.Text)
		}
		// TODO: Handle other content types (images, etc.) when supported
	}

	if len(textParts) == 0 {
		return "", fmt.Errorf("no text content in prompt")
	}

	// Join all text parts
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

	// Run the agent
	eventChan, err := a.agentService.Run(ctx, pandoSessionID, promptText)
	if err != nil {
		return "", fmt.Errorf("failed to start agent: %w", err)
	}

	// Process agent events and send updates to client
	var finalStopReason acpsdk.StopReason
	for event := range eventChan {
		switch event.Type {
		case AgentEventTypeError:
			if event.Error != nil {
				a.logger.Printf("[ACP AGENT] Agent error: %v", event.Error)
				return acpsdk.StopReasonError, event.Error
			}

		case AgentEventTypeResponse:
			// Process the agent's response message
			err := a.processAgentResponse(acpSession, event.Message)
			if err != nil {
				a.logger.Printf("[ACP AGENT] Failed to process response: %v", err)
				return acpsdk.StopReasonError, err
			}

			// Determine stop reason based on finish reason
			finalStopReason = a.mapFinishReasonToStopReason(event.Message.FinishReason())

		case AgentEventTypeSummarize:
			// Summarization events - just log for now
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
	// Send text content chunks
	if content := msg.Content(); content.String() != "" {
		update := acpsdk.UpdateAgentMessageText(content.String())
		if err := acpSession.SendUpdate(update); err != nil {
			a.logger.Printf("[ACP AGENT] Failed to send message update: %v", err)
		}
	}

	// Send reasoning content chunks
	if reasoning := msg.ReasoningContent(); reasoning.String() != "" {
		update := acpsdk.UpdateAgentThoughtText(reasoning.String())
		if err := acpSession.SendUpdate(update); err != nil {
			a.logger.Printf("[ACP AGENT] Failed to send thought update: %v", err)
		}
	}

	// Send tool calls
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
		return acpsdk.StopReasonError
	default:
		return acpsdk.StopReasonEndTurn
	}
}

// SetSessionMode handles session mode changes (not implemented yet).
func (a *PandoACPAgent) SetSessionMode(ctx context.Context, req acpsdk.SetSessionModeRequest) (acpsdk.SetSessionModeResponse, error) {
	a.logger.Printf("[ACP AGENT] SetSessionMode called (not implemented)")
	return acpsdk.SetSessionModeResponse{}, fmt.Errorf("session mode not implemented")
}

// GetVersion returns the agent version.
func (a *PandoACPAgent) GetVersion() string {
	return a.version
}

// GetCapabilities returns the agent capabilities.
func (a *PandoACPAgent) GetCapabilities() acpsdk.AgentCapabilities {
	return a.capabilities
}
