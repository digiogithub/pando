package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/notify"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/safety"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

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

	// planService handles plan-related operations for ACP sessions.
	planService PlanService

	// clientSupportsWriteFile indicates the connected client supports fs/write_text_file.
	// Set during Initialize from ClientCapabilities.Fs.WriteTextFile.
	clientSupportsWriteFile bool

	// clientSupportsTerminalOutput indicates the connected client supports
	// terminal transport metadata via _meta.terminal_output / terminal_exit.
	clientSupportsTerminalOutput bool

	// pendingToolCalls maps tool call IDs to their raw input JSON for edit/write operations.
	// Used to extract the file path when sending WriteTextFile after a successful tool result.
	pendingToolCallsMu sync.Mutex
	pendingToolCalls   map[string]string

	// startedToolCalls tracks tool calls already announced to the client in the
	// live streaming path so we can guarantee a tool_call exists before any
	// tool_call_update for the same ID.
	startedToolCalls map[string]bool

	extensionHandlers map[string]func(ctx context.Context, params json.RawMessage) (any, error)
}

const askModeInstruction = "You are in Ask mode. Prefer direct answers and avoid tool use unless the user explicitly requests tool-driven work or a tool is required to answer accurately."

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

	agent := &PandoACPAgent{
		version:           version,
		workDir:           workDir,
		logger:            logger,
		sessions:          make(map[acpsdk.SessionId]*ACPServerSession),
		pendingToolCalls:  make(map[string]string),
		startedToolCalls:  make(map[string]bool),
		agentService:      agentService,
		sessionService:    sessionService,
		permissionService: permSvc,
		planService:       NewPlanService(logger),
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

	agent.extensionHandlers = map[string]func(ctx context.Context, params json.RawMessage) (any, error){
		"_pando.setPersona":       agent.handleExtensionSetPersona,
		"_pando.openCopilotUsage": agent.handleExtensionOpenCopilotUsage,
		"_pando.openClaudeUsage":  agent.handleExtensionOpenClaudeUsage,
	}

	return agent
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
	a.clientSupportsTerminalOutput = req.ClientCapabilities.Terminal

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
	if goal, err := a.sessionService.CancelGoal(context.Background(), acpSession.PandoSessionID()); err == nil {
		a.sendGoalStateUpdate(params.SessionId, goalStateFromDB(goal, time.Now()))
	}

	a.logger.Printf("[ACP AGENT] Session cancelled: %s", params.SessionId)
	return nil
}

