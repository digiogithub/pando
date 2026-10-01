package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	"github.com/digiogithub/pando/internal/tlsutil"
)

const (
	webHealthProbeInterval = 200 * time.Millisecond
	webStartupTimeout      = 20 * time.Second
	webOutputBufferLimit   = 8 * 1024
	webLoopbackHost        = "127.0.0.1"
	webTokenPath           = "/api/v1/token"
)

// WebInstanceState describes the lifecycle state of a background WebUI child.
type WebInstanceState string

const (
	// WebStateStarting means the child process exists but has not finished the
	// pinned TLS and token handshake yet.
	WebStateStarting WebInstanceState = "starting"
	// WebStateRunning means the child answered the pinned health probe and the
	// parent fetched its API token successfully.
	WebStateRunning WebInstanceState = "running"
	// WebStateError means the child crashed or failed startup.
	WebStateError WebInstanceState = "error"
	// WebStateStopped means the child exited cleanly or was shut down.
	WebStateStopped WebInstanceState = "stopped"
)

// WebInstance represents a background `pando serve` child owned or adopted by
// the project manager.
type WebInstance struct {
	Project   Project
	StartedAt time.Time

	cmd    *exec.Cmd
	errCh  chan error
	cancel context.CancelFunc
	stdout *tailBuffer
	stderr *tailBuffer

	mu       sync.RWMutex
	state    WebInstanceState
	port     int
	pid      int
	token    string
	done     chan struct{}
	exitErr  error
	stopping bool
	adopted  bool
}

// WebInstanceSnapshot is a race-free copy of a WebInstance's live runtime data.
type WebInstanceSnapshot struct {
	Project   Project
	Port      int
	PID       int
	StartedAt time.Time
	State     WebInstanceState
	Adopted   bool
}

// Snapshot returns a race-free copy of the instance state for callers that need
// stable port, pid, and lifecycle fields together.
func (w *WebInstance) Snapshot() WebInstanceSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return WebInstanceSnapshot{
		Project:   w.Project,
		Port:      w.port,
		PID:       w.pid,
		StartedAt: w.StartedAt,
		State:     w.state,
		Adopted:   w.adopted,
	}
}

// BaseURL returns the pinned loopback origin of the child WebUI server.
func (w *WebInstance) BaseURL() string {
	return loopbackWebBaseURL(w.Port())
}

// APIToken returns the in-memory API token learned from the child's token
// endpoint. The token is never persisted.
func (w *WebInstance) APIToken() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.token
}

// Port returns the loopback HTTPS port of the child.
func (w *WebInstance) Port() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.port
}

// PID returns the operating-system process id of the child when known.
func (w *WebInstance) PID() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.pid
}

// State returns the current lifecycle state of the child.
func (w *WebInstance) State() WebInstanceState {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.state
}

func (w *WebInstance) setState(state WebInstanceState) {
	w.mu.Lock()
	w.state = state
	w.mu.Unlock()
}

