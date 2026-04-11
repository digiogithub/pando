// Package agent handles spawning and managing ACP agent processes.
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/digiogithub/pando/internal/mesnada/acp"
	"github.com/digiogithub/pando/internal/mesnada/config"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// followUpRequest carries a follow-up prompt message and a channel for the result.
type followUpRequest struct {
	message  string
	resultCh chan followUpResult
}

// followUpResult holds the outcome of a follow-up prompt call.
type followUpResult struct {
	stopReason acpsdk.StopReason
	err        error
}

// ACPProcess represents a running ACP agent process.
type ACPProcess struct {
	task       *models.Task
	cmd        *exec.Cmd
	conn       *acpsdk.ClientSideConnection
	sessionID  acpsdk.SessionId
	client     *acp.MesnadaACPClient
	output     *strings.Builder
	logFile    *os.File
	cancel     context.CancelFunc
	done       chan struct{}
	mu         sync.Mutex
	followUpCh chan followUpRequest // receives follow-up prompts after initial prompt completes
}

// ACPSpawner manages ACP agent process spawning.
type ACPSpawner struct {
	config     *mesnadaconfig.ACPConfig
	logDir     string
	processes  map[string]*ACPProcess
	mu         sync.RWMutex
	onComplete func(task *models.Task)
}

// NewACPSpawner creates a new spawner for ACP agents.
func NewACPSpawner(acpConfig *mesnadaconfig.ACPConfig, logDir string, onComplete func(task *models.Task)) *ACPSpawner {
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = filepath.Join(home, defaultLogDir)
	}
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = abs
	}
	os.MkdirAll(logDir, 0755)

	return &ACPSpawner{
		config:     acpConfig,
		logDir:     logDir,
		processes:  make(map[string]*ACPProcess),
		onComplete: onComplete,
	}
}

// Spawn implements the Spawner interface for ACP agents.
// It creates a process, connects via ACP, and runs the agent session.
func (s *ACPSpawner) Spawn(ctx context.Context, task *models.Task) error {
	// Resolve agent configuration based on task engine
	agentConfig := s.resolveAgentConfig(task)
	if agentConfig == nil {
		return fmt.Errorf("no ACP agent configuration found for engine: %s", task.Engine)
	}

	// Create cancellable context
	procCtx, cancel := context.WithCancel(ctx)
	if task.Timeout > 0 {
		procCtx, cancel = context.WithTimeout(ctx, time.Duration(task.Timeout))
	}

	// Build command
	cmd := exec.CommandContext(procCtx, agentConfig.Command, agentConfig.Args...)
	cmd.Dir = task.WorkDir

	// Set up environment
	cmd.Env = os.Environ()
	if agentConfig.Env != nil {
		for k, v := range agentConfig.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	cmd.Env = append(cmd.Env, "NO_COLOR=1")

	// Create pipes for stdin/stdout
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Create or append to log file
	logFile, err := openOrCreateLogFile(s.logDir, task)
	if err != nil {
		cancel()
		return err
	}

	// Set up output capture
	output := &strings.Builder{}

	// Create MesnadaACPClient with a simple callback that just writes to output
	// The client already logs everything to the logFile
	onUpdate := func(update acp.SessionUpdateInfo) {
		// Append messages to output (but don't add extra newline - already in text)
		if update.MessageText != "" {
			output.WriteString(update.MessageText)
		}
	}

	client := acp.NewMesnadaACPClient(task.ID, task.WorkDir, logFile, onUpdate)

	// Configure auto-permission based on config
	if s.config != nil {
		client.SetAutoPermission(s.config.AutoPermission)
	}

	// Set capabilities from agent config
	if agentConfig.Capabilities.Terminals || agentConfig.Capabilities.FileAccess {
		client.SetCapabilities(acp.ACPCapabilities{
			Terminals:   agentConfig.Capabilities.Terminals,
			FileAccess:  agentConfig.Capabilities.FileAccess,
			Permissions: agentConfig.Capabilities.Permissions,
		})
	}

	// Create ClientSideConnection from SDK
	// Signature: NewClientSideConnection(client Client, peerInput io.Writer, peerOutput io.Reader)
	// peerInput = where we write to the agent (stdin of agent process)
	// peerOutput = where we read from the agent (stdout of agent process)
	conn := acpsdk.NewClientSideConnection(client, stdinPipe, stdoutPipe)

	// Start the process
	if err := cmd.Start(); err != nil {
		cancel()
		logFile.Close()
		return fmt.Errorf("failed to start ACP agent: %w", err)
	}

	task.PID = cmd.Process.Pid
	now := time.Now()
	task.StartedAt = &now
	task.Status = models.TaskStatusRunning

	log.Printf(
		"task_event=started task_id=%s status=%s pid=%d log_file=%q work_dir=%q engine=%s agent=%s",
		task.ID,
		task.Status,
		task.PID,
		task.LogFile,
		task.WorkDir,
		task.Engine,
		agentConfig.Name,
	)

	// Create process record
	proc := &ACPProcess{
		task:       task,
		cmd:        cmd,
		conn:       conn,
		client:     client,
		output:     output,
		logFile:    logFile,
		cancel:     cancel,
		done:       make(chan struct{}),
		followUpCh: make(chan followUpRequest, 1),
	}

	s.mu.Lock()
	s.processes[task.ID] = proc
	s.mu.Unlock()

	// Start stderr capture in background
	go s.captureStderr(proc, stderrPipe)

	// Start main ACP session goroutine
	go s.runACPSession(procCtx, proc, agentConfig)

	return nil
}

// captureStderr captures stderr output and writes it to the log file.
func (s *ACPSpawner) captureStderr(proc *ACPProcess, stderr io.ReadCloser) {
	scanner := bufio.NewScanner(stderr)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(proc.logFile, "[stderr] %s\n", line)

		// Also capture to memory (with limit)
		if proc.output.Len() < maxOutputCapture {
			proc.output.WriteString("[stderr] ")
			proc.output.WriteString(line)
			proc.output.WriteString("\n")
		}
	}
}

