package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
)

type podmanRuntime struct {
	cfg        config.ContainerConfig
	podmanPath string
	socket     string

	sessions sync.Map // sessionID -> *containerSession
}

func NewPodmanRuntime(cfg config.ContainerConfig) (ExecutionRuntime, error) {
	cfg = normalizeContainerConfig(cfg)
	if strings.TrimSpace(cfg.Image) == "" {
		return nil, fmt.Errorf("podman runtime requires container.image to be set")
	}

	path, err := exec.LookPath("podman")
	if err != nil {
		return nil, fmt.Errorf("find podman binary: %w", err)
	}

	return &podmanRuntime{
		cfg:        cfg,
		podmanPath: path,
		socket:     resolvePodmanSocket(cfg),
	}, nil
}

func (p *podmanRuntime) Type() RuntimeType { return RuntimePodman }

func (p *podmanRuntime) StartSession(ctx context.Context, sessionID string, workDir string) error {
	if _, ok := p.getSession(sessionID); ok {
		return nil
	}

	if err := p.ensureImage(ctx, sessionID); err != nil {
		return err
	}

	containerName := sanitizeContainerName("podman", sessionID)
	containerWorkDir := containerWorkDir(p.cfg, workDir)
	policy, allowedEnv, allowedMounts, err := applySecurityPolicy(p.cfg)
	if err != nil {
		return err
	}

	if _, _, _, err := p.runPodman(ctx, append(p.connectionArgs(), "rm", "-f", containerName)...); err != nil {
		// Best-effort cleanup for stale sessions.
	}

	createArgs := append(p.connectionArgs(),
		"create",
		"--name", containerName,
		"--workdir", containerWorkDir,
		"--volume", fmt.Sprintf("%s:%s", workDir, containerWorkDir),
	)
	if policy.Network != "" {
		createArgs = append(createArgs, "--network", policy.Network)
	}
	if policy.ReadOnly {
		createArgs = append(createArgs, "--read-only")
	}
	if policy.User != "" {
		createArgs = append(createArgs, "--user", policy.User)
	}
	if policy.CPULimit != "" {
		createArgs = append(createArgs, "--cpus", policy.CPULimit)
	}
	if policy.MemLimit != "" {
		createArgs = append(createArgs, "--memory", policy.MemLimit)
	}
	if policy.PidsLimit > 0 {
		createArgs = append(createArgs, "--pids-limit", fmt.Sprint(policy.PidsLimit))
	}
	if policy.NoNewPrivileges {
		createArgs = append(createArgs, "--security-opt", "no-new-privileges")
	}
	for _, mount := range allowedMounts {
		createArgs = append(createArgs, "--volume", mount)
	}
	for _, env := range allowedEnv {
		createArgs = append(createArgs, "--env", env)
	}
	createArgs = append(createArgs, p.cfg.Image, "sh", "-lc", "trap 'exit 0' TERM INT; while :; do sleep 3600; done")

	stdout, stderr, exitCode, err := p.runPodman(ctx, createArgs...)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("create podman container: %s", firstNonEmpty(stderr, stdout))
	}

	containerID := strings.TrimSpace(stdout)
	stdout, stderr, exitCode, err = p.runPodman(ctx, append(p.connectionArgs(), "start", containerID)...)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("start podman container: %s", firstNonEmpty(stderr, stdout))
	}

	p.sessions.Store(sessionID, &containerSession{
		ContainerID: containerID,
		WorkDir:     containerWorkDir,
	})
	return nil
}

func (p *podmanRuntime) Exec(ctx context.Context, sessionID string, cmd string, env []string) (ExecResult, error) {
	session, ok := p.getSession(sessionID)
	if !ok {
		return ExecResult{}, fmt.Errorf("no session %q: call StartSession first", sessionID)
	}

	_, allowedEnv, _, err := applySecurityPolicy(p.cfg)
	if err != nil {
		return ExecResult{}, err
	}

	args := append(p.connectionArgs(), "exec", "--workdir", session.WorkDir)
	if user := PolicyFromConfig(p.cfg).User; user != "" {
		args = append(args, "--user", user)
	}
	for _, item := range append(append([]string{}, allowedEnv...), env...) {
		args = append(args, "--env", item)
	}
	args = append(args, session.ContainerID, "sh", "-lc", cmd)

	stdout, stderr, exitCode, err := p.runPodman(ctx, args...)
	if err != nil {
		return ExecResult{}, err
	}

	result := ExecResult{
		Stdout:      stdout,
		Stderr:      stderr,
		ExitCode:    exitCode,
		Interrupted: ctx.Err() != nil,
	}
	session.setOutput(stdout, stderr)
	return result, nil
}