func (w *WebInstance) setToken(token string) {
	w.mu.Lock()
	w.token = token
	w.mu.Unlock()
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
	w.state = state
	w.token = ""
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

type webProcessConfig struct {
	ParentInstanceID string
	TLSCertFile      string
	TLSKeyFile       string
}

type webTokenResponse struct {
	Token string `json:"token"`
}

var (
	spawnWebProcess = defaultSpawnWebProcess
	probeWebHealth  = defaultProbeWebHealth
	fetchWebToken   = defaultFetchWebToken
)

func defaultSpawnWebProcess(_ context.Context, pandoBin string, cfg webProcessConfig, proj Project, port int) (*webProcessStart, error) {
	cmd := exec.Command(pandoBin, "serve",
		"--host", webLoopbackHost,
		"--port", strconv.Itoa(port),
		"--cwd", proj.Path,
		"--tls-cert", cfg.TLSCertFile,
		"--tls-key", cfg.TLSKeyFile,
	)
	cmd.Dir = proj.Path
	cmd.Env = append(os.Environ(),
		"NO_COLOR=1",
		"PANDO_PROJECT_ID="+proj.ID,
		"PANDO_PUBLIC_BASE=/api/v1/projects/"+proj.ID+"/web",
	)
	if cfg.ParentInstanceID != "" {
		cmd.Env = append(cmd.Env, "PANDO_PARENT_INSTANCE="+cfg.ParentInstanceID)
	}
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

func defaultProbeWebHealth(ctx context.Context, client *http.Client, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
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

func defaultFetchWebToken(ctx context.Context, client *http.Client, baseURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+webTokenPath, nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var payload webTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if strings.TrimSpace(payload.Token) == "" {
		return "", fmt.Errorf("token endpoint returned an empty token")
	}
	return payload.Token, nil
}

// OpenWeb starts or reuses the background WebUI child for projectID.
func (m *Manager) OpenWeb(ctx context.Context, projectID string) (*WebInstance, error) {
	if m.spawnDisabled {
		return nil, ErrChildInstance
	}

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

	webClient, err := m.webHTTPClient()
	if err != nil {
		return nil, fmt.Errorf("project manager: configure pinned web client: %w", err)
	}
	certPaths, err := m.webTLSCertPaths()
	if err != nil {
		return nil, fmt.Errorf("project manager: resolve child TLS certificate: %w", err)
	}

	m.mu.Lock()
	if inst, ok := m.webInstances[projectID]; ok && inst != nil {
		switch inst.State() {
		case WebStateStopped, WebStateError:
			delete(m.webInstances, projectID)
		default:
			m.mu.Unlock()
			if err := m.waitForWebStartup(ctx, projectID, inst, webClient); err != nil {
				return nil, err
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
	port, err := chooseAvailablePort(webLoopbackHost, preferredPort)
	if err != nil {
		return nil, fmt.Errorf("project manager: choose web port for %s: %w", proj.Path, err)
	}

	started, err := spawnWebProcess(ctx, m.pandoBin, webProcessConfig{
		ParentInstanceID: m.parentInstanceID,
		TLSCertFile:      certPaths.CertFile,
		TLSKeyFile:       certPaths.KeyFile,
	}, *proj, port)
	if err != nil {
		return nil, &ChildStartupError{
			Detail: fmt.Sprintf("project manager: spawn web child for %s: %v", proj.Path, err),
			Cause:  ErrChildStartupFailed,
		}
	}

	inst := &WebInstance{
		Project:   *proj,
		StartedAt: time.Now(),
		cmd:       started.cmd,
		errCh:     make(chan error, 1),
		cancel:    started.cancel,
		stdout:    started.stdout,
		stderr:    started.stderr,
		state:     WebStateStarting,
		port:      port,
		pid:       started.cmd.Process.Pid,
		done:      make(chan struct{}),
	}

	m.mu.Lock()
	if existing, ok := m.webInstances[projectID]; ok && existing != nil {
		m.mu.Unlock()
		terminateWebProcess(inst, syscall.SIGTERM)
		return existing, m.waitForWebStartup(ctx, projectID, existing, webClient)
	}
	m.webInstances[projectID] = inst
	m.mu.Unlock()

	go m.monitorOwnedWebInstance(projectID, inst)

	if err := m.waitForWebStartup(ctx, projectID, inst, webClient); err != nil {
		if errors.Is(err, ErrChildStartupTimeout) || errors.Is(err, ErrChildStartupFailed) {
			terminateWebProcess(inst, syscall.SIGTERM)
		}
		return nil, err
	}

	_ = m.service.TouchLastOpened(ctx, projectID)
	return inst, nil
}

// CloseWeb stops the background WebUI child for projectID if one is running.
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

// WebInstances returns the currently tracked background WebUI children keyed by
// project id.
func (m *Manager) WebInstances() map[string]*WebInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]*WebInstance, len(m.webInstances))
	for id, inst := range m.webInstances {
		out[id] = inst
	}
	return out
}

// WebInstance returns the tracked background WebUI child for projectID, if any.
func (m *Manager) WebInstance(projectID string) (*WebInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inst, ok := m.webInstances[projectID]
	return inst, ok
}

// WebTransport returns the pinned transport used to talk to background WebUI
// children. It is safe for long-lived streaming responses and WebSocket
// upgrades.
func (m *Manager) WebTransport() http.RoundTripper {
	transport, _ := m.ensureWebTransport()
	return transport
}

func (m *Manager) waitForWebStartup(ctx context.Context, projectID string, inst *WebInstance, client *http.Client) error {
	if inst == nil {
		return &ChildStartupError{
			Detail: ErrChildStartupTimeout.Error(),
			Cause:  ErrChildStartupTimeout,
		}
	}
	if inst.State() == WebStateRunning && inst.APIToken() != "" {
		return nil
	}

	deadline := time.NewTimer(webStartupTimeout)
	defer deadline.Stop()

	ticker := time.NewTicker(webHealthProbeInterval)
	defer ticker.Stop()

	for {
		probeCtx, cancel := context.WithTimeout(ctx, webHealthProbeInterval)
		err := probeWebHealth(probeCtx, client, inst.BaseURL())
		cancel()
		if err == nil {
			tokenCtx, tokenCancel := context.WithTimeout(ctx, webHealthProbeInterval)
			token, tokenErr := fetchWebToken(tokenCtx, client, inst.BaseURL())
			tokenCancel()
			if tokenErr == nil {
				inst.setToken(token)
				inst.setState(WebStateRunning)
				if m.service != nil {
					_ = m.service.UpdateWebRuntime(context.Background(), projectID, inst.PID(), inst.Port())
				}
				if m.broker != nil {
					m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
						Type:      EvWebStarted,
						ProjectID: projectID,
						Port:      inst.Port(),
					})
				}
				return nil
			}
		}

		select {
		case <-inst.done:
			exitErr := inst.exitError()
			if exitErr == nil {
				return &ChildStartupError{
					Detail: fmt.Sprintf("web instance for %s stopped before startup completed", projectID),
					Cause:  ErrChildStartupFailed,
				}
			}
			return &ChildStartupError{
				Detail: webInstanceError(inst, exitErr),
				Cause:  ErrChildStartupFailed,
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			detail := strings.TrimSpace(webInstanceError(inst, ErrChildStartupTimeout))
			if detail == "" {
				detail = ErrChildStartupTimeout.Error()
			}
			return &ChildStartupError{
				Detail: detail,
				Cause:  ErrChildStartupTimeout,
			}
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
			Port:      inst.Port(),
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
			if inst.PID() > 0 && !pidIsAlive(inst.PID()) {
				state := WebStateStopped
				eventType := EvWebStopped
				eventErr := ""
				if !inst.isStopping() {
					state = WebStateError
					eventType = EvWebError
					eventErr = fmt.Sprintf("web child pid %d is no longer running", inst.PID())
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
						Port:      inst.Port(),
						Error:     eventErr,
					})
				}
				return
			}
		}
	}
}

