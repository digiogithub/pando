package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/sandbox"
)

// PersistentShell is the long-lived shell the bash tool (and the host
// runtime) runs commands in. There is one per process (GetPersistentShell).
//
// When the host sandbox covers bash, the shell is spawned through
// sandbox.WrapCmd: its environment is scrubbed and the process is confined to
// the policy that was current at spawn time. That policy's hash is recorded so
// GetPersistentShell can replace the shell as soon as the policy changes (a
// settings toggle applies to the next command).
type PersistentShell struct {
	cmd          *exec.Cmd
	stdin        *os.File
	isAlive      atomic.Bool
	mu           sync.Mutex // serializes command execution
	commandQueue chan *commandExecution
	// done is closed once the shell process has exited.
	done chan struct{}

	cwdMu sync.Mutex
	cwd   string

	// sandbox describes the confinement applied at spawn time.
	sandbox SandboxInfo
	// launcher is true when the wrapper replaced the shell binary with a
	// launcher (sandbox-exec, the re-exec helper, bwrap). A launcher may leave
	// an extra process between Pando and the shell, so interrupts kill the
	// whole process group instead of the shell's direct children.
	launcher bool
	// releaseTree releases the Windows Job Object (no-op elsewhere).
	releaseTree func()
	closeOnce   sync.Once
	// spawnErr is set on a placeholder returned when the shell could not be
	// started; Exec reports it.
	spawnErr error
}

// SandboxInfo describes how the persistent shell was confined when spawned.
type SandboxInfo struct {
	// Policy is the policy resolved at spawn time.
	Policy sandbox.Policy
	// Capability is the backend that wrapped the shell.
	Capability sandbox.Capability
	// PolicyHash is Policy.Hash(); a mismatch with
	// sandbox.CurrentPolicyHash() makes GetPersistentShell re-spawn.
	PolicyHash string
}

// Active reports whether the shell really runs confined: the policy covers
// bash and the backend enforces it.
func (i SandboxInfo) Active() bool {
	return i.Policy.Covers(sandbox.PurposeBash) && i.Capability.Enforced
}

// AutoAllowBash reports whether the shell's policy lets the bash tool skip
// the permission prompt for commands that are not dangerous. It is only true
// while the shell is Active and the sandbox gives its full guarantees
// (sandbox.Guarantees: protected paths and Pando's own ports enforced); a
// partial sandbox keeps the prompt.
func (i SandboxInfo) AutoAllowBash() bool {
	if !i.Active() || !i.Policy.AutoAllowBash {
		return false
	}
	full, _ := sandbox.Guarantees(i.Policy, i.Capability)
	return full
}

type commandExecution struct {
	command    string
	timeout    time.Duration
	resultChan chan commandResult
	ctx        context.Context
}

type commandResult struct {
	stdout      string
	stderr      string
	exitCode    int
	interrupted bool
	err         error
}

var (
	shellMu       sync.Mutex // guards shellInstance
	shellInstance *PersistentShell
)

// GetPersistentShell returns the process-wide persistent shell, starting it in
// workingDir on first use. A new shell replaces the current one when it has
// died or when the sandbox policy changed since it was spawned (the
// replacement starts in the old shell's current directory). The old shell of
// a policy change is retired gracefully: its stdin is closed, so it exits
// after the command it may be running.
//
// It never returns nil: when the shell cannot be started, the returned
// placeholder reports the reason from Exec and the next call retries.
func GetPersistentShell(workingDir string) *PersistentShell {
	shellMu.Lock()
	defer shellMu.Unlock()

	hash := sandbox.CurrentPolicyHash()
	old := shellInstance
	if old != nil && old.isAlive.Load() && old.sandbox.PolicyHash == hash {
		return old
	}

	cwd := workingDir
	if old != nil {
		if c := old.currentCwd(); c != "" {
			cwd = c
		}
		if old.isAlive.Load() {
			logging.Info("sandbox: policy changed, re-spawning persistent shell",
				"backend", old.sandbox.Capability.Backend)
			old.retire()
		}
	}

	sh, err := newPersistentShell(cwd)
	if err != nil {
		logging.Warn("persistent shell unavailable", "error", err)
		sh = deadShell(cwd, err)
	}
	shellInstance = sh
	return sh
}

// ResetForTests closes the process-wide shell so the next GetPersistentShell
// spawns a fresh one. For tests in other packages.
func ResetForTests() {
	shellMu.Lock()
	defer shellMu.Unlock()
	if shellInstance != nil {
		shellInstance.Close()
		shellInstance = nil
	}
}

// deadShell is the placeholder GetPersistentShell returns when a spawn fails.
func deadShell(cwd string, err error) *PersistentShell {
	s := &PersistentShell{cwd: cwd, done: make(chan struct{}), spawnErr: err, releaseTree: func() {}}
	close(s.done)
	return s
}