// ListSessions returns the historical sessions known by Pando.
// Implements the acpsdk.Agent interface (session/list method).
func (a *PandoACPAgent) ListSessions(ctx context.Context, _ acpsdk.ListSessionsRequest) (acpsdk.ListSessionsResponse, error) {
	sessions, err := a.sessionService.ListSessions(ctx)
	if err != nil {
		a.logger.Printf("[ACP AGENT] ListSessions failed: %v", err)
		return acpsdk.ListSessionsResponse{}, err
	}

	infos := make([]acpsdk.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		title := s.Title
		infos = append(infos, acpsdk.SessionInfo{
			SessionId: acpsdk.SessionId(s.ID),
			Title:     &title,
		})
	}
	return acpsdk.ListSessionsResponse{Sessions: infos}, nil
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
	acpSession.SetMode(defaultACPMode)
	acpSession.SetAskPermission(false)

	currentMode := acpSession.Mode()
	a.sessionsMu.Lock()
	if existing, exists := a.sessions[sessionID]; exists {
		existing.SetWorkDir(workDir)
		existing.SetAgentConnection(a.conn)
		if existing.Mode() == "" {
			existing.SetMode(defaultACPMode)
		}
		currentMode = existing.Mode()
		acpSession = existing
		a.logger.Printf("[ACP AGENT] NewSession reused existing ACP session mapping: SessionID=%s", sessionID)
	} else {
		a.sessions[sessionID] = acpSession
	}
	a.sessionsMu.Unlock()

	a.logger.Printf("[ACP AGENT] NewSession created: SessionID=%s, PandoSessionID=%s, WorkDir=%s",
		sessionID, pandoSessionID, workDir)

	go a.sendAvailableCommandsUpdate(context.Background(), sessionID)

	return acpsdk.NewSessionResponse{
		SessionId:     sessionID,
		ConfigOptions: buildSessionConfigOptions(a.agentService, acpSession.Model(), currentMode, acpSession.Persona(), acpSession.AskPermission()),
		Modes:         buildSessionModeState(a.agentService, currentMode),
		Models:        buildSessionModelState(a.agentService, acpSession.Model()),
		Meta:          mergeMetaMaps(personaStateToMeta(buildSessionPersonaState(a.agentService, acpSession.Persona())), goalMeta(acpSession.Goal())),
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

	// Apply per-session persona override if set.
	if personaName := acpSession.Persona(); personaName != "" {
		if err := a.agentService.SetActivePersona(personaName); err != nil {
			a.logger.Printf("[ACP AGENT] Warning: could not apply persona %q: %v", personaName, err)
		} else {
			a.logger.Printf("[ACP AGENT] Applied persona %q for session %s", personaName, req.SessionId)
		}
	} else {
		// Clear any previous persona override so the default behaviour (auto-select or none) applies.
		_ = a.agentService.SetActivePersona("")
	}

	mode := acpSession.Mode()
	if mode == "" {
		mode = defaultACPMode
	}

	// Configure permissions from the dedicated approval selector.
	askPermission := acpSession.AskPermission()
	if mode == askModeID && !acpSession.PermissionConfigured() {
		askPermission = true
	} else if mode == goalModeID && !acpSession.PermissionConfigured() {
		askPermission = !goalAutoApproveEnabled()
	}

	cleanupPermissions := a.configurePermissionMode(req.SessionId, mode, askPermission)
	defer cleanupPermissions()
	a.logger.Printf("[ACP AGENT] Session mode=%q askPermission=%t applied for session %s", mode, askPermission, req.SessionId)

	if command, ok := parseSlashCommand(promptText); ok {
		stopReason, err := a.handleSlashCommand(ctx, req.SessionId, acpSession, command)
		if err != nil {
			a.logger.Printf("[ACP AGENT] Slash command failed: %v", err)
			return acpsdk.PromptResponse{}, fmt.Errorf("prompt processing failed: %w", err)
		}
		return a.finishPrompt(ctx, req.SessionId, acpSession, stopReason)
	}

	var stopReason acpsdk.StopReason
	if mode == goalModeID {
		stopReason, err = a.processGoalPrompt(ctx, req.SessionId, acpSession, promptText)
	} else {
		if mode == askModeID {
			promptText = askModeInstruction + "\n\nUser request:\n" + promptText
		}
		stopReason, err = a.processPromptWithAgent(ctx, acpSession, promptText, attachments...)
	}
	if err != nil {
		a.logger.Printf("[ACP AGENT] Prompt processing failed: %v", err)
		return acpsdk.PromptResponse{}, fmt.Errorf("prompt processing failed: %w", err)
	}

	return a.finishPrompt(ctx, req.SessionId, acpSession, stopReason)
}

func (a *PandoACPAgent) finishPrompt(ctx context.Context, sessionID acpsdk.SessionId, acpSession *ACPServerSession, stopReason acpsdk.StopReason) (acpsdk.PromptResponse, error) {
	a.sendRunStatusMeta(ctx, sessionID, acpSession)

	// Send usage_update and session_info_update after the prompt completes.
	// Fetch the latest session state so we have accurate token counts and title.
	if used, size, ok := a.currentUsageSnapshot(ctx, acpSession); ok {
		if used > 0 {
			if sendErr := acpSession.SendUpdate(acpsdk.UpdateUsage(used, size)); sendErr != nil {
				a.logger.Printf("[ACP AGENT] Failed to send usage_update: %v", sendErr)
			}
		}
	}
	if sessionInfo, sessErr := a.sessionService.GetSession(ctx, acpSession.PandoSessionID()); sessErr == nil {
		// session_info_update: send the current session title so clients can
		// display it in session lists without requiring a full ListSessions call.
		if sessionInfo.Title != "" && sessionInfo.Title != "ACP Session" {
			if sendErr := acpSession.SendUpdate(acpsdk.UpdateSessionTitle(sessionInfo.Title)); sendErr != nil {
				a.logger.Printf("[ACP AGENT] Failed to send session_info_update: %v", sendErr)
			}
		}
	} else {
		a.logger.Printf("[ACP AGENT] Could not fetch session for post-prompt updates: %v", sessErr)
	}

	a.logger.Printf("[ACP AGENT] Prompt completed: SessionID=%s, StopReason=%s",
		sessionID, stopReason)

	return acpsdk.PromptResponse{
		StopReason: stopReason,
	}, nil
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

	persistedGoal, goalErr := a.loadGoalState(ctx, string(req.SessionId), false)
	if goalErr != nil && !isGoalNotFound(goalErr) {
		return acpsdk.LoadSessionResponse{}, fmt.Errorf("load goal state: %w", goalErr)
	}

	a.sessionsMu.Lock()
	currentMode := defaultACPMode
	if existing, exists := a.sessions[req.SessionId]; exists {
		existing.SetWorkDir(workDir)
		existing.SetAgentConnection(a.conn)
		existing.SetGoal(persistedGoal)
		if persistedGoal != nil && persistedGoal.Status == "running" {
			existing.SetMode(goalModeID)
		}
		if existing.Mode() != "" {
			currentMode = existing.Mode()
		} else {
			existing.SetMode(currentMode)
		}
		a.logger.Printf("[ACP AGENT] LoadSession: synchronized existing session %s", req.SessionId)
	} else {
		acpSession := NewACPServerSession(req.SessionId, workDir, a.conn, string(req.SessionId))
		if persistedGoal != nil {
			acpSession.SetGoal(persistedGoal)
			if persistedGoal.Status == "running" {
				currentMode = goalModeID
			}
		}
		acpSession.SetMode(currentMode)
		acpSession.SetAskPermission(false)
		a.sessions[req.SessionId] = acpSession
		a.logger.Printf("[ACP AGENT] LoadSession: registered session %s", req.SessionId)
	}
	a.sessionsMu.Unlock()

	var currentPersona string
	var acpSession *ACPServerSession
	a.sessionsMu.RLock()
	if sess, ok := a.sessions[req.SessionId]; ok {
		currentPersona = sess.Persona()
		acpSession = sess
	}
	a.sessionsMu.RUnlock()

	go a.sendAvailableCommandsUpdate(context.Background(), req.SessionId)

	// Stream the full conversation history back to the client as required by the ACP protocol:
	// "Stream the entire conversation history back to the client via notifications"
	go a.streamSessionHistory(context.Background(), req.SessionId, string(req.SessionId))

	return acpsdk.LoadSessionResponse{
		ConfigOptions: buildSessionConfigOptions(a.agentService, a.mustSessionModel(req.SessionId), currentMode, currentPersona, a.mustSessionAskPermission(req.SessionId)),
		Modes:         buildSessionModeState(a.agentService, currentMode),
		Models:        buildSessionModelState(a.agentService, a.mustSessionModel(req.SessionId)),
		Meta:          mergeMetaMaps(personaStateToMeta(buildSessionPersonaState(a.agentService, currentPersona)), goalMeta(acpSession.Goal())),
	}, nil
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

	modeID := strings.TrimSpace(string(req.ModeId))
	if err := validateModeID(modeID); err != nil {
		return acpsdk.SetSessionModeResponse{}, fmt.Errorf("unknown mode: %s", req.ModeId)
	}

	acpSession.SetMode(modeID)
	acpSession.SetAskPermission(defaultAskPermissionForMode(modeID))
	acpSession.SetPermissionConfigured(false)
	a.logger.Printf("[ACP AGENT] Session mode set: SessionID=%s, Mode=%s (mode will take effect on next prompt)", req.SessionId, modeID)

	a.sendCurrentModeUpdate(ctx, req.SessionId, modeID)
	a.sendSessionConfigOptionsUpdate(ctx, req.SessionId)

	return acpsdk.SetSessionModeResponse{}, nil
}

// SetSessionConfigOption handles runtime configuration changes from the client.
func (a *PandoACPAgent) SetSessionConfigOption(ctx context.Context, req acpsdk.SetSessionConfigOptionRequest) (acpsdk.SetSessionConfigOptionResponse, error) {
	sessionID, configID, value, err := parseSessionConfigOptionRequest(req)
	if err != nil {
		return acpsdk.SetSessionConfigOptionResponse{}, err
	}

	acpSession, err := a.getSession(sessionID)
	if err != nil {
		return acpsdk.SetSessionConfigOptionResponse{}, err
	}

	switch configID {
	case sessionConfigModelID:
		if err := a.validateModel(value); err != nil {
			return acpsdk.SetSessionConfigOptionResponse{}, err
		}
		acpSession.SetModel(value)
	case sessionConfigModeID:
		if err := validateModeID(value); err != nil {
			return acpsdk.SetSessionConfigOptionResponse{}, err
		}
		acpSession.SetMode(value)
		if !acpSession.PermissionConfigured() {
			acpSession.SetAskPermission(defaultAskPermissionForMode(value))
		}
		a.sendCurrentModeUpdate(ctx, sessionID, value)
	case sessionConfigAskPermissionID:
		enabled, err := parseAskPermissionValue(value)
		if err != nil {
			return acpsdk.SetSessionConfigOptionResponse{}, err
		}
		acpSession.SetAskPermission(enabled)
		acpSession.SetPermissionConfigured(true)
	case sessionConfigAgentID:
		if err := a.validatePersona(value); err != nil {
			return acpsdk.SetSessionConfigOptionResponse{}, err
		}
		acpSession.SetPersona(value)
	default:
		return acpsdk.SetSessionConfigOptionResponse{}, fmt.Errorf("unknown config option: %s", configID)
	}

	configOptions := buildSessionConfigOptions(a.agentService, acpSession.Model(), acpSession.Mode(), acpSession.Persona(), acpSession.AskPermission())
	a.sendSessionConfigOptionsUpdate(ctx, sessionID)
	return acpsdk.SetSessionConfigOptionResponse{ConfigOptions: configOptions}, nil
}

// UnstableSetSessionModel implements AgentExperimental.
// It stores the requested model on the ACP session for use in future prompts.
func (a *PandoACPAgent) UnstableSetSessionModel(ctx context.Context, req acpsdk.UnstableSetSessionModelRequest) (acpsdk.UnstableSetSessionModelResponse, error) {
	return a.setSessionModel(ctx, req.SessionId, string(req.ModelId))
}

// SetSessionModel implements Agent.
// It stores the requested model on the ACP session for use in future prompts.
func (a *PandoACPAgent) SetSessionModel(ctx context.Context, req acpsdk.SetSessionModelRequest) (acpsdk.SetSessionModelResponse, error) {
	return a.setSessionModel(ctx, req.SessionId, string(req.ModelId))
}

func (a *PandoACPAgent) setSessionModel(ctx context.Context, sessionID acpsdk.SessionId, modelID string) (acpsdk.SetSessionModelResponse, error) {
	a.logger.Printf("[ACP AGENT] SetSessionModel: SessionID=%s, ModelID=%s", sessionID, modelID)

	a.sessionsMu.RLock()
	acpSession, exists := a.sessions[sessionID]
	a.sessionsMu.RUnlock()

	if !exists {
		return acpsdk.SetSessionModelResponse{}, fmt.Errorf("session not found: %s", sessionID)
	}

	acpSession.SetModel(modelID)
	a.logger.Printf("[ACP AGENT] SetSessionModel: model set to %s for session %s", modelID, sessionID)
	a.sendSessionConfigOptionsUpdate(ctx, sessionID)
	return acpsdk.SetSessionModelResponse{}, nil
}

// CloseSession implements Agent.
// It cancels ongoing work, removes the session mapping, and cleans up
// permission handlers and auto-approve state associated with this session.
func (a *PandoACPAgent) CloseSession(ctx context.Context, req acpsdk.CloseSessionRequest) (acpsdk.CloseSessionResponse, error) {
	a.logger.Printf("[ACP AGENT] CloseSession: SessionID=%s", req.SessionId)

	a.sessionsMu.RLock()
	acpSession, exists := a.sessions[req.SessionId]
	a.sessionsMu.RUnlock()

	if !exists {
		return acpsdk.CloseSessionResponse{}, fmt.Errorf("session not found: %s", req.SessionId)
	}

	acpSession.Cancel()
	a.agentService.Cancel(acpSession.PandoSessionID())

	sid := string(req.SessionId)
	if a.permissionService != nil {
		a.permissionService.RemoveAutoApproveSession(sid)
		a.permissionService.UnregisterSessionHandler(sid)
	}

	a.sessionsMu.Lock()
	delete(a.sessions, req.SessionId)
	a.sessionsMu.Unlock()

	a.logger.Printf("[ACP AGENT] CloseSession: session %s closed", req.SessionId)
	return acpsdk.CloseSessionResponse{}, nil
}

// ResumeSession implements Agent.
// It registers an existing session for use without replaying history.
func (a *PandoACPAgent) ResumeSession(ctx context.Context, req acpsdk.ResumeSessionRequest) (acpsdk.ResumeSessionResponse, error) {
	a.logger.Printf("[ACP AGENT] ResumeSession: SessionID=%s, Cwd=%s", req.SessionId, req.Cwd)

	_, err := a.sessionService.GetSession(ctx, string(req.SessionId))
	if err != nil {
		a.logger.Printf("[ACP AGENT] ResumeSession: session not found: %v", err)
		return acpsdk.ResumeSessionResponse{}, fmt.Errorf("session not found: %w", err)
	}

	workDir := a.workDir
	if req.Cwd != "" {
		workDir = req.Cwd
	}

	persistedGoal, goalErr := a.loadGoalState(ctx, string(req.SessionId), false)
	if goalErr != nil && !isGoalNotFound(goalErr) {
		return acpsdk.ResumeSessionResponse{}, fmt.Errorf("load goal state: %w", goalErr)
	}

	a.sessionsMu.Lock()
	currentMode := defaultACPMode
	if existing, exists := a.sessions[req.SessionId]; exists {
		existing.SetWorkDir(workDir)
		existing.SetAgentConnection(a.conn)
		existing.SetGoal(persistedGoal)
		if persistedGoal != nil && persistedGoal.Status == "running" {
			existing.SetMode(goalModeID)
		}
		if existing.Mode() != "" {
			currentMode = existing.Mode()
		} else {
			existing.SetMode(currentMode)
		}
	} else {
		acpSession := NewACPServerSession(req.SessionId, workDir, a.conn, string(req.SessionId))
		if persistedGoal != nil {
			acpSession.SetGoal(persistedGoal)
			if persistedGoal.Status == "running" {
				currentMode = goalModeID
			}
		}
		acpSession.SetMode(currentMode)
		acpSession.SetAskPermission(defaultAskPermissionForMode(currentMode))
		a.sessions[req.SessionId] = acpSession
	}
	a.sessionsMu.Unlock()

	a.logger.Printf("[ACP AGENT] ResumeSession: session %s resumed", req.SessionId)
	acpSession, _ := a.getSession(req.SessionId)
	configOptions := buildSessionConfigOptions(a.agentService, acpSession.Model(), currentMode, acpSession.Persona(), acpSession.AskPermission())
	return acpsdk.ResumeSessionResponse{
		ConfigOptions: buildUnstableSessionConfigOptions(configOptions),
		Modes:         buildSessionModeState(a.agentService, currentMode),
		Meta:          mergeMetaMaps(personaStateToMeta(buildSessionPersonaState(a.agentService, acpSession.Persona())), goalMeta(acpSession.Goal())),
	}, nil
}

// SetSessionPersona stores the requested persona on the ACP session for use in future prompts.
// This is a Pando-specific extension, not part of the standard ACP protocol.
func (a *PandoACPAgent) SetSessionPersona(ctx context.Context, sessionID acpsdk.SessionId, personaName string) error {
	a.logger.Printf("[ACP AGENT] SetSessionPersona: SessionID=%s, Persona=%s", sessionID, personaName)

	acpSession, err := a.getSession(sessionID)
	if err != nil {
		return err
	}
	if err := a.validatePersona(personaName); err != nil {
		return err
	}

	acpSession.SetPersona(personaName)
	a.logger.Printf("[ACP AGENT] SetSessionPersona: persona set to %q for session %s", personaName, sessionID)
	a.sendSessionConfigOptionsUpdate(ctx, sessionID)
	return nil
}

func (a *PandoACPAgent) HandleExtensionMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	handler, ok := a.extensionHandlers[method]
	if !ok {
		return nil, fmt.Errorf("unknown extension method: %s", method)
	}
	return handler(ctx, params)
}

