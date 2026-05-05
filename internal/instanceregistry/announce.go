package instanceregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Announce writes the Entry to /tmp/pando-instances/<instanceID>.json.
// Call this on startup after determining ports and the primary/secondary role.
func Announce(entry *Entry) error {
	if err := os.MkdirAll(instancesDir, 0755); err != nil {
		return fmt.Errorf("instanceregistry: announce: create dir: %w", err)
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("instanceregistry: announce: marshal: %w", err)
	}

	fpath := entryFilePath(entry.InstanceID)
	if err := os.WriteFile(fpath, data, 0644); err != nil {
		return fmt.Errorf("instanceregistry: announce: write file: %w", err)
	}

	return nil
}

// Revoke removes the instance's JSON file from /tmp/pando-instances/.
// Call this on shutdown. It is a no-op if the file does not exist.
func Revoke(instanceID string) error {
	fpath := entryFilePath(instanceID)
	err := os.Remove(fpath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("instanceregistry: revoke: remove file: %w", err)
	}
	return nil
}

// entryFilePath returns the filesystem path for an instance's JSON file.
func entryFilePath(instanceID string) string {
	return filepath.Join(instancesDir, instanceID+".json")
}
