package shell

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// Cwd returns the shell's current working directory (tracked after every
// command).
func (s *PersistentShell) Cwd() string {
	return s.currentCwd()
}

// shellBinary is the configured shell (Shell.Path, then $SHELL, then
// /bin/bash), the same one the persistent shell starts.
func shellBinary() string {
	if cfg := config.Get(); cfg != nil && cfg.Shell.Path != "" {
		return cfg.Shell.Path
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/bash"
}

// ExecUnsandboxed runs command once, outside the host sandbox, as
// `<shell> -c command` in dir with Pando's own (unscrubbed) environment. It is
// the bash tool's escalation path after the user approved it: the persistent
// shell (sandboxed, with its state) is not used, so nothing the command does
// to shell state (cd, exports) persists.
//
// The result mirrors PersistentShell.Exec: stdout, stderr, exit code, whether
// the command was interrupted (timeout or ctx), and an error only when the
// command could not be started. timeoutMs <= 0 means no timeout besides ctx.
func ExecUnsandboxed(ctx context.Context, dir, command string, timeoutMs int) (string, string, int, bool, error) {
	runCtx := ctx
	if timeoutMs > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
		defer cancel()
	}

	cmd := exec.Command(shellBinary(), "-c", command)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_EDITOR=true")
	cmd.Stdin = nil // /dev/null
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// A background child that keeps the output pipes open must not hold the
	// tool once the shell itself has exited.
	cmd.WaitDelay = 2 * time.Second
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return "", err.Error(), 1, false, err
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	var waitErr error
	interrupted := false
	select {
	case waitErr = <-waitCh:
	case <-runCtx.Done():
		interrupted = true
		done := make(chan struct{})
		if !killProcessGroup(cmd.Process.Pid, done) {
			_ = cmd.Process.Kill()
		}
		select {
		case waitErr = <-waitCh:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			waitErr = <-waitCh
		}
		close(done)
	}

	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.As(waitErr, &exitErr):
			exitCode = exitErr.ExitCode()
		case cmd.ProcessState != nil:
			// exec.ErrWaitDelay: the shell exited, a child held the pipes.
			exitCode = cmd.ProcessState.ExitCode()
		default:
			exitCode = 1
		}
	}
	errOut := stderr.String()
	if interrupted {
		if exitCode == 0 || exitCode == -1 {
			exitCode = 143
		}
		errOut += "\nCommand execution timed out or was interrupted"
	}
	return stdout.String(), errOut, exitCode, interrupted, nil
}
