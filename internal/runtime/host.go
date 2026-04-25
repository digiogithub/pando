package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/digiogithub/pando/internal/llm/tools/shell"
)

// hostRuntime implements ExecutionRuntime by delegating to the persistent host shell.
// It preserves existing behaviour 100% — no behavioural changes from the current
// shell.GetPersistentShell path.
type hostRuntime struct {
	mu       sync.Mutex
	sessions map[string]string // sessionID → workDir
}

// NewHostRuntime returns an ExecutionRuntime backed by the host shell.
func NewHostRuntime() ExecutionRuntime {
	return &hostRuntime{
		sessions: make(map[string]string),
	}
}

func (h *hostRuntime) Type() RuntimeType { return RuntimeHost }

func (h *hostRuntime) StartSession(_ context.Context, sessionID string, workDir string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[sessionID] = workDir
	return nil
}

func (h *hostRuntime) StopSession(_ context.Context, sessionID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, sessionID)
	return nil
}

func (h *hostRuntime) workDir(sessionID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[sessionID]
}

// Exec runs cmd in the persistent host shell for the session's working directory.
// The env parameter is ignored for the host runtime (the shell inherits the process env).
// A default timeout of 5 minutes is used if no cancellation is set on ctx.
func (h *hostRuntime) Exec(ctx context.Context, sessionID string, cmd string, _ []string) (ExecResult, error) {
	wd := h.workDir(sessionID)
	if wd == "" {
		return ExecResult{}, fmt.Errorf("no session %q: call StartSession first", sessionID)
	}
	sh := shell.GetPersistentShell(wd)
	const defaultTimeoutMs = 5 * 60 * 1000 // 5 minutes
	stdout, stderr, exitCode, interrupted, err := sh.Exec(ctx, cmd, defaultTimeoutMs)
	if err != nil {
		return ExecResult{}, err
	}
	return ExecResult{
		Stdout:      stdout,
		Stderr:      stderr,
		ExitCode:    exitCode,
		Interrupted: interrupted,
	}, nil
}

// Output is not meaningful for the host shell (output is returned from Exec directly).
func (h *hostRuntime) Output(_ context.Context, _ string) (string, error) {
	return "", nil
}

// Kill sends a kill signal to the current host shell children.
// The host shell's built-in kill mechanism is invoked via a cancel on the Exec ctx;
// this method is provided for interface completeness.
func (h *hostRuntime) Kill(_ context.Context, _ string) error {
	return nil
}
