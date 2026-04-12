package acp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	acpsdk "github.com/coder/acp-go-sdk"
)

// MesnadaACPClient implements the ACP client interface required by the SDK.
// It handles callbacks from ACP agents, managing file operations, terminal sessions,
// and session state updates for mesnada tasks.
type MesnadaACPClient struct {
	// taskID is the mesnada task identifier
	taskID string

	// workDir is the working directory for file operations
	workDir string

	// logFile is the file descriptor for logging agent output
	logFile *os.File

	// output accumulates the full output from the agent
	output *strings.Builder

	// onUpdate is the callback function for session updates
	onUpdate func(update SessionUpdateInfo)

	// mu protects concurrent access to output and onUpdate
	mu sync.Mutex

	// toolCalls tracks started tool calls to enrich subsequent updates
	toolCalls map[string]ToolCallInfo

	// terminals tracks active terminal sessions
	terminals map[string]*terminalState

	// terminalsMu protects concurrent access to terminals
	terminalsMu sync.RWMutex

	// toolCallCount tracks the number of tool calls made
	toolCallCount int

	// currentProgress tracks the current progress percentage
	currentProgress int

	// currentPlanDescription tracks the current plan description
	currentPlanDescription string

	// autoPermission controls whether to auto-approve all permission requests
	autoPermission bool

	// permissionQueue manages manual permission requests
	permissionQueue *PermissionQueue

	// capabilities defines what the agent is allowed to do
	capabilities ACPCapabilities
}

// terminalState represents the state of an active terminal session.
type terminalState struct {
	id         string
	cmd        *exec.Cmd
	command    string
	args       []string
	cwd        string
	outputBuf  bytes.Buffer
	exitCh     chan int
	cancelF    context.CancelFunc
	mu         sync.Mutex
	exitCode   *int
	isRunning  bool
	isReleased bool
}

// ACPCapabilities defines what an ACP agent is allowed to do.
type ACPCapabilities struct {
	Terminals   bool
	FileAccess  bool
	Permissions bool
}

// NewMesnadaACPClient creates a new MesnadaACPClient.
func NewMesnadaACPClient(taskID string, workDir string, logFile *os.File, onUpdate func(SessionUpdateInfo)) *MesnadaACPClient {
	return &MesnadaACPClient{
		taskID:          taskID,
		workDir:         workDir,
		logFile:         logFile,
		output:          &strings.Builder{},
		onUpdate:        onUpdate,
		toolCalls:       make(map[string]ToolCallInfo),
		terminals:       make(map[string]*terminalState),
		autoPermission:  false, // Default to requiring manual approval
		permissionQueue: NewPermissionQueue(),
		capabilities: ACPCapabilities{
			Terminals:   true, // Default capabilities
			FileAccess:  true,
			Permissions: true,
		},
	}
}

// SetAutoPermission sets whether to auto-approve all permission requests.
func (c *MesnadaACPClient) SetAutoPermission(auto bool) {
	c.autoPermission = auto
}

// SetCapabilities sets the capabilities for this client.
func (c *MesnadaACPClient) SetCapabilities(caps ACPCapabilities) {
	c.capabilities = caps
}

// GetPermissionQueue returns the permission queue for manual approval.
func (c *MesnadaACPClient) GetPermissionQueue() *PermissionQueue {
	return c.permissionQueue
}

// SessionUpdate handles session updates from the ACP agent.
// This is called when the agent sends new messages, tool calls, or state changes.
func (c *MesnadaACPClient) SessionUpdate(ctx context.Context, params acpsdk.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	update := params.Update

	// Process agent message chunks (text output)
	if update.AgentMessageChunk != nil {
		c.processAgentMessageChunk(update.AgentMessageChunk)
	}

	// Process agent thought chunks (thinking/reasoning)
	if update.AgentThoughtChunk != nil {
		c.processAgentThoughtChunk(update.AgentThoughtChunk)
	}

	// Process tool call notifications
	if update.ToolCall != nil {
		c.processToolCall(update.ToolCall)
	}

	// Process tool call updates
	if update.ToolCallUpdate != nil {
		c.processToolCallUpdate(update.ToolCallUpdate)
	}

	// Process plan updates
	if update.Plan != nil {
		c.processPlan(update.Plan)
	}

	return nil
}

