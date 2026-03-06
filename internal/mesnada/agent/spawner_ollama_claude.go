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

// OllamaClaudeSpawner manages Ollama Claude CLI process spawning.
type OllamaClaudeSpawner struct {
	logDir     string
	processes  map[string]*OllamaClaudeProcess
	mu         sync.RWMutex
	onComplete func(task *models.Task)
}

// OllamaClaudeProcess represents a running Ollama Claude CLI process.
type OllamaClaudeProcess struct {
	cmd        *exec.Cmd
	task       *models.Task
	output     *strings.Builder
	logFile    *os.File
	cancel     context.CancelFunc
	done       chan struct{}
	mcpTempDir string // Temp dir for converted MCP config
}

// NewOllamaClaudeSpawner creates a new Ollama Claude CLI agent spawner.
func NewOllamaClaudeSpawner(logDir string, onComplete func(task *models.Task)) *OllamaClaudeSpawner {
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = filepath.Join(home, defaultLogDir)
	}
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = abs
	}
	os.MkdirAll(logDir, 0755)

	return &OllamaClaudeSpawner{
		logDir:     logDir,
		processes:  make(map[string]*OllamaClaudeProcess),
		onComplete: onComplete,
	}
}

// Spawn starts a new Ollama Claude CLI agent.
func (s *OllamaClaudeSpawner) Spawn(ctx context.Context, task *models.Task) error {
	// Convert MCP config if provided (use Claude's MCP config format)
	var mcpConfigPath string
	var mcpTempDir string
	if task.MCPConfig != "" {
		var err error
		mcpTempDir = filepath.Join(s.logDir, "ollama-claude-mcp", task.ID)
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
	log.Printf("Executing: claude %v (routed to Ollama)", args)

	// Create cancellable context
	procCtx, cancel := context.WithCancel(ctx)
	if task.Timeout > 0 {
		procCtx, cancel = context.WithTimeout(ctx, time.Duration(task.Timeout))
	}

	// Create command - use 'claude' CLI directly but configured for Ollama
	cmd := exec.CommandContext(procCtx, "claude", args...)
	cmd.Dir = task.WorkDir

	// Set up environment to point Claude to Ollama
	// See "Option 1" in conversation: invoke integration directly
	env := os.Environ()
	env = append(env,
		"NO_COLOR=1",
		"ANTHROPIC_BASE_URL=http://localhost:11434",
		"ANTHROPIC_AUTH_TOKEN=ollama",
		"ANTHROPIC_API_KEY=", // Empty key for Ollama
	)

	// If model is specified, ensure environment vars force it for all tiers
	if task.Model != "" {
		env = append(env,
			"ANTHROPIC_DEFAULT_OPUS_MODEL="+task.Model,
			"ANTHROPIC_DEFAULT_SONNET_MODEL="+task.Model,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL="+task.Model,
			"CLAUDE_CODE_SUBAGENT_MODEL="+task.Model,
		)
	}

	cmd.Env = env

	// Create or append to log file
	logFile, err := openOrCreateLogFile(s.logDir, task)
	if err != nil {
		cancel()
		return err
	}

	// Set up output capture
	var output strings.Builder
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		logFile.Close()
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		logFile.Close()
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	process := &OllamaClaudeProcess{
		cmd:        cmd,
		task:       task,
		output:     &output,
		logFile:    logFile,
		cancel:     cancel,
		done:       make(chan struct{}),
		mcpTempDir: mcpTempDir,
	}

	s.mu.Lock()
	s.processes[task.ID] = process
	s.mu.Unlock()

	// Start the command
	if err := cmd.Start(); err != nil {
		cancel()
		logFile.Close()
		s.mu.Lock()
		delete(s.processes, task.ID)
		s.mu.Unlock()
		return fmt.Errorf("start ollama launch claude: %w", err)
	}

	log.Printf("Started Ollama Claude CLI process for task %s (PID: %d)", task.ID, cmd.Process.Pid)

	// Handle output
	go s.captureOutput(stdout, stderr, process)

	// Wait for completion
	go s.waitForCompletion(process)

	return nil
}

// buildArgs constructs the command-line arguments for Ollama Claude CLI.
func (s *OllamaClaudeSpawner) buildArgs(task *models.Task, mcpConfigPath string) []string {
	promptWithTaskID := fmt.Sprintf("You are the task_id: %s\n\n%s", task.ID, task.Prompt)

	args := []string{"--print", "--output-format", "text", "--verbose", "--dangerously-skip-permissions"}

	// Note: --model flag is still useful even if env vars are set, to ensure 'claude' logic respects it
	if task.Model != "" {
		args = append(args, "--model", task.Model)
	}

	if mcpConfigPath != "" {
		args = append(args, "--mcp-config", mcpConfigPath)
	}

	// Add persona file if specified
	if task.Persona != "" {
		args = append(args, "--persona", task.Persona)
	}

	if len(task.ExtraArgs) > 0 {
		args = append(args, task.ExtraArgs...)
	}

	args = append(args, promptWithTaskID)

	// Store the modified prompt for logging/output consistency
	task.Prompt = promptWithTaskID

	return args
}

// captureOutput reads stdout and stderr concurrently.
func (s *OllamaClaudeSpawner) captureOutput(stdout, stderr io.ReadCloser, proc *OllamaClaudeProcess) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Read stdout
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			proc.output.WriteString(line + "\n")
			proc.logFile.WriteString(line + "\n")
		}
	}()

	// Read stderr
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			proc.output.WriteString(line + "\n")
			proc.logFile.WriteString(line + "\n")
		}
	}()

	wg.Wait()
}

