package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type dockerRuntime struct {
	client *client.Client
	cfg    config.ContainerConfig

	sessions sync.Map // sessionID -> *containerSession
}

func NewDockerRuntime(cfg config.ContainerConfig) (ExecutionRuntime, error) {
	cfg = normalizeContainerConfig(cfg)
	if strings.TrimSpace(cfg.Image) == "" {
		return nil, errors.New("docker runtime requires container.image to be set")
	}

	cli, err := newDockerClient(cfg)
	if err != nil {
		return nil, err
	}

	return &dockerRuntime{
		client: cli,
		cfg:    cfg,
	}, nil
}

func newDockerClient(cfg config.ContainerConfig) (*client.Client, error) {
	opts := []client.Opt{
		client.WithAPIVersionNegotiation(),
	}

	host := strings.TrimSpace(cfg.Socket)
	switch {
	case host != "":
		opts = append(opts, client.WithHost(normalizeDockerHost(host)))
	case os.Getenv("DOCKER_HOST") != "":
		opts = append(opts, client.FromEnv)
	default:
		opts = append(opts, client.WithHost("unix:///var/run/docker.sock"))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	return cli, nil
}

func normalizeDockerHost(host string) string {
	if strings.Contains(host, "://") {
		return host
	}
	if strings.HasPrefix(host, "/") {
		return "unix://" + host
	}
	return "unix://" + host
}

func (d *dockerRuntime) Type() RuntimeType { return RuntimeDocker }

func (d *dockerRuntime) StartSession(ctx context.Context, sessionID string, workDir string) error {
	if _, ok := d.getSession(sessionID); ok {
		return nil
	}

	if err := d.ensureImage(ctx, sessionID); err != nil {
		return err
	}

	containerWorkDir := containerWorkDir(d.cfg, workDir)
	policy, allowedEnv, allowedMounts, err := applySecurityPolicy(d.cfg)
	if err != nil {
		return err
	}
	nanoCPUs, err := parseNanoCPUs(policy.CPULimit)
	if err != nil {
		return err
	}
	memLimit, err := parseMemoryLimit(policy.MemLimit)
	if err != nil {
		return err
	}
	var pidsLimit *int64
	if policy.PidsLimit > 0 {
		pidsLimit = &policy.PidsLimit
	}
	securityOpts := []string(nil)
	if policy.NoNewPrivileges {
		securityOpts = append(securityOpts, "no-new-privileges")
	}

	containerName := sanitizeContainerName("docker", sessionID)
	if err := d.removeContainerIfExists(ctx, containerName); err != nil {
		return err
	}

	resp, err := d.client.ContainerCreate(
		ctx,
		&container.Config{
			Image:      d.cfg.Image,
			WorkingDir: containerWorkDir,
			Env:        allowedEnv,
			User:       policy.User,
			Cmd:        []string{"sh", "-lc", "trap 'exit 0' TERM INT; while :; do sleep 3600; done"},
		},
		&container.HostConfig{
			Binds:           append([]string{fmt.Sprintf("%s:%s", workDir, containerWorkDir)}, allowedMounts...),
			NetworkMode:     container.NetworkMode(policy.Network),
			ReadonlyRootfs:  policy.ReadOnly,
			AutoRemove:      false,
			RestartPolicy:   container.RestartPolicy{Name: "no"},
			Resources:       container.Resources{NanoCPUs: nanoCPUs, Memory: memLimit, PidsLimit: pidsLimit},
			Privileged:      false,
			PublishAllPorts: false,
			SecurityOpt:     securityOpts,
		},
		nil,
		nil,
		containerName,
	)
	if err != nil {
		return fmt.Errorf("create docker container: %w", err)
	}

	if err := d.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start docker container: %w", err)
	}

	d.sessions.Store(sessionID, &containerSession{
		ContainerID: resp.ID,
		WorkDir:     containerWorkDir,
	})
	return nil
}

func (d *dockerRuntime) Exec(ctx context.Context, sessionID string, cmd string, env []string) (ExecResult, error) {
	session, ok := d.getSession(sessionID)
	if !ok {
		return ExecResult{}, fmt.Errorf("no session %q: call StartSession first", sessionID)
	}

	_, allowedEnv, _, err := applySecurityPolicy(d.cfg)
	if err != nil {
		return ExecResult{}, err
	}

	execResp, err := d.client.ContainerExecCreate(ctx, session.ContainerID, container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"sh", "-lc", cmd},
		Env:          append(append([]string{}, allowedEnv...), env...),
		User:         PolicyFromConfig(d.cfg).User,
		WorkingDir:   session.WorkDir,
	})
	if err != nil {
		return ExecResult{}, fmt.Errorf("create docker exec: %w", err)
	}

	attachResp, err := d.client.ContainerExecAttach(ctx, execResp.ID, container.ExecStartOptions{})
	if err != nil {
		return ExecResult{}, fmt.Errorf("attach docker exec: %w", err)
	}
	defer attachResp.Close()

	var stdoutBuf, stderrBuf strings.Builder
	if _, err := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, attachResp.Reader); err != nil && ctx.Err() == nil {
		return ExecResult{}, fmt.Errorf("read docker exec output: %w", err)
	}

	inspectResp, err := d.client.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return ExecResult{}, fmt.Errorf("inspect docker exec: %w", err)
	}

	result := ExecResult{
		Stdout:      stdoutBuf.String(),
		Stderr:      stderrBuf.String(),
		ExitCode:    inspectResp.ExitCode,
		Interrupted: ctx.Err() != nil,
	}
	session.setOutput(result.Stdout, result.Stderr)
	return result, nil
}

