// Package agent handles spawning and managing CLI agent processes.
package agent

import (
	"bufio"
	"bytes"
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
	"text/template"
	"time"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// templateVars holds values substituted into EngineTemplate.Args entries.
type templateVars struct {
	Model   string
	WorkDir string
	TaskID  string
	LogFile string
}

// TemplateProcess represents a running custom-engine process.
type TemplateProcess struct {
	cmd     *exec.Cmd
	task    *models.Task
	output  *strings.Builder
	logFile *os.File
	cancel  context.CancelFunc
	done    chan struct{}
}

// TemplateSpawner is a generic spawner driven by an EngineTemplate config.
type TemplateSpawner struct {
	tpl        *EngineTemplate
	logDir     string
	processes  map[string]*TemplateProcess
	mu         sync.RWMutex
	onComplete func(task *models.Task)
}

// NewTemplateSpawner creates a spawner for the given engine template.
func NewTemplateSpawner(tpl *EngineTemplate, logDir string, onComplete func(task *models.Task)) *TemplateSpawner {
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = home + "/" + defaultLogDir
	}
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = abs
	}
	os.MkdirAll(logDir, 0755)
	return &TemplateSpawner{
		tpl:        tpl,
		logDir:     logDir,
		processes:  make(map[string]*TemplateProcess),
		onComplete: onComplete,
	}
}

