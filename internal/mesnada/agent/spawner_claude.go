// Package agent handles spawning and managing CLI agent processes.
package agent

import (
	"bufio"
	"context"
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

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// ClaudeSpawner manages Claude CLI process spawning.
type ClaudeSpawner struct {
	logDir     string
	processes  map[string]*ClaudeProcess
	mu         sync.RWMutex
	onComplete func(task *models.Task)
}

// ClaudeProcess represents a running Claude CLI process.
type ClaudeProcess struct {
	cmd        *exec.Cmd
	task       *models.Task
	output     *strings.Builder
	logFile    *os.File
	cancel     context.CancelFunc
	done       chan struct{}
	mcpTempDir string // Temp dir for converted MCP config
}

// NewClaudeSpawner creates a new Claude CLI agent spawner.
func NewClaudeSpawner(logDir string, onComplete func(task *models.Task)) *ClaudeSpawner {
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = filepath.Join(home, defaultLogDir)
	}
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = abs
	}
	os.MkdirAll(logDir, 0755)

	return &ClaudeSpawner{
		logDir:     logDir,
		processes:  make(map[string]*ClaudeProcess),
		onComplete: onComplete,
	}
}

// Spawn starts a new Claude CLI agent.
func (s *ClaudeSpawner) Spawn(ctx context.Context, task *models.Task) error {
	// Convert MCP config if provided
	var mcpConfigPath string
	var mcpTempDir string
	if task.MCPConfig != "" {
		var err error
		mcpTempDir = filepath.Join(s.logDir, "claude-mcp", task.ID)
		mcpConfigPath, err = ConvertMCPConfigForTask(task.MCPConfig, task.ID, s.logDir, task.WorkDir)
		if err != nil {
			log.Printf("ERROR: failed to convert MCP config for task %s: %v (MCPConfig=%q, WorkDir=%q, LogDir=%q)",
				task.ID, err, task.MCPConfig, task.WorkDir, s.logDir)
			// Continue without MCP config
		} else {
			log.Printf("INFO: MCP config converted successfully for task %s: %s", task.ID, mcpConfigPath)
		}
	}

	// Build command arguments
	args := s.buildArgs(task, mcpConfigPath)

	// Log the command being executed for debugging
	log.Printf("Executing: claude %v", args)

	// Create cancellable context
	procCtx, cancel := context.WithCancel(ctx)
	if task.Timeout > 0 {
		procCtx, cancel = context.WithTimeout(ctx, time.Duration(task.Timeout))
	}

	// Create command - use 'claude' CLI
	cmd := exec.CommandContext(procCtx, "claude", args...)
	cmd.Dir = task.WorkDir

	// Set up environment with Claude Code configuration
	cmd.Env = append(os.Environ(), "NO_COLOR=1")

	// Create or append to log file
	logFile, err := openOrCreateLogFile(s.logDir, task)
	if err != nil {
		cancel()
		return err
	}

	// Set up output capture
	output := &strings.Builder{}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		logFile.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		logFile.Close()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start process
	if err := cmd.Start(); err != nil {
		cancel()
		logFile.Close()
		return fmt.Errorf("failed to start claude: %w", err)
	}

	task.PID = cmd.Process.Pid
	now := time.Now()
	task.StartedAt = &now
	task.Status = models.TaskStatusRunning

	log.Printf(
		"task_event=started task_id=%s status=%s pid=%d log_file=%q work_dir=%q model=%q engine=claude",
		task.ID,
		task.Status,
		task.PID,
		task.LogFile,
		task.WorkDir,
		task.Model,
	)

	proc := &ClaudeProcess{
		cmd:        cmd,
		task:       task,
		output:     output,
		logFile:    logFile,
		cancel:     cancel,
		done:       make(chan struct{}),
		mcpTempDir: mcpTempDir,
	}

	s.mu.Lock()
	s.processes[task.ID] = proc
	s.mu.Unlock()

	// Start output capture goroutines
	go s.captureOutput(proc, stdout, stderr)

	// Wait for completion in background
	go s.waitForCompletion(proc)

	return nil
}

