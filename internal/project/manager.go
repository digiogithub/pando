package project

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/config"
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
)

// ManagerEvent is the union event type published by Manager.
type ManagerEvent struct {
	Type      ManagerEventType
	ProjectID string
	Status    string
	Error     string
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

	client := newProjectACPClient(proj)
	conn := acpsdk.NewClientSideConnection(client, stdinPipe, stdoutPipe)

	inst := &Instance{
		Project: proj,
		cmd:     cmd,
		conn:    conn,
		cancel:  cancel,
		ready:   make(chan struct{}),
		errCh:   make(chan error, 1),
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

		// Update persistent status.
		svcCtx := context.Background()
		if waitErr != nil {
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

// resolvePath expands a leading ~ to the user home directory and evaluates
// any symlinks to return a canonical absolute path.
func resolvePath(p string) (string, error) {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p, err
		}
		p = filepath.Join(home, p[2:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// If symlink eval fails (e.g. path doesn't exist yet), just use abs.
		return abs, nil
	}
	return real, nil
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
