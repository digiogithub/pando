package project

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/instanceregistry"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/procgroup"
	"github.com/digiogithub/pando/internal/pubsub"
)

const (
	webHealthProbeInterval = 200 * time.Millisecond
	webStartupTimeout      = 20 * time.Second
	webOutputBufferLimit   = 8 * 1024
)

type WebInstanceState string

const (
	WebStateStarting WebInstanceState = "starting"
	WebStateRunning  WebInstanceState = "running"
	WebStateError    WebInstanceState = "error"
	WebStateStopped  WebInstanceState = "stopped"
)

type WebInstance struct {
	Project   Project
	cmd       *exec.Cmd
	Port      int
	PID       int
	Token     string
	StartedAt time.Time
	errCh     chan error
	cancel    context.CancelFunc
	State     WebInstanceState
	stdout    *tailBuffer
	stderr    *tailBuffer

	mu       sync.RWMutex
	done     chan struct{}
	exitErr  error
	stopping bool
	adopted  bool
}

func (w *WebInstance) setState(state WebInstanceState) {
	w.mu.Lock()
	w.State = state
	w.mu.Unlock()
}

func (w *WebInstance) state() WebInstanceState {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.State
}

func (w *WebInstance) markStopping() {
	w.mu.Lock()
	w.stopping = true
	w.mu.Unlock()
}

func (w *WebInstance) isStopping() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.stopping
}

func (w *WebInstance) recordExit(err error, state WebInstanceState) {
	w.mu.Lock()
	w.exitErr = err
	w.State = state
	w.mu.Unlock()
	close(w.done)
}

func (w *WebInstance) exitError() error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.exitErr
}

func (w *WebInstance) stderrText() string {
	if w.stderr == nil {
		return ""
	}
	return strings.TrimSpace(w.stderr.String())
}

func (w *WebInstance) stdoutText() string {
	if w.stdout == nil {
		return ""
	}
	return strings.TrimSpace(w.stdout.String())
}

type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func newTailBuffer(limit int) *tailBuffer {
	return &tailBuffer{limit: limit}
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.data = append(b.data, p...)
	if overflow := len(b.data) - b.limit; overflow > 0 {
		b.data = append([]byte(nil), b.data[overflow:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.data...))
}

type webProcessStart struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	stdout *tailBuffer
	stderr *tailBuffer
}

var (
	spawnWebProcess = defaultSpawnWebProcess
	probeWebHealth  = defaultProbeWebHealth
)

func defaultSpawnWebProcess(_ context.Context, pandoBin, parentInstanceID string, proj Project, port int) (*webProcessStart, error) {
	cmd := exec.Command(pandoBin, "serve",
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"--cwd", proj.Path,
	)
	cmd.Dir = proj.Path
	cmd.Env = append(os.Environ(),
		"NO_COLOR=1",
		"PANDO_PARENT_INSTANCE="+parentInstanceID,
		"PANDO_PROJECT_ID="+proj.ID,
	)
	procgroup.Ensure(cmd)

	if err := checkDirAccessible(proj.Path); err != nil {
		return nil, fmt.Errorf("project directory not accessible: %w", err)
	}

	stdout := newTailBuffer(webOutputBufferLimit)
	stderr := newTailBuffer(webOutputBufferLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	return &webProcessStart{
		cmd:    cmd,
		cancel: func() {},
		stdout: stdout,
		stderr: stderr,
	}, nil
}

func defaultProbeWebHealth(ctx context.Context, port int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://127.0.0.1:%d/health", port), nil)
	if err != nil {
		return err
	}
	resp, err := insecureLoopbackHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health probe returned status %d", resp.StatusCode)
	}
	return nil
}

func insecureLoopbackHTTPClient() *http.Client {
	// TODO(PANDO-US-0104): pin the child certificate instead of skipping TLS
	// verification for the loopback health probe.
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
}