// RequestPermission handles permission requests from the ACP agent.
// In auto-permission mode (CI/batch), this auto-approves all requests.
// In manual mode, this queues the request for approval via API endpoints.
func (c *MesnadaACPClient) RequestPermission(ctx context.Context, params acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	toolCallTitle := "unknown"
	if params.ToolCall.Title != nil && *params.ToolCall.Title != "" {
		toolCallTitle = *params.ToolCall.Title
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[PERMISSION REQUEST] SessionID=%s, ToolCall=%s, Options=%d\n",
			params.SessionId, toolCallTitle, len(params.Options))
	}

	// Auto-permission mode: approve everything automatically
	if c.autoPermission {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[PERMISSION] Auto-approved: %s\n", toolCallTitle)
		}

		// Select the first option by default
		if len(params.Options) > 0 {
			outcome := acpsdk.NewRequestPermissionOutcomeSelected(params.Options[0].OptionId)
			return acpsdk.RequestPermissionResponse{
				Outcome: outcome,
			}, nil
		}

		// No options available - this shouldn't happen but handle it
		return acpsdk.RequestPermissionResponse{}, fmt.Errorf("no permission options available")
	}

	// Manual mode: queue for approval
	requestID := c.permissionQueue.QueuePermission(c.taskID, params.SessionId, params)

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[PERMISSION] Queued for approval: %s (ID: %s)\n", toolCallTitle, requestID)
	}

	// Wait for resolution (with context cancellation support)
	outcome, err := c.permissionQueue.WaitForResolution(ctx, requestID)
	if err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[PERMISSION] Error waiting for resolution: %v\n", err)
		}
		return acpsdk.RequestPermissionResponse{}, err
	}

	if c.logFile != nil {
		if outcome.Selected != nil {
			fmt.Fprintf(c.logFile, "[PERMISSION] Resolved (selected option %s): %s\n",
				outcome.Selected.OptionId, toolCallTitle)
		} else if outcome.Cancelled != nil {
			fmt.Fprintf(c.logFile, "[PERMISSION] Cancelled: %s\n", toolCallTitle)
		}
	}

	return acpsdk.RequestPermissionResponse{
		Outcome: outcome,
	}, nil
}

// ReadTextFile reads a file from the task's workspace.
// This implements strict security: the file must be within the task's workDir.
func (c *MesnadaACPClient) ReadTextFile(ctx context.Context, params acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	// Check file access capability
	if !c.capabilities.FileAccess {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[READ FILE DENIED] File access not allowed\n")
		}
		return acpsdk.ReadTextFileResponse{}, fmt.Errorf("file access not allowed for this agent")
	}

	// Resolve the path and validate it's within the workspace
	absPath, err := c.validatePathInWorkspace(params.Path)
	if err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[READ FILE DENIED] Path validation failed: %v\n", err)
		}
		return acpsdk.ReadTextFileResponse{}, err
	}

	// Read the file
	content, err := os.ReadFile(absPath)
	if err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[READ FILE ERROR] Failed to read %s: %v\n", params.Path, err)
		}
		return acpsdk.ReadTextFileResponse{}, fmt.Errorf("failed to read file: %w", err)
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[READ FILE] %s (%d bytes)\n", params.Path, len(content))
	}

	return acpsdk.ReadTextFileResponse{
		Content: string(content),
	}, nil
}

// validatePathInWorkspace validates that a path is within the workspace and returns the absolute path.
// This prevents path traversal attacks (e.g., "../../../etc/passwd").
func (c *MesnadaACPClient) validatePathInWorkspace(path string) (string, error) {
	// Reject absolute paths immediately
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("access denied: absolute paths not allowed (attempted: %s)", path)
	}

	// Clean the path to resolve any ".." or "." components
	cleanPath := filepath.Clean(path)

	// Convert to absolute path relative to workDir
	absPath := filepath.Join(c.workDir, cleanPath)

	// Ensure the absolute path is within the workspace
	// Use filepath.Rel to check if absPath is a subdirectory of workDir
	relPath, err := filepath.Rel(c.workDir, absPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// If the relative path starts with "..", it's trying to escape the workspace
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("access denied: path outside workspace (attempted: %s)", path)
	}

	return absPath, nil
}