func (a *PandoACPAgent) getSession(sessionID acpsdk.SessionId) (*ACPServerSession, error) {
	a.sessionsMu.RLock()
	acpSession, exists := a.sessions[sessionID]
	a.sessionsMu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return acpSession, nil
}

func (a *PandoACPAgent) validatePersona(personaName string) error {
	if personaName == "" {
		return nil
	}

	available := a.agentService.ListPersonas()
	for _, p := range available {
		if p == personaName {
			return nil
		}
	}
	return fmt.Errorf("unknown persona: %s", personaName)
}

func (a *PandoACPAgent) validateModel(modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return fmt.Errorf("model is required")
	}
	for _, model := range a.agentService.AvailableModels() {
		if strings.TrimSpace(model.ID) == modelID {
			return nil
		}
	}
	return fmt.Errorf("unknown model: %s", modelID)
}

func (a *PandoACPAgent) sendCurrentModeUpdate(_ context.Context, sessionID acpsdk.SessionId, modeID string) {
	if a.conn == nil {
		return
	}
	acpSession, err := a.getSession(sessionID)
	if err != nil {
		return
	}
	if err := a.safeSessionUpdate(acpSession, sessionID, acpsdk.UpdateCurrentMode(acpsdk.SessionModeId(modeID))); err != nil {
		a.logger.Printf("[ACP AGENT] Failed to send current_mode_update for session %s: %v", sessionID, err)
	}
}