func (m *Manager) OpenWeb(ctx context.Context, projectID string) (*WebInstance, error) {
	proj, err := m.service.Get(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("project manager: get project %s: %w", projectID, err)
	}

	resolvedPath, err := resolvePath(proj.Path)
	if err != nil {
		return nil, fmt.Errorf("project manager: resolve path %s: %w", proj.Path, err)
	}
	if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("project path does not exist: %s", resolvedPath)
	}
	if !config.HasConfigFileAt(resolvedPath) {
		return nil, ErrProjectNeedsInit
	}

	m.mu.Lock()
	if inst, ok := m.webInstances[projectID]; ok && inst != nil {
		if state := inst.state(); state == WebStateStopped || state == WebStateError {
			delete(m.webInstances, projectID)
		} else {
			m.mu.Unlock()
			if err := m.waitForWebStartup(ctx, projectID, inst); err != nil {
				if errors.Is(err, ErrChildStartupTimeout) {
					return nil, err
				}
				return nil, fmt.Errorf("project manager: reuse web instance for %s: %w", proj.Path, err)
			}
			_ = m.service.TouchLastOpened(ctx, projectID)
			return inst, nil
		}
	}
	m.mu.Unlock()

	preferredPort := proj.WebPort
	if preferredPort == 0 {
		preferredPort = defaultWebPort
	}
	port, err := chooseAvailablePort("127.0.0.1", preferredPort)
	if err != nil {
		return nil, fmt.Errorf("project manager: choose web port for %s: %w", proj.Path, err)
	}

	started, err := spawnWebProcess(ctx, m.pandoBin, m.parentInstanceID, *proj, port)
	if err != nil {
		return nil, fmt.Errorf("project manager: spawn web child for %s: %w", proj.Path, err)
	}

	inst := &WebInstance{
		Project:   *proj,
		cmd:       started.cmd,
		Port:      port,
		PID:       started.cmd.Process.Pid,
		StartedAt: time.Now(),
		errCh:     make(chan error, 1),
		cancel:    started.cancel,
		State:     WebStateStarting,
		stdout:    started.stdout,
		stderr:    started.stderr,
		done:      make(chan struct{}),
	}

	m.mu.Lock()
	if existing, ok := m.webInstances[projectID]; ok && existing != nil {
		m.mu.Unlock()
		terminateWebProcess(inst, syscall.SIGTERM)
		return existing, m.waitForWebStartup(ctx, projectID, existing)
	}
	m.webInstances[projectID] = inst
	m.mu.Unlock()

	go m.monitorOwnedWebInstance(projectID, inst)

	if err := m.waitForWebStartup(ctx, projectID, inst); err != nil {
		if errors.Is(err, ErrChildStartupTimeout) {
			terminateWebProcess(inst, syscall.SIGTERM)
		}
		return nil, err
	}

	_ = m.service.TouchLastOpened(ctx, projectID)
	return inst, nil
}

func (m *Manager) CloseWeb(ctx context.Context, projectID string) error {
	inst, ok := m.takeWebInstance(projectID)
	if !ok || inst == nil {
		if m.service != nil {
			_ = m.service.UpdateWebRuntime(ctx, projectID, 0, 0)
		}
		return nil
	}

	inst.markStopping()
	terminateWebProcess(inst, syscall.SIGTERM)

	select {
	case <-inst.done:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		terminateWebProcess(inst, syscall.SIGKILL)
	}

	return nil
}

func (m *Manager) WebInstances() map[string]*WebInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]*WebInstance, len(m.webInstances))
	for id, inst := range m.webInstances {
		out[id] = inst
	}
	return out
}

func (m *Manager) WebInstance(projectID string) (*WebInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inst, ok := m.webInstances[projectID]
	return inst, ok
}

func (m *Manager) waitForWebStartup(ctx context.Context, projectID string, inst *WebInstance) error {
	if inst == nil {
		return ErrChildStartupTimeout
	}
	if inst.state() == WebStateRunning {
		return nil
	}

	deadline := time.NewTimer(webStartupTimeout)
	defer deadline.Stop()

	ticker := time.NewTicker(webHealthProbeInterval)
	defer ticker.Stop()

	for {
		probeCtx, cancel := context.WithTimeout(ctx, webHealthProbeInterval)
		err := probeWebHealth(probeCtx, inst.Port)
		cancel()
		if err == nil {
			inst.setState(WebStateRunning)
			if m.service != nil {
				_ = m.service.UpdateWebRuntime(context.Background(), projectID, inst.PID, inst.Port)
			}
			if m.broker != nil {
				m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
					Type:      EvWebStarted,
					ProjectID: projectID,
					Port:      inst.Port,
				})
			}
			return nil
		}

		select {
		case <-inst.done:
			exitErr := inst.exitError()
			if exitErr == nil {
				return fmt.Errorf("web instance for %s stopped before health probe completed", projectID)
			}
			return fmt.Errorf("web instance for %s exited before health probe completed: %w", projectID, exitErr)
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return ErrChildStartupTimeout
		case <-ticker.C:
		}
	}
}

func (m *Manager) monitorOwnedWebInstance(projectID string, inst *WebInstance) {
	waitErr := inst.cmd.Wait()
	select {
	case inst.errCh <- waitErr:
	default:
	}

	state := WebStateStopped
	eventType := EvWebStopped
	eventErr := ""
	if waitErr != nil && !inst.isStopping() {
		state = WebStateError
		eventType = EvWebError
		eventErr = webInstanceError(inst, waitErr)
	}

	inst.recordExit(waitErr, state)
	inst.cancel()
	if m.service != nil {
		_ = m.service.UpdateWebRuntime(context.Background(), projectID, 0, 0)
	}
	m.removeWebInstance(projectID, inst)

	if m.broker != nil {
		m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
			Type:      eventType,
			ProjectID: projectID,
			Port:      inst.Port,
			Error:     eventErr,
		})
	}
}