// runACPSession executes the complete ACP session flow:
// 1. Initialize - handshake of capabilities
// 2. NewSession - create session with work dir and MCP servers
// 3. Prompt - send the task prompt
// 4. Wait for completion (updates come via SessionUpdate callbacks)
func (s *ACPSpawner) runACPSession(ctx context.Context, proc *ACPProcess, agentConfig *mesnadaconfig.ACPAgentConfig) {
	defer s.waitForCompletion(proc)

	// Step 1: Initialize - capability negotiation
	fmt.Fprintf(proc.logFile, "[ACP] Starting initialize handshake...\n")
	initResp, err := proc.conn.Initialize(ctx, acpsdk.InitializeRequest{
		ProtocolVersion: acpsdk.ProtocolVersionNumber,
		ClientInfo: &acpsdk.Implementation{
			Name:    "mesnada",
			Version: "1.0",
		},
		ClientCapabilities: acpsdk.ClientCapabilities{
			// Declare what mesnada supports as a client
			Fs: acpsdk.FileSystemCapability{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: true, // Enable all terminal/* methods
		},
	})

	if err != nil {
		fmt.Fprintf(proc.logFile, "[ACP] Initialize failed: %v\n", err)
		proc.task.Status = models.TaskStatusFailed
		proc.task.Error = fmt.Sprintf("ACP initialize failed: %v", err)
		return
	}

	fmt.Fprintf(proc.logFile, "[ACP] Initialize successful (protocol %d)\n", initResp.ProtocolVersion)

	// Step 2: NewSession - create the session
	fmt.Fprintf(proc.logFile, "[ACP] Creating new session...\n")

	// Convert MCP servers from config to ACP format
	var mcpServers []acpsdk.McpServer
	for _, mcpConfig := range agentConfig.MCPServers {
		// Default to stdio transport (all agents MUST support this)
		envVars := make([]acpsdk.EnvVariable, 0, len(mcpConfig.Env))
		for k, v := range mcpConfig.Env {
			envVars = append(envVars, acpsdk.EnvVariable{
				Name:  k,
				Value: v,
			})
		}

		mcpServer := acpsdk.McpServer{
			Stdio: &acpsdk.McpServerStdio{
				Name:    mcpConfig.Name,
				Command: mcpConfig.Command,
				Args:    mcpConfig.Args,
				Env:     envVars,
			},
		}

		mcpServers = append(mcpServers, mcpServer)
	}

	sessionResp, err := proc.conn.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        proc.task.WorkDir,
		McpServers: mcpServers,
	})

	if err != nil {
		fmt.Fprintf(proc.logFile, "[ACP] NewSession failed: %v\n", err)
		proc.task.Status = models.TaskStatusFailed
		proc.task.Error = fmt.Sprintf("ACP NewSession failed: %v", err)
		return
	}

	proc.sessionID = sessionResp.SessionId
	proc.task.ACPSessionID = string(sessionResp.SessionId)
	fmt.Fprintf(proc.logFile, "[ACP] Session created: %s\n", proc.sessionID)

	// Step 3: Send the initial prompt (prepend task_id like other spawners).
	promptWithTaskID := fmt.Sprintf("You are the task_id: %s\n\n%s", proc.task.ID, proc.task.Prompt)
	fmt.Fprintf(proc.logFile, "[ACP] Sending initial prompt...\n")

	lastStopReason, err := s.sendPrompt(ctx, proc, promptWithTaskID)
	if err != nil {
		fmt.Fprintf(proc.logFile, "[ACP] Initial prompt failed: %v\n", err)
		proc.mu.Lock()
		proc.task.Status = models.TaskStatusFailed
		proc.task.Error = fmt.Sprintf("ACP Prompt failed: %v", err)
		proc.mu.Unlock()
		return
	}
	fmt.Fprintf(proc.logFile, "[ACP] Initial prompt stop reason: %s\n", lastStopReason)

	if lastStopReason == acpsdk.StopReasonCancelled {
		proc.mu.Lock()
		proc.task.Status = models.TaskStatusCancelled
		proc.mu.Unlock()
		return
	}

	// Step 4: Follow-up loop — wait for additional prompts until context is
	// cancelled or the session ends.
	for {
		select {
		case <-ctx.Done():
			proc.mu.Lock()
			if proc.task.Status != models.TaskStatusFailed {
				proc.task.Status = models.TaskStatusCancelled
			}
			proc.mu.Unlock()
			return
		case req, ok := <-proc.followUpCh:
			if !ok {
				// Channel closed — no more follow-ups, fall through to set final status.
				goto setFinalStatus
			}
			fmt.Fprintf(proc.logFile, "[ACP] Sending follow-up prompt...\n")
			stopReason, err := s.sendPrompt(ctx, proc, req.message)
			if err != nil {
				fmt.Fprintf(proc.logFile, "[ACP] Follow-up prompt failed: %v\n", err)
				req.resultCh <- followUpResult{err: err}
				return
			}
			fmt.Fprintf(proc.logFile, "[ACP] Follow-up stop reason: %s\n", stopReason)
			lastStopReason = stopReason
			req.resultCh <- followUpResult{stopReason: stopReason}
			if stopReason == acpsdk.StopReasonCancelled {
				proc.mu.Lock()
				proc.task.Status = models.TaskStatusCancelled
				proc.mu.Unlock()
				return
			}
		}
	}