// Spawn starts a new process for the custom engine.
func (s *TemplateSpawner) Spawn(ctx context.Context, task *models.Task) error {
	// Prepend task_id to prompt (same convention as other spawners).
	promptWithID := fmt.Sprintf("You are the task_id: %s\n\n%s", task.ID, task.Prompt)
	task.Prompt = promptWithID

	args, err := s.buildArgs(task)
	if err != nil {
		return fmt.Errorf("template spawner %q: build args: %w", s.tpl.Name, err)
	}

	var (
		procCtx context.Context
		cancel  context.CancelFunc
	)
	if task.Timeout > 0 {
		procCtx, cancel = context.WithTimeout(ctx, time.Duration(task.Timeout))
	} else {
		procCtx, cancel = context.WithCancel(ctx)
	}

	cmd := exec.CommandContext(procCtx, s.tpl.Command, args...)
	cmd.Dir = task.WorkDir

	// Build environment: inherit parent env + template-defined overrides.
	taskEnv, err := s.buildEnv(task)
	if err != nil {
		cancel()
		return fmt.Errorf("template spawner %q: build env: %w", s.tpl.Name, err)
	}
	cmd.Env = taskEnv

	logFile, err := openOrCreateLogFile(s.logDir, task)
	if err != nil {
		cancel()
		return err
	}

	output := &strings.Builder{}

	// For stdin prompt mode, wire up stdin pipe.
	if s.tpl.effectivePromptMode() == PromptModeStdin {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			cancel()
			logFile.Close()
			return fmt.Errorf("template spawner %q: stdin pipe: %w", s.tpl.Name, err)
		}
		go func() {
			defer stdin.Close()
			io.WriteString(stdin, promptWithID)
		}()
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		logFile.Close()
		return fmt.Errorf("template spawner %q: stdout pipe: %w", s.tpl.Name, err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		logFile.Close()
		return fmt.Errorf("template spawner %q: stderr pipe: %w", s.tpl.Name, err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		logFile.Close()
		return fmt.Errorf("template spawner %q: start %q: %w", s.tpl.Name, s.tpl.Command, err)
	}

	task.PID = cmd.Process.Pid
	now := time.Now()
	task.StartedAt = &now
	task.Status = models.TaskStatusRunning

	log.Printf(
		"task_event=started task_id=%s status=%s pid=%d log_file=%q work_dir=%q model=%q engine=%s",
		task.ID, task.Status, task.PID, task.LogFile, task.WorkDir, task.Model, s.tpl.Name,
	)

	proc := &TemplateProcess{
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

// buildArgs constructs the final argument slice for the command.
func (s *TemplateSpawner) buildArgs(task *models.Task) ([]string, error) {
	vars := templateVars{
		Model:   task.Model,
		WorkDir: task.WorkDir,
		TaskID:  task.ID,
		LogFile: task.LogFile,
	}

	var args []string

	// Expand template expressions in fixed args.
	for _, raw := range s.tpl.Args {
		expanded, err := expandTemplateArg(raw, vars)
		if err != nil {
			return nil, fmt.Errorf("expand arg %q: %w", raw, err)
		}
		args = append(args, expanded)
	}

	// Append model arg when configured.
	if s.tpl.ModelArg != "" && task.Model != "" {
		args = append(args, s.tpl.ModelArg, task.Model)
	}

	// Append extra args from the task.
	args = append(args, task.ExtraArgs...)

	// Append prompt arg only for "arg" mode.
	if s.tpl.effectivePromptMode() == PromptModeArg {
		args = append(args, s.tpl.effectivePromptArg(), task.Prompt)
	}

	return args, nil
}

// buildEnv constructs the full environment slice for the command.
// It starts from the current process environment, adds NO_COLOR=1, then applies
// template-defined variables from both Env (map) and EnvVars (list).
// Values in both forms are expanded with Go template expressions.
func (s *TemplateSpawner) buildEnv(task *models.Task) ([]string, error) {
	vars := templateVars{
		Model:   task.Model,
		WorkDir: task.WorkDir,
		TaskID:  task.ID,
		LogFile: task.LogFile,
	}

	env := append(os.Environ(), "NO_COLOR=1")

	// Apply map-style env entries (Env field).
	for k, rawVal := range s.tpl.Env {
		expanded, err := expandTemplateArg(rawVal, vars)
		if err != nil {
			return nil, fmt.Errorf("env %q: %w", k, err)
		}
		env = append(env, k+"="+expanded)
	}

	// Apply list-style env entries (EnvVars field) — processed after Env so
	// they take precedence when the same variable name appears in both.
	for _, ev := range s.tpl.EnvVars {
		expanded, err := expandTemplateArg(ev.Value, vars)
		if err != nil {
			return nil, fmt.Errorf("env_vars[%q]: %w", ev.Name, err)
		}
		env = append(env, ev.Name+"="+expanded)
	}

	return env, nil
}

// expandTemplateArg renders a single arg string with the given variables.
func expandTemplateArg(raw string, vars templateVars) (string, error) {
	if !strings.Contains(raw, "{{") {
		return raw, nil
	}
	tpl, err := template.New("arg").Parse(raw)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (s *TemplateSpawner) captureOutput(proc *TemplateProcess, stdout, stderr io.ReadCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	// stdout: parse according to output_format
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintf(proc.logFile, "%s\n", line)

			if proc.output.Len() < maxOutputCapture {
				text := s.extractOutputLine(line)
				if text != "" {
					proc.output.WriteString(text)
					proc.output.WriteString("\n")
				}
			}
		}
	}()

	// stderr: always captured as-is
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

// extractOutputLine returns the text to accumulate for one line of stdout.
// For JSONL mode it extracts the configured field; for text mode it returns the line as-is.
func (s *TemplateSpawner) extractOutputLine(line string) string {
	if s.tpl.effectiveOutputFormat() != OutputFormatJSONL {
		return line
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		// Not valid JSON — skip silently (header/footer lines are common).
		return ""
	}

	// Apply filter if configured.
	if s.tpl.JSONLFilterField != "" {
		val, ok := dotGet(obj, s.tpl.JSONLFilterField)
		if !ok {
			return ""
		}
		if fmt.Sprintf("%v", val) != s.tpl.JSONLFilterValue {
			return ""
		}
	}

	// Extract output field.
	val, ok := dotGet(obj, s.tpl.JSONLOutputField)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%v", val)
}

// dotGet traverses a nested map using dot-delimited key path.
func dotGet(obj map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.SplitN(path, ".", 2)
	val, ok := obj[parts[0]]
	if !ok {
		return nil, false
	}
	if len(parts) == 1 {
		return val, true
	}
	nested, ok := val.(map[string]interface{})
	if !ok {
		return nil, false
	}
	return dotGet(nested, parts[1])
}

func (s *TemplateSpawner) waitForCompletion(proc *TemplateProcess) {
	defer close(proc.done)
	defer proc.logFile.Close()

	err := proc.cmd.Wait()

	now := time.Now()
	proc.task.CompletedAt = &now
	proc.task.Output = proc.output.String()
	proc.task.OutputTail = getTailLines(proc.output.String(), outputTailLines)

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

// getTailLines returns the last n lines of output (shared helper for template spawner).
func getTailLines(output string, lines int) string {
	all := strings.Split(output, "\n")
	if len(all) <= lines {
		return output
	}
	return strings.Join(all[len(all)-lines:], "\n")
}

// Cancel stops a running custom-engine process.
func (s *TemplateSpawner) Cancel(taskID string) error {
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
func (s *TemplateSpawner) Pause(taskID string) error {
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

// IsRunning reports whether a task is currently running.
func (s *TemplateSpawner) IsRunning(taskID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.processes[taskID]
	return exists
}

// Wait blocks until the task completes or ctx is cancelled.
func (s *TemplateSpawner) Wait(ctx context.Context, taskID string) error {
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

// RunningCount returns the number of active processes.
func (s *TemplateSpawner) RunningCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.processes)
}

// Shutdown cancels all running processes.
func (s *TemplateSpawner) Shutdown() {
	s.mu.Lock()
	procs := make([]*TemplateProcess, 0, len(s.processes))
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