func (m *Manager) monitorAdoptedWebInstance(projectID string, inst *WebInstance) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			inst.markStopping()
			inst.recordExit(nil, WebStateStopped)
			inst.cancel()
			return
		case <-inst.done:
			inst.cancel()
			return
		case <-ticker.C:
			if inst.PID > 0 && !pidIsAlive(inst.PID) {
				state := WebStateStopped
				eventType := EvWebStopped
				eventErr := ""
				if !inst.isStopping() {
					state = WebStateError
					eventType = EvWebError
					eventErr = fmt.Sprintf("web child pid %d is no longer running", inst.PID)
				}
				inst.recordExit(nil, state)
				if m.service != nil {
					_ = m.service.UpdateWebRuntime(context.Background(), projectID, 0, 0)
				}
				m.removeWebInstance(projectID, inst)
				if m.broker != nil {
					m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
						Type:      eventType,
						ProjectID: projectID,
						Port:      inst.Port,
						Error:     eventErr,
					})
				}
				return
			}
		}
	}
}

func (m *Manager) takeWebInstance(projectID string) (*WebInstance, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	inst, ok := m.webInstances[projectID]
	if ok {
		delete(m.webInstances, projectID)
	}
	return inst, ok
}

func (m *Manager) removeWebInstance(projectID string, inst *WebInstance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.webInstances[projectID]; ok && current == inst {
		delete(m.webInstances, projectID)
	}
}

func (m *Manager) adoptExistingWebInstances(ctx context.Context) {
	projects, err := m.service.List(ctx)
	if err != nil {
		logging.Warn("project manager: list projects for web adoption failed", "error", err)
		return
	}

	entries, err := m.registry.List()
	if err != nil {
		logging.Warn("project manager: list instance registry for web adoption failed", "error", err)
		return
	}

	byPath := make(map[string][]*instanceregistry.Entry)
	for _, entry := range entries {
		if entry == nil || entry.Mode != instanceregistry.ModeWebUI || entry.Path == "" || entry.PID <= 0 {
			continue
		}
		byPath[entry.Path] = append(byPath[entry.Path], entry)
	}

	for _, proj := range projects {
		projectEntries := byPath[proj.Path]
		var adopted bool
		for _, entry := range projectEntries {
			port := proj.WebPort
			if port == 0 {
				continue
			}
			// TODO(PANDO-US-0104): read the adopted web port from instanceregistry.Entry
			// once WebPort and ParentInstanceID are persisted there.
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := probeWebHealth(probeCtx, port)
			cancel()
			if err != nil {
				continue
			}

			inst := &WebInstance{
				Project:   proj,
				Port:      port,
				PID:       entry.PID,
				StartedAt: entry.StartedAt,
				errCh:     make(chan error, 1),
				cancel:    func() {},
				State:     WebStateRunning,
				stdout:    newTailBuffer(webOutputBufferLimit),
				stderr:    newTailBuffer(webOutputBufferLimit),
				done:      make(chan struct{}),
				adopted:   true,
			}
			m.mu.Lock()
			m.webInstances[proj.ID] = inst
			m.mu.Unlock()
			go m.monitorAdoptedWebInstance(proj.ID, inst)
			adopted = true
			break
		}
		if !adopted && (proj.WebPID != 0 || proj.WebPort != 0) {
			if err := m.service.UpdateWebRuntime(ctx, proj.ID, 0, 0); err != nil {
				logging.Warn("project manager: clear stale web runtime failed", "project", proj.ID, "error", err)
			}
		}
	}
}

func webInstanceError(inst *WebInstance, waitErr error) string {
	parts := []string{}
	if waitErr != nil {
		parts = append(parts, waitErr.Error())
	}
	if stderr := inst.stderrText(); stderr != "" {
		parts = append(parts, stderr)
	} else if stdout := inst.stdoutText(); stdout != "" {
		parts = append(parts, stdout)
	}
	return strings.Join(parts, "\n")
}

func terminateWebProcess(inst *WebInstance, sig syscall.Signal) {
	if inst == nil {
		return
	}
	pid := inst.PID
	if pid == 0 && inst.cmd != nil && inst.cmd.Process != nil {
		pid = inst.cmd.Process.Pid
	}
	if pid > 0 && procgroup.Kill(pid, sig) {
		return
	}
	if inst.cmd != nil && inst.cmd.Process != nil {
		if sig == syscall.SIGKILL {
			_ = inst.cmd.Process.Kill()
			return
		}
		_ = inst.cmd.Process.Signal(sig)
		return
	}
	if pid > 0 {
		if proc, err := os.FindProcess(pid); err == nil {
			if sig == syscall.SIGKILL {
				_ = proc.Kill()
			} else {
				_ = proc.Signal(sig)
			}
		}
	}
}