func (m *Manager) webHTTPClient() (*http.Client, error) {
	m.webClientMu.Lock()
	defer m.webClientMu.Unlock()

	if m.webClient != nil {
		return m.webClient, nil
	}

	transport, err := m.ensureWebTransportLocked()
	if err != nil {
		return nil, err
	}

	m.webClient = &http.Client{Transport: transport}
	return m.webClient, nil
}

func (m *Manager) ensureWebTransport() (http.RoundTripper, error) {
	m.webClientMu.Lock()
	defer m.webClientMu.Unlock()
	return m.ensureWebTransportLocked()
}

func (m *Manager) ensureWebTransportLocked() (http.RoundTripper, error) {
	if m.webTransport != nil {
		return m.webTransport, nil
	}

	paths, err := m.webTLSCertPaths()
	if err != nil {
		return nil, err
	}

	tlsConfig, err := tlsutil.LoadPinnedLoopbackTLSConfig(paths.CertFile)
	if err != nil {
		return nil, fmt.Errorf("load pinned loopback TLS config: %w", err)
	}

	m.webTransport = &http.Transport{
		// Loopback children are never reached through an HTTP proxy.
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       tlsConfig,
	}

	return m.webTransport, nil
}

func (m *Manager) webTLSCertPaths() (tlsutil.CertPaths, error) {
	if m.webTLSCertFile != "" || m.webTLSKeyFile != "" {
		if m.webTLSCertFile == "" || m.webTLSKeyFile == "" {
			return tlsutil.CertPaths{}, fmt.Errorf("both web TLS certificate and key files must be set")
		}
		return absoluteCertPaths(tlsutil.CertPaths{
			CertFile: m.webTLSCertFile,
			KeyFile:  m.webTLSKeyFile,
		})
	}

	dataDir := m.webTLSDataDir
	if dataDir == "" {
		dataDir = ".pando"
		if cfg := config.Get(); cfg != nil && cfg.Data.Directory != "" {
			dataDir = cfg.Data.Directory
		}
	}

	paths, err := tlsutil.EnsureCert(dataDir)
	if err != nil {
		return tlsutil.CertPaths{}, err
	}
	return absoluteCertPaths(paths)
}

