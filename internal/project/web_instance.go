package project

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/procgroup"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/tlsutil"
)

const (
	webHealthProbeInterval   = 200 * time.Millisecond
	defaultWebStartupTimeout = 20 * time.Second
	webOutputBufferLimit     = 8 * 1024
	webLoopbackHost          = "127.0.0.1"
)

// WebInstanceState describes the lifecycle state of a background WebUI child.
type WebInstanceState string

const (
	// WebStateStarting means the child process exists but has not yet passed the
	// pinned TLS health probe and identity check.
	WebStateStarting WebInstanceState = "starting"
	// WebStateRunning means the child answered the pinned health probe and proved
	// it is the process the parent started for this project.
	WebStateRunning WebInstanceState = "running"
	// WebStateError means the child crashed or failed startup.
	WebStateError WebInstanceState = "error"
	// WebStateStopped means the child exited cleanly or was shut down.
	WebStateStopped WebInstanceState = "stopped"
)

// WebInstance represents a background `pando serve` child started by the
// project manager. Children are never adopted: the parent keeps the only copy
// of the child's API token and the child exits when its parent does.
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
	rpcPort  int
	token    string
	done     chan struct{}
	exitErr  error
	stopping bool

	slots delegationSlots
}

// WebInstanceSnapshot is a race-free copy of a WebInstance's live runtime data.
type WebInstanceSnapshot struct {
	Project   Project
	Port      int
	PID       int
	StartedAt time.Time
	State     WebInstanceState
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
	}
}

// BaseURL returns the pinned loopback origin of the child WebUI server.
func (w *WebInstance) BaseURL() string {
	return loopbackWebBaseURL(w.Port())
}

// APIToken returns the in-memory API token the parent minted for the child. The
// token is never persisted.
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

// RPCPort returns the IPC ROUTER port served by the child when known.
func (w *WebInstance) RPCPort() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.rpcPort
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

func (w *WebInstance) setRPCPort(port int) {
	w.mu.Lock()
	w.rpcPort = port
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
	w.rpcPort = 0
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

func (w *WebInstance) acquireDelegationSlotOrQueue(ctx context.Context, max, queueDepth int) bool {
	return w.slots.acquireOrQueue(ctx, max, queueDepth)
}

func (w *WebInstance) releaseDelegationSlot() {
	w.slots.release()
}

func (w *WebInstance) beginDelegationShutdown() {
	w.slots.beginCloseAndWake()
}

func (w *WebInstance) InflightDelegations() int {
	return w.slots.inflightCount()
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
	// APIToken is handed to the child through its environment only.
	APIToken string
}

// webHealthExpectation is the identity a child must report on /health before
// the parent trusts the process answering on its port.
type webHealthExpectation struct {
	ProjectID        string
	ParentInstanceID string
	PID              int
}

type webHealthBody struct {
	StartupMode      string `json:"startup_mode"`
	ProjectID        string `json:"project_id"`
	ParentInstanceID string `json:"parent_instance_id"`
	PID              int    `json:"pid"`
}

// webIdentityError means something answered on the child's port but is not the
// process the parent started. It is fatal for the startup, never retried.
type webIdentityError struct{ Detail string }

func (e *webIdentityError) Error() string { return "web child identity mismatch: " + e.Detail }

var (
	spawnWebProcess = defaultSpawnWebProcess
	probeWebHealth  = defaultProbeWebHealth
)

func newWebChildToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate web child token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func defaultSpawnWebProcess(_ context.Context, pandoBin string, cfg webProcessConfig, proj Project, port int) (*webProcessStart, error) {
	cmd := exec.Command(pandoBin, "serve",
		"--host", webLoopbackHost,
		"--port", strconv.Itoa(port),
		"--tls-cert", cfg.TLSCertFile,
		"--tls-key", cfg.TLSKeyFile,
	)
	// pando serve has no --cwd flag: it works in its process working directory.
	cmd.Dir = proj.Path
	cmd.Env = append(os.Environ(),
		"NO_COLOR=1",
		"PANDO_PROJECT_ID="+proj.ID,
		"PANDO_PUBLIC_BASE=/api/v1/projects/"+proj.ID+"/web",
	)
	if cfg.ParentInstanceID != "" {
		cmd.Env = append(cmd.Env, "PANDO_PARENT_INSTANCE="+cfg.ParentInstanceID)
	}
	cmd.Env = append(cmd.Env,
		"PANDO_PARENT_PID="+strconv.Itoa(os.Getpid()),
		"PANDO_CHILD_API_TOKEN="+cfg.APIToken,
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

func defaultProbeWebHealth(ctx context.Context, client *http.Client, baseURL string, expect webHealthExpectation) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("health probe returned status %d", resp.StatusCode)
	}

	var body webHealthBody
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&body); err != nil {
		return &webIdentityError{Detail: fmt.Sprintf("undecodable health response: %v", err)}
	}
	switch {
	case body.StartupMode != "project-child":
		return &webIdentityError{Detail: fmt.Sprintf("startup_mode %q, want project-child", body.StartupMode)}
	case body.ProjectID != expect.ProjectID:
		return &webIdentityError{Detail: fmt.Sprintf("project_id %q, want %q", body.ProjectID, expect.ProjectID)}
	case body.ParentInstanceID != expect.ParentInstanceID:
		return &webIdentityError{Detail: fmt.Sprintf("parent_instance_id %q, want %q", body.ParentInstanceID, expect.ParentInstanceID)}
	case body.PID != expect.PID:
		return &webIdentityError{Detail: fmt.Sprintf("pid %d, want %d", body.PID, expect.PID)}
	}
	return nil
}

