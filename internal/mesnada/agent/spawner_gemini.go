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

// GeminiSpawner manages Gemini CLI process spawning.
type GeminiSpawner struct {
	logDir     string
	processes  map[string]*GeminiProcess
	mu         sync.RWMutex
	onComplete func(task *models.Task)
}

// GeminiProcess represents a running Gemini CLI process.
type GeminiProcess struct {
	cmd                *exec.Cmd
	task               *models.Task
	output             *strings.Builder
	logFile            *os.File
	cancel             context.CancelFunc
	done               chan struct{}
	geminiSettingsPath string // Temp settings.json path for MCP config
}

// NewGeminiSpawner creates a new Gemini CLI agent spawner.
func NewGeminiSpawner(logDir string, onComplete func(task *models.Task)) *GeminiSpawner {
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = filepath.Join(home, defaultLogDir)
	}
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = abs
	}
	os.MkdirAll(logDir, 0755)

	return &GeminiSpawner{
		logDir:     logDir,
		processes:  make(map[string]*GeminiProcess),
		onComplete: onComplete,
	}
}

// Spawn starts a new Gemini CLI agent.
func (s *GeminiSpawner) Spawn(ctx context.Context, task *models.Task) error {
	// Create temporary settings.json with MCP config
	var geminiSettingsPath string
	if task.MCPConfig != "" {
		var err error
		geminiSettingsPath, err = CreateGeminiSettingsFile(task.MCPConfig, task.ID, s.logDir, task.WorkDir)
		if err != nil {
			log.Printf("Warning: failed to create Gemini settings for MCP config: %v", err)
			// Continue without MCP config
		}
	}

	// Build command arguments
	args := s.buildArgs(task)

	// Create cancellable context
	procCtx, cancel := context.WithCancel(ctx)
	if task.Timeout > 0 {
		procCtx, cancel = context.WithTimeout(ctx, time.Duration(task.Timeout))
	}

	// Create command - use 'gemini' CLI
	cmd := exec.CommandContext(procCtx, "gemini", args...)
	cmd.Dir = task.WorkDir

	// Set up environment
	env := append(os.Environ(), "NO_COLOR=1")

	// If we have MCP config, pass it via GEMINI_CLI_SYSTEM_SETTINGS_PATH
	if geminiSettingsPath != "" {
		env = append(env, fmt.Sprintf("GEMINI_CLI_SYSTEM_SETTINGS_PATH=%s", geminiSettingsPath))
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
		return fmt.Errorf("failed to start gemini: %w", err)
	}

	task.PID = cmd.Process.Pid
	now := time.Now()
	task.StartedAt = &now
	task.Status = models.TaskStatusRunning

	log.Printf(
		"task_event=started task_id=%s status=%s pid=%d log_file=%q work_dir=%q model=%q engine=gemini",
		task.ID,
		task.Status,
		task.PID,
		task.LogFile,
		task.WorkDir,
		task.Model,
	)

	proc := &GeminiProcess{
		cmd:                cmd,
		task:               task,
		output:             output,
		logFile:            logFile,
		cancel:             cancel,
		done:               make(chan struct{}),
		geminiSettingsPath: geminiSettingsPath,
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

func (s *GeminiSpawner) buildArgs(task *models.Task) []string {
	// Prepend task_id to the prompt
	promptWithTaskID := fmt.Sprintf("You are the task_id: %s\n\n%s", task.ID, task.Prompt)

	args := []string{
		"--yolo", // Auto-approve all actions for non-interactive mode
	}

	if task.Model != "" {
		args = append(args, "--model", task.Model)
	}

	// MCP servers are configured via GEMINI_CLI_SYSTEM_SETTINGS_PATH env var
	// pointing to a task-specific temporary settings.json file

	args = append(args, task.ExtraArgs...)

	// Add the prompt as positional argument (not with -p flag)
	args = append(args, promptWithTaskID)

	// Store the modified prompt
	task.Prompt = promptWithTaskID

	return args
}

func (s *GeminiSpawner) captureOutput(proc *GeminiProcess, stdout, stderr io.ReadCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Capture stdout as-is (Gemini CLI outputs text by default)
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

func (s *GeminiSpawner) waitForCompletion(proc *GeminiProcess) {
	defer close(proc.done)
	defer proc.logFile.Close()

	err := proc.cmd.Wait()

	// Clean up temporary settings file
	if proc.geminiSettingsPath != "" {
		CleanupGeminiSettingsFile(proc.geminiSettingsPath)
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

func (s *GeminiSpawner) getTail(output string, lines int) string {
	allLines := strings.Split(output, "\n")
	if len(allLines) <= lines {
		return output
	}
	return strings.Join(allLines[len(allLines)-lines:], "\n")
}

// Cancel stops a running agent.
func (s *GeminiSpawner) Cancel(taskID string) error {
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
func (s *GeminiSpawner) Pause(taskID string) error {
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
func (s *GeminiSpawner) IsRunning(taskID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.processes[taskID]
	return exists
}

// Wait blocks until a task completes or context is cancelled.
func (s *GeminiSpawner) Wait(ctx context.Context, taskID string) error {
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
func (s *GeminiSpawner) RunningCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.processes)
}

// Shutdown cancels all running processes.
func (s *GeminiSpawner) Shutdown() {
	s.mu.Lock()
	procs := make([]*GeminiProcess, 0, len(s.processes))
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
