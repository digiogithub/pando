package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	embeddedrt "github.com/digiogithub/pando/internal/runtime/embedded"
	"github.com/google/go-containerregistry/pkg/authn"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// EmbeddedRuntime executes commands against an unpacked OCI image without
// requiring a host-side Docker or Podman daemon. The current MVP uses a
// fallback strategy that prepares a rootfs and then executes commands via the
// host shell with the unpacked image exposed through PATH and bind-like
// symlinks. Namespace-based isolation can be added later without changing the
// public runtime interface.
type EmbeddedRuntime struct {
	cfg      config.ContainerConfig
	store    *embeddedrt.ImageStore
	registry *embeddedrt.RegistryClient
	sessions sync.Map // sessionID -> *embeddedSession
	shell    string
}

type embeddedSession struct {
	containerSession

	SessionDir string
	RootFSDir  string
	BaseEnv    []string

	execMu sync.Mutex
	cmd    *exec.Cmd
}

type embeddedFallbackRuntime struct {
	cfg config.ContainerConfig

	mu      sync.Mutex
	current ExecutionRuntime
}

// NewEmbeddedRuntime returns a runtime that prefers the built-in OCI executor
// and falls back to Docker, Podman, then host execution if embedded session
// startup fails.
func NewEmbeddedRuntime(cfg config.ContainerConfig) (ExecutionRuntime, error) {
	cfg = normalizeContainerConfig(cfg)
	if strings.TrimSpace(cfg.Image) == "" {
		return nil, errors.New("embedded runtime requires container.image to be set")
	}
	return &embeddedFallbackRuntime{cfg: cfg}, nil
}

func (r *embeddedFallbackRuntime) Type() RuntimeType {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current != nil {
		return r.current.Type()
	}
	return RuntimeEmbedded
}

func (r *embeddedFallbackRuntime) StartSession(ctx context.Context, sessionID string, workDir string) error {
	current := r.runtime()
	if current != nil {
		return current.StartSession(ctx, sessionID, workDir)
	}

	primary, primaryErr := newEmbeddedRuntimeCore(r.cfg)
	if primaryErr == nil {
		if err := primary.StartSession(ctx, sessionID, workDir); err == nil {
			r.setRuntime(primary)
			return nil
		} else {
			primaryErr = err
		}
	}

	fallback, fallbackErr := newEmbeddedFallback(r.cfg)
	if fallbackErr == nil {
		if err := fallback.StartSession(ctx, sessionID, workDir); err == nil {
			r.setRuntime(fallback)
			return nil
		} else {
			fallbackErr = err
		}
	}

	if primaryErr != nil && fallbackErr != nil {
		return fmt.Errorf("start embedded runtime: %w; fallback runtime: %w", primaryErr, fallbackErr)
	}
	if primaryErr != nil {
		return primaryErr
	}
	return fallbackErr
}

func (r *embeddedFallbackRuntime) Exec(ctx context.Context, sessionID, cmd string, env []string) (ExecResult, error) {
	current := r.runtime()
	if current == nil {
		return ExecResult{}, fmt.Errorf("no session %q: call StartSession first", sessionID)
	}
	return current.Exec(ctx, sessionID, cmd, env)
}

func (r *embeddedFallbackRuntime) StopSession(ctx context.Context, sessionID string) error {
	current := r.runtime()
	if current == nil {
		return nil
	}
	return current.StopSession(ctx, sessionID)
}

func (r *embeddedFallbackRuntime) Output(ctx context.Context, sessionID string) (string, error) {
	current := r.runtime()
	if current == nil {
		return "", fmt.Errorf("no session %q", sessionID)
	}
	return current.Output(ctx, sessionID)
}

func (r *embeddedFallbackRuntime) Kill(ctx context.Context, sessionID string) error {
	current := r.runtime()
	if current == nil {
		return nil
	}
	return current.Kill(ctx, sessionID)
}