func (a *PandoACPAgent) sendSessionConfigOptionsUpdate(_ context.Context, sessionID acpsdk.SessionId) {
	if a.conn == nil {
		return
	}
	acpSession, err := a.getSession(sessionID)
	if err != nil {
		return
	}
	update := acpsdk.UpdateConfigOptions(buildSessionConfigOptions(a.agentService, acpSession.Model(), acpSession.Mode(), acpSession.Persona(), acpSession.AskPermission()))
	if err := a.safeSessionUpdate(acpSession, sessionID, update); err != nil {
		a.logger.Printf("[ACP AGENT] Failed to send config_option_update for session %s: %v", sessionID, err)
	}
}

func (a *PandoACPAgent) safeSessionUpdate(acpSession *ACPServerSession, sessionID acpsdk.SessionId, update acpsdk.SessionUpdate) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic sending session update: %v", r)
		}
	}()
	if acpSession == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	sendErr := acpSession.SendUpdate(update)
	if sendErr != nil && isEntityReleasedError(sendErr) {
		// The ACP client released this session entity (e.g. Zed closed the thread).
		// Remove it from our map so we stop sending updates to a ghost session.
		a.sessionsMu.Lock()
		delete(a.sessions, sessionID)
		a.sessionsMu.Unlock()
		a.logger.Printf("[ACP AGENT] Session %s removed: ACP client released the entity", sessionID)
	}
	return sendErr
}