func absoluteCertPaths(paths tlsutil.CertPaths) (tlsutil.CertPaths, error) {
	certFile, err := filepath.Abs(paths.CertFile)
	if err != nil {
		return tlsutil.CertPaths{}, fmt.Errorf("resolve TLS certificate path: %w", err)
	}
	keyFile, err := filepath.Abs(paths.KeyFile)
	if err != nil {
		return tlsutil.CertPaths{}, fmt.Errorf("resolve TLS key path: %w", err)
	}
	return tlsutil.CertPaths{CertFile: certFile, KeyFile: keyFile}, nil
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

	liveIDs := make(map[string]struct{}, len(entries))
	byPath := make(map[string][]*instanceregistry.Entry)
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		liveIDs[entry.InstanceID] = struct{}{}
		if entry.Mode != instanceregistry.ModeWebUI || entry.Path == "" || entry.PID <= 0 || entry.WebPort <= 0 || entry.WebPort > 65535 || entry.ParentInstanceID == "" {
			continue
		}
		byPath[entry.Path] = append(byPath[entry.Path], entry)
	}

	var webClient *http.Client
	for _, proj := range projects {
		projectEntries := byPath[proj.Path]
		var adopted bool
		for _, entry := range projectEntries {
			if _, parentAlive := liveIDs[entry.ParentInstanceID]; parentAlive {
				continue
			}
			if webClient == nil {
				webClient, err = m.webHTTPClient()
				if err != nil {
					logging.Warn("project manager: configure pinned web client for adoption failed", "error", err)
					return
				}
			}

			inst := &WebInstance{
				Project:   proj,
				StartedAt: entry.StartedAt,
				errCh:     make(chan error, 1),
				cancel:    func() {},
				stdout:    newTailBuffer(webOutputBufferLimit),
				stderr:    newTailBuffer(webOutputBufferLimit),
				state:     WebStateStarting,
				port:      entry.WebPort,
				pid:       entry.PID,
				done:      make(chan struct{}),
				adopted:   true,
			}

			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := probeWebHealth(probeCtx, webClient, inst.BaseURL())
			cancel()
			if err != nil {
				continue
			}

			tokenCtx, tokenCancel := context.WithTimeout(ctx, 2*time.Second)
			token, err := fetchWebToken(tokenCtx, webClient, inst.BaseURL())
			tokenCancel()
			if err != nil {
				continue
			}

			inst.setToken(token)
			inst.setState(WebStateRunning)
			if m.service != nil {
				_ = m.service.UpdateWebRuntime(ctx, proj.ID, inst.PID(), inst.Port())
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

func loopbackWebBaseURL(port int) string {
	return fmt.Sprintf("https://%s:%d", webLoopbackHost, port)
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

	pid := inst.PID()
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