func (r *embeddedFallbackRuntime) sessionContainerID(sessionID string) string {
	current := r.runtime()
	provider, ok := current.(sessionContainerIDProvider)
	if !ok {
		return ""
	}
	return provider.sessionContainerID(sessionID)
}

func (r *embeddedFallbackRuntime) runtime() ExecutionRuntime {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

func (r *embeddedFallbackRuntime) setRuntime(runtime ExecutionRuntime) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current == nil {
		r.current = runtime
	}
}

func newEmbeddedRuntimeCore(cfg config.ContainerConfig) (*EmbeddedRuntime, error) {
	cacheDir, err := embeddedrt.ResolveCacheDir(cfg.EmbeddedCacheDir)
	if err != nil {
		return nil, err
	}

	shellPath, err := exec.LookPath("sh")
	if err != nil {
		return nil, fmt.Errorf("find host shell: %w", err)
	}

	return &EmbeddedRuntime{
		cfg:      cfg,
		store:    &embeddedrt.ImageStore{Root: cacheDir},
		registry: &embeddedrt.RegistryClient{CacheDir: cacheDir, Auth: authn.DefaultKeychain},
		shell:    shellPath,
	}, nil
}

func newEmbeddedFallback(cfg config.ContainerConfig) (ExecutionRuntime, error) {
	if runtime, err := NewDockerRuntime(cfg); err == nil {
		return runtime, nil
	}
	if runtime, err := NewPodmanRuntime(cfg); err == nil {
		return runtime, nil
	}
	return NewHostRuntime(), nil
}

func (e *EmbeddedRuntime) Type() RuntimeType { return RuntimeEmbedded }

func (e *EmbeddedRuntime) StartSession(ctx context.Context, sessionID, workDir string) error {
	if _, ok := e.getSession(sessionID); ok {
		return nil
	}

	img, cfgFile, err := e.resolveImage(ctx)
	if err != nil {
		return err
	}

	policy, allowedEnv, allowedMounts, err := applySecurityPolicy(e.cfg)
	if err != nil {
		return err
	}

	sessionDir, err := os.MkdirTemp("", sanitizeContainerName("embedded", sessionID)+"-")
	if err != nil {
		return fmt.Errorf("create embedded session directory: %w", err)
	}

	finished := false
	defer func() {
		if !finished {
			_ = os.RemoveAll(sessionDir)
		}
	}()

	rootfsDir := filepath.Join(sessionDir, "rootfs")
	if err := embeddedrt.Unpack(ctx, img, rootfsDir); err != nil {
		return fmt.Errorf("unpack embedded rootfs: %w", err)
	}

	containerWD := embeddedWorkDir(e.cfg, cfgFile, workDir)
	mountedWorkDir, err := linkIntoRootfs(rootfsDir, containerWD, workDir)
	if err != nil {
		return err
	}
	for _, mount := range allowedMounts {
		if err := mountIntoRootfs(rootfsDir, mount); err != nil {
			return err
		}
	}

	session := &embeddedSession{
		containerSession: containerSession{
			ContainerID: rootfsDir,
			WorkDir:     mountedWorkDir,
		},
		SessionDir: sessionDir,
		RootFSDir:  rootfsDir,
		BaseEnv:    buildEmbeddedEnv(policy, cfgFile, rootfsDir, mountedWorkDir, allowedEnv),
	}
	e.sessions.Store(sessionID, session)

	finished = true
	return nil
}

func (e *EmbeddedRuntime) Exec(ctx context.Context, sessionID, command string, env []string) (ExecResult, error) {
	session, ok := e.getSession(sessionID)
	if !ok {
		return ExecResult{}, fmt.Errorf("no session %q: call StartSession first", sessionID)
	}

	cmd := exec.CommandContext(ctx, e.shell, "-lc", command)
	cmd.Dir = session.WorkDir
	cmd.Env = mergeEnv(session.BaseEnv, env)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	session.execMu.Lock()
	session.cmd = cmd
	session.execMu.Unlock()

	err := cmd.Run()

	session.execMu.Lock()
	session.cmd = nil
	session.execMu.Unlock()

	result := ExecResult{
		Stdout:      stdoutBuf.String(),
		Stderr:      stderrBuf.String(),
		Interrupted: ctx.Err() != nil,
	}
	if err == nil {
		result.ExitCode = 0
	} else {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else if ctx.Err() != nil {
			result.ExitCode = -1
		} else {
			return ExecResult{}, fmt.Errorf("execute embedded command: %w", err)
		}
	}

	session.setOutput(result.Stdout, result.Stderr)
	return result, nil
}