// isEntityReleasedError returns true when the error originates from the ACP
// client reporting that the session entity was already released. Zed encodes
// this as a JSON-RPC -32603 error with data="entity released".
func isEntityReleasedError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "entity released")
}

func (a *PandoACPAgent) currentUsageSnapshot(ctx context.Context, acpSession *ACPServerSession) (used int, size int, ok bool) {
	if acpSession == nil {
		return 0, 0, false
	}
	sessionInfo, err := a.sessionService.GetSession(ctx, acpSession.PandoSessionID())
	if err != nil {
		a.logger.Printf("[ACP AGENT] Could not fetch session usage snapshot: %v", err)
		return 0, 0, false
	}
	used = int(sessionInfo.PromptTokens + sessionInfo.CompletionTokens)
	size = int(sessionInfo.ContextWindow)
	if size == 0 {
		size = 200000 // safe default for modern frontier models
	}
	return used, size, true
}

func (a *PandoACPAgent) normalizeSystemMessage(ctx context.Context, acpSession *ACPServerSession, msg string) (*acpsdk.SessionUpdate, string, bool) {
	normalized := strings.TrimSpace(msg)
	if normalized == "" {
		return nil, "", true
	}
	lowerMsg := strings.ToLower(normalized)

	switch {
	case strings.Contains(lowerMsg, "selected persona:"):
		return nil, "", true
	case strings.Contains(lowerMsg, "auto-compacting context") || strings.Contains(lowerMsg, "compacting context"):
		return nil, "Compacting...", false
	case strings.Contains(lowerMsg, "context compacted"):
		if used, size, ok := a.currentUsageSnapshot(ctx, acpSession); ok {
			if used < 0 {
				used = 0
			}
			update := acpsdk.UpdateUsage(used, size)
			return &update, "\n\nCompacting completed.", false
		}
		return nil, "\n\nCompacting completed.", false
	case strings.Contains(lowerMsg, "continuing with trimmed history"):
		return nil, "Context compaction failed; continuing with trimmed history.", false
	case strings.Contains(lowerMsg, "retrying with fallback model"):
		return nil, normalized, false
	case strings.Contains(lowerMsg, "retrying due to rate limit"):
		return nil, normalized, false
	case strings.Contains(lowerMsg, "rate limit") && strings.Contains(lowerMsg, "attempt"):
		return nil, normalized, false
	case strings.Contains(lowerMsg, "authentication failed"):
		return nil, normalized, false
	case strings.Contains(lowerMsg, "oauth required"):
		return nil, "Authentication required. Please sign in again.", false
	case strings.Contains(lowerMsg, "quota exceeded") || strings.Contains(lowerMsg, "too many requests"):
		return nil, normalized, false
	default:
		return nil, normalized, false
	}
}