setFinalStatus:
	proc.mu.Lock()
	switch lastStopReason {
	case acpsdk.StopReasonEndTurn:
		if proc.task.Status != models.TaskStatusFailed {
			proc.task.Status = models.TaskStatusCompleted
		}
	case acpsdk.StopReasonCancelled:
		proc.task.Status = models.TaskStatusCancelled
	default:
		fmt.Fprintf(proc.logFile, "[ACP] Unrecognised stop reason: %s\n", lastStopReason)
		if proc.task.Status != models.TaskStatusFailed {
			proc.task.Status = models.TaskStatusCompleted
		}
	}
	proc.mu.Unlock()
}

// sendPrompt sends a single prompt to the ACP agent and returns the stop reason.
func (s *ACPSpawner) sendPrompt(ctx context.Context, proc *ACPProcess, text string) (acpsdk.StopReason, error) {
	resp, err := proc.conn.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: proc.sessionID,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock(text)},
	})
	if err != nil {
		return "", err
	}
	return resp.StopReason, nil
}

// waitForCompletion waits for the process to complete and updates task status.
func (s *ACPSpawner) waitForCompletion(proc *ACPProcess) {
	defer close(proc.done)
	defer proc.logFile.Close()

	err := proc.cmd.Wait()

	now := time.Now()
	proc.task.CompletedAt = &now
	proc.task.Output = proc.client.GetOutput()
	proc.task.OutputTail = s.getTail(proc.task.Output, outputTailLines)

	// Update task with final progress and tool call counts from the client
	if progress, description := proc.client.GetProgress(); progress > 0 {
		proc.task.Progress = &models.TaskProgress{
			Percentage:  progress,
			Description: description,
			UpdatedAt:   now,
		}
	}

	// Update ACP status with tool call count
	if proc.task.ACPStatus == nil {
		proc.task.ACPStatus = &models.ACPStatus{}
	}
	proc.task.ACPStatus.ToolCalls = proc.client.GetToolCallCount()

	explicitStop := proc.task.Status == models.TaskStatusCancelled || proc.task.Status == models.TaskStatusPaused

	if err != nil {
		if explicitStop {
			if exitErr, ok := err.(*exec.ExitError); ok {
				code := exitErr.ExitCode()
				proc.task.ExitCode = &code
			}
		} else {
			proc.task.Status = models.TaskStatusFailed
			proc.task.Error = err.Error()

			if exitErr, ok := err.(*exec.ExitError); ok {
				code := exitErr.ExitCode()
				proc.task.ExitCode = &code
			}
		}
	} else {
		if !explicitStop {
			proc.task.Status = models.TaskStatusCompleted
		}
		code := 0
		proc.task.ExitCode = &code
	}

	s.mu.Lock()
	delete(s.processes, proc.task.ID)
	s.mu.Unlock()

	log.Printf(
		"task_event=completed task_id=%s status=%s exit_code=%v duration=%v",
		proc.task.ID,
		proc.task.Status,
		proc.task.ExitCode,
		proc.task.CompletedAt.Sub(*proc.task.StartedAt),
	)

	if s.onComplete != nil {
		s.onComplete(proc.task)
	}
}

