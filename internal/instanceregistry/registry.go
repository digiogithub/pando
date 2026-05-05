package instanceregistry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// instancesDir is the directory where instance JSON files are written.
// It is a var (not const) so tests can override it.
var instancesDir = "/tmp/pando-instances"

// Registry scans instancesDir for instance JSON files and provides
// methods to discover live Pando instances.
type Registry struct{}

// New returns a new Registry.
func New() *Registry {
	return &Registry{}
}

// List returns all live instances whose PID is still running.
// Stale entries (process no longer running) are pruned automatically.
func (r *Registry) List() ([]*Entry, error) {
	if err := os.MkdirAll(instancesDir, 0755); err != nil {
		return nil, fmt.Errorf("instanceregistry: list: create dir: %w", err)
	}

	dirEntries, err := os.ReadDir(instancesDir)
	if err != nil {
		return nil, fmt.Errorf("instanceregistry: list: read dir: %w", err)
	}

	var live []*Entry
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}

		fpath := filepath.Join(instancesDir, de.Name())
		data, err := os.ReadFile(fpath)
		if err != nil {
			// File may have been concurrently removed; skip it.
			continue
		}

		var entry Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			// Corrupt file; skip.
			continue
		}

		if !isProcessAlive(entry.PID) {
			// Best-effort cleanup of stale file.
			_ = os.Remove(fpath)
			continue
		}

		live = append(live, &entry)
	}

	return live, nil
}

// ListByPath returns live instances with the given working directory path.
func (r *Registry) ListByPath(absPath string) ([]*Entry, error) {
	all, err := r.List()
	if err != nil {
		return nil, err
	}

	var matched []*Entry
	for _, e := range all {
		if e.Path == absPath {
			matched = append(matched, e)
		}
	}
	return matched, nil
}

// Get returns the entry for the given instanceID, or nil if not found/alive.
func (r *Registry) Get(instanceID string) (*Entry, error) {
	fpath := entryFilePath(instanceID)
	data, err := os.ReadFile(fpath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("instanceregistry: get: read file: %w", err)
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("instanceregistry: get: unmarshal: %w", err)
	}

	if !isProcessAlive(entry.PID) {
		// Best-effort cleanup.
		_ = os.Remove(fpath)
		return nil, nil
	}

	return &entry, nil
}

// isProcessAlive reports whether the process with the given PID is running.
// It uses signal 0 which does not send an actual signal but checks whether
// the process exists and the caller has permission to signal it.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; send signal 0 to check liveness.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