func (a *PandoACPAgent) sendRunStatusMeta(_ context.Context, sessionID acpsdk.SessionId, acpSession *ACPServerSession) {
	if a.conn == nil || acpSession == nil {
		return
	}
	messages := a.agentService.LastRunSystemMessages(acpSession.PandoSessionID())
	if len(messages) == 0 {
		return
	}
	meta := map[string]any{
		"pando:serverEvents": messages,
	}
	update := acpsdk.UpdateSessionInfo(acpsdk.SessionInfoUpdate{Meta: meta})
	if err := a.safeSessionUpdate(acpSession, sessionID, update); err != nil {
		a.logger.Printf("[ACP AGENT] Failed to send session_info_update meta for session %s: %v", sessionID, err)
	}
}

func parseSessionConfigOptionRequest(req acpsdk.SetSessionConfigOptionRequest) (acpsdk.SessionId, string, string, error) {
	switch {
	case req.ValueId != nil:
		return req.ValueId.SessionId, string(req.ValueId.ConfigId), strings.TrimSpace(string(req.ValueId.Value)), nil
	case req.Boolean != nil:
		return req.Boolean.SessionId, string(req.Boolean.ConfigId), boolToAskPermissionValue(req.Boolean.Value), nil
	default:
		return "", "", "", fmt.Errorf("invalid config option request")
	}
}

