package acp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/digiogithub/pando/internal/message"
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

// ListSessions returns the historical sessions known by Pando.
// ACP v0.6.3 doesn't define a session/list request, so this helper is exposed
// for HTTP/API adapters that need to provide discovery endpoints.
func (a *PandoACPAgent) ListSessions(ctx context.Context) ([]ACPSessionInfo, error) {
	sessions, err := a.sessionService.ListSessions(ctx)
	if err != nil {
		a.logger.Printf("[ACP AGENT] ListSessions failed: %v", err)
		return nil, err
	}
	return sessions, nil
}

// PermissionService is a minimal interface for configuring tool permissions per session.
// This avoids import cycles with the permission package.
type PermissionService interface {
	AutoApproveSession(sessionID string)
	RemoveAutoApproveSession(sessionID string)
	RegisterSessionHandler(sessionID string, handler func(sessionID, toolName, description string) bool)
	UnregisterSessionHandler(sessionID string)
}

// editToolInput is used to parse fields from tool call input JSON for edit/write tools.
type editToolInput struct {
	FilePath  string `json:"file_path"`
	Content   string `json:"content"`    // write tool
	OldString string `json:"old_string"` // edit tool
	NewString string `json:"new_string"` // edit tool
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

	// permissionService handles tool permission approvals for ACP sessions.
	// If nil, permissions are handled by the default TUI flow.
	permissionService PermissionService

	// clientSupportsWriteFile indicates the connected client supports fs/write_text_file.
	// Set during Initialize from ClientCapabilities.Fs.WriteTextFile.
	clientSupportsWriteFile bool

	// pendingToolCalls maps tool call IDs to their raw input JSON for edit/write operations.
	// Used to extract the file path when sending WriteTextFile after a successful tool result.
	pendingToolCallsMu sync.Mutex
	pendingToolCalls   map[string]string
}

// NewPandoACPAgent creates a new ACP agent instance.
func NewPandoACPAgent(
	version string,
	workDir string,
	logger *log.Logger,
	agentService AgentService,
	sessionService SessionService,
	permSvc PermissionService,
) *PandoACPAgent {
	if logger == nil {
		logger = log.Default()
	}

	return &PandoACPAgent{
		version:           version,
		workDir:           workDir,
		logger:            logger,
		sessions:          make(map[acpsdk.SessionId]*ACPServerSession),
		pendingToolCalls:  make(map[string]string),
		agentService:      agentService,
		sessionService:    sessionService,
		permissionService: permSvc,
		capabilities: acpsdk.AgentCapabilities{
			LoadSession: true,
			McpCapabilities: acpsdk.McpCapabilities{
				Http: false,
				Sse:  false,
			},
			PromptCapabilities: acpsdk.PromptCapabilities{
				Audio:           false,
				EmbeddedContext: true,
				Image:           true,
			},
		},
	}
}

