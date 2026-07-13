package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// databaseFileName is the SQLite main database file name used by internal/db.
const databaseFileName = "pando.db"

// databaseSidecarSuffixes are the SQLite artifacts that can accompany the main
// database file. WAL mode keeps committed transactions in -wal until a
// checkpoint, so the sidecars must travel with the main file.
var databaseSidecarSuffixes = []string{"-wal", "-shm", "-journal"}

// LegacyDatabaseMigrationResult reports what MigrateLegacyProjectDatabase did.
type LegacyDatabaseMigrationResult struct {
	// Migrated is true when the legacy database was moved to the current path.
	Migrated bool
	// Cleaned is true when legacy artifacts were deleted because the current
	// database already existed and is authoritative.
	Cleaned bool
}

// MigrateLegacyProjectDatabase moves the obsolete project-local SQLite database
// from <workingDir>/.pando/pando.db to <dataDirectory>/pando.db, or deletes the
// legacy artifacts when the current database already exists (current data always
// wins). It never opens SQLite, never overwrites a target artifact and is
// idempotent, so it is safe to call on every startup before any DB connection.
func MigrateLegacyProjectDatabase(workingDir string, dataDirectory string) (LegacyDatabaseMigrationResult, error) {
	var result LegacyDatabaseMigrationResult

	if workingDir == "" || dataDirectory == "" {
		return result, nil
	}

	legacyMain := filepath.Clean(filepath.Join(workingDir, pandoDirName, databaseFileName))

	currentDir := dataDirectory
	if !filepath.IsAbs(currentDir) {
		currentDir = filepath.Join(workingDir, currentDir)
	}
	currentMain := filepath.Clean(filepath.Join(currentDir, databaseFileName))

	// Configurations that still use .pando as the data directory have nothing
	// to migrate: the legacy path is the current path.
	if legacyMain == currentMain {
		return result, nil
	}

	legacyPaths := databaseArtifactPaths(legacyMain)
	currentPaths := databaseArtifactPaths(currentMain)

	legacyExists := make([]bool, len(legacyPaths))
	for i, path := range legacyPaths {
		exists, err := regularFileExists(path)
		if err != nil {
			return result, err
		}
		legacyExists[i] = exists
	}

	currentMainExists, err := regularFileExists(currentMain)
	if err != nil {
		return result, err
	}

	// The current database is authoritative: drop every legacy artifact.
	if currentMainExists {
		for i, path := range legacyPaths {
			if !legacyExists[i] {
				continue
			}
			if err := os.Remove(path); err != nil {
				return result, fmt.Errorf("failed to remove legacy database artifact %s: %w", path, err)
			}
			result.Cleaned = true
		}
		return result, nil
	}

	// Index 0 is always the main database file.
	if !legacyExists[0] {
		return result, nil
	}

	if err := os.MkdirAll(filepath.Dir(currentMain), 0o700); err != nil {
		return result, fmt.Errorf("failed to create data directory %s: %w", filepath.Dir(currentMain), err)
	}

	// Move the sidecars first and the main database last, so any failure leaves
	// the legacy database usable and the migration retryable.
	for i := len(legacyPaths) - 1; i >= 0; i-- {
		if !legacyExists[i] {
			continue
		}
		if err := moveDatabaseArtifact(legacyPaths[i], currentPaths[i]); err != nil {
			return result, err
		}
	}

	result.Migrated = true
	return result, nil
}

// databaseArtifactPaths returns the main database path followed by its sidecars.
func databaseArtifactPaths(mainPath string) []string {
	paths := make([]string, 0, len(databaseSidecarSuffixes)+1)
	paths = append(paths, mainPath)
	for _, suffix := range databaseSidecarSuffixes {
		paths = append(paths, mainPath+suffix)
	}
	return paths
}

// regularFileExists reports whether path holds a regular file. A directory (or
// any other non-regular entry) at a database artifact path is unexpected and
// must never be removed or overwritten.
func regularFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to inspect database artifact %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("database artifact %s is not a regular file", path)
	}
	return true, nil
}

// moveDatabaseArtifact moves src to dst without ever overwriting dst.
func moveDatabaseArtifact(src string, dst string) error {
	dstExists, err := regularFileExists(dst)
	if err != nil {
		return err
	}
	if dstExists {
		return fmt.Errorf("cannot migrate legacy database artifact %s: %s already exists", src, dst)
	}

	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Rename fails across filesystems (a data directory can be a mount or a
	// symlink to another device), so fall back to copy-then-remove.
	if err := copyDatabaseArtifact(src, dst); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("failed to remove legacy database artifact %s after copy: %w", src, err)
	}
	return nil
}

// copyDatabaseArtifact copies src to a newly created dst, failing if dst exists.
func copyDatabaseArtifact(src string, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open legacy database artifact %s: %w", src, err)
	}
	defer source.Close()

	target, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create database artifact %s: %w", dst, err)
	}

	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		os.Remove(dst)
		return fmt.Errorf("failed to copy legacy database artifact %s to %s: %w", src, dst, err)
	}
	if err := target.Sync(); err != nil {
		target.Close()
		os.Remove(dst)
		return fmt.Errorf("failed to flush database artifact %s: %w", dst, err)
	}
	if err := target.Close(); err != nil {
		os.Remove(dst)
		return fmt.Errorf("failed to close database artifact %s: %w", dst, err)
	}
	return nil
}