func (d *dockerRuntime) StopSession(ctx context.Context, sessionID string) error {
	session, ok := d.getSession(sessionID)
	if !ok {
		return nil
	}

	defer d.sessions.Delete(sessionID)

	if err := d.client.ContainerStop(ctx, session.ContainerID, container.StopOptions{}); err != nil && !client.IsErrNotFound(err) {
		return fmt.Errorf("stop docker container: %w", err)
	}
	if err := d.client.ContainerRemove(ctx, session.ContainerID, container.RemoveOptions{Force: true}); err != nil && !client.IsErrNotFound(err) {
		return fmt.Errorf("remove docker container: %w", err)
	}
	return nil
}

func (d *dockerRuntime) Output(_ context.Context, sessionID string) (string, error) {
	session, ok := d.getSession(sessionID)
	if !ok {
		return "", fmt.Errorf("no session %q", sessionID)
	}
	return session.getOutput(), nil
}

func (d *dockerRuntime) Kill(ctx context.Context, sessionID string) error {
	session, ok := d.getSession(sessionID)
	if !ok {
		return nil
	}
	if err := d.client.ContainerKill(ctx, session.ContainerID, "SIGKILL"); err != nil && !client.IsErrNotFound(err) {
		return fmt.Errorf("kill docker container: %w", err)
	}
	return nil
}

func (d *dockerRuntime) sessionContainerID(sessionID string) string {
	session, ok := d.getSession(sessionID)
	if !ok {
		return ""
	}
	return session.ContainerID
}

func (d *dockerRuntime) getSession(sessionID string) (*containerSession, bool) {
	value, ok := d.sessions.Load(sessionID)
	if !ok {
		return nil, false
	}
	session, ok := value.(*containerSession)
	return session, ok
}

func (d *dockerRuntime) ensureImage(ctx context.Context, sessionID string) error {
	images, err := d.client.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list docker images: %w", err)
	}

	found := false
	for _, img := range images {
		for _, repoTag := range img.RepoTags {
			if matchesImageReference(d.cfg.Image, repoTag) {
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	switch d.cfg.PullPolicy {
	case "always":
		return d.pullImage(ctx, sessionID)
	case "never":
		if !found {
			return fmt.Errorf("docker image %q not found locally and pull_policy=never", d.cfg.Image)
		}
		return nil
	case "", defaultContainerPullPolicy:
		if found {
			return nil
		}
		return d.pullImage(ctx, sessionID)
	default:
		return fmt.Errorf("unsupported docker pull policy %q", d.cfg.PullPolicy)
	}
}

func (d *dockerRuntime) pullImage(ctx context.Context, sessionID string) error {
	DefaultSessionManager().RecordEvent(ContainerEvent{
		SessionID:   sessionID,
		RuntimeType: d.Type(),
		Event:       "pull_started",
		Details:     d.cfg.Image,
	})
	reader, err := d.client.ImagePull(ctx, d.cfg.Image, image.PullOptions{})
	if err != nil {
		DefaultSessionManager().RecordEvent(ContainerEvent{
			SessionID:   sessionID,
			RuntimeType: d.Type(),
			Event:       "error",
			Details:     err.Error(),
		})
		return fmt.Errorf("pull docker image %q: %w", d.cfg.Image, err)
	}
	defer reader.Close()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		DefaultSessionManager().RecordEvent(ContainerEvent{
			SessionID:   sessionID,
			RuntimeType: d.Type(),
			Event:       "error",
			Details:     err.Error(),
		})
		return fmt.Errorf("read docker image pull stream: %w", err)
	}
	DefaultSessionManager().RecordEvent(ContainerEvent{
		SessionID:   sessionID,
		RuntimeType: d.Type(),
		Event:       "pull_done",
		Details:     d.cfg.Image,
	})
	return nil
}

func (d *dockerRuntime) removeContainerIfExists(ctx context.Context, containerName string) error {
	containers, err := d.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list docker containers: %w", err)
	}
	for _, existing := range containers {
		for _, name := range existing.Names {
			if strings.TrimPrefix(name, "/") == containerName {
				if err := d.client.ContainerRemove(ctx, existing.ID, container.RemoveOptions{Force: true}); err != nil {
					return fmt.Errorf("remove existing docker container %s: %w", containerName, err)
				}
				return nil
			}
		}
	}
	return nil
}