func newPersistentShell(cwd string) (*PersistentShell, error) {
	// Get shell configuration from config
	cfg := config.Get()

	// Default to environment variable if config is not set or nil
	var shellPath string
	var shellArgs []string

	if cfg != nil {
		shellPath = cfg.Shell.Path
		shellArgs = cfg.Shell.Args
	}

	if shellPath == "" {
		shellPath = os.Getenv("SHELL")
		if shellPath == "" {
			shellPath = "/bin/bash"
		}
	}

	// Default shell args
	if len(shellArgs) == 0 {
		shellArgs = []string{"-l"}
	}

	cmd := exec.Command(shellPath, shellArgs...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "GIT_EDITOR=true")

	// Scrub the environment and confine the shell when the sandbox policy
	// covers bash. The wrapper fails open when the backend is unavailable; an
	// error means enforcement was possible but could not be set up, and the
	// command must not be started unconfined.
	origPath := cmd.Path
	policy, capability, err := sandbox.WrapCmd(cmd, sandbox.PurposeBash)
	if err != nil {
		return nil, fmt.Errorf("sandbox: cannot start the shell confined: %w", err)
	}
	setProcessGroup(cmd)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("shell stdin: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start shell %s: %w", shellPath, err)
	}

	release, err := sandbox.AttachProcessTree(cmd.Process)
	if err != nil {
		logging.Warn("sandbox: cannot attach the shell to a job object", "error", err)
	}

	shell := &PersistentShell{
		cmd:          cmd,
		stdin:        stdinPipe.(*os.File),
		cwd:          cwd,
		commandQueue: make(chan *commandExecution, 10),
		done:         make(chan struct{}),
		sandbox: SandboxInfo{
			Policy:     policy,
			Capability: capability,
			PolicyHash: policy.Hash(),
		},
		launcher:    cmd.Path != origPath,
		releaseTree: release,
	}
	shell.isAlive.Store(true)

	if shell.sandbox.Active() {
		logging.Debug("sandbox: persistent shell confined",
			"mode", policy.Mode, "backend", capability.String())
		sandbox.Emit(context.Background(), sandbox.Event{
			Type:    sandbox.EventApplied,
			Backend: capability.Backend,
			Mode:    string(policy.Mode),
		})
	} else if policy.Covers(sandbox.PurposeBash) {
		logging.Debug("sandbox: persistent shell not confined",
			"mode", policy.Mode, "backend", capability.String())
		sandbox.Emit(context.Background(), sandbox.Event{
			Type:    sandbox.EventUnavailable,
			Backend: capability.Backend,
			Mode:    string(policy.Mode),
			Reason:  capability.Reason,
		})
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "Panic in shell command processor: %v\n", r)
				shell.isAlive.Store(false)
			}
		}()
		shell.processCommands()
	}()

	go func() {
		_ = cmd.Wait()
		shell.isAlive.Store(false)
		shell.releaseTree()
		close(shell.done)
	}()

	return shell, nil
}

// Sandbox describes how the shell was confined when it was spawned.
func (s *PersistentShell) Sandbox() SandboxInfo {
	return s.sandbox
}

func (s *PersistentShell) currentCwd() string {
	s.cwdMu.Lock()
	defer s.cwdMu.Unlock()
	return s.cwd
}

func (s *PersistentShell) setCwd(cwd string) {
	s.cwdMu.Lock()
	defer s.cwdMu.Unlock()
	s.cwd = cwd
}

func (s *PersistentShell) processCommands() {
	for {
		select {
		case cmd := <-s.commandQueue:
			cmd.resultChan <- s.execCommand(cmd.command, cmd.timeout, cmd.ctx)
		case <-s.done:
			// Fail whatever is still queued; Exec also stops waiting on done.
			for {
				select {
				case cmd := <-s.commandQueue:
					cmd.resultChan <- notAliveResult()
				default:
					return
				}
			}
		}
	}
}

func notAliveResult() commandResult {
	return commandResult{
		stderr:   "Shell is not alive",
		exitCode: 1,
		err:      errors.New("shell is not alive"),
	}
}