// WriteTextFile writes a file to the task's workspace.
// This implements strict security: the file must be within the task's workDir.
func (c *MesnadaACPClient) WriteTextFile(ctx context.Context, params acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	// Check file access capability
	if !c.capabilities.FileAccess {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[WRITE FILE DENIED] File access not allowed\n")
		}
		return acpsdk.WriteTextFileResponse{}, fmt.Errorf("file access not allowed for this agent")
	}

	// Resolve the path and validate it's within the workspace
	absPath, err := c.validatePathInWorkspace(params.Path)
	if err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[WRITE FILE DENIED] Path validation failed: %v\n", err)
		}
		return acpsdk.WriteTextFileResponse{}, err
	}

	// Create parent directories if they don't exist
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[WRITE FILE ERROR] Failed to create directory %s: %v\n", dir, err)
		}
		return acpsdk.WriteTextFileResponse{}, fmt.Errorf("failed to create directory: %w", err)
	}

	// Write the file
	err = os.WriteFile(absPath, []byte(params.Content), 0644)
	if err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[WRITE FILE ERROR] Failed to write %s: %v\n", params.Path, err)
		}
		return acpsdk.WriteTextFileResponse{}, fmt.Errorf("failed to write file: %w", err)
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[WRITE FILE] %s (%d bytes)\n", params.Path, len(params.Content))
	}

	return acpsdk.WriteTextFileResponse{}, nil
}

// CreateTerminal creates a new terminal session for executing commands.
func (c *MesnadaACPClient) CreateTerminal(ctx context.Context, params acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	// Check terminal capability
	if !c.capabilities.Terminals {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[CREATE TERMINAL DENIED] Terminal access not allowed\n")
		}
		return acpsdk.CreateTerminalResponse{}, fmt.Errorf("terminal access not allowed for this agent")
	}

	// Generate terminal ID
	c.terminalsMu.Lock()
	terminalID := fmt.Sprintf("terminal-%s-%d", c.taskID, len(c.terminals)+1)
	c.terminalsMu.Unlock()

	// Determine working directory
	cwd := c.workDir
	if params.Cwd != nil && *params.Cwd != "" {
		// Validate that cwd is within workspace
		requestedCwd, err := c.validatePathInWorkspace(*params.Cwd)
		if err != nil {
			if c.logFile != nil {
				fmt.Fprintf(c.logFile, "[CREATE TERMINAL DENIED] Invalid cwd: %v\n", err)
			}
			return acpsdk.CreateTerminalResponse{}, fmt.Errorf("invalid cwd: %w", err)
		}
		cwd = requestedCwd
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CREATE TERMINAL] ID=%s, Command=%s, Args=%v, Cwd=%s\n",
			terminalID, params.Command, params.Args, cwd)
	}

	// Create context with cancellation
	termCtx, cancel := context.WithCancel(ctx)

	// Create the command
	cmd := exec.CommandContext(termCtx, params.Command, params.Args...)
	cmd.Dir = cwd

	// Create terminal state
	term := &terminalState{
		id:        terminalID,
		cmd:       cmd,
		command:   params.Command,
		args:      params.Args,
		cwd:       cwd,
		exitCh:    make(chan int, 1),
		cancelF:   cancel,
		isRunning: true,
	}

	// Capture output
	cmd.Stdout = &term.outputBuf
	cmd.Stderr = &term.outputBuf

	// Store terminal state before starting
	c.terminalsMu.Lock()
	c.terminals[terminalID] = term
	c.terminalsMu.Unlock()

	// Start the command in a goroutine
	go func() {
		err := cmd.Run()

		term.mu.Lock()
		term.isRunning = false

		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1 // Generic error
			}
		}
		term.exitCode = &exitCode
		term.mu.Unlock()

		// Notify exit
		select {
		case term.exitCh <- exitCode:
		default:
		}

		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[TERMINAL EXIT] ID=%s, ExitCode=%d\n", terminalID, exitCode)
		}
	}()

	return acpsdk.CreateTerminalResponse{
		TerminalId: terminalID,
	}, nil
}

