package project

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/pubsub"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// ManagerEventType identifies the kind of ManagerEvent.
type ManagerEventType string

const (
	// EvProjectSwitched is published when the active project changes.
	EvProjectSwitched ManagerEventType = "switched"
	// EvStatusChanged is published when a project's running status changes.
	EvStatusChanged ManagerEventType = "status_changed"
	// EvInitRequired is published when activation fails because the project
	// path has no Pando configuration file.
	EvInitRequired ManagerEventType = "init_required"
	// EvDelegationChanged is published when the count of in-flight delegated
	// sessions running inside a warm instance changes (start/end). The current
	// count is carried in ManagerEvent.Count.
	EvDelegationChanged ManagerEventType = "delegation_changed"
)

// ManagerEvent is the union event type published by Manager.
type ManagerEvent struct {
	Type      ManagerEventType
	ProjectID string
	Status    string
	Error     string
	// Count carries the current in-flight delegated-session count for
	// EvDelegationChanged events; zero for other event types.
	Count int
}

// Manager tracks child Pando ACP processes for registered project directories
// and routes lifecycle events to subscribers via a generic pubsub broker.
type Manager struct {
	service   Service
	instances map[string]*Instance // keyed by project ID
	activeID  string               // currently active project ("" = main instance)
	mu        sync.RWMutex

	broker *pubsub.Broker[ManagerEvent]

	// pandoBin is the path to the current pando executable.
	pandoBin string

	ctx    context.Context
	cancel context.CancelFunc
}

// NewManager creates a new Manager.
// Call Shutdown() when done to stop all child processes and release resources.
func NewManager(ctx context.Context, service Service) (*Manager, error) {
	pandoBin, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("project manager: cannot determine executable path: %w", err)
	}

	mgrCtx, cancel := context.WithCancel(ctx)

	m := &Manager{
		service:   service,
		instances: make(map[string]*Instance),
		broker:    pubsub.NewBroker[ManagerEvent](),
		pandoBin:  pandoBin,
		ctx:       mgrCtx,
		cancel:    cancel,
	}
	return m, nil
}

// Activate starts (if needed) the child ACP process for projectID and makes it
// the active project.  Returns ErrProjectNeedsInit if the path has no
// .pando.toml or .pando.json configuration file.
func (m *Manager) Activate(ctx context.Context, projectID string) error {
	// 1. Fetch project record.
	proj, err := m.service.Get(ctx, projectID)
	if err != nil {
		return fmt.Errorf("project manager: get project %s: %w", projectID, err)
	}

	// 2. Expand ~ and resolve symlinks for a canonical absolute path.
	resolvedPath, err := resolvePath(proj.Path)
	if err != nil {
		return fmt.Errorf("project manager: resolve path %s: %w", proj.Path, err)
	}

	// Ensure the path exists on disk.
	if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
		_ = m.service.UpdateStatus(ctx, projectID, StatusMissing, 0, 0)
		return fmt.Errorf("project path does not exist: %s", resolvedPath)
	}

	// 3. Check for a Pando configuration file.
	if !config.HasConfigFileAt(resolvedPath) {
		_ = m.service.UpdateStatus(ctx, projectID, StatusInitializing, 0, 0)
		m.broker.Publish(pubsub.CreatedEvent, ManagerEvent{
			Type:      EvInitRequired,
			ProjectID: projectID,
		})
		return ErrProjectNeedsInit
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 4. If an instance already exists and is ready, just switch the active pointer.
	if inst, ok := m.instances[projectID]; ok {
		select {
		case <-inst.ready:
			// Already ready.
		default:
			// Not ready yet — still acceptable to switch; the caller can wait on inst.ready.
		}
		// A user-driven activation claims the instance as user-focused, even if it
		// was originally auto-started by the delegation router.
		inst.markDelegationSpawned(false)
		m.activeID = projectID
		_ = m.service.TouchLastOpened(ctx, projectID)
		m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
			Type:      EvProjectSwitched,
			ProjectID: projectID,
		})
		return nil
	}

	// 5. Spawn a new child ACP process.
	inst, err := m.spawnChild(*proj)
	if err != nil {
		_ = m.service.UpdateStatus(ctx, projectID, StatusError, 0, 0)
		return fmt.Errorf("project manager: spawn ACP child for %s: %w", proj.Path, err)
	}

	m.instances[projectID] = inst
	m.activeID = projectID

	_ = m.service.UpdateStatus(ctx, projectID, StatusRunning, inst.cmd.Process.Pid, 0)
	_ = m.service.TouchLastOpened(ctx, projectID)
	m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
		Type:      EvProjectSwitched,
		ProjectID: projectID,
	})
	return nil
}

