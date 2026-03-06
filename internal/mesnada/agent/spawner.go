// Package agent handles spawning and managing Copilot CLI processes.
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

const (
	defaultLogDir    = ".mesnada/logs"
	outputTailLines  = 50
	maxOutputCapture = 1024 * 1024 // 1MB max output capture
)

// openOrCreateLogFile opens an existing log file in append mode (for retries)
// or creates a new one. When appending, it writes a retry separator.
func openOrCreateLogFile(logDir string, task *models.Task) (*os.File, error) {
	if task.LogFile != "" {
		// Retry: append to existing log file
		f, err := os.OpenFile(task.LogFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file for append: %w", err)
		}
		separator := fmt.Sprintf("\n\n=== RETRY %s (new task: %s) ===\n\n", time.Now().Format(time.RFC3339), task.ID)
		f.WriteString(separator)
		return f, nil
	}
	// New task: create fresh log file
	logPath := filepath.Join(logDir, fmt.Sprintf("%s.log", task.ID))
	f, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}
	task.LogFile = logPath
	return f, nil
}

// CopilotSpawner manages Copilot CLI process spawning.
type CopilotSpawner struct {
	logDir     string
	processes  map[string]*Process
	mu         sync.RWMutex
	onComplete func(task *models.Task)
}

// Process represents a running Copilot CLI process.
type Process struct {
	cmd     *exec.Cmd
	task    *models.Task
	output  *strings.Builder
	logFile *os.File
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewCopilotSpawner creates a new Copilot CLI agent spawner.
func NewCopilotSpawner(logDir string, onComplete func(task *models.Task)) *CopilotSpawner {
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = filepath.Join(home, defaultLogDir)
	}
	// Ensure logDir is absolute so task.LogFile is a full path.
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = abs
	}
	os.MkdirAll(logDir, 0755)

	return &CopilotSpawner{
		logDir:     logDir,
		processes:  make(map[string]*Process),
		onComplete: onComplete,
	}
}

// Spawn starts a new Copilot CLI agent.
func (s *CopilotSpawner) Spawn(ctx context.Context, task *models.Task) error {
	// Build command arguments
	args := s.buildArgs(task)

	// Create cancellable context
	procCtx, cancel := context.WithCancel(ctx)
	if task.Timeout > 0 {
		procCtx, cancel = context.WithTimeout(ctx, time.Duration(task.Timeout))
	}

	// Create command
	cmd := exec.CommandContext(procCtx, "copilot", args...)
	cmd.Dir = task.WorkDir

	// Set up environment
	cmd.Env = append(os.Environ(),
		"COPILOT_ALLOW_ALL=1",
		"NO_COLOR=1",
	)

	// Create or append to log file
	logFile, err := openOrCreateLogFile(s.logDir, task)
	if err != nil {
		cancel()
		return err
	}

	// Set up output capture
	output := &strings.Builder{}

	// Set up stdin pipe to send the prompt
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		logFile.Close()
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

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
		return fmt.Errorf("failed to start copilot: %w", err)
	}

	// Send the prompt to stdin and close it
	go func() {
		defer stdin.Close()
		stdin.Write([]byte(task.Prompt))
	}()

	task.PID = cmd.Process.Pid
	now := time.Now()
	task.StartedAt = &now
	task.Status = models.TaskStatusRunning

	log.Printf(
		"task_event=started task_id=%s status=%s pid=%d log_file=%q work_dir=%q model=%q",
		task.ID,
		task.Status,
		task.PID,
		task.LogFile,
		task.WorkDir,
		task.Model,
	)

	proc := &Process{
		cmd:     cmd,
		task:    task,
		output:  output,
		logFile: logFile,
		cancel:  cancel,
		done:    make(chan struct{}),
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

func (s *CopilotSpawner) buildArgs(task *models.Task) []string {
	// Prepend task_id to the prompt
	promptWithTaskID := fmt.Sprintf("You are the task_id: %s\n\n%s", task.ID, task.Prompt)

	args := []string{
		"--allow-all-tools",
		"--no-color",
		"--no-custom-instructions",
	}

	if task.Model != "" {
		args = append(args, "--model", task.Model)
	}

	if task.MCPConfig != "" {
		// Copilot expects @ prefix for file references
		mcpConfigArg := task.MCPConfig
		if !strings.HasPrefix(mcpConfigArg, "@") {
			mcpConfigArg = "@" + mcpConfigArg
		}
		args = append(args, "--additional-mcp-config", mcpConfigArg)
	}

	args = append(args, task.ExtraArgs...)

	// Store the modified prompt for stdin
	task.Prompt = promptWithTaskID

	return args
}

func (s *CopilotSpawner) captureOutput(proc *Process, stdout, stderr io.ReadCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	capture := func(r io.ReadCloser, prefix string) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()

			// Write to log file
			fmt.Fprintf(proc.logFile, "%s%s\n", prefix, line)

			// Capture to memory (with limit)
			if proc.output.Len() < maxOutputCapture {
				proc.output.WriteString(line)
				proc.output.WriteString("\n")
			}
		}
	}

	go capture(stdout, "")
	go capture(stderr, "[stderr] ")

	wg.Wait()
}

func (s *CopilotSpawner) waitForCompletion(proc *Process) {
	defer close(proc.done)
	defer proc.logFile.Close()

	err := proc.cmd.Wait()

	now := time.Now()
	proc.task.CompletedAt = &now
	proc.task.Output = proc.output.String()
	proc.task.OutputTail = s.getTail(proc.output.String(), outputTailLines)

	explicitStop := proc.task.Status == models.TaskStatusCancelled || proc.task.Status == models.TaskStatusPaused

	if err != nil {
		// Preserve explicit stop statuses (cancelled/paused) as the final status.
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

func (s *CopilotSpawner) getTail(output string, lines int) string {
	allLines := strings.Split(output, "\n")
	if len(allLines) <= lines {
		return output
	}
	return strings.Join(allLines[len(allLines)-lines:], "\n")
}

// Cancel stops a running agent.
func (s *CopilotSpawner) Cancel(taskID string) error {
	s.mu.RLock()
	proc, exists := s.processes[taskID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("process not found: %s", taskID)
	}

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
func (s *CopilotSpawner) Pause(taskID string) error {
	s.mu.RLock()
	proc, exists := s.processes[taskID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("process not found: %s", taskID)
	}

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

	proc.task.Status = models.TaskStatusPaused

	return nil
}

// GetProcess returns information about a running process.
func (s *CopilotSpawner) GetProcess(taskID string) (*Process, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	proc, exists := s.processes[taskID]
	return proc, exists
}

// IsRunning checks if a task is currently running.
func (s *CopilotSpawner) IsRunning(taskID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.processes[taskID]
	return exists
}

// Wait blocks until a task completes or context is cancelled.
func (s *CopilotSpawner) Wait(ctx context.Context, taskID string) error {
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

// RunningCount returns the number of currently running processes.
func (s *CopilotSpawner) RunningCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.processes)
}

// Shutdown cancels all running processes.
func (s *CopilotSpawner) Shutdown() {
	s.mu.Lock()
	procs := make([]*Process, 0, len(s.processes))
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