func (s *PersistentShell) execCommand(command string, timeout time.Duration, ctx context.Context) commandResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isAlive.Load() {
		return notAliveResult()
	}

	tempDir := os.TempDir()
	stdoutFile := filepath.Join(tempDir, fmt.Sprintf("pando-stdout-%d", time.Now().UnixNano()))
	stderrFile := filepath.Join(tempDir, fmt.Sprintf("pando-stderr-%d", time.Now().UnixNano()))
	statusFile := filepath.Join(tempDir, fmt.Sprintf("pando-status-%d", time.Now().UnixNano()))
	cwdFile := filepath.Join(tempDir, fmt.Sprintf("pando-cwd-%d", time.Now().UnixNano()))

	defer func() {
		os.Remove(stdoutFile)
		os.Remove(stderrFile)
		os.Remove(statusFile)
		os.Remove(cwdFile)
	}()

	fullCommand := fmt.Sprintf(`
eval %s < /dev/null > %s 2> %s
EXEC_EXIT_CODE=$?
pwd > %s
echo $EXEC_EXIT_CODE > %s
`,
		shellQuote(command),
		shellQuote(stdoutFile),
		shellQuote(stderrFile),
		shellQuote(cwdFile),
		shellQuote(statusFile),
	)

	_, err := s.stdin.Write([]byte(fullCommand + "\n"))
	if err != nil {
		return commandResult{
			stderr:   fmt.Sprintf("Failed to write command to shell: %v", err),
			exitCode: 1,
			err:      err,
		}
	}

	interrupted := false

	startTime := time.Now()

	done := make(chan bool)
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.killChildren()
				interrupted = true
				done <- true
				return

			case <-ticker.C:
				if fileExists(statusFile) && fileSize(statusFile) > 0 {
					done <- true
					return
				}

				select {
				case <-s.done:
					// The shell exited (killed, or retired after a policy
					// change) before reporting a status.
					interrupted = true
					done <- true
					return
				default:
				}

				if timeout > 0 {
					elapsed := time.Since(startTime)
					if elapsed > timeout {
						s.killChildren()
						interrupted = true
						done <- true
						return
					}
				}
			}
		}
	}()

	<-done

	stdout := readFileOrEmpty(stdoutFile)
	stderr := readFileOrEmpty(stderrFile)
	exitCodeStr := readFileOrEmpty(statusFile)
	newCwd := readFileOrEmpty(cwdFile)

	exitCode := 0
	if exitCodeStr != "" {
		fmt.Sscanf(exitCodeStr, "%d", &exitCode)
	} else if interrupted {
		exitCode = 143
		stderr += "\nCommand execution timed out or was interrupted"
	}

	if newCwd != "" {
		s.setCwd(strings.TrimSpace(newCwd))
	}

	return commandResult{
		stdout:      stdout,
		stderr:      stderr,
		exitCode:    exitCode,
		interrupted: interrupted,
	}
}

// killChildren stops the command the shell is running. Unwrapped, it sends
// SIGTERM to the shell's direct children and keeps the shell. When a sandbox
// launcher sits between Pando and the shell (e.g. a bwrap parent), the
// shell's children are not Pando's grandchildren, so the whole process group
// is terminated instead, shell included; GetPersistentShell then re-spawns it
// in the tracked working directory. On Windows only the direct-children path
// exists (and the Job Object reaps the tree on close).
func (s *PersistentShell) killChildren() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}

	if s.launcher && killProcessGroup(s.cmd.Process.Pid, s.done) {
		s.isAlive.Store(false)
		return
	}

	pgrepCmd := exec.Command("pgrep", "-P", fmt.Sprintf("%d", s.cmd.Process.Pid))
	output, err := pgrepCmd.Output()
	if err != nil {
		return
	}

	for pidStr := range strings.SplitSeq(string(output), "\n") {
		if pidStr = strings.TrimSpace(pidStr); pidStr != "" {
			var pid int
			fmt.Sscanf(pidStr, "%d", &pid)
			if pid > 0 {
				proc, err := os.FindProcess(pid)
				if err == nil {
					proc.Signal(syscall.SIGTERM)
				}
			}
		}
	}
}

func (s *PersistentShell) Exec(ctx context.Context, command string, timeoutMs int) (string, string, int, bool, error) {
	if s.spawnErr != nil {
		return "", s.spawnErr.Error(), 1, false, s.spawnErr
	}
	if !s.isAlive.Load() {
		return "", "Shell is not alive", 1, false, errors.New("shell is not alive")
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond

	resultChan := make(chan commandResult, 1)
	select {
	case s.commandQueue <- &commandExecution{
		command:    command,
		timeout:    timeout,
		resultChan: resultChan,
		ctx:        ctx,
	}:
	case <-s.done:
		return "", "Shell is not alive", 1, false, errors.New("shell is not alive")
	}

	var result commandResult
	select {
	case result = <-resultChan:
	case <-s.done:
		// The processor answers queued commands once the shell is gone; the
		// timer only covers a command queued after its final drain.
		select {
		case result = <-resultChan:
		case <-time.After(5 * time.Second):
			result = notAliveResult()
		}
	}
	return result.stdout, result.stderr, result.exitCode, result.interrupted, result.err
}

// Close terminates the shell (and, where the platform allows, its whole
// process tree) immediately.
func (s *PersistentShell) Close() {
	s.closeOnce.Do(func() {
		if s.cmd == nil || s.cmd.Process == nil {
			return
		}
		s.isAlive.Store(false)
		_, _ = s.stdin.Write([]byte("exit\n"))
		if !killProcessGroup(s.cmd.Process.Pid, s.done) {
			_ = s.cmd.Process.Kill()
		}
		s.releaseTree()
	})
}

// retire lets the shell finish the command it may be running and exit: its
// stdin is closed, so the shell reads EOF after the current script. Commands
// queued behind it fail with "shell is not alive".
func (s *PersistentShell) retire() {
	s.closeOnce.Do(func() {
		s.isAlive.Store(false)
		if s.stdin != nil {
			_ = s.stdin.Close()
		}
	})
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func readFileOrEmpty(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
