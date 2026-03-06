// Package agent handles spawning and managing CLI agent processes.
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

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// OllamaOpenCodeSpawner manages Ollama OpenCode CLI process spawning.
type OllamaOpenCodeSpawner struct {
	logDir     string
	processes  map[string]*OllamaOpenCodeProcess
	mu         sync.RWMutex
	onComplete func(task *models.Task)
}

// OllamaOpenCodeProcess represents a running Ollama OpenCode CLI process.
type OllamaOpenCodeProcess struct {
	cmd        *exec.Cmd
	task       *models.Task
	output     *strings.Builder
	logFile    *os.File
	cancel     context.CancelFunc
	done       chan struct{}
	mcpTempDir string // Temp dir for converted MCP config
}

// NewOllamaOpenCodeSpawner creates a new Ollama OpenCode CLI agent spawner.
func NewOllamaOpenCodeSpawner(logDir string, onComplete func(task *models.Task)) *OllamaOpenCodeSpawner {
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = filepath.Join(home, defaultLogDir)
	}
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = abs
	}
	os.MkdirAll(logDir, 0755)

	return &OllamaOpenCodeSpawner{
		logDir:     logDir,
		processes:  make(map[string]*OllamaOpenCodeProcess),
		onComplete: onComplete,
	}
}

// Spawn starts a new Ollama OpenCode CLI agent.
func (s *OllamaOpenCodeSpawner) Spawn(ctx context.Context, task *models.Task) error {
	// Prepare configuration directory for OpenCode
	configHome := filepath.Join(s.logDir, "ollama-opencode-config", task.ID)
	opencodeConfigDir := filepath.Join(configHome, "opencode")
	if err := os.MkdirAll(opencodeConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	// Load MCP config if provided
	config := make(map[string]interface{})
	var mcpConfigPath string
	var mcpTempDir string

	if task.MCPConfig != "" {
		var err error
		// We use a temporary path for MCP conversion, but we'll merge it into our main config
		mcpTempDir = filepath.Join(s.logDir, "ollama-opencode-mcp-temp", task.ID)
		mcpConfigPath, err = ConvertMCPConfigForOpenCode(task.MCPConfig, task.ID, s.logDir, task.WorkDir)
		if err != nil {
			log.Printf("Warning: failed to convert MCP config: %v", err)
		} else {
			// Read the converted config
			if data, err := os.ReadFile(mcpConfigPath); err == nil {
				json.Unmarshal(data, &config)
			}
			// Clean up the temp file from conversion as we'll write a new one
			// os.Remove(mcpConfigPath) // Optional: clean up
		}
	}

	// Configure 'local' provider for Ollama usage
	// OpenCode's Go version requires 'local' provider to be enabled in config
	// and LOCAL_ENDPOINT env var to be set.
	providers, _ := config["providers"].(map[string]interface{})
	if providers == nil {
		providers = make(map[string]interface{})
		config["providers"] = providers
	}
	providers["local"] = map[string]interface{}{
		"disabled": false,
		"apiKey":   "dummy", // Required to pass validation in OpenCode config loader
	}

	// Write the final config file to <XDG_CONFIG_HOME>/opencode/opencode.json
	finalConfigPath := filepath.Join(opencodeConfigDir, "opencode.json")
	if data, err := json.MarshalIndent(config, "", "  "); err == nil {
		if err := os.WriteFile(finalConfigPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write config file: %w", err)
		}
	}

	// Track config dir for cleanup
	// Note: mcpTempDir (if created by Convert function) is separate, we should track both or just this one
	// Ideally we use a struct field or slice for cleanup paths
	// For now, we reuse the mcpTempDir field on process struct, but it might be misleading if we have multiple dirs
	// Since we set mcpTempDir above for MCP conversion, let's keep it if set, otherwise use configHome
	// A better way is to set mcpTempDir to configHome, and let the conversion temp dir linger or clean it immediately
	if mcpTempDir != "" {
		os.RemoveAll(mcpTempDir) // Clean up the intermediate dir immediately
	}
	mcpTempDir = configHome // Set the main config home as the dir to clean up

	// Build command arguments (use 'run' instead of 'launch')
	// We don't pass mcpConfigPath anymore as it's embedded in the config file
	args := s.buildArgs(task, "")

	// Log the command being executed for debugging
	log.Printf("Executing: opencode %v (routed to Ollama)", args)

	// Create cancellable context
	procCtx, cancel := context.WithCancel(ctx)
	if task.Timeout > 0 {
		procCtx, cancel = context.WithTimeout(ctx, time.Duration(task.Timeout))
	}

	// Create command - use 'opencode' CLI directly
	cmd := exec.CommandContext(procCtx, "opencode", args...)
	cmd.Dir = task.WorkDir

	// Set up environment
	env := append(os.Environ(),
		"NO_COLOR=1",
		"LOCAL_ENDPOINT=http://localhost:11434",       // Point OpenCode's local provider to Ollama
		fmt.Sprintf("XDG_CONFIG_HOME=%s", configHome), // Force OpenCode to use our generated config
	)

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

	// Get stdin for sending prompt
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		logFile.Close()
		return fmt.Errorf("create stdin pipe: %w", err)
	}

	process := &OllamaOpenCodeProcess{
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
		return fmt.Errorf("start ollama launch opencode: %w", err)
	}

	log.Printf("Started Ollama OpenCode CLI process for task %s (PID: %d)", task.ID, cmd.Process.Pid)

	// Send prompt via stdin and close it
	go func() {
		defer stdin.Close()
		if _, err := stdin.Write([]byte(task.Prompt)); err != nil {
			log.Printf("Error writing prompt to stdin for task %s: %v", task.ID, err)
		}
	}()

	// Handle output
	go s.captureOutput(stdout, stderr, process)

	// Wait for completion
	go s.waitForCompletion(process)

	return nil
}