// TerminalOutput retrieves the current output from a terminal session.
func (c *MesnadaACPClient) TerminalOutput(ctx context.Context, params acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	c.terminalsMu.RLock()
	term, exists := c.terminals[params.TerminalId]
	c.terminalsMu.RUnlock()

	if !exists {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[TERMINAL OUTPUT ERROR] Terminal not found: %s\n", params.TerminalId)
		}
		return acpsdk.TerminalOutputResponse{}, fmt.Errorf("terminal not found: %s", params.TerminalId)
	}

	term.mu.Lock()
	output := term.outputBuf.String()
	term.mu.Unlock()

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[TERMINAL OUTPUT] ID=%s, Bytes=%d\n", params.TerminalId, len(output))
	}

	return acpsdk.TerminalOutputResponse{
		Output: output,
	}, nil
}

// ReleaseTerminal releases a terminal session without waiting for it to complete.
// The terminal continues running but we stop tracking it.
func (c *MesnadaACPClient) ReleaseTerminal(ctx context.Context, params acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	c.terminalsMu.Lock()
	term, exists := c.terminals[params.TerminalId]
	if exists {
		term.mu.Lock()
		term.isReleased = true
		term.mu.Unlock()
	}
	c.terminalsMu.Unlock()

	if !exists {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[RELEASE TERMINAL ERROR] Terminal not found: %s\n", params.TerminalId)
		}
		return acpsdk.ReleaseTerminalResponse{}, fmt.Errorf("terminal not found: %s", params.TerminalId)
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[RELEASE TERMINAL] ID=%s (continues running in background)\n", params.TerminalId)
	}

	return acpsdk.ReleaseTerminalResponse{}, nil
}

// WaitForTerminalExit waits for a terminal session to complete and returns its exit code.
func (c *MesnadaACPClient) WaitForTerminalExit(ctx context.Context, params acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	c.terminalsMu.RLock()
	term, exists := c.terminals[params.TerminalId]
	c.terminalsMu.RUnlock()

	if !exists {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[WAIT TERMINAL ERROR] Terminal not found: %s\n", params.TerminalId)
		}
		return acpsdk.WaitForTerminalExitResponse{}, fmt.Errorf("terminal not found: %s", params.TerminalId)
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[WAIT TERMINAL] ID=%s, waiting for exit...\n", params.TerminalId)
	}

	// Check if already exited
	term.mu.Lock()
	if term.exitCode != nil {
		exitCode := *term.exitCode
		term.mu.Unlock()

		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[WAIT TERMINAL] ID=%s already exited with code %d\n", params.TerminalId, exitCode)
		}

		return acpsdk.WaitForTerminalExitResponse{
			ExitCode: &exitCode,
		}, nil
	}
	term.mu.Unlock()

	// Wait for exit or context cancellation
	select {
	case exitCode := <-term.exitCh:
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[WAIT TERMINAL] ID=%s exited with code %d\n", params.TerminalId, exitCode)
		}
		return acpsdk.WaitForTerminalExitResponse{
			ExitCode: &exitCode,
		}, nil
	case <-ctx.Done():
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[WAIT TERMINAL] ID=%s wait cancelled\n", params.TerminalId)
		}
		return acpsdk.WaitForTerminalExitResponse{}, ctx.Err()
	}
}

// KillTerminalCommand kills a running terminal command.
func (c *MesnadaACPClient) KillTerminalCommand(ctx context.Context, params acpsdk.KillTerminalCommandRequest) (acpsdk.KillTerminalCommandResponse, error) {
	c.terminalsMu.Lock()
	term, exists := c.terminals[params.TerminalId]
	c.terminalsMu.Unlock()

	if !exists {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[KILL TERMINAL ERROR] Terminal not found: %s\n", params.TerminalId)
		}
		return acpsdk.KillTerminalCommandResponse{}, fmt.Errorf("terminal not found: %s", params.TerminalId)
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[KILL TERMINAL] ID=%s, sending kill signal\n", params.TerminalId)
	}

	// Cancel the context to trigger command termination
	if term.cancelF != nil {
		term.cancelF()
	}

	// Also try to kill the process directly if it's still running
	term.mu.Lock()
	if term.cmd != nil && term.cmd.Process != nil && term.isRunning {
		if err := term.cmd.Process.Kill(); err != nil {
			if c.logFile != nil {
				fmt.Fprintf(c.logFile, "[KILL TERMINAL] ID=%s, kill error: %v\n", params.TerminalId, err)
			}
		}
	}
	term.mu.Unlock()

	// Remove from tracking
	c.terminalsMu.Lock()
	delete(c.terminals, params.TerminalId)
	c.terminalsMu.Unlock()

	return acpsdk.KillTerminalCommandResponse{}, nil
}