// CompleteInit initializes the Pando configuration at the project's directory
// and then activates the project. It is called after the user confirms the
// initialization prompt shown when Activate returns ErrProjectNeedsInit.
//
// It creates .pando.toml, .pando/ directory structure, and the init flag,
// then retries Activate. On success the project becomes the active project.
func (m *Manager) CompleteInit(ctx context.Context, projectID string) error {
	proj, err := m.service.Get(ctx, projectID)
	if err != nil {
		return fmt.Errorf("CompleteInit: get project: %w", err)
	}

	// Update status to "initializing" so the UI can show a spinner.
	if err := m.service.UpdateStatus(ctx, projectID, StatusInitializing, 0, 0); err != nil {
		return fmt.Errorf("CompleteInit: update status: %w", err)
	}

	// Perform filesystem initialization.
	if err := config.InitializeProjectAt(proj.Path); err != nil {
		// Revert to error state so the UI knows it failed.
		_ = m.service.UpdateStatus(ctx, projectID, StatusError, 0, 0)
		return fmt.Errorf("CompleteInit: initialize %s: %w", proj.Path, err)
	}

	// Mark as initialized in the registry.
	if err := m.service.MarkInitialized(ctx, projectID); err != nil {
		return fmt.Errorf("CompleteInit: mark initialized: %w", err)
	}

	// Activate (now that config exists this should succeed).
	return m.Activate(ctx, projectID)
}

// spawnChild starts a new child `pando acp` process for the given project and
// returns an initialised Instance.  The caller must hold m.mu.Lock().
func (m *Manager) spawnChild(proj Project) (*Instance, error) {
	procCtx, cancel := context.WithCancel(m.ctx)

	cmd := exec.CommandContext(procCtx, m.pandoBin, "acp")
	cmd.Dir = proj.Path
	cmd.Env = append(os.Environ(), "NO_COLOR=1")

	// Verify the working directory is accessible.
	if err := checkDirAccessible(proj.Path); err != nil {
		cancel()
		return nil, fmt.Errorf("project directory not accessible: %w", err)
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start: %w", err)
	}

	client := newDelegationClient(proj)
	conn := acpsdk.NewClientSideConnection(client, stdinPipe, stdoutPipe)

	inst := &Instance{
		Project:   proj,
		cmd:       cmd,
		conn:      conn,
		delClient: client,
		cancel:    cancel,
		ready:     make(chan struct{}),
		errCh:     make(chan error, 1),
	}

	// The stdio ACP server is ready as soon as the process starts.
	close(inst.ready)

	// Monitor process exit asynchronously.
	go func() {
		waitErr := cmd.Wait()

		// Send exit error (nil on clean exit) to the buffered channel.
		select {
		case inst.errCh <- waitErr:
		default:
		}

		// Update persistent status. A non-nil waitErr caused by an intentional
		// stop (Stop/Unregister/Shutdown cancel procCtx, which SIGKILLs the
		// child) is a clean stop, not a crash — only a process that exited on
		// its own with an error is recorded as StatusError.
		svcCtx := context.Background()
		if waitErr != nil && procCtx.Err() == nil {
			_ = m.service.UpdateStatus(svcCtx, proj.ID, StatusError, 0, 0)
		} else {
			_ = m.service.UpdateStatus(svcCtx, proj.ID, StatusStopped, 0, 0)
		}

		// Remove from the live-instance map and clear activeID if needed.
		m.mu.Lock()
		if m.activeID == proj.ID {
			m.activeID = ""
		}
		delete(m.instances, proj.ID)
		m.mu.Unlock()

		m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
			Type:      EvStatusChanged,
			ProjectID: proj.ID,
			Status:    StatusStopped,
		})
	}()

	return inst, nil
}