// getTail returns the last N lines of output.
func (s *ACPSpawner) getTail(output string, lines int) string {
	allLines := strings.Split(output, "\n")
	if len(allLines) <= lines {
		return output
	}
	return strings.Join(allLines[len(allLines)-lines:], "\n")
}

// Cancel stops a running agent and sends a session/cancel notification if possible.
func (s *ACPSpawner) Cancel(taskID string) error {
	s.mu.RLock()
	proc, exists := s.processes[taskID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("process not found: %s", taskID)
	}

	// Try to send session/cancel via ACP before killing the process
	if proc.conn != nil && proc.sessionID != "" {
		cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		fmt.Fprintf(proc.logFile, "[ACP] Sending session/cancel notification...\n")
		err := proc.conn.Cancel(cancelCtx, acpsdk.CancelNotification{
			SessionId: proc.sessionID,
		})
		if err != nil {
			fmt.Fprintf(proc.logFile, "[ACP] session/cancel failed: %v\n", err)
		} else {
			fmt.Fprintf(proc.logFile, "[ACP] session/cancel sent successfully\n")
		}
	}

	// Cancel context
	proc.cancel()

	// Send SIGTERM first
	if proc.cmd.Process != nil {
		proc.cmd.Process.Signal(syscall.SIGTERM)

		// Wait briefly, then force kill
		select {
		case <-proc.done:
			// Process exited gracefully
		case <-time.After(5 * time.Second):
			proc.cmd.Process.Kill()
		}
	}

	proc.task.Status = models.TaskStatusCancelled

	return nil
}