// GetOutput returns the accumulated output from the agent session.
func (c *MesnadaACPClient) GetOutput() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.output.String()
}

// GetToolCallCount returns the number of tool calls made during the session.
func (c *MesnadaACPClient) GetToolCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.toolCallCount
}

// GetProgress returns the current progress percentage and description.
func (c *MesnadaACPClient) GetProgress() (int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentProgress, c.currentPlanDescription
}

// ExtractDiffsFromContent extracts file modification diffs from a slice of ToolCallContent.
func (c *MesnadaACPClient) ExtractDiffsFromContent(content []acpsdk.ToolCallContent) map[string]string {
	diffs := make(map[string]string)
	for _, block := range content {
		if block.Diff != nil {
			path := block.Diff.Path
			newText := block.Diff.NewText
			oldText := ""
			if block.Diff.OldText != nil {
				oldText = *block.Diff.OldText
			}

			// For now, we just store the new text or a simple representation.
			// In a real TUI we would compute a proper unified diff here.
			if oldText == "" {
				diffs[path] = fmt.Sprintf("Create file: %s (%d bytes)", path, len(newText))
			} else {
				diffs[path] = fmt.Sprintf("Modify file: %s (new length: %d bytes)", path, len(newText))
			}
		}
	}
	return diffs
}

// processAgentMessageChunk handles chunks of agent messages.
func (c *MesnadaACPClient) processAgentMessageChunk(chunk *acpsdk.SessionUpdateAgentMessageChunk) {
	// Extract text from content block
	text := c.extractTextFromContentBlock(chunk.Content)
	if text == "" {
		return
	}

	// Append to output
	c.output.WriteString(text)

	// Log the message
	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[AGENT] %s", text)
	}

	// Notify callback
	if c.onUpdate != nil {
		c.onUpdate(SessionUpdateInfo{
			TaskID:      c.taskID,
			MessageText: text,
		})
	}
}

// processAgentThoughtChunk handles chunks of agent thinking/reasoning.
func (c *MesnadaACPClient) processAgentThoughtChunk(chunk *acpsdk.SessionUpdateAgentThoughtChunk) {
	text := c.extractTextFromContentBlock(chunk.Content)
	if text == "" {
		return
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[THINKING] %s", text)
	}

	if c.onUpdate != nil {
		c.onUpdate(SessionUpdateInfo{
			TaskID:      c.taskID,
			MessageText: text,
		})
	}
}

// processToolCall handles tool call start notifications.
func (c *MesnadaACPClient) processToolCall(toolCall *acpsdk.SessionUpdateToolCall) {
	c.toolCallCount++

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[TOOL_CALL] %s (id=%s, status=%s)\n", toolCall.Title, toolCall.ToolCallId, toolCall.Status)
		if toolCall.RawInput != nil {
			fmt.Fprintf(c.logFile, "[TOOL_INPUT] %+v\n", toolCall.RawInput)
		}
	}

	var args map[string]interface{}
	if toolCall.RawInput != nil {
		if m, ok := toolCall.RawInput.(map[string]interface{}); ok {
			args = m
		}
	}

	locations := make([]string, 0, len(toolCall.Locations))
	for _, loc := range toolCall.Locations {
		if loc.Path != "" {
			locations = append(locations, loc.Path)
		}
	}

	title := toolCall.Title
	if title == "" {
		title = string(toolCall.ToolCallId)
	}

	info := ToolCallInfo{
		ID:        string(toolCall.ToolCallId),
		Name:      title,
		Title:     title,
		Kind:      string(toolCall.Kind),
		Arguments: args,
		RawInput:  toolCall.RawInput,
		Locations: locations,
		Status:    string(toolCall.Status),
		Content:   toolCall.Content,
		Diffs:     c.ExtractDiffsFromContent(toolCall.Content),
	}
	c.toolCalls[string(toolCall.ToolCallId)] = info

	if c.onUpdate != nil {
		infoCopy := info
		c.onUpdate(SessionUpdateInfo{
			TaskID:   c.taskID,
			ToolCall: &infoCopy,
		})
	}
}