// Deactivate clears the active project (the main pando instance becomes active again).
func (m *Manager) Deactivate(_ context.Context) error {
	m.mu.Lock()
	m.activeID = ""
	m.mu.Unlock()

	m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
		Type:      EvProjectSwitched,
		ProjectID: "",
	})
	return nil
}

// Runtime reports the live execution state of a project.
//
// running is true when a Pando ACP instance is currently serving the project's
// directory. external is true when that instance was launched by another
// application (e.g. an editor's ACP integration) rather than by this manager,
// and therefore cannot be stopped from here. pid is the OS process id of the
// serving instance when known.
//
// Ownership is determined by the in-memory instance map: an instance this
// manager spawned is tracked there. Anything else that holds the project's IPC
// lock on disk with a live PID is considered an external instance.
func (m *Manager) Runtime(projectID, path string) (running, external bool, pid int) {
	m.mu.RLock()
	inst, ours := m.instances[projectID]
	if ours {
		if inst.cmd != nil && inst.cmd.Process != nil {
			pid = inst.cmd.Process.Pid
		}
		m.mu.RUnlock()
		return true, false, pid
	}
	m.mu.RUnlock()

	// Not tracked here — inspect the IPC lock the serving instance leaves on disk.
	resolved, err := resolvePath(path)
	if err != nil {
		return false, false, 0
	}
	info, err := ipc.ReadLockForPath(resolved)
	if err != nil || info == nil {
		return false, false, 0
	}
	if !pidIsAlive(info.PID) {
		return false, false, 0
	}
	return true, true, info.PID
}

// Stop terminates the child ACP instance this manager launched for projectID.
//
// It returns ErrExternalInstance when the project is running but its instance
// was launched by another application, which this manager has no authority to
// terminate. When nothing is running, Stop is a no-op that ensures the registry
// reflects a stopped state.
func (m *Manager) Stop(ctx context.Context, projectID string) error {
	_, err := m.StopReport(ctx, projectID)
	return err
}

// StopReport is Stop with bookkeeping: it returns the number of in-flight
// delegated sessions that were running inside the instance at stop time. Those
// sessions are cancelled by the process teardown; the orchestrator observes the
// failed turn and synthesizes a terminal conclusion (cold-path fallback), so the
// parent loop never hangs. The count lets the UI warn the user how many
// delegated loops were interrupted.
func (m *Manager) StopReport(ctx context.Context, projectID string) (cancelled int, err error) {
	m.mu.Lock()
	inst, ours := m.instances[projectID]
	if ours {
		cancelled = inst.InflightDelegations()
		inst.cancel()
		if inst.cmd.Process != nil {
			_ = inst.cmd.Process.Signal(syscall.SIGTERM)
		}
		delete(m.instances, projectID)
		if m.activeID == projectID {
			m.activeID = ""
		}
	}
	m.mu.Unlock()

	if ours {
		// Give the process a moment to exit cleanly, then force-kill.
		select {
		case <-inst.errCh:
		case <-time.After(5 * time.Second):
			if inst.cmd.Process != nil {
				_ = inst.cmd.Process.Kill()
			}
		}
		// The spawnChild monitor goroutine updates the persistent status and
		// publishes EvStatusChanged on exit; additionally announce the active
		// switch so the UI reflects that the main instance is active again.
		m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
			Type:      EvProjectSwitched,
			ProjectID: "",
		})
		return cancelled, nil
	}

	// Not launched by us: distinguish "running externally" from "not running".
	proj, getErr := m.service.Get(ctx, projectID)
	if getErr != nil {
		return 0, fmt.Errorf("project manager: stop: get project %s: %w", projectID, getErr)
	}
	if running, external, _ := m.Runtime(projectID, proj.Path); running && external {
		return 0, ErrExternalInstance
	}

	// Nothing live; make sure the registry reflects a stopped state.
	_ = m.service.UpdateStatus(ctx, projectID, StatusStopped, 0, 0)
	m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
		Type:      EvStatusChanged,
		ProjectID: projectID,
		Status:    StatusStopped,
	})
	return 0, nil
}