func (e *EmbeddedRuntime) StopSession(ctx context.Context, sessionID string) error {
	session, ok := e.getSession(sessionID)
	if !ok {
		return nil
	}
	defer e.sessions.Delete(sessionID)

	if err := e.killSessionProcess(ctx, session); err != nil {
		return err
	}
	if err := os.RemoveAll(session.SessionDir); err != nil {
		return fmt.Errorf("remove embedded session directory %q: %w", session.SessionDir, err)
	}
	return nil
}

func (e *EmbeddedRuntime) Output(_ context.Context, sessionID string) (string, error) {
	session, ok := e.getSession(sessionID)
	if !ok {
		return "", fmt.Errorf("no session %q", sessionID)
	}
	return session.getOutput(), nil
}

func (e *EmbeddedRuntime) Kill(ctx context.Context, sessionID string) error {
	session, ok := e.getSession(sessionID)
	if !ok {
		return nil
	}
	return e.killSessionProcess(ctx, session)
}

func (e *EmbeddedRuntime) sessionContainerID(sessionID string) string {
	session, ok := e.getSession(sessionID)
	if !ok {
		return ""
	}
	return session.ContainerID
}

func (e *EmbeddedRuntime) getSession(sessionID string) (*embeddedSession, bool) {
	value, ok := e.sessions.Load(sessionID)
	if !ok {
		return nil, false
	}
	session, ok := value.(*embeddedSession)
	return session, ok
}

func (e *EmbeddedRuntime) killSessionProcess(_ context.Context, session *embeddedSession) error {
	session.execMu.Lock()
	defer session.execMu.Unlock()
	if session.cmd == nil || session.cmd.Process == nil {
		return nil
	}
	if err := session.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill embedded process: %w", err)
	}
	return nil
}

func (e *EmbeddedRuntime) resolveImage(ctx context.Context) (v1.Image, *v1.ConfigFile, error) {
	ref, err := embeddedrt.ParseImageRef(e.cfg.Image)
	if err != nil {
		return nil, nil, err
	}

	refName := ref.String()
	var img v1.Image
	switch e.cfg.PullPolicy {
	case "always":
		img, err = e.registry.Pull(ctx, ref)
	case "never":
		img, err = e.store.Get(refName)
	default:
		img, err = e.store.Get(refName)
		if errors.Is(err, embeddedrt.ErrImageNotCached) {
			img, err = e.registry.Pull(ctx, ref)
		}
	}
	if err != nil {
		return nil, nil, err
	}

	cfgFile, err := img.ConfigFile()
	if err != nil {
		return nil, nil, fmt.Errorf("read embedded image config for %q: %w", refName, err)
	}
	return img, cfgFile, nil
}

func embeddedWorkDir(cfg config.ContainerConfig, cfgFile *v1.ConfigFile, hostWorkDir string) string {
	if wd := strings.TrimSpace(cfg.WorkDir); wd != "" {
		return wd
	}
	if cfgFile != nil {
		if wd := strings.TrimSpace(cfgFile.Config.WorkingDir); wd != "" {
			return wd
		}
	}
	return hostWorkDir
}

