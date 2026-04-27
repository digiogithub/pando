package acp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	acpsdk "github.com/madeindigio/acp-go-sdk"
	"github.com/digiogithub/pando/internal/notify"
	"github.com/digiogithub/pando/internal/pubsub"
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

	// clientSupportsWriteFile indicates the connected client supports fs/write_text_file.
	// Set during Initialize from ClientCapabilities.Fs.WriteTextFile.
	clientSupportsWriteFile bool

	// pendingToolCalls maps tool call IDs to their raw input JSON for edit/write operations.
	// Used to extract the file path when sending WriteTextFile after a successful tool result.
	pendingToolCallsMu sync.Mutex
	pendingToolCalls   map[string]string

	// startedToolCalls tracks tool calls already announced to the client in the
	// live streaming path so we can guarantee a tool_call exists before any
	// tool_call_update for the same ID.
	startedToolCalls map[string]bool
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
		startedToolCalls:  make(map[string]bool),
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

	// Send available_commands_update asynchronously after session creation so
	// clients (Zed, multicoder, etc.) can display the tool names and descriptions.
	go a.sendAvailableCommandsUpdate(context.Background(), sessionID)

	return acpsdk.NewSessionResponse{
		SessionId: sessionID,
		Modes:     buildSessionModeState(a.agentService, currentMode, acpSession.Persona()),
		Models:    buildSessionModelState(a.agentService),
		Meta:      personaStateToMeta(buildSessionPersonaState(a.agentService)),
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

	// Send usage_update and session_info_update after the prompt completes.
	// Fetch the latest session state so we have accurate token counts and title.
	if sessionInfo, sessErr := a.sessionService.GetSession(ctx, acpSession.PandoSessionID()); sessErr == nil {
		// usage_update: report tokens currently in context.
		// used = input + output tokens for this turn; size = model context window.
		used := int(sessionInfo.PromptTokens + sessionInfo.CompletionTokens)
		size := int(sessionInfo.ContextWindow)
		if size == 0 {
			size = 200000 // safe default for modern frontier models
		}
		if used > 0 {
			if sendErr := acpSession.SendUpdate(acpsdk.UpdateUsage(used, size)); sendErr != nil {
				a.logger.Printf("[ACP AGENT] Failed to send usage_update: %v", sendErr)
			}
		}

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
		req.SessionId, stopReason)

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

	var currentPersona string
	a.sessionsMu.RLock()
	if sess, ok := a.sessions[req.SessionId]; ok {
		currentPersona = sess.Persona()
	}
	a.sessionsMu.RUnlock()

	// Send available_commands_update asynchronously so clients can display tool names.
	go a.sendAvailableCommandsUpdate(context.Background(), req.SessionId)

	// Stream the full conversation history back to the client as required by the ACP protocol:
	// "Stream the entire conversation history back to the client via notifications"
	go a.streamSessionHistory(context.Background(), req.SessionId, string(req.SessionId))

	return acpsdk.LoadSessionResponse{
		Modes:  buildSessionModeState(a.agentService, currentMode, currentPersona),
		Models: buildSessionModelState(a.agentService),
		Meta:   personaStateToMeta(buildSessionPersonaState(a.agentService)),
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
	baseMode := modeID
	personaName := ""

	if strings.Contains(modeID, ":") {
		parts := strings.SplitN(modeID, ":", 2)
		personaName = strings.TrimSpace(parts[0])
		baseMode = strings.TrimSpace(parts[1])
	}

	validModes := map[string]bool{"agent": true, "ask": true}
	if !validModes[baseMode] {
		return acpsdk.SetSessionModeResponse{}, fmt.Errorf("unknown mode: %s", req.ModeId)
	}

	if personaName != "" {
		available := a.agentService.ListPersonas()
		found := false
		for _, p := range available {
			if p == personaName {
				found = true
				break
			}
		}
		if !found {
			return acpsdk.SetSessionModeResponse{}, fmt.Errorf("unknown persona: %s", personaName)
		}
	}

	acpSession.SetPersona(personaName)
	acpSession.SetMode(baseMode)
	a.logger.Printf("[ACP AGENT] Session mode set: SessionID=%s, Mode=%s, Persona=%q (mode will take effect on next prompt)", req.SessionId, baseMode, personaName)

	return acpsdk.SetSessionModeResponse{}, nil
}

// SetSessionConfigOption handles runtime configuration changes from the client.
// Currently treated as a no-op; individual options are not yet mapped to agent settings.
func (a *PandoACPAgent) SetSessionConfigOption(_ context.Context, req acpsdk.SetSessionConfigOptionRequest) (acpsdk.SetSessionConfigOptionResponse, error) {
	a.logger.Printf("[ACP AGENT] SetSessionConfigOption: (not implemented)")
	return acpsdk.SetSessionConfigOptionResponse{}, nil
}

// UnstableSetSessionModel implements AgentExperimental.
// It stores the requested model on the ACP session for use in future prompts.
func (a *PandoACPAgent) UnstableSetSessionModel(ctx context.Context, req acpsdk.UnstableSetSessionModelRequest) (acpsdk.UnstableSetSessionModelResponse, error) {
	a.logger.Printf("[ACP AGENT] SetSessionModel: SessionID=%s, ModelID=%s", req.SessionId, req.ModelId)

	a.sessionsMu.RLock()
	acpSession, exists := a.sessions[req.SessionId]
	a.sessionsMu.RUnlock()

	if !exists {
		return acpsdk.UnstableSetSessionModelResponse{}, fmt.Errorf("session not found: %s", req.SessionId)
	}

	acpSession.SetModel(string(req.ModelId))
	a.logger.Printf("[ACP AGENT] SetSessionModel: model set to %s for session %s", req.ModelId, req.SessionId)
	return acpsdk.UnstableSetSessionModelResponse{}, nil
}

// SetSessionPersona stores the requested persona on the ACP session for use in future prompts.
// This is a Pando-specific extension, not part of the standard ACP protocol.
func (a *PandoACPAgent) SetSessionPersona(ctx context.Context, sessionID acpsdk.SessionId, personaName string) error {
	a.logger.Printf("[ACP AGENT] SetSessionPersona: SessionID=%s, Persona=%s", sessionID, personaName)

	a.sessionsMu.RLock()
	acpSession, exists := a.sessions[sessionID]
	a.sessionsMu.RUnlock()

	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Validate persona exists (empty string means "clear / auto").
	if personaName != "" {
		available := a.agentService.ListPersonas()
		found := false
		for _, p := range available {
			if p == personaName {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown persona: %s", personaName)
		}
	}

	acpSession.SetPersona(personaName)
	a.logger.Printf("[ACP AGENT] SetSessionPersona: persona set to %q for session %s", personaName, sessionID)
	return nil
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
			update := acpsdk.SessionUpdate{
				SessionInfoUpdate: &acpsdk.SessionSessionInfoUpdate{
					SessionUpdate: "session_info_update",
					Meta:          meta,
				},
			}

			a.sessionsMu.RLock()
			sessions := make([]*ACPServerSession, 0, len(a.sessions))
			for _, sess := range a.sessions {
				sessions = append(sessions, sess)
			}
			a.sessionsMu.RUnlock()

			for _, sess := range sessions {
				if err := sess.SendUpdate(update); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to broadcast notification to session %s: %v", sess.ID, err)
				}
			}
		}
	}
}
