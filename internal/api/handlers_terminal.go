package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	terminalExecTimeout = 30 * time.Second
	terminalMaxOutput   = 64 * 1024 // 64 KB
)

// dangerousPrefixes is the list of command prefixes that are never allowed.
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

// ExecRequest is the body for POST /api/v1/terminal/exec.
type ExecRequest struct {
	Command string `json:"command"`
	Dir     string `json:"dir,omitempty"`
}

// ExecResponse is the JSON result returned after executing a command.
type ExecResponse struct {
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code"`
}

// isCommandDangerous returns true if the command matches any forbidden prefix.
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

// resolveWorkDir sanitizes the requested directory so it always stays inside
// the project CWD. An empty dir defaults to the CWD itself.
func resolveWorkDir(cwd, dir string) string {
	if strings.TrimSpace(dir) == "" {
		return cwd
	}

	// If the caller passed an absolute path, ignore it and use cwd.
	if filepath.IsAbs(dir) {
		return cwd
	}

	// Join and clean to prevent "../../../" traversal.
	candidate := filepath.Clean(filepath.Join(cwd, dir))

	// Ensure the resolved path is still inside the project cwd.
	if !strings.HasPrefix(candidate, cwd) {
		return cwd
	}

	return candidate
}

// handleTerminalExec executes a shell command inside the project directory.
// POST /api/v1/terminal/exec
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

	workDir := resolveWorkDir(s.config.CWD, req.Dir)

	ctx, cancel := context.WithTimeout(r.Context(), terminalExecTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", req.Command)
	cmd.Dir = workDir

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()

	// Truncate output to avoid oversized responses.
	output := buf.String()
	if len(output) > terminalMaxOutput {
		output = output[:terminalMaxOutput] + "\n... [output truncated]"
	}

	resp := ExecResponse{
		Output:   output,
		ExitCode: 0,
	}

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			resp.ExitCode = exitErr.ExitCode()
		} else {
			// Timeout or other execution error.
			resp.ExitCode = -1
			resp.Error = runErr.Error()
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