// pidIsAlive reports whether pid refers to a live process. Signal 0 performs
// error checking only, so a nil result means the process exists.
func pidIsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// ActiveID returns the currently active project ID.
// An empty string means the main (non-project) pando instance is active.
func (m *Manager) ActiveID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeID
}

// ActiveProject returns the currently active Project, or nil when the main
// instance is active.
func (m *Manager) ActiveProject(ctx context.Context) (*Project, error) {
	m.mu.RLock()
	id := m.activeID
	m.mu.RUnlock()

	if id == "" {
		return nil, nil
	}
	return m.service.Get(ctx, id)
}

// List returns all registered projects from the underlying service.
func (m *Manager) List(ctx context.Context) ([]Project, error) {
	return m.service.List(ctx)
}

// Register adds a new project path to the registry.
// If name is empty, filepath.Base(path) is used.
// The path is expanded (~ resolved) and symlinks are evaluated before storing.
func (m *Manager) Register(ctx context.Context, name, path string) (*Project, error) {
	resolved, err := resolvePath(path)
	if err != nil {
		return nil, fmt.Errorf("project manager: resolve path: %w", err)
	}
	// Verify the path is an existing directory.
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("project manager: path does not exist: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project manager: path is not a directory: %s", resolved)
	}
	return m.service.Create(ctx, name, resolved)
}

// Unregister removes a project from the registry and stops its running instance
// (if any).
func (m *Manager) Unregister(ctx context.Context, projectID string) error {
	// Stop the child process first if it is running.
	m.mu.Lock()
	inst, running := m.instances[projectID]
	if running {
		inst.cancel()
		if inst.cmd.Process != nil {
			_ = inst.cmd.Process.Signal(syscall.SIGTERM)
		}
		delete(m.instances, projectID)
		if m.activeID == projectID {
			m.activeID = ""
		}
	}
	m.mu.Unlock()

	if running {
		// Give the process a moment to exit cleanly.
		select {
		case <-inst.errCh:
		case <-time.After(5 * time.Second):
			if inst.cmd.Process != nil {
				_ = inst.cmd.Process.Kill()
			}
		}
	}

	if err := m.service.Delete(ctx, projectID); err != nil {
		return fmt.Errorf("project manager: delete project %s: %w", projectID, err)
	}

	m.broker.Publish(pubsub.DeletedEvent, ManagerEvent{
		Type:      EvStatusChanged,
		ProjectID: projectID,
		Status:    StatusStopped,
	})
	return nil
}

// ListSessions returns the cached session list for the given project instance.
// Returns nil when the project is not currently running.
//
// NOTE: Actual session fetching will be implemented in Phase 5 via the REST API.
// For now this returns the in-memory cache which starts empty and is refreshed
// by future implementations.
func (m *Manager) ListSessions(_ context.Context, projectID string) ([]sessionEntry, error) {
	m.mu.RLock()
	inst, ok := m.instances[projectID]
	m.mu.RUnlock()

	if !ok {
		return nil, nil
	}

	inst.mu.RLock()
	defer inst.mu.RUnlock()
	// Return a copy to avoid races.
	result := make([]sessionEntry, len(inst.sessions))
	copy(result, inst.sessions)
	return result, nil
}

// DelegationInfo reports the warm-delegation state of a manager-owned instance:
// the count of in-flight delegated sessions, whether the instance was
// auto-started by the delegation router (vs user-activated), and whether an
// instance is running at all. All zero/false when the project has no
// manager-owned instance (e.g. stopped or external).
func (m *Manager) DelegationInfo(projectID string) (inflight int, spawned, running bool) {
	m.mu.RLock()
	inst, ok := m.instances[projectID]
	m.mu.RUnlock()
	if !ok || inst == nil {
		return 0, false, false
	}
	return inst.InflightDelegations(), inst.isDelegationSpawned(), true
}