func (p *podmanRuntime) StopSession(ctx context.Context, sessionID string) error {
	session, ok := p.getSession(sessionID)
	if !ok {
		return nil
	}
	defer p.sessions.Delete(sessionID)

	_, stderr, exitCode, err := p.runPodman(ctx, append(p.connectionArgs(), "rm", "-f", session.ContainerID)...)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("remove podman container: %s", stderr)
	}
	return nil
}

func (p *podmanRuntime) Output(_ context.Context, sessionID string) (string, error) {
	session, ok := p.getSession(sessionID)
	if !ok {
		return "", fmt.Errorf("no session %q", sessionID)
	}
	return session.getOutput(), nil
}

func (p *podmanRuntime) Kill(ctx context.Context, sessionID string) error {
	session, ok := p.getSession(sessionID)
	if !ok {
		return nil
	}
	_, stderr, exitCode, err := p.runPodman(ctx, append(p.connectionArgs(), "kill", session.ContainerID)...)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("kill podman container: %s", stderr)
	}
	return nil
}

func (p *podmanRuntime) sessionContainerID(sessionID string) string {
	session, ok := p.getSession(sessionID)
	if !ok {
		return ""
	}
	return session.ContainerID
}

func (p *podmanRuntime) getSession(sessionID string) (*containerSession, bool) {
	value, ok := p.sessions.Load(sessionID)
	if !ok {
		return nil, false
	}
	session, ok := value.(*containerSession)
	return session, ok
}

func (p *podmanRuntime) ensureImage(ctx context.Context, sessionID string) error {
	found := false
	_, _, exitCode, err := p.runPodman(ctx, append(p.connectionArgs(), "image", "exists", p.cfg.Image)...)
	if err == nil {
		found = exitCode == 0
	}

	switch p.cfg.PullPolicy {
	case "always":
		return p.pullImage(ctx, sessionID)
	case "never":
		if !found {
			return fmt.Errorf("podman image %q not found locally and pull_policy=never", p.cfg.Image)
		}
		return nil
	case "", defaultContainerPullPolicy:
		if found {
			return nil
		}
		return p.pullImage(ctx, sessionID)
	default:
		return fmt.Errorf("unsupported podman pull policy %q", p.cfg.PullPolicy)
	}
}

func (p *podmanRuntime) pullImage(ctx context.Context, sessionID string) error {
	DefaultSessionManager().RecordEvent(ContainerEvent{
		SessionID:   sessionID,
		RuntimeType: p.Type(),
		Event:       "pull_started",
		Details:     p.cfg.Image,
	})
	stdout, stderr, exitCode, err := p.runPodman(ctx, append(p.connectionArgs(), "pull", p.cfg.Image)...)
	if err != nil {
		DefaultSessionManager().RecordEvent(ContainerEvent{
			SessionID:   sessionID,
			RuntimeType: p.Type(),
			Event:       "error",
			Details:     err.Error(),
		})
		return err
	}
	if exitCode != 0 {
		DefaultSessionManager().RecordEvent(ContainerEvent{
			SessionID:   sessionID,
			RuntimeType: p.Type(),
			Event:       "error",
			Details:     firstNonEmpty(stderr, stdout),
		})
		return fmt.Errorf("pull podman image: %s", firstNonEmpty(stderr, stdout))
	}
	DefaultSessionManager().RecordEvent(ContainerEvent{
		SessionID:   sessionID,
		RuntimeType: p.Type(),
		Event:       "pull_done",
		Details:     p.cfg.Image,
	})
	return nil
}

func (p *podmanRuntime) runPodman(ctx context.Context, args ...string) (string, string, int, error) {
	// TODO: Switch to Podman Go bindings when go.podman.io/podman/v5/pkg/bindings
	// is added to go.mod. The CLI fallback keeps Phase 2 self-contained.
	cmd := exec.CommandContext(ctx, p.podmanPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() != nil {
			exitCode = 143
		} else {
			return stdout.String(), stderr.String(), exitCode, fmt.Errorf("run podman %s: %w", strings.Join(args, " "), err)
		}
	}

	return stdout.String(), stderr.String(), exitCode, nil
}

func (p *podmanRuntime) connectionArgs() []string {
	if strings.TrimSpace(p.socket) == "" {
		return nil
	}
	return []string{"--url", normalizePodmanSocket(p.socket)}
}

func resolvePodmanSocket(cfg config.ContainerConfig) string {
	if strings.TrimSpace(cfg.Socket) != "" {
		return cfg.Socket
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); xdg != "" {
		candidate := xdg + "/podman/podman.sock"
		if fileExists(candidate) {
			return candidate
		}
	}
	if fileExists("/run/podman/podman.sock") {
		return "/run/podman/podman.sock"
	}
	return ""
}

func normalizePodmanSocket(socket string) string {
	if strings.Contains(socket, "://") {
		return socket
	}
	if strings.HasPrefix(socket, "/") {
		return "unix://" + socket
	}
	return "unix://" + socket
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
