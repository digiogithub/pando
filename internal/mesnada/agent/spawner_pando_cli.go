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

// PandoCLISpawner manages pando CLI process spawning.
// It runs the pando binary itself as a subprocess with --yolo --output-format text,
// making it the default engine when no specific engine is requested.
type PandoCLISpawner struct {
	logDir        string
	processes     map[string]*Process
	mu            sync.RWMutex
	onComplete    func(task *models.Task)
	resolveModel  func(modelID string) string // resolves model to "provider.model" format
}

// NewPandoCLISpawner creates a new pando CLI spawner.
// resolveModel converts a model ID (possibly empty or shorthand) into the full
// "provider.model" string expected by pando's -m flag. If nil, model IDs are
// passed through as-is.
func NewPandoCLISpawner(logDir string, onComplete func(task *models.Task), resolveModel func(string) string) *PandoCLISpawner {
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = filepath.Join(home, defaultLogDir)
	}
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = abs
	}
	os.MkdirAll(logDir, 0755)

	return &PandoCLISpawner{
		logDir:       logDir,
		processes:    make(map[string]*Process),
		onComplete:   onComplete,
		resolveModel: resolveModel,
	}
}

// Spawn starts a new pando CLI subprocess with the task prompt.
func (s *PandoCLISpawner) Spawn(ctx context.Context, task *models.Task) error {
	args, prompt := s.buildArgs(task)

	procCtx, cancel := context.WithCancel(ctx)
	if task.Timeout > 0 {
		procCtx, cancel = context.WithTimeout(ctx, time.Duration(task.Timeout))
	}

	pandoBin, err := s.resolvePandoBinary()
	if err != nil {
		cancel()
		return err
	}

	cmd := exec.CommandContext(procCtx, pandoBin, args...)
	if task.WorkDir != "" {
		cmd.Dir = task.WorkDir
	}
	cmd.Env = append(os.Environ(), "NO_COLOR=1")

	logFile, err := openOrCreateLogFile(s.logDir, task)
	if err != nil {
		cancel()
		return err
	}

	output := &strings.Builder{}

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

	if err := cmd.Start(); err != nil {
		cancel()
		logFile.Close()
		return fmt.Errorf("failed to start pando: %w", err)
	}

	// Write prompt to stdin and close it.
	go func() {
		defer stdin.Close()
		stdin.Write([]byte(prompt))
	}()

	task.PID = cmd.Process.Pid
	now := time.Now()
	task.StartedAt = &now
	task.Status = models.TaskStatusRunning

	log.Printf(
		"task_event=started engine=pando-cli task_id=%s status=%s pid=%d log_file=%q work_dir=%q model=%q",
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

	go s.captureOutput(proc, stdout, stderr)
	go s.waitForCompletion(proc)

	return nil
}

// buildArgs constructs the pando CLI arguments and returns them together with
// the prompt text that should be sent via stdin.
func (s *PandoCLISpawner) buildArgs(task *models.Task) (args []string, prompt string) {
	// Prepend task ID to prompt so the subprocess knows its identity.
	prompt = fmt.Sprintf("You are the task_id: %s\n\n%s", task.ID, task.Prompt)
	task.Prompt = prompt

	args = []string{
		"--yolo",
		"--output-format", "text",
	}

	// Resolve model to "provider.model" format.
	modelArg := task.Model
	if s.resolveModel != nil {
		modelArg = s.resolveModel(task.Model)
	}
	if modelArg != "" {
		args = append(args, "-m", modelArg)
	}

	// Extra args forwarded from the task.
	args = append(args, task.ExtraArgs...)

	return args, prompt
}

// resolvePandoBinary returns the path to the pando binary.
// It first tries to find the running process's own executable, then falls back
// to PATH lookup for "pando".
func (s *PandoCLISpawner) resolvePandoBinary() (string, error) {
	// Prefer the binary that is currently running (self-spawn).
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe, nil
	}
	// Fallback to PATH.
	p, err := exec.LookPath("pando")
	if err != nil {
		return "", fmt.Errorf("pando binary not found in PATH: %w", err)
	}
	return p, nil
}

func (s *PandoCLISpawner) captureOutput(proc *Process, stdout, stderr io.ReadCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	capture := func(r io.ReadCloser, prefix string) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintf(proc.logFile, "%s%s\n", prefix, line)
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

func (s *PandoCLISpawner) waitForCompletion(proc *Process) {
	defer close(proc.done)
	defer proc.logFile.Close()

	err := proc.cmd.Wait()

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

func (s *PandoCLISpawner) getTail(output string, lines int) string {
	allLines := strings.Split(output, "\n")
	if len(allLines) <= lines {
		return output
	}
	return strings.Join(allLines[len(allLines)-lines:], "\n")
}

// Cancel stops a running pando CLI process.
func (s *PandoCLISpawner) Cancel(taskID string) error {
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

// Pause stops a running process without marking it as cancelled.
func (s *PandoCLISpawner) Pause(taskID string) error {
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

// Wait blocks until the task completes or the context is cancelled.
func (s *PandoCLISpawner) Wait(ctx context.Context, taskID string) error {
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

// IsRunning reports whether the task is currently running.
func (s *PandoCLISpawner) IsRunning(taskID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.processes[taskID]
	return exists
}

// RunningCount returns the number of currently running processes.
func (s *PandoCLISpawner) RunningCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.processes)
}

// Shutdown cancels all running processes.
func (s *PandoCLISpawner) Shutdown() {
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