func (s *ClaudeSpawner) buildArgs(task *models.Task, mcpConfigPath string) []string {
	// Prepend task_id to the prompt
	promptWithTaskID := fmt.Sprintf("You are the task_id: %s\n\n%s", task.ID, task.Prompt)

	// Only pass model and prompt as arguments
	// Other configuration is passed via environment variables
	args := []string{"--print", "--output-format", "text", "--verbose", "--dangerously-skip-permissions"}

	if task.Model != "" {
		args = append(args, "--model", task.Model)
	}

	if mcpConfigPath != "" {
		args = append(args, "--mcp-config", mcpConfigPath)
	}

	// Add extra args if needed (but most should be env vars now)
	args = append(args, task.ExtraArgs...)

	// Pass prompt via -p flag (positional args are interpreted as file paths by Claude CLI)
	args = append(args, "-p", promptWithTaskID)

	// Store the modified prompt
	task.Prompt = promptWithTaskID

	return args
}

func (s *ClaudeSpawner) captureOutput(proc *ClaudeProcess, stdout, stderr io.ReadCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Capture stdout as plain text
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()

			// Write to log file
			fmt.Fprintf(proc.logFile, "%s\n", line)

			// Capture to memory (with limit)
			if proc.output.Len() < maxOutputCapture {
				proc.output.WriteString(line)
				proc.output.WriteString("\n")
			}
		}
	}()

	// Capture stderr as-is
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintf(proc.logFile, "[stderr] %s\n", line)

			if proc.output.Len() < maxOutputCapture {
				proc.output.WriteString("[stderr] ")
				proc.output.WriteString(line)
				proc.output.WriteString("\n")
			}
		}
	}()

	wg.Wait()
}

func (s *ClaudeSpawner) waitForCompletion(proc *ClaudeProcess) {
	defer close(proc.done)
	defer proc.logFile.Close()

	err := proc.cmd.Wait()

	// Clean up temp MCP config
	if proc.mcpTempDir != "" {
		os.RemoveAll(proc.mcpTempDir)
	}

	now := time.Now()
	proc.task.CompletedAt = &now
	proc.task.Output = proc.output.String()
	proc.task.OutputTail = s.getTail(proc.output.String(), outputTailLines)

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

	if s.onComplete != nil {
		s.onComplete(proc.task)
	}
}

func (s *ClaudeSpawner) getTail(output string, lines int) string {
	allLines := strings.Split(output, "\n")
	if len(allLines) <= lines {
		return output
	}
	return strings.Join(allLines[len(allLines)-lines:], "\n")
}

// Cancel stops a running agent.
func (s *ClaudeSpawner) Cancel(taskID string) error {
	s.mu.RLock()
	proc, exists := s.processes[taskID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("process not found: %s", taskID)
	}

	proc.cancel()

	if proc.cmd.Process != nil {
		proc.cmd.Process.Signal(syscall.SIGTERM)

		select {
		case <-proc.done:
		case <-time.After(5 * time.Second):
			proc.cmd.Process.Kill()
		}
	}

	proc.task.Status = models.TaskStatusCancelled

	return nil
}

// Pause stops a running agent without marking it as cancelled.
func (s *ClaudeSpawner) Pause(taskID string) error {
	s.mu.RLock()
	proc, exists := s.processes[taskID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("process not found: %s", taskID)
	}

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

// IsRunning checks if a task is currently running.
func (s *ClaudeSpawner) IsRunning(taskID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.processes[taskID]
	return exists
}

// Wait blocks until a task completes or context is cancelled.
func (s *ClaudeSpawner) Wait(ctx context.Context, taskID string) error {
	s.mu.RLock()
	proc, exists := s.processes[taskID]
	s.mu.RUnlock()

	if !exists {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-proc.done:
		return nil
	}
}

// RunningCount returns the number of currently running processes.
func (s *ClaudeSpawner) RunningCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.processes)
}

// Shutdown cancels all running processes.
func (s *ClaudeSpawner) Shutdown() {
	s.mu.Lock()
	procs := make([]*ClaudeProcess, 0, len(s.processes))
	for _, p := range s.processes {
		procs = append(procs, p)
	}
	s.mu.Unlock()

	for _, proc := range procs {
		proc.cancel()
		if proc.cmd.Process != nil {
			proc.cmd.Process.Signal(syscall.SIGTERM)
		}
	}

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
