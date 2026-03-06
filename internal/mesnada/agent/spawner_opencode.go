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

// OpenCodeSpawner manages OpenCode.ai CLI process spawning.
type OpenCodeSpawner struct {
	logDir     string
	processes  map[string]*OpenCodeProcess
	mu         sync.RWMutex
	onComplete func(task *models.Task)
}

// OpenCodeProcess represents a running OpenCode CLI process.
type OpenCodeProcess struct {
	cmd        *exec.Cmd
	task       *models.Task
	output     *strings.Builder
	logFile    *os.File
	cancel     context.CancelFunc
	done       chan struct{}
	mcpTempDir string // Temp dir for converted MCP config
}

// NewOpenCodeSpawner creates a new OpenCode.ai CLI agent spawner.
func NewOpenCodeSpawner(logDir string, onComplete func(task *models.Task)) *OpenCodeSpawner {
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = filepath.Join(home, defaultLogDir)
	}
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = abs
	}
	os.MkdirAll(logDir, 0755)

	return &OpenCodeSpawner{
		logDir:     logDir,
		processes:  make(map[string]*OpenCodeProcess),
		onComplete: onComplete,
	}
}

// Spawn starts a new OpenCode.ai CLI agent.
func (s *OpenCodeSpawner) Spawn(ctx context.Context, task *models.Task) error {
	// Convert MCP config if provided
	var mcpConfigPath string
	var mcpTempDir string
	if task.MCPConfig != "" {
		var err error
		mcpTempDir = filepath.Join(s.logDir, "opencode-mcp", task.ID)
		mcpConfigPath, err = ConvertMCPConfigForOpenCode(task.MCPConfig, task.ID, s.logDir, task.WorkDir)
		if err != nil {
			log.Printf("Warning: failed to convert MCP config for OpenCode CLI: %v", err)
			// Continue without MCP config
		}
	}

	// Build command arguments
	args := s.buildArgs(task, mcpConfigPath)

	// Create cancellable context
	procCtx, cancel := context.WithCancel(ctx)
	if task.Timeout > 0 {
		procCtx, cancel = context.WithTimeout(ctx, time.Duration(task.Timeout))
	}

	// Create command - use 'opencode' CLI
	cmd := exec.CommandContext(procCtx, "opencode", args...)
	cmd.Dir = task.WorkDir

	// Set up environment
	env := append(os.Environ(),
		"NO_COLOR=1",
	)

	// Add MCP config via OPENCODE_CONFIG environment variable
	if mcpConfigPath != "" {
		env = append(env, fmt.Sprintf("OPENCODE_CONFIG=%s", mcpConfigPath))
	}

	cmd.Env = env

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
		return fmt.Errorf("failed to start opencode: %w", err)
	}

	task.PID = cmd.Process.Pid
	now := time.Now()
	task.StartedAt = &now
	task.Status = models.TaskStatusRunning

	log.Printf(
		"task_event=started task_id=%s status=%s pid=%d log_file=%q work_dir=%q model=%q engine=opencode",
		task.ID,
		task.Status,
		task.PID,
		task.LogFile,
		task.WorkDir,
		task.Model,
	)

	proc := &OpenCodeProcess{
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

func (s *OpenCodeSpawner) buildArgs(task *models.Task, mcpConfigPath string) []string {
	// Prepend task_id to the prompt
	promptWithTaskID := fmt.Sprintf("You are the task_id: %s\n\n%s", task.ID, task.Prompt)

	// Note: OpenCode doesn't support MCP config via CLI flag
	// MCP configuration is passed via OPENCODE_CONFIG environment variable
	// The mcpConfigPath is used in the Spawn method to set the env var
	_ = mcpConfigPath // unused in buildArgs, used in Spawn

	args := []string{
		"run", // Use run subcommand for non-interactive execution
	}

	if task.Model != "" {
		args = append(args, "-m", task.Model)
	}

	args = append(args, task.ExtraArgs...)

	// Add the prompt as the final positional argument
	// Note: OpenCode run expects message as a positional argument
	args = append(args, promptWithTaskID)

	// Store the modified prompt
	task.Prompt = promptWithTaskID

	return args
}

func (s *OpenCodeSpawner) captureOutput(proc *OpenCodeProcess, stdout, stderr io.ReadCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Capture stdout as-is (OpenCode outputs text by default in non-interactive mode)
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

	// Discard stderr completely (don't capture to log file or memory)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			// Silently discard stderr lines
		}
	}()

	wg.Wait()
}

func (s *OpenCodeSpawner) waitForCompletion(proc *OpenCodeProcess) {
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

func (s *OpenCodeSpawner) getTail(output string, lines int) string {
	allLines := strings.Split(output, "\n")
	if len(allLines) <= lines {
		return output
	}
	return strings.Join(allLines[len(allLines)-lines:], "\n")
}

// Cancel stops a running agent.
func (s *OpenCodeSpawner) Cancel(taskID string) error {
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
func (s *OpenCodeSpawner) Pause(taskID string) error {
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
func (s *OpenCodeSpawner) IsRunning(taskID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.processes[taskID]
	return exists
}

// Wait blocks until a task completes or context is cancelled.
func (s *OpenCodeSpawner) Wait(ctx context.Context, taskID string) error {
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
func (s *OpenCodeSpawner) RunningCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.processes)
}

// Shutdown cancels all running processes.
func (s *OpenCodeSpawner) Shutdown() {
	s.mu.Lock()
	procs := make([]*OpenCodeProcess, 0, len(s.processes))
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
