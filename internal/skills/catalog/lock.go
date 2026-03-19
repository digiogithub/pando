package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const lockFileName = "catalog-lock.json"

// LockEntry holds metadata for a single installed catalog skill.
type LockEntry struct {
	Name        string    `json:"name"`
	Source      string    `json:"source"`
	SkillID     string    `json:"skillId"`
	Scope       string    `json:"scope"`
	InstalledAt time.Time `json:"installedAt"`
	Checksum    string    `json:"checksum"`
}

// CatalogLock represents the catalog-lock.json file.
type CatalogLock struct {
	Version string               `json:"version"`
	Skills  map[string]LockEntry `json:"skills"`
}

// ChecksumContent returns the SHA256 hex digest of the given content.
func ChecksumContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// ReadLock reads {dir}/catalog-lock.json. If the file does not exist, it returns
// an empty lock with version "1".
func ReadLock(dir string) (*CatalogLock, error) {
	path := filepath.Join(dir, lockFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &CatalogLock{Version: "1", Skills: make(map[string]LockEntry)}, nil
		}
		return nil, fmt.Errorf("catalog: read lock: %w", err)
	}

	var lock CatalogLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("catalog: parse lock: %w", err)
	}

	if lock.Skills == nil {
		lock.Skills = make(map[string]LockEntry)
	}

	return &lock, nil
}

// WriteLock serialises lock to {dir}/catalog-lock.json, creating parent directories
// as needed.
func WriteLock(dir string, lock *CatalogLock) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("catalog: create lock dir: %w", err)
	}

	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("catalog: marshal lock: %w", err)
	}

	path := filepath.Join(dir, lockFileName)
	return os.WriteFile(path, data, 0o644)
}

// AddLockEntry reads the lock, adds (or replaces) entry.Name, and writes it back.
func AddLockEntry(dir string, entry LockEntry) error {
	lock, err := ReadLock(dir)
	if err != nil {
		return err
	}
	lock.Skills[entry.Name] = entry
	return WriteLock(dir, lock)
}

// RemoveLockEntry reads the lock, removes the entry for skillName, and writes it back.
func RemoveLockEntry(dir string, skillName string) error {
	lock, err := ReadLock(dir)
	if err != nil {
		return err
	}
	delete(lock.Skills, skillName)
	return WriteLock(dir, lock)
}