// Pause stops a running agent without marking it as cancelled.
// Note: ACP doesn't have a native "pause" concept, so we treat it like cancel.
func (s *ACPSpawner) Pause(taskID string) error {
	s.mu.RLock()
	proc, exists := s.processes[taskID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("process not found: %s", taskID)
	}

	// ACP doesn't support pause natively, so we just terminate the process
	// without sending session/cancel (since this is pause, not cancel)
	proc.cancel()

	if proc.cmd.Process != nil {
		proc.cmd.Process.Signal(syscall.SIGTERM)

		select {
		case <-proc.done:
		case <-time.After(5 * time.Second):
			proc.cmd.Process.Kill()
		}
	}

	proc.task.Status = models.TaskStatusPaused

	return nil
}

// Wait blocks until a task completes or context is cancelled.
func (s *ACPSpawner) Wait(ctx context.Context, taskID string) error {
	s.mu.RLock()
	proc, exists := s.processes[taskID]
	s.mu.RUnlock()

	if !exists {
		return nil // Already completed
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-proc.done:
		return nil
	}
}

// IsRunning checks if a task is currently running.
func (s *ACPSpawner) IsRunning(taskID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.processes[taskID]
	return exists
}

// RunningCount returns the number of currently running processes.
func (s *ACPSpawner) RunningCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.processes)
}

// Shutdown cancels all running processes.
func (s *ACPSpawner) Shutdown() {
	s.mu.Lock()
	procs := make([]*ACPProcess, 0, len(s.processes))
	for _, p := range s.processes {
		procs = append(procs, p)
	}
	s.mu.Unlock()

	for _, proc := range procs {
		// Try to send session/cancel for each
		if proc.conn != nil && proc.sessionID != "" {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			proc.conn.Cancel(cancelCtx, acpsdk.CancelNotification{
				SessionId: proc.sessionID,
			})
			cancel()
		}

		proc.cancel()
		if proc.cmd.Process != nil {
			proc.cmd.Process.Signal(syscall.SIGTERM)
		}
	}

	// Wait for all to finish
	for _, proc := range procs {
		select {
		case <-proc.done:
		case <-time.After(10 * time.Second):
			if proc.cmd.Process != nil {
				proc.cmd.Process.Kill()
			}
		}
	}
}