// processToolCallUpdate handles tool call status updates.
func (c *MesnadaACPClient) processToolCallUpdate(update *acpsdk.SessionToolCallUpdate) {
	if c.logFile != nil {
		statusStr := "unknown"
		if update.Status != nil {
			statusStr = string(*update.Status)
		}
		fmt.Fprintf(c.logFile, "[TOOL_UPDATE] id=%s, status=%s\n", update.ToolCallId, statusStr)
		if update.RawOutput != nil {
			fmt.Fprintf(c.logFile, "[TOOL_OUTPUT] %+v\n", update.RawOutput)
		}
	}

	toolCallID := string(update.ToolCallId)
	info, ok := c.toolCalls[toolCallID]
	if !ok {
		info = ToolCallInfo{Name: toolCallID}
	}

	if update.Status != nil {
		info.Status = string(*update.Status)
	}
	if update.Title != nil && *update.Title != "" {
		info.Name = *update.Title
		info.Title = *update.Title
	}
	if update.Kind != nil {
		info.Kind = string(*update.Kind)
	}
	// Update arguments if rawInput is provided (e.g., on the in_progress update).
	if update.RawInput != nil {
		info.RawInput = update.RawInput
		if m, ok := update.RawInput.(map[string]interface{}); ok && len(m) > 0 {
			info.Arguments = m
		}
	}
	if len(update.Locations) > 0 {
		locations := make([]string, 0, len(update.Locations))
		for _, loc := range update.Locations {
			if loc.Path != "" {
				locations = append(locations, loc.Path)
			}
		}
		info.Locations = locations
	}
	if update.RawOutput != nil {
		info.RawOutput = update.RawOutput
		info.Result = fmt.Sprintf("%v", update.RawOutput)
	}
	if len(update.Content) > 0 {
		info.Content = update.Content
		info.Diffs = c.ExtractDiffsFromContent(update.Content)
	}
	c.toolCalls[toolCallID] = info

	if c.onUpdate != nil {
		infoCopy := info
		c.onUpdate(SessionUpdateInfo{
			TaskID:   c.taskID,
			ToolCall: &infoCopy,
		})
	}
}

// processPlan handles plan updates and calculates progress.
func (c *MesnadaACPClient) processPlan(planUpdate *acpsdk.SessionUpdatePlan) {
	totalSteps := len(planUpdate.Entries)
	if totalSteps == 0 {
		return
	}

	// Count completed steps
	completedSteps := 0
	for _, entry := range planUpdate.Entries {
		if entry.Status == acpsdk.PlanEntryStatusCompleted {
			completedSteps++
		}
	}

	// Calculate percentage
	percentage := (completedSteps * 100) / totalSteps

	// Build plan description
	description := fmt.Sprintf("Plan: %d/%d steps completed", completedSteps, totalSteps)

	// Update internal state
	c.currentProgress = percentage
	c.currentPlanDescription = description

	// Log plan
	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[PLAN] %s (%d%%)\n", description, percentage)
		for i, entry := range planUpdate.Entries {
			fmt.Fprintf(c.logFile, "  [%d] [%s] %s\n", i+1, entry.Status, entry.Content)
		}
	}

	// Notify callback
	if c.onUpdate != nil {
		c.onUpdate(SessionUpdateInfo{
			TaskID: c.taskID,
			Plan:   description,
		})
	}
}

// extractTextFromContentBlock extracts text from a ContentBlock.
func (c *MesnadaACPClient) extractTextFromContentBlock(content acpsdk.ContentBlock) string {
	if content.Text != nil {
		return content.Text.Text
	}
	return ""
}