// publishDelegationChanged announces a change in the in-flight delegated-session
// count for a project so the Projects panel (WebUI SSE / TUI) can refresh live.
func (m *Manager) publishDelegationChanged(projectID string, count int) {
	if m.broker == nil {
		return
	}
	m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
		Type:      EvDelegationChanged,
		ProjectID: projectID,
		Count:     count,
	})
}

// resolvePath expands a leading ~ to the user home directory and evaluates
// any symlinks to return a canonical absolute path.
func resolvePath(p string) (string, error) {
	// Delegate to the shared canonicaliser so the manager, the local DB
	// service and the global registry all normalise paths identically.
	return config.CanonicalProjectPath(p), nil
}

// checkDirAccessible verifies that dir can be opened (read permission check).
func checkDirAccessible(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	f.Close()
	return nil
}

// Rename changes the display name of a registered project and propagates
// the change to the global projects registry so other instances can see it.
func (m *Manager) Rename(ctx context.Context, projectID, newName string) error {
	proj, err := m.service.Get(ctx, projectID)
	if err != nil {
		return fmt.Errorf("project manager: rename: get project %s: %w", projectID, err)
	}
	if err := m.service.Rename(ctx, projectID, newName); err != nil {
		return fmt.Errorf("project manager: rename: %w", err)
	}
	// Propagate to global registry.
	if err := config.UpdateGlobalProjectName(proj.Path, newName); err != nil {
		logging.Warn("project manager: rename: failed to update global registry", "error", err)
	}
	m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
		Type:      EvStatusChanged,
		ProjectID: projectID,
	})
	return nil
}

// SeedFromGlobal upserts every entry from the global projects registry into
// the local DB so that any Pando instance can see all known projects.
// Entries whose path is already registered are skipped.
// This is called once during startup.
func (m *Manager) SeedFromGlobal(ctx context.Context) {
	entries, err := config.LoadGlobalProjects()
	if err != nil {
		logging.Warn("project manager: seed from global: failed to load registry", "error", err)
		return
	}
	for _, entry := range entries {
		absPath := entry.Path
		if absPath == "" {
			continue
		}
		// Skip if already present in the local DB.
		if _, err := m.service.GetByPath(ctx, absPath); err == nil {
			continue
		}
		name := entry.Name
		if name == "" {
			name = filepath.Base(absPath)
		}
		proj, err := m.service.Create(ctx, name, absPath)
		if err != nil {
			logging.Debug("project manager: seed from global: create failed", "path", absPath, "error", err)
			continue
		}
		// Mark as initialized when the path has a Pando config file.
		if config.HasConfigFileAt(absPath) {
			if err := m.service.MarkInitialized(ctx, proj.ID); err != nil {
				logging.Debug("project manager: seed from global: mark initialized failed", "id", proj.ID, "error", err)
			}
		}
		logging.Debug("project manager: seeded project from global registry", "path", absPath, "name", name)
	}
}

// Subscribe returns a channel of ManagerEvents for real-time notifications.
func (m *Manager) Subscribe(ctx context.Context) <-chan pubsub.Event[ManagerEvent] {
	return m.broker.Subscribe(ctx)
}

// Shutdown stops all running child processes and cleans up resources.
// It is safe to call Shutdown more than once.
func (m *Manager) Shutdown() {
	m.cancel()

	m.mu.Lock()
	instances := make([]*Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		instances = append(instances, inst)
	}
	m.instances = make(map[string]*Instance)
	m.activeID = ""
	m.mu.Unlock()

	for _, inst := range instances {
		inst.cancel()
		if inst.cmd.Process != nil {
			_ = inst.cmd.Process.Signal(syscall.SIGTERM)
		}
	}

	// Wait for all processes to exit (with a timeout).
	for _, inst := range instances {
		select {
		case <-inst.errCh:
		case <-time.After(10 * time.Second):
			if inst.cmd.Process != nil {
				_ = inst.cmd.Process.Kill()
			}
		}
	}

	m.broker.Shutdown()
}