// SessionControl sends a control command to an active ACP session.
// Supported actions: "follow_up", "set_mode", "cancel", "status"
// This is part of Phase 5 API extension.
// Full implementation will be completed in Phase 6.
func (s *ACPSpawner) SessionControl(taskID, action, message, mode string) (interface{}, error) {
	s.mu.RLock()
	proc, exists := s.processes[taskID]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no active ACP session found for task %s", taskID)
	}

	switch action {
	case "status":
		// Return current session status
		status := map[string]interface{}{
			"task_id":    taskID,
			"session_id": string(proc.sessionID),
			"connected":  proc.conn != nil,
			"mode":       proc.task.ACPMode,
		}
		if proc.task.ACPStatus != nil {
			status["acp_status"] = proc.task.ACPStatus
		}
		return status, nil

	case "follow_up":
		if message == "" {
			return nil, fmt.Errorf("message is required for follow_up action")
		}
		resultCh := make(chan followUpResult, 1)
		req := followUpRequest{message: message, resultCh: resultCh}
		// Send the request to the running goroutine; time out if the initial prompt
		// is still executing or if the session has already finished.
		select {
		case proc.followUpCh <- req:
		case <-proc.done:
			return nil, fmt.Errorf("follow_up: session has already completed")
		case <-time.After(30 * time.Second):
			return nil, fmt.Errorf("follow_up: session not ready to accept messages (initial prompt still running?)")
		}
		// Wait for the agent to finish processing the follow-up.
		select {
		case result := <-resultCh:
			if result.err != nil {
				return nil, fmt.Errorf("follow_up prompt failed: %w", result.err)
			}
			return map[string]interface{}{
				"task_id":     taskID,
				"stop_reason": string(result.stopReason),
				"output":      proc.client.GetOutput(),
			}, nil
		case <-proc.done:
			return nil, fmt.Errorf("follow_up: session ended before response arrived")
		}

	case "set_mode":
		// Change session mode
		// TODO: Implement in Phase 6 - requires ACP SDK enhancement
		if mode == "" {
			return nil, fmt.Errorf("mode parameter required for set_mode action")
		}
		return nil, fmt.Errorf("set_mode action not yet implemented (Phase 6)")

	case "set_persona":
		// Change session persona (Pando-specific extension)
		// For external ACP processes, persona control requires sending a persona/set_session
		// RPC via the ACP connection. This is only fully supported for Pando's built-in agent.
		if mode == "" {
			return nil, fmt.Errorf("persona parameter required for set_persona action")
		}
		return nil, fmt.Errorf("set_persona action not yet supported for external ACP processes")

	case "cancel":
		// Cancel the session gracefully
		return nil, s.Cancel(taskID)

	case "list_permissions":
		// List pending permission requests
		queue := proc.client.GetPermissionQueue()
		pending := queue.GetPending(taskID)
		return map[string]interface{}{
			"task_id":     taskID,
			"permissions": pending,
		}, nil

	case "resolve_permission":
		// Resolve a permission request (approve or deny)
		// message contains JSON: {"request_id":"...","action":"approve/deny","option_id":"..."}
		var data struct {
			RequestID string `json:"request_id"`
			Action    string `json:"action"`
			OptionID  string `json:"option_id"`
		}
		if err := json.Unmarshal([]byte(message), &data); err != nil {
			return nil, fmt.Errorf("invalid permission resolution data: %w", err)
		}

		queue := proc.client.GetPermissionQueue()
		perm, exists := queue.GetPermission(data.RequestID)
		if !exists {
			return nil, fmt.Errorf("permission request not found: %s", data.RequestID)
		}

		var outcome acpsdk.RequestPermissionOutcome
		if data.Action == "approve" {
			// Find the option to approve
			optionID := acpsdk.PermissionOptionId(data.OptionID)
			if data.OptionID == "" && len(perm.Options) > 0 {
				// Default to first option if not specified
				optionID = perm.Options[0].OptionId
			}
			outcome = acpsdk.NewRequestPermissionOutcomeSelected(optionID)
		} else if data.Action == "deny" {
			// Treat deny as cancellation (there's no explicit deny in the protocol)
			outcome = acpsdk.NewRequestPermissionOutcomeCancelled()
		} else {
			return nil, fmt.Errorf("invalid action: %s (must be 'approve' or 'deny')", data.Action)
		}

		if err := queue.ResolvePermission(data.RequestID, outcome); err != nil {
			return nil, fmt.Errorf("failed to resolve permission: %w", err)
		}

		return map[string]interface{}{
			"request_id": data.RequestID,
			"action":     data.Action,
			"resolved":   true,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported action: %s (supported: follow_up, set_mode, set_persona, cancel, status, list_permissions, resolve_permission)", action)
	}
}

// resolveAgentConfig resolves the appropriate ACP agent configuration for a task.
// It checks the task engine and matches it to configured ACP agents.
func (s *ACPSpawner) resolveAgentConfig(task *models.Task) *mesnadaconfig.ACPAgentConfig {
	if s.config == nil || s.config.Agents == nil {
		return nil
	}

	// If task specifies an explicit ACP agent name, use that
	if task.Engine == models.EngineACP && task.Model != "" {
		if agentConfig, exists := s.config.Agents[task.Model]; exists {
			return &agentConfig
		}
	}

	// Map engine types to agent configurations
	var agentName string
	switch task.Engine {
	case models.EngineACPClaudeCode:
		agentName = "claude-code"
	case models.EngineACPCodex:
		agentName = "codex"
	case models.EngineACPCustom:
		agentName = "custom"
	case models.EngineACP:
		// Use default agent if specified
		if s.config.DefaultAgent != "" {
			agentName = s.config.DefaultAgent
		}
	default:
		// Try to find an agent matching the engine name
		agentName = string(task.Engine)
	}

	if agentName != "" {
		if agentConfig, exists := s.config.Agents[agentName]; exists {
			return &agentConfig
		}
	}

	// Fallback to default agent
	if s.config.DefaultAgent != "" {
		if agentConfig, exists := s.config.Agents[s.config.DefaultAgent]; exists {
			return &agentConfig
		}
	}

	return nil
}