// Initialize handles the initialization handshake from an ACP client.
// This is the first method called when a client connects.
func (a *PandoACPAgent) Initialize(ctx context.Context, req acpsdk.InitializeRequest) (acpsdk.InitializeResponse, error) {
	a.logger.Printf("[ACP AGENT] Initialize request from client: %+v", req.ClientInfo)
	a.logger.Printf("[ACP AGENT] Client protocol version: %v", req.ProtocolVersion)
	a.logger.Printf("[ACP AGENT] Client capabilities: fs.readTextFile=%v fs.writeTextFile=%v terminal=%v",
		req.ClientCapabilities.Fs.ReadTextFile,
		req.ClientCapabilities.Fs.WriteTextFile,
		req.ClientCapabilities.Terminal,
	)

	// Store whether this client supports receiving file content via WriteTextFile (6a).
	a.clientSupportsWriteFile = req.ClientCapabilities.Fs.WriteTextFile

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

	// 6b: Per-session MCP servers from client.
	// TODO: wire into Pando's MCP registry once per-session MCP is supported.
	if len(req.McpServers) > 0 {
		a.logger.Printf("[ACP AGENT] Warning: %d MCP server(s) requested by client but per-session MCP is not yet supported — ignoring", len(req.McpServers))
	}

	workDir := a.workDir
	if req.Cwd != "" {
		workDir = req.Cwd
	}

	// Create internal Pando session via the minimal SessionService interface
	pandoSessionID, err := a.sessionService.CreateSession(ctx, "ACP Session")
	if err != nil {
		a.logger.Printf("[ACP AGENT] Failed to create Pando session: %v", err)
		return acpsdk.NewSessionResponse{}, fmt.Errorf("failed to create session: %w", err)
	}

	// Keep ACP session ID synchronized with Pando session ID so clients can
	// reliably LoadSession with the same identifier.
	sessionID := acpsdk.SessionId(pandoSessionID)

	acpSession := NewACPServerSession(
		sessionID,
		workDir,
		a.conn, // AgentSideConnection for streaming updates
		pandoSessionID,
	)
	acpSession.SetMode("agent")

	currentMode := acpSession.Mode()
	a.sessionsMu.Lock()
	if existing, exists := a.sessions[sessionID]; exists {
		existing.SetWorkDir(workDir)
		existing.SetAgentConnection(a.conn)
		if existing.Mode() == "" {
			existing.SetMode("agent")
		}
		currentMode = existing.Mode()
		a.logger.Printf("[ACP AGENT] NewSession reused existing ACP session mapping: SessionID=%s", sessionID)
	} else {
		a.sessions[sessionID] = acpSession
	}
	a.sessionsMu.Unlock()

	a.logger.Printf("[ACP AGENT] NewSession created: SessionID=%s, PandoSessionID=%s, WorkDir=%s",
		sessionID, pandoSessionID, workDir)

	return acpsdk.NewSessionResponse{
		SessionId: sessionID,
		Modes:     buildSessionModeState(currentMode),
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

	promptText, attachments, err := a.extractPromptContent(req.Prompt)
	if err != nil {
		a.logger.Printf("[ACP AGENT] Failed to extract prompt content: %v", err)
		return acpsdk.PromptResponse{}, fmt.Errorf("invalid prompt: %w", err)
	}

	a.logger.Printf("[ACP AGENT] Processing prompt (length=%d, attachments=%d) for session %s",
		len(promptText), len(attachments), req.SessionId)

	// Apply per-session model override if set.
	if modelID := acpSession.Model(); modelID != "" {
		if err := a.agentService.SetModelOverride(modelID); err != nil {
			a.logger.Printf("[ACP AGENT] Warning: could not apply model override %q: %v", modelID, err)
		} else {
			a.logger.Printf("[ACP AGENT] Applied model override %q for session %s", modelID, req.SessionId)
		}
	}

	// Enforce session mode: configure permissions based on Agent vs Ask mode.
	mode := acpSession.Mode()
	if mode == "" {
		mode = "agent"
	}
	switch mode {
	case "agent":
		// Agent mode: auto-approve all tool calls for this session (no permission dialogs).
		if a.permissionService != nil {
			a.permissionService.AutoApproveSession(string(req.SessionId))
		}
	case "ask":
		// Ask mode: route permission requests to the connected editor via ACP.
		if a.permissionService != nil {
			a.permissionService.RemoveAutoApproveSession(string(req.SessionId))
			if a.conn == nil {
				a.logger.Printf("[ACP AGENT] Ask mode requested for session %s but no ACP connection is available; using default permission handling", req.SessionId)
			} else {
				bridge := NewACPPermissionBridge(a.conn, req.SessionId, a.logger)
				a.permissionService.RegisterSessionHandler(string(req.SessionId), bridge.Handle)
				defer a.permissionService.UnregisterSessionHandler(string(req.SessionId))
			}
		}
	default:
		a.logger.Printf("[ACP AGENT] Unknown session mode %q — defaulting to agent behavior", mode)
		if a.permissionService != nil {
			a.permissionService.AutoApproveSession(string(req.SessionId))
		}
	}
	a.logger.Printf("[ACP AGENT] Session mode %q applied for session %s", mode, req.SessionId)

	stopReason, err := a.processPromptWithAgent(ctx, acpSession, promptText, attachments...)
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

// extractPromptContent extracts text and image attachments from a Prompt (slice of ContentBlocks).
// Supports text blocks (ContentBlock::Text) and image blocks (ContentBlock::Image, requires 6d capability).
func (a *PandoACPAgent) extractPromptContent(prompt []acpsdk.ContentBlock) (string, []message.Attachment, error) {
	if len(prompt) == 0 {
		return "", nil, fmt.Errorf("empty prompt content")
	}

	var textParts []string
	var attachments []message.Attachment

	for _, block := range prompt {
		if block.Text != nil {
			textParts = append(textParts, block.Text.Text)
		}
		// 6d: handle image content blocks
		if block.Image != nil {
			img := block.Image
			var data []byte
			if img.Data != "" {
				// base64-encoded inline image
				decoded, err := base64.StdEncoding.DecodeString(img.Data)
				if err != nil {
					a.logger.Printf("[ACP AGENT] Warning: failed to decode image data: %v", err)
					continue
				}
				data = decoded
			}
			mimeType := img.MimeType
			if mimeType == "" {
				mimeType = "image/png"
			}
			attachments = append(attachments, message.Attachment{
				MimeType: mimeType,
				Content:  data,
			})
		}
	}

	if len(textParts) == 0 {
		return "", nil, fmt.Errorf("no text content in prompt")
	}

	return joinTextParts(textParts), attachments, nil
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
// attachments carries any image/binary content extracted from the prompt (6d).
func (a *PandoACPAgent) processPromptWithAgent(
	ctx context.Context,
	acpSession *ACPServerSession,
	promptText string,
	attachments ...message.Attachment,
) (acpsdk.StopReason, error) {
	pandoSessionID := acpSession.PandoSessionID()

	// 6c: TODO: send token usage update via SessionUpdate after prompt completion.
	// The current acpsdk v0.6.3 SessionUpdate type does not include a usage/token variant.
	// When the SDK adds UsageUpdate support, send it here after the event loop completes.

	eventChan, err := a.agentService.Run(ctx, pandoSessionID, promptText, attachments...)
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
				// Store input for ALL tools so we can send rawInput in the tool result.
				// For edit tools this is also used by sendWriteTextFile (6a).
				a.pendingToolCallsMu.Lock()
				a.pendingToolCalls[tc.ID] = tc.Input
				a.pendingToolCallsMu.Unlock()

				kind := mapToolKind(tc.Name)

				// 1. Send "pending" state first — matches opencode's toolStart() behaviour.
				// Clients need to see the tool registered before input arrives.
				pendingUpdate := acpsdk.StartToolCall(
					acpsdk.ToolCallId(tc.ID),
					tc.Name,
					acpsdk.WithStartKind(kind),
					acpsdk.WithStartStatus(acpsdk.ToolCallStatusPending),
					acpsdk.WithStartRawInput(map[string]interface{}{}),
				)
				if err := acpSession.SendUpdate(pendingUpdate); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send tool call pending: %v", err)
				}

				// 2. Immediately follow with "in_progress" + actual input so the client
				// can render the tool arguments while it executes.
				rawInput := parseJSONInput(tc.Input)
				inProgressOpts := []acpsdk.ToolCallUpdateOpt{
					acpsdk.WithUpdateStatus(acpsdk.ToolCallStatusInProgress),
					acpsdk.WithUpdateKind(kind),
					acpsdk.WithUpdateTitle(tc.Name),
					acpsdk.WithUpdateRawInput(rawInput),
				}
				// For edit/write tools, attach the file path as a location so editors can
				// show which file is being modified while the tool runs.
				if isEditTool(tc.Name) {
					var ep editToolInput
					if jerr := json.Unmarshal([]byte(tc.Input), &ep); jerr == nil && ep.FilePath != "" {
						inProgressOpts = append(inProgressOpts, acpsdk.WithUpdateLocations([]acpsdk.ToolCallLocation{{Path: ep.FilePath}}))
					}
				}
				inProgressUpdate := acpsdk.UpdateToolCall(acpsdk.ToolCallId(tc.ID), inProgressOpts...)
				if err := acpSession.SendUpdate(inProgressUpdate); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send tool call in_progress: %v", err)
				}
			}

		case AgentEventTypeToolResult:
			if event.ToolResult != nil {
				tr := event.ToolResult
				status := acpsdk.ToolCallStatusCompleted
				if tr.IsError {
					status = acpsdk.ToolCallStatusFailed
				}

				// Retrieve the stored input for this tool call.
				// For edit tools we do NOT delete here — sendWriteTextFile needs it.
				// For all other tools we clean up immediately to avoid leaking memory.
				a.pendingToolCallsMu.Lock()
				storedInput := a.pendingToolCalls[tr.ToolCallID]
				if !isEditTool(tr.Name) {
					delete(a.pendingToolCalls, tr.ToolCallID)
				}
				a.pendingToolCallsMu.Unlock()

				// Rebuild rawInput so clients can display tool arguments alongside the result.
				rawInput := parseJSONInput(storedInput)

				// Build rawOutput matching the opencode format: { output, metadata }.
				rawOutput := map[string]interface{}{
					"output": tr.Content,
				}
				if tr.Metadata != "" {
					var meta interface{}
					if jerr := json.Unmarshal([]byte(tr.Metadata), &meta); jerr == nil {
						rawOutput["metadata"] = meta
					} else {
						rawOutput["metadata"] = tr.Metadata
					}
				}

				// Use ToolCallContent so editors display the output correctly.
				// The ACP TypeScript SDK clients (VS Code, Zed, JetBrains) render
				// content entries; rawOutput is used as structured fallback.
				outputContent := []acpsdk.ToolCallContent{
					acpsdk.ToolContent(acpsdk.TextBlock(tr.Content)),
				}

				// For edit tools, also attach a diff content block so editors can display
				// exactly what changed. write uses Content (full file); edit uses OldString/NewString.
				if isEditTool(tr.Name) && !tr.IsError && storedInput != "" {
					var ep editToolInput
					if jerr := json.Unmarshal([]byte(storedInput), &ep); jerr == nil && ep.FilePath != "" {
						if tr.Name == "write" {
							outputContent = append(outputContent, acpsdk.ToolDiffContent(ep.FilePath, ep.Content))
						} else {
							// edit / multiedit: pass oldText so the client can render a proper diff
							outputContent = append(outputContent, acpsdk.ToolDiffContent(ep.FilePath, ep.NewString, ep.OldString))
						}
					}
				}

				kind := mapToolKind(tr.Name)
				update := acpsdk.UpdateToolCall(
					acpsdk.ToolCallId(tr.ToolCallID),
					acpsdk.WithUpdateStatus(status),
					acpsdk.WithUpdateKind(kind),
					acpsdk.WithUpdateTitle(tr.Name),
					acpsdk.WithUpdateContent(outputContent),
					acpsdk.WithUpdateRawInput(rawInput),
					acpsdk.WithUpdateRawOutput(rawOutput),
				)
				if err := acpSession.SendUpdate(update); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send tool result: %v", err)
				}

				// 6a: if client supports writeTextFile and this was a successful edit/write,
				// push the new file content so the editor can refresh its buffers.
				if !tr.IsError && a.clientSupportsWriteFile && a.conn != nil && isEditTool(tr.Name) {
					a.sendWriteTextFile(ctx, acpSession.ID, tr.ToolCallID)
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

// isEditTool returns true for tool names that modify files on disk.
func isEditTool(name string) bool {
	switch name {
	case "edit", "write", "patch", "multiedit":
		return true
	}
	return false
}

// sendWriteTextFile reads the edited file and sends its content to the ACP client.
// This allows the editor to update open buffers without reloading from disk (6a).
func (a *PandoACPAgent) sendWriteTextFile(ctx context.Context, sessionID acpsdk.SessionId, toolCallID string) {
	a.pendingToolCallsMu.Lock()
	input, ok := a.pendingToolCalls[toolCallID]
	if ok {
		delete(a.pendingToolCalls, toolCallID)
	}
	a.pendingToolCallsMu.Unlock()

	if !ok || input == "" {
		return
	}

	var params editToolInput
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		a.logger.Printf("[ACP AGENT] WriteTextFile: failed to parse tool input: %v", err)
		return
	}

	filePath := params.FilePath
	if filePath == "" {
		return
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		a.logger.Printf("[ACP AGENT] WriteTextFile: failed to read %s: %v", filePath, err)
		return
	}

	_, err = a.conn.WriteTextFile(ctx, acpsdk.WriteTextFileRequest{
		SessionId: sessionID,
		Path:      filePath,
		Content:   string(content),
	})
	if err != nil {
		a.logger.Printf("[ACP AGENT] WriteTextFile: failed to send %s: %v", filePath, err)
		return
	}

	a.logger.Printf("[ACP AGENT] WriteTextFile: sent updated content for %s (%d bytes)", filePath, len(content))
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
// The updated mode is applied when the next Prompt call begins.
func (a *PandoACPAgent) SetSessionMode(ctx context.Context, req acpsdk.SetSessionModeRequest) (acpsdk.SetSessionModeResponse, error) {
	a.logger.Printf("[ACP AGENT] SetSessionMode: SessionID=%s, ModeID=%s", req.SessionId, req.ModeId)

	a.sessionsMu.RLock()
	acpSession, exists := a.sessions[req.SessionId]
	a.sessionsMu.RUnlock()

	if !exists {
		return acpsdk.SetSessionModeResponse{}, fmt.Errorf("session not found: %s", req.SessionId)
	}

	validModes := map[string]bool{"agent": true, "ask": true}
	if !validModes[string(req.ModeId)] {
		return acpsdk.SetSessionModeResponse{}, fmt.Errorf("unknown mode: %s", req.ModeId)
	}

	acpSession.SetMode(string(req.ModeId))
	a.logger.Printf("[ACP AGENT] Session mode set: SessionID=%s, Mode=%s (mode will take effect on next prompt)", req.SessionId, req.ModeId)

	return acpsdk.SetSessionModeResponse{}, nil
}

// SetConnection stores a reference to the AgentSideConnection so the agent can
// stream session updates back to the client.  Called by transport_stdio.go
// immediately after NewAgentSideConnection() returns.
func (a *PandoACPAgent) SetConnection(conn *acpsdk.AgentSideConnection) {
	a.sessionsMu.Lock()
	defer a.sessionsMu.Unlock()

	a.conn = conn
	for _, sess := range a.sessions {
		sess.SetAgentConnection(conn)
	}
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

	// 6b: Per-session MCP servers from client.
	// TODO: wire into Pando's MCP registry once per-session MCP is supported.
	if len(req.McpServers) > 0 {
		a.logger.Printf("[ACP AGENT] Warning: %d MCP server(s) requested by client but per-session MCP is not yet supported — ignoring", len(req.McpServers))
	}

	_, err := a.sessionService.GetSession(ctx, string(req.SessionId))
	if err != nil {
		a.logger.Printf("[ACP AGENT] LoadSession: session not found: %v", err)
		return acpsdk.LoadSessionResponse{}, fmt.Errorf("session not found: %w", err)
	}

	workDir := a.workDir
	if req.Cwd != "" {
		workDir = req.Cwd
	}

	a.sessionsMu.Lock()
	currentMode := "agent"
	if existing, exists := a.sessions[req.SessionId]; exists {
		existing.SetWorkDir(workDir)
		existing.SetAgentConnection(a.conn)
		if existing.Mode() != "" {
			currentMode = existing.Mode()
		} else {
			existing.SetMode(currentMode)
		}
		a.logger.Printf("[ACP AGENT] LoadSession: synchronized existing session %s", req.SessionId)
	} else {
		acpSession := NewACPServerSession(req.SessionId, workDir, a.conn, string(req.SessionId))
		acpSession.SetMode(currentMode)
		a.sessions[req.SessionId] = acpSession
		a.logger.Printf("[ACP AGENT] LoadSession: registered session %s", req.SessionId)
	}
	a.sessionsMu.Unlock()

	return acpsdk.LoadSessionResponse{
		Modes:  buildSessionModeState(currentMode),
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

// availableModes returns the fixed set of session modes supported by Pando.
func availableModes() []acpsdk.SessionMode {
	descPtr := func(s string) *string { return &s }
	return []acpsdk.SessionMode{
		{
			Id:          "agent",
			Name:        "Agent",
			Description: descPtr("Full agent — tools auto-approved without prompting"),
		},
		{
			Id:          "ask",
			Name:        "Ask",
			Description: descPtr("Ask for permission before each tool use"),
		},
	}
}

// buildSessionModeState constructs the SessionModeState for ACP responses.
func buildSessionModeState(currentModeID string) *acpsdk.SessionModeState {
	if currentModeID == "" {
		currentModeID = "agent"
	}
	return &acpsdk.SessionModeState{
		AvailableModes: availableModes(),
		CurrentModeId:  acpsdk.SessionModeId(currentModeID),
	}
}

// buildSessionModelState constructs the SessionModelState from the AgentService.
func buildSessionModelState(svc AgentService) *acpsdk.SessionModelState {
	currentID := svc.CurrentModelID()
	available := svc.AvailableModels()

	infos := make([]acpsdk.ModelInfo, 0, len(available))
	for _, m := range available {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		infos = append(infos, acpsdk.ModelInfo{
			ModelId: acpsdk.ModelId(m.ID),
			Name:    name,
		})
	}

	return &acpsdk.SessionModelState{
		AvailableModels: infos,
		CurrentModelId:  acpsdk.ModelId(currentID),
	}
}

// parseJSONInput attempts to decode a JSON string into a native map or slice.
// ACP clients expect rawInput to be a JSON object, not an encoded string.
// If decoding fails the original string is returned as-is.
func parseJSONInput(s string) interface{} {
	if s == "" {
		return map[string]interface{}{}
	}
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	return s
}

// mapToolKind maps a Pando tool name to the corresponding ACP ToolKind.
func mapToolKind(toolName string) acpsdk.ToolKind {
	switch toolName {
	case "bash", "execute_command":
		return acpsdk.ToolKindExecute
	case "edit", "write", "multiedit", "patch":
		return acpsdk.ToolKindEdit
	case "read", "view", "ls":
		return acpsdk.ToolKindRead
	case "glob", "grep",
		"c7_resolve_library_id", "c7_get_library_docs",
		"brave_search", "google_search", "perplexity_search", "exa_search":
		return acpsdk.ToolKindSearch
	case "web_search", "web_fetch", "fetch":
		return acpsdk.ToolKindFetch
	default:
		return acpsdk.ToolKindOther
	}
}

// toolLocationInput holds fields used to extract file/directory path from a tool
// call input for different tool types. Each field corresponds to the JSON key used
// by the respective tool.
type toolLocationInput struct {
	FilePath string `json:"file_path"` // edit, write, view, read
	Path     string `json:"path"`      // glob, grep, ls
}

// toLocations extracts file/directory locations from a tool call input JSON string.
// This mirrors opencode's toLocations() function and is used so ACP clients (VS Code,
// Zed, JetBrains) can show which files are being accessed while a tool runs.
func toLocations(toolName, inputJSON string) []acpsdk.ToolCallLocation {
	if inputJSON == "" {
		return nil
	}
	var inp toolLocationInput
	if err := json.Unmarshal([]byte(inputJSON), &inp); err != nil {
		return nil
	}

	switch toolName {
	case "edit", "write", "patch", "multiedit", "view", "read":
		if inp.FilePath != "" {
			return []acpsdk.ToolCallLocation{{Path: inp.FilePath}}
		}
	case "glob", "grep", "ls":
		if inp.Path != "" {
			return []acpsdk.ToolCallLocation{{Path: inp.Path}}
		}
	}
	return nil
}
