package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/config"
)

const (
	terminalExecTimeout      = 30 * time.Second
	terminalSessionIdleTTL   = 30 * time.Minute
	terminalMaxOutput        = 64 * 1024
	terminalMaxCombinedBytes = 256 * 1024
)

var dangerousPrefixes = []string{
	"rm -rf /",
	"rm -fr /",
	"sudo",
	"chmod 777 /",
	"chmod -R 777 /",
	"dd if=",
	"mkfs",
	":(){ :|:& };:",
	"> /dev/sda",
	"shred /dev/",
	"wipefs",
	"parted /dev/",
	"fdisk /dev/",
}

type ExecRequest struct {
	Command   string `json:"command"`
	Dir       string `json:"dir,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

type ExecResponse struct {
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
	ExitCode  int    `json:"exit_code"`
	SessionID string `json:"session_id,omitempty"`
	Shell     string `json:"shell,omitempty"`
	Dir       string `json:"dir,omitempty"`
}

type terminalSession struct {
	ID         string
	Dir        string
	LastUsedAt time.Time
}

var terminalSessions = struct {
	sync.Mutex
	items map[string]*terminalSession
}{items: map[string]*terminalSession{}}

func isCommandDangerous(command string) bool {
	trimmed := strings.TrimSpace(command)
	lower := strings.ToLower(trimmed)
	for _, prefix := range dangerousPrefixes {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

func resolveWorkDir(cwd, dir string) string {
	if strings.TrimSpace(dir) == "" {
		return cwd
	}
	if filepath.IsAbs(dir) {
		return cwd
	}
	candidate := filepath.Clean(filepath.Join(cwd, dir))
	if !strings.HasPrefix(candidate, cwd) {
		return cwd
	}
	return candidate
}

func pruneTerminalSessionsLocked() {
	cutoff := time.Now().Add(-terminalSessionIdleTTL)
	for id, session := range terminalSessions.items {
		if session.LastUsedAt.Before(cutoff) {
			delete(terminalSessions.items, id)
		}
	}
}

func getOrCreateTerminalSession(cwd string, req ExecRequest) *terminalSession {
	terminalSessions.Lock()
	defer terminalSessions.Unlock()

	pruneTerminalSessionsLocked()

	if req.SessionID != "" {
		if session, ok := terminalSessions.items[req.SessionID]; ok {
			if strings.TrimSpace(req.Dir) != "" {
				session.Dir = resolveWorkDir(cwd, req.Dir)
			}
			session.LastUsedAt = time.Now()
			return session
		}
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	session := &terminalSession{
		ID:         sessionID,
		Dir:        resolveWorkDir(cwd, req.Dir),
		LastUsedAt: time.Now(),
	}
	terminalSessions.items[sessionID] = session
	return session
}

func resolveShellFromConfig() (string, []string) {
	if cfg := config.Get(); cfg != nil {
		if shell := strings.TrimSpace(cfg.Shell.Path); shell != "" {
			args := append([]string(nil), cfg.Shell.Args...)
			if len(args) == 0 {
				args = []string{"-i", "-c"}
			}
			return shell, args
		}
	}
	return "", nil
}

func choosePreferredShell() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell
	}
	if runtime.GOOS == "darwin" {
		if path, err := exec.LookPath("zsh"); err == nil {
			return path
		}
	}
	if path, err := exec.LookPath("bash"); err == nil {
		return path
	}
	if runtime.GOOS == "darwin" {
		if path, err := exec.LookPath("sh"); err == nil {
			return path
		}
	}
	if path, err := exec.LookPath("zsh"); err == nil {
		return path
	}
	return "/bin/sh"
}

func shellCommandForExec() (string, []string) {
	if shell, args := resolveShellFromConfig(); shell != "" {
		return shell, args
	}
	return choosePreferredShell(), []string{"-i", "-c"}
}

func buildShellEnv(base []string) []string {
	env := append([]string(nil), base...)
	termSet := false
	for i, entry := range env {
		if strings.HasPrefix(entry, "TERM=") {
			env[i] = "TERM=xterm-256color"
			termSet = true
			break
		}
	}
	if !termSet {
		env = append(env, "TERM=xterm-256color")
	}
	return env
}

func quoteShellArg(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func makeShellCommand(workDir, command string) string {
	return fmt.Sprintf("cd %s && %s", quoteShellArg(workDir), command)
}

func classifyExecError(runErr error, output string) (int, string) {
	if runErr == nil {
		return 0, ""
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return exitErr.ExitCode(), ""
	}
	if output != "" {
		return -1, ""
	}
	return -1, runErr.Error()
}

func truncateTerminalOutput(output string) string {
	if len(output) > terminalMaxCombinedBytes {
		output = output[:terminalMaxCombinedBytes] + "\n... [output truncated]"
	}
	if len(output) > terminalMaxOutput {
		output = output[:terminalMaxOutput] + "\n... [output truncated]"
	}
	return output
}

func (s *Server) handleTerminalExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}
	if isCommandDangerous(req.Command) {
		writeError(w, http.StatusForbidden, "command is not allowed for security reasons")
		return
	}

	session := getOrCreateTerminalSession(s.config.CWD, req)
	workDir := session.Dir
	shell, shellArgs := shellCommandForExec()
	args := append([]string(nil), shellArgs...)
	args = append(args, makeShellCommand(workDir, req.Command))

	ctx, cancel := context.WithTimeout(r.Context(), terminalExecTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Env = buildShellEnv(os.Environ())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	combinedOutput := stdout.String()
	if stderr.Len() > 0 {
		combinedOutput += stderr.String()
	}
	combinedOutput = truncateTerminalOutput(combinedOutput)

	exitCode, execError := classifyExecError(runErr, combinedOutput)
	resp := ExecResponse{
		Output:    combinedOutput,
		Error:     execError,
		ExitCode:  exitCode,
		SessionID: session.ID,
		Shell:     shell,
		Dir:       workDir,
	}
	writeJSON(w, http.StatusOK, resp)
}