func parseAskPermissionValue(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case askPermissionYesValue, "true":
		return true, nil
	case askPermissionNoValue, "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid askPermission value: %s", value)
	}
}

func defaultAskPermissionForMode(modeID string) bool {
	switch strings.TrimSpace(modeID) {
	case askModeID:
		return true
	case goalModeID:
		return !goalAutoApproveEnabled()
	default:
		return false
	}
}

func goalAutoApproveEnabled() bool {
	cfg := config.Get()
	if cfg == nil {
		return true
	}
	return cfg.Goal.AutoApprove
}

func dangerousGoalPatterns() []string {
	cfg := config.Get()
	if cfg == nil {
		return nil
	}
	return cfg.Goal.DangerousPatterns
}

func isDangerousPermissionRequest(req PermissionRequestData) bool {
	if !strings.EqualFold(strings.TrimSpace(req.ToolName), "bash") || req.Params == nil {
		return false
	}

	var params struct {
		Command string `json:"command"`
	}
	data, err := json.Marshal(req.Params)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(data, &params); err != nil {
		return false
	}

	return safety.IsDangerousShellCommand(params.Command, dangerousGoalPatterns())
}

func (a *PandoACPAgent) configurePermissionMode(sessionID acpsdk.SessionId, mode string, askPermission bool) func() {
	if a.permissionService == nil {
		return func() {}
	}

	sid := string(sessionID)
	cleanup := func() {
		a.permissionService.RemoveAutoApproveSession(sid)
		a.permissionService.UnregisterSessionHandler(sid)
	}
	cleanup()

	switch {
	case askPermission:
		if a.conn == nil {
			a.logger.Printf("[ACP AGENT] Ask permission enabled for session %s but no ACP connection is available; using default permission handling", sessionID)
			return cleanup
		}
		bridge := NewACPPermissionBridge(a.conn, sessionID, a.logger)
		a.permissionService.RegisterSessionHandler(sid, bridge.Handle)
	case mode == goalModeID:
		goalBridge := func(req PermissionRequestData) bool {
			if !goalAutoApproveEnabled() {
				if a.conn == nil {
					a.logger.Printf("[ACP AGENT] Goal mode requires permission prompts for session %s but no ACP connection is available; denying tool %s", sessionID, req.ToolName)
					return false
				}
				return NewACPPermissionBridge(a.conn, sessionID, a.logger).Handle(req)
			}
			if !isDangerousPermissionRequest(req) {
				return true
			}
			if a.conn == nil {
				a.logger.Printf("[ACP AGENT] Dangerous goal-mode tool call requires confirmation but no ACP connection is available; denying tool %s", req.ToolName)
				return false
			}
			return NewACPPermissionBridge(a.conn, sessionID, a.logger).Handle(req)
		}
		a.permissionService.RegisterSessionHandler(sid, goalBridge)
	default:
		a.permissionService.AutoApproveSession(sid)
	}

	return cleanup
}

func validateModeID(modeID string) error {
	switch strings.TrimSpace(modeID) {
	case agentModeID, askModeID, goalModeID:
		return nil
	default:
		return fmt.Errorf("unknown mode: %s", modeID)
	}
}