// buildArgs constructs the command-line arguments for Ollama OpenCode CLI.
func (s *OllamaOpenCodeSpawner) buildArgs(task *models.Task, mcpConfigPath string) []string {
	args := []string{
		"run", // Use 'run' subcommand
	}

	// Add model if specified
	// OpenCode's local provider should discover models from Ollama
	// We pass the model name directly
	if task.Model != "" {
		args = append(args, "-m", task.Model)
	}

	// Add persona file if specified
	if task.Persona != "" {
		args = append(args, "--persona", task.Persona)
	}

	args = append(args, task.ExtraArgs...)

	return args
}

// captureOutput reads stdout and stderr concurrently.
func (s *OllamaOpenCodeSpawner) captureOutput(stdout, stderr io.ReadCloser, proc *OllamaOpenCodeProcess) {
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
func (s *OllamaOpenCodeSpawner) waitForCompletion(proc *OllamaOpenCodeProcess) {
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
					log.Printf("Ollama OpenCode CLI task %s was cancelled (signal: %v)", proc.task.ID, status.Signal())
				} else {
					proc.task.Status = models.TaskStatusFailed
					log.Printf("Ollama OpenCode CLI task %s failed with exit code %d", proc.task.ID, *proc.task.ExitCode)
				}
			} else {
				proc.task.Status = models.TaskStatusFailed
			}
		} else {
			proc.task.Status = models.TaskStatusFailed
			exitCode := -1
			proc.task.ExitCode = &exitCode
			log.Printf("Ollama OpenCode CLI task %s failed: %v", proc.task.ID, err)
		}
	} else {
		proc.task.Status = models.TaskStatusCompleted
		exitCode := 0
		proc.task.ExitCode = &exitCode
		log.Printf("Ollama OpenCode CLI task %s completed successfully", proc.task.ID)
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
func (s *OllamaOpenCodeSpawner) Cancel(taskID string) error {
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
func (s *OllamaOpenCodeSpawner) GetOutput(taskID string) (string, error) {
	s.mu.RLock()
	proc, exists := s.processes[taskID]
	s.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("task %s not found", taskID)
	}

	return proc.output.String(), nil
}

// IsRunning checks if a task is currently running.
func (s *OllamaOpenCodeSpawner) IsRunning(taskID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.processes[taskID]
	return exists
}

// Cleanup performs cleanup operations for the spawner.
func (s *OllamaOpenCodeSpawner) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for taskID := range s.processes {
		if err := s.Cancel(taskID); err != nil {
			log.Printf("Error cancelling task %s during cleanup: %v", taskID, err)
		}
	}

	return nil
}
