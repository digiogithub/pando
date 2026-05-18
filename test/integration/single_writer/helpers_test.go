//go:build integration

// Package single_writer_test contains multi-process integration tests that
// verify the single-writer SQLite architecture. Tests spawn real Pando
// subprocesses in a shared temp directory and assert that exactly one process
// acquires the IPC lock (becomes primary) while others become secondaries.
package single_writer_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// pandoBinary is resolved once at package init from the PANDO_BINARY env var,
// falling back to a path relative to the repository root for local runs.
var pandoBinary string

func init() {
	pandoBinary = os.Getenv("PANDO_BINARY")
	if pandoBinary == "" {
		// When running via `go test ./test/integration/single_writer/...` from the
		// repo root, the binary lives at ./pando relative to the root.
		pandoBinary = "../../../pando"
	}
}

// testEnv holds the temp working directory for a single integration test.
// Use newTestEnv to create one; cleanup is registered automatically via t.Cleanup.
type testEnv struct {
	// Workdir is the temp directory that acts as the Pando working directory.
	// Each test gets its own directory so lock files and DBs do not collide.
	Workdir string
	t       *testing.T
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	return &testEnv{Workdir: dir, t: t}
}

// instance represents a running Pando subprocess.
// The exitErr field is set exactly once when the process exits, protected by
// exitOnce so that stop() and waitForExit() can both be called safely without
// racing on the Done channel.
type instance struct {
	Cmd     *exec.Cmd
	PID     int
	Workdir string

	// exited is closed when the process has finished. Reading from it never
	// blocks after the process exits. Use this instead of Done for polling.
	exited  chan struct{}
	exitErr error
	exitMu  sync.Mutex
}

// startInstance launches a Pando subprocess with the given args in workdir.
// stdout and stderr are forwarded to the test output.
// Register inst.stop(t) in t.Cleanup after calling startInstance.
func startInstance(t *testing.T, workdir string, args ...string) *instance {
	t.Helper()
	binary := pandoBinary
	if binary == "" {
		t.Fatal("pandoBinary is empty; set PANDO_BINARY or use 'make test-integration'")
	}

	cmd := exec.Command(binary, args...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(),
		"PANDO_DATA_DIR="+filepath.Join(workdir, ".pando"),
		"NO_COLOR=1",
		// Disable browser open in desktop mode.
		"DO_NOT_OPEN_BROWSER=1",
	)
	// Forward output to test log so failures are diagnosable.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("startInstance: failed to start pando %v: %v", args, err)
	}

	inst := &instance{
		Cmd:     cmd,
		PID:     cmd.Process.Pid,
		Workdir: workdir,
		exited:  make(chan struct{}),
	}

	go func() {
		err := cmd.Wait()
		inst.exitMu.Lock()
		inst.exitErr = err
		inst.exitMu.Unlock()
		close(inst.exited)
	}()

	return inst
}

// isRunning returns true if the process has not yet exited.
func (inst *instance) isRunning() bool {
	select {
	case <-inst.exited:
		return false
	default:
		return true
	}
}

// stop sends SIGTERM to the instance and waits up to 5 seconds for it to exit,
// then sends SIGKILL. Safe to call after waitForExit or multiple times.
func (inst *instance) stop(t *testing.T) {
	t.Helper()
	if inst.Cmd.Process == nil {
		return
	}
	// Signal; ignore error — process may have already exited.
	_ = inst.Cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-inst.exited:
		// exited cleanly
	case <-time.After(5 * time.Second):
		_ = inst.Cmd.Process.Kill()
		select {
		case <-inst.exited:
		case <-time.After(2 * time.Second):
			// Give up — process is stuck.
		}
	}
}

// waitForExit blocks until the instance exits or the timeout elapses.
// Returns the process exit error (nil on clean exit).
func (inst *instance) waitForExit(t *testing.T, timeout time.Duration) error {
	t.Helper()
	select {
	case <-inst.exited:
		inst.exitMu.Lock()
		err := inst.exitErr
		inst.exitMu.Unlock()
		return err
	case <-time.After(timeout):
		t.Fatalf("waitForExit: timeout after %s waiting for PID %d to exit", timeout, inst.PID)
		return nil
	}
}

// lockInfo is the JSON structure written by the primary into .pando/ipc.lock.
type lockInfo struct {
	InstanceID string    `json:"instance_id"`
	PID        int       `json:"pid"`
	PubPort    int       `json:"pub_port"`
	RPCPort    int       `json:"rpc_port"`
	StartedAt  time.Time `json:"started_at"`
}

// readLockFile reads and parses the IPC lock file from the given workdir.
// Returns nil if the file does not exist.
func readLockFile(t *testing.T, workdir string) *lockInfo {
	t.Helper()
	path := filepath.Join(workdir, ".pando", "ipc.lock")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("readLockFile: %v", err)
	}
	var info lockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("readLockFile: parse %s: %v", path, err)
	}
	return &info
}

// waitForLockFile polls until the IPC lock file appears in workdir or timeout elapses.
func waitForLockFile(t *testing.T, workdir string, timeout time.Duration) *lockInfo {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info := readLockFile(t, workdir); info != nil {
			return info
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("waitForLockFile: lock file not created within %s in %s", timeout, workdir)
	return nil
}

// waitForDB polls until the SQLite DB file appears in workdir or timeout elapses.
func waitForDB(t *testing.T, workdir string, timeout time.Duration) string {
	t.Helper()
	dbPath := filepath.Join(workdir, ".pando", "pando.db")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(dbPath); err == nil {
			return dbPath
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("waitForDB: database not created within %s in %s", timeout, workdir)
	return ""
}