func linkIntoRootfs(rootfsDir, targetPath, source string) (string, error) {
	if _, err := os.Stat(source); err != nil {
		return "", fmt.Errorf("stat mount source %q: %w", source, err)
	}

	dest, err := embeddedPath(rootfsDir, targetPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("create parent directory for %q: %w", dest, err)
	}
	if err := os.RemoveAll(dest); err != nil {
		return "", fmt.Errorf("replace rootfs mount %q: %w", dest, err)
	}
	if err := os.Symlink(source, dest); err != nil {
		return "", fmt.Errorf("create rootfs mount %q -> %q: %w", dest, source, err)
	}
	return dest, nil
}

func mountIntoRootfs(rootfsDir, mount string) error {
	source, target, err := parseMountSpec(mount)
	if err != nil {
		return err
	}
	_, err = linkIntoRootfs(rootfsDir, target, source)
	return err
}

func parseMountSpec(mount string) (string, string, error) {
	parts := strings.Split(mount, ":")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid mount entry %q", mount)
	}

	source := strings.TrimSpace(parts[0])
	target := strings.TrimSpace(parts[1])
	if source == "" || target == "" {
		return "", "", fmt.Errorf("invalid mount entry %q", mount)
	}
	return source, target, nil
}

func embeddedPath(rootfsDir, containerPath string) (string, error) {
	clean := filepath.Clean(containerPath)
	if clean == "." || clean == string(os.PathSeparator) || clean == "" {
		return rootfsDir, nil
	}
	if filepath.IsAbs(clean) {
		clean = strings.TrimPrefix(clean, string(os.PathSeparator))
	}
	target := filepath.Join(rootfsDir, clean)
	cleanRoot := filepath.Clean(rootfsDir)
	if target != cleanRoot && !strings.HasPrefix(target, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("embedded path %q escapes rootfs", containerPath)
	}
	return target, nil
}

func buildEmbeddedEnv(policy SecurityPolicy, cfgFile *v1.ConfigFile, rootfsDir, workDir string, allowedEnv []string) []string {
	rootfsPath := buildRootfsPath(rootfsDir)
	base := []string{
		"HOME=" + filepath.Join(rootfsDir, "root"),
		"PWD=" + workDir,
		"PATH=" + rootfsPath,
		"PANDO_EMBEDDED_ROOTFS=" + rootfsDir,
		"PANDO_EMBEDDED_ISOLATION=none",
		"PANDO_CONTAINER_WORKDIR=" + workDir,
		"PANDO_CONTAINER_NETWORK=" + policy.Network,
		"PANDO_CONTAINER_READ_ONLY=" + strconv.FormatBool(policy.ReadOnly),
	}
	if policy.User != "" {
		base = append(base, "USER="+policy.User)
	}
	if cfgFile != nil {
		base = append(base, cfgFile.Config.Env...)
	}
	return mergeEnv(base, allowedEnv)
}

func buildRootfsPath(rootfsDir string) string {
	paths := []string{
		filepath.Join(rootfsDir, "usr", "local", "sbin"),
		filepath.Join(rootfsDir, "usr", "local", "bin"),
		filepath.Join(rootfsDir, "usr", "sbin"),
		filepath.Join(rootfsDir, "usr", "bin"),
		filepath.Join(rootfsDir, "sbin"),
		filepath.Join(rootfsDir, "bin"),
	}

	filtered := make([]string, 0, len(paths)+1)
	for _, item := range paths {
		if _, err := os.Stat(item); err == nil {
			filtered = append(filtered, item)
		}
	}
	if hostPath := strings.TrimSpace(os.Getenv("PATH")); hostPath != "" {
		filtered = append(filtered, hostPath)
	}
	return strings.Join(filtered, string(os.PathListSeparator))
}

func mergeEnv(parts ...[]string) []string {
	indexByName := make(map[string]int)
	out := make([]string, 0)
	for _, part := range parts {
		for _, item := range part {
			name, _, ok := strings.Cut(item, "=")
			if !ok {
				continue
			}
			if idx, exists := indexByName[name]; exists {
				out[idx] = item
				continue
			}
			indexByName[name] = len(out)
			out = append(out, item)
		}
	}
	return out
}