// waitForCompletion waits for the process to finish.
func (s *OllamaClaudeSpawner) waitForCompletion(proc *OllamaClaudeProcess) {
	defer close(proc.done)
	defer proc.logFile.Close()
	defer proc.cancel()

	// Clean up MCP temp dir when done
	if proc.mcpTempDir != "" {
		defer func() {
			if err := os.RemoveAll(proc.mcpTempDir); err != nil {
				log.Printf("Warning: failed to clean up MCP temp dir %s: %v", proc.mcpTempDir, err)
			}
		}()
	}

	err := proc.cmd.Wait()

	proc.task.Output = proc.output.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			proc.task.ExitCode = &exitCode
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() {
					proc.task.Status = models.TaskStatusCancelled
					log.Printf("Ollama Claude CLI task %s was cancelled (signal: %v)", proc.task.ID, status.Signal())
				} else {
					proc.task.Status = models.TaskStatusFailed
					log.Printf("Ollama Claude CLI task %s failed with exit code %d", proc.task.ID, *proc.task.ExitCode)
				}
			} else {
				proc.task.Status = models.TaskStatusFailed
			}
		} else {
			proc.task.Status = models.TaskStatusFailed
			exitCode := -1
			proc.task.ExitCode = &exitCode
			log.Printf("Ollama Claude CLI task %s failed: %v", proc.task.ID, err)
		}
	} else {
		proc.task.Status = models.TaskStatusCompleted
		exitCode := 0
		proc.task.ExitCode = &exitCode
		log.Printf("Ollama Claude CLI task %s completed successfully", proc.task.ID)
	}

	now := time.Now().UTC()
	proc.task.CompletedAt = &now

	s.mu.Lock()
	delete(s.processes, proc.task.ID)
	s.mu.Unlock()

	if s.onComplete != nil {
		s.onComplete(proc.task)
	}
}

// Cancel stops a running process.
func (s *OllamaClaudeSpawner) Cancel(taskID string) error {
	s.mu.RLock()
	proc, exists := s.processes[taskID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	proc.cancel()

	if proc.cmd.Process != nil {
		if err := proc.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill process: %w", err)
		}
	}

	<-proc.done
	return nil
}

// GetOutput returns the current output of a running task.
func (s *OllamaClaudeSpawner) GetOutput(taskID string) (string, error) {
	s.mu.RLock()
	proc, exists := s.processes[taskID]
	s.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("task %s not found", taskID)
	}

	return proc.output.String(), nil
}

// IsRunning checks if a task is currently running.
func (s *OllamaClaudeSpawner) IsRunning(taskID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.processes[taskID]
	return exists
}

// Cleanup performs cleanup operations for the spawner.
func (s *OllamaClaudeSpawner) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for taskID := range s.processes {
		if err := s.Cancel(taskID); err != nil {
			log.Printf("Error cancelling task %s during cleanup: %v", taskID, err)
		}
	}

	return nil
}