func (a *PandoACPAgent) mustSessionAskPermission(sessionID acpsdk.SessionId) bool {
	acpSession, err := a.getSession(sessionID)
	if err != nil {
		return false
	}
	return acpSession.AskPermission()
}

func (a *PandoACPAgent) mustSessionModel(sessionID acpsdk.SessionId) string {
	acpSession, err := a.getSession(sessionID)
	if err != nil {
		return ""
	}
	return acpSession.Model()
}

type extensionSetPersonaParams struct {
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
}

func (a *PandoACPAgent) handleExtensionSetPersona(ctx context.Context, params json.RawMessage) (any, error) {
	var req extensionSetPersonaParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return nil, fmt.Errorf("sessionId is required")
	}

	if err := a.SetSessionPersona(ctx, acpsdk.SessionId(req.SessionID), strings.TrimSpace(req.Name)); err != nil {
		return nil, err
	}
	return personaGetResult{Active: strings.TrimSpace(req.Name)}, nil
}

func (a *PandoACPAgent) handleExtensionOpenCopilotUsage(context.Context, json.RawMessage) (any, error) {
	const url = "https://github.com/settings/copilot/features"
	if err := a.agentService.OpenCopilotUsage(); err != nil {
		return nil, err
	}
	return usageOpenResult{Opened: true, URL: url}, nil
}

func (a *PandoACPAgent) handleExtensionOpenClaudeUsage(context.Context, json.RawMessage) (any, error) {
	const url = "https://claude.ai/settings/usage"
	if err := a.agentService.OpenClaudeUsage(); err != nil {
		return nil, err
	}
	return usageOpenResult{Opened: true, URL: url}, nil
}

// SetConnection stores a reference to the AgentSideConnection so the agent can
// stream session updates back to the client.  Called by transport_stdio.go
// immediately after NewAgentSideConnection() returns.
//
// NOTE: we deliberately do NOT send sendAvailableCommandsUpdate for all known
// sessions here. When the transport reconnects (e.g. Zed restarts), existing
// sessions in the map were registered during the previous connection. The new
// client does not know about them yet and will report "unknown session" warnings
// for any proactive notifications. Instead, each session receives its available
// commands only when the client explicitly calls NewSession or LoadSession,
// both of which already call sendAvailableCommandsUpdate.
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

// StartNotificationBroadcast subscribes to the global notify bus and fans out
// each Notification to all currently active ACP sessions via a
// SessionSessionInfoUpdate with _meta["pando:notification"] carrying the payload.
// This allows ACP clients to receive LLM retry info, tool errors, LSP diagnostics,
// etc. in real time alongside the normal session stream.
//
// Call this in a goroutine after creating the agent; it blocks until ctx is done.
func (a *PandoACPAgent) StartNotificationBroadcast(ctx context.Context) {
	ch := notify.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if event.Type != pubsub.CreatedEvent {
				continue
			}
			n := event.Payload
			meta := map[string]any{
				"pando:notification": map[string]any{
					"id":      n.ID,
					"time":    n.Time,
					"level":   string(n.Level),
					"source":  string(n.Source),
					"message": n.Message,
					"ttl":     int64(n.TTL),
				},
			}
			var visibleMessage string
			if n.Source == notify.SourceLLMProvider {
				if _, normalized, suppress := a.normalizeSystemMessage(ctx, nil, n.Message); !suppress {
					visibleMessage = normalized
				}
			}
			update := acpsdk.UpdateSessionInfo(acpsdk.SessionInfoUpdate{
				Meta: meta,
			})

			a.sessionsMu.RLock()
			sessions := make([]*ACPServerSession, 0, len(a.sessions))
			for _, sess := range a.sessions {
				sessions = append(sessions, sess)
			}
			a.sessionsMu.RUnlock()

			for _, sess := range sessions {
				if err := sess.SendUpdate(update); err != nil {
					if isEntityReleasedError(err) {
						a.sessionsMu.Lock()
						delete(a.sessions, sess.ID)
						a.sessionsMu.Unlock()
						a.logger.Printf("[ACP AGENT] Session %s removed during broadcast: ACP client released the entity", sess.ID)
					} else {
						a.logger.Printf("[ACP AGENT] Failed to broadcast notification to session %s: %v", sess.ID, err)
					}
					continue
				}
				if visibleMessage != "" {
					if err := sess.SendUpdate(acpsdk.UpdateAgentMessageText(visibleMessage)); err != nil {
						a.logger.Printf("[ACP AGENT] Failed to broadcast visible notification to session %s: %v", sess.ID, err)
					}
				}
			}
		}
	}
}