// freeLoopbackPort asks the kernel for an unused loopback port.
func freeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(webLoopbackHost, "0"))
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("failed to determine free port")
	}
	return addr.Port, nil
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

	if err := m.stopConflictingACPInstance(ctx, projectID); err != nil {
		return nil, err
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
	if limit := m.maxWebInstances; limit > 0 && m.activeWebInstanceCountLocked() >= limit {
		m.mu.Unlock()
		return nil, &ErrWebInstanceLimit{Limit: limit}
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

	inst, err := m.startWebInstance(ctx, projectID, *proj, port, certPaths, webClient)
	if err != nil && errors.Is(err, ErrChildStartupFailed) && !errors.Is(err, ErrChildStartupTimeout) && ctx.Err() == nil {
		// The port can be taken between choosing it and the child binding it
		// (the child refuses to fall back to another port): retry once on a
		// fresh one.
		if fresh, portErr := freeLoopbackPort(); portErr == nil {
			logging.Warn("project manager: web child startup failed, retrying on a fresh port", "project", projectID, "port", port, "error", err)
			inst, err = m.startWebInstance(ctx, projectID, *proj, fresh, certPaths, webClient)
		}
	}
	if err != nil {
		return nil, err
	}

	_ = m.service.TouchLastOpened(ctx, projectID)
	return inst, nil
}

// startWebInstance spawns one child on port, registers it and waits until it
// has proved its identity. A failed startup leaves no tracked instance behind.
func (m *Manager) startWebInstance(ctx context.Context, projectID string, proj Project, port int, certPaths tlsutil.CertPaths, webClient *http.Client) (*WebInstance, error) {
	token, err := newWebChildToken()
	if err != nil {
		return nil, &ChildStartupError{Detail: err.Error(), Cause: ErrChildStartupFailed}
	}

	started, err := spawnWebProcess(ctx, m.pandoBin, webProcessConfig{
		ParentInstanceID: m.parentInstanceID,
		TLSCertFile:      certPaths.CertFile,
		TLSKeyFile:       certPaths.KeyFile,
		APIToken:         token,
	}, proj, port)
	if err != nil {
		return nil, &ChildStartupError{
			Detail: fmt.Sprintf("project manager: spawn web child for %s: %v", proj.Path, err),
			Cause:  ErrChildStartupFailed,
		}
	}

	inst := &WebInstance{
		Project:   proj,
		StartedAt: time.Now(),
		cmd:       started.cmd,
		errCh:     make(chan error, 1),
		cancel:    started.cancel,
		stdout:    started.stdout,
		stderr:    started.stderr,
		state:     WebStateStarting,
		port:      port,
		pid:       started.cmd.Process.Pid,
		token:     token,
		done:      make(chan struct{}),
		slots:     newDelegationSlotsAt(time.Now()),
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
	return inst, nil
}

// CloseWeb stops the background WebUI child for projectID if one is running.
func (m *Manager) CloseWeb(ctx context.Context, projectID string) error {
	_, err := m.CloseWebReport(ctx, projectID)
	return err
}

// CloseWebReport is CloseWeb with bookkeeping: it returns the number of
// web-routed delegations that were still in flight when the child was asked to
// stop. Those delegations are cancelled by the process teardown; the parent warm
// call exits with a terminal error instead of hanging.
func (m *Manager) CloseWebReport(ctx context.Context, projectID string) (int, error) {
	inst, ok := m.takeWebInstance(projectID)
	if !ok || inst == nil {
		if m.service != nil {
			_ = m.service.UpdateWebRuntime(ctx, projectID, 0, 0)
		}
		return 0, nil
	}

	cancelled := inst.InflightDelegations()
	inst.beginDelegationShutdown()
	inst.markStopping()
	terminateWebProcess(inst, syscall.SIGTERM)

	select {
	case <-inst.done:
	case <-ctx.Done():
		return cancelled, ctx.Err()
	case <-time.After(5 * time.Second):
		terminateWebProcess(inst, syscall.SIGKILL)
	}

	return cancelled, nil
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
	if inst.State() == WebStateRunning {
		return nil
	}
	expect := webHealthExpectation{
		ProjectID:        projectID,
		ParentInstanceID: m.parentInstanceID,
		PID:              inst.PID(),
	}

	deadline := time.NewTimer(m.webStartupTimeout)
	defer deadline.Stop()

	ticker := time.NewTicker(webHealthProbeInterval)
	defer ticker.Stop()

	for {
		probeCtx, cancel := context.WithTimeout(ctx, webHealthProbeInterval)
		err := probeWebHealth(probeCtx, client, inst.BaseURL(), expect)
		cancel()
		if err == nil {
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
		var identityErr *webIdentityError
		if errors.As(err, &identityErr) {
			// Not the process we started: never show it as a running tab. Only
			// the process this manager spawned is signalled.
			inst.markStopping()
			terminateWebProcess(inst, syscall.SIGTERM)
			detail := identityErr.Error()
			if m.broker != nil {
				m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
					Type:      EvWebError,
					ProjectID: projectID,
					Port:      inst.Port(),
					Error:     detail,
				})
			}
			return &ChildStartupError{Detail: detail, Cause: ErrChildStartupFailed}
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

func (m *Manager) activeWebInstanceCountLocked() int {
	count := 0
	for _, inst := range m.webInstances {
		if inst == nil {
			continue
		}
		switch inst.State() {
		case WebStateStarting, WebStateRunning:
			count++
		}
	}
	return count
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

// clearStaleWebRuntime forgets web runtime data left in the project store by a
// previous parent. Web children do not outlive their parent (they exit when it
// is gone) and the API token handed to a child lives only in memory, so nothing
// from a previous run can be adopted or signalled.
func (m *Manager) clearStaleWebRuntime(ctx context.Context) {
	projects, err := m.service.List(ctx)
	if err != nil {
		logging.Warn("project manager: list projects to clear stale web runtime failed", "error", err)
		return
	}
	for _, proj := range projects {
		if proj.WebPID == 0 && proj.WebPort == 0 {
			continue
		}
		if err := m.service.UpdateWebRuntime(ctx, proj.ID, 0, 0); err != nil {
			logging.Warn("project manager: clear stale web runtime failed", "project", proj.ID, "error", err)
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

func (m *Manager) stopConflictingACPInstance(ctx context.Context, projectID string) error {
	m.mu.Lock()
	inst, ok := m.instances[projectID]
	if !ok || inst == nil {
		m.mu.Unlock()
		return nil
	}
	if inflight := inst.InflightDelegations(); inflight > 0 {
		m.mu.Unlock()
		return &DelegationsInFlightError{Count: inflight}
	}
	inst.beginCloseAndWake()
	inst.cancel()
	if inst.cmd != nil && inst.cmd.Process != nil {
		_ = inst.cmd.Process.Signal(syscall.SIGTERM)
	}
	delete(m.instances, projectID)
	m.mu.Unlock()

	select {
	case <-inst.errCh:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		if inst.cmd != nil && inst.cmd.Process != nil {
			_ = inst.cmd.Process.Kill()
		}
	}
	return nil
}
