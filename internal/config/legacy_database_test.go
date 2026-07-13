package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// legacyLayout builds a project working directory holding the obsolete
// .pando/pando.db artifacts requested by suffixes ("" is the main database).
func legacyLayout(t *testing.T, suffixes ...string) (workingDir string, legacyMain string) {
	t.Helper()

	workingDir = t.TempDir()
	legacyDir := filepath.Join(workingDir, pandoDirName)
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatalf("create legacy dir: %v", err)
	}
	legacyMain = filepath.Join(legacyDir, databaseFileName)

	for _, suffix := range suffixes {
		writeFile(t, legacyMain+suffix, "legacy"+suffix)
	}
	return workingDir, legacyMain
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be gone, stat err = %v", path, err)
	}
}

// TestMigrateLegacyDatabaseMovesAllArtifacts covers a legacy-only project: the
// main database and every sidecar move to the current data directory byte for
// byte, and nothing is left behind at the obsolete path.
func TestMigrateLegacyDatabaseMovesAllArtifacts(t *testing.T) {
	workingDir, legacyMain := legacyLayout(t, "", "-wal", "-shm", "-journal")

	result, err := MigrateLegacyProjectDatabase(workingDir, "./.pando/data")
	if err != nil {
		t.Fatalf("MigrateLegacyProjectDatabase: %v", err)
	}
	if !result.Migrated || result.Cleaned {
		t.Fatalf("result = %+v, want Migrated only", result)
	}

	currentMain := filepath.Join(workingDir, pandoDirName, "data", databaseFileName)
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		assertFileContent(t, currentMain+suffix, "legacy"+suffix)
		assertMissing(t, legacyMain+suffix)
	}
}

// TestMigrateLegacyDatabaseKeepsCurrentAndCleansLegacy covers a project holding
// both databases: the current one is authoritative and untouched, while every
// legacy artifact is removed.
func TestMigrateLegacyDatabaseKeepsCurrentAndCleansLegacy(t *testing.T) {
	workingDir, legacyMain := legacyLayout(t, "", "-wal", "-shm")

	currentMain := filepath.Join(workingDir, pandoDirName, "data", databaseFileName)
	writeFile(t, currentMain, "current")
	writeFile(t, currentMain+"-wal", "current-wal")

	result, err := MigrateLegacyProjectDatabase(workingDir, "./.pando/data")
	if err != nil {
		t.Fatalf("MigrateLegacyProjectDatabase: %v", err)
	}
	if result.Migrated || !result.Cleaned {
		t.Fatalf("result = %+v, want Cleaned only", result)
	}

	assertFileContent(t, currentMain, "current")
	assertFileContent(t, currentMain+"-wal", "current-wal")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		assertMissing(t, legacyMain+suffix)
	}
}

// TestMigrateLegacyDatabaseIsIdempotent verifies a second startup after a
// migration does nothing.
func TestMigrateLegacyDatabaseIsIdempotent(t *testing.T) {
	workingDir, _ := legacyLayout(t, "", "-wal")

	if _, err := MigrateLegacyProjectDatabase(workingDir, "./.pando/data"); err != nil {
		t.Fatalf("first migration: %v", err)
	}

	result, err := MigrateLegacyProjectDatabase(workingDir, "./.pando/data")
	if err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if result.Migrated || result.Cleaned {
		t.Fatalf("result = %+v, want a no-op", result)
	}

	currentMain := filepath.Join(workingDir, pandoDirName, "data", databaseFileName)
	assertFileContent(t, currentMain, "legacy")
	assertFileContent(t, currentMain+"-wal", "legacy-wal")
}

// TestMigrateLegacyDatabaseWithoutLegacyCreatesNothing verifies projects that
// never used the obsolete path are left exactly as they are: no data directory
// and no empty database are created here, db.Connect still owns that.
func TestMigrateLegacyDatabaseWithoutLegacyCreatesNothing(t *testing.T) {
	workingDir := t.TempDir()

	result, err := MigrateLegacyProjectDatabase(workingDir, "./.pando/data")
	if err != nil {
		t.Fatalf("MigrateLegacyProjectDatabase: %v", err)
	}
	if result.Migrated || result.Cleaned {
		t.Fatalf("result = %+v, want a no-op", result)
	}

	assertMissing(t, filepath.Join(workingDir, pandoDirName, "data"))
	assertMissing(t, filepath.Join(workingDir, pandoDirName, "data", databaseFileName))
}

// TestMigrateLegacyDatabaseSamePathIsNoOp keeps compatibility with setups whose
// data directory still is .pando: legacy and current paths are the same file.
func TestMigrateLegacyDatabaseSamePathIsNoOp(t *testing.T) {
	workingDir, legacyMain := legacyLayout(t, "", "-wal")

	result, err := MigrateLegacyProjectDatabase(workingDir, defaultDataDirectory)
	if err != nil {
		t.Fatalf("MigrateLegacyProjectDatabase: %v", err)
	}
	if result.Migrated || result.Cleaned {
		t.Fatalf("result = %+v, want a no-op", result)
	}

	assertFileContent(t, legacyMain, "legacy")
	assertFileContent(t, legacyMain+"-wal", "legacy-wal")
}

// TestMigrateLegacyDatabaseSidecarConflictFails covers a half-migrated project
// where a target sidecar exists while its legacy source is still present:
// overwriting could drop committed WAL transactions, so it must fail with the
// legacy database intact.
func TestMigrateLegacyDatabaseSidecarConflictFails(t *testing.T) {
	workingDir, legacyMain := legacyLayout(t, "", "-wal")

	currentMain := filepath.Join(workingDir, pandoDirName, "data", databaseFileName)
	writeFile(t, currentMain+"-wal", "target-wal")

	if _, err := MigrateLegacyProjectDatabase(workingDir, "./.pando/data"); err == nil {
		t.Fatal("expected an error on a sidecar conflict, got nil")
	}

	assertFileContent(t, legacyMain, "legacy")
	assertFileContent(t, legacyMain+"-wal", "legacy-wal")
	assertFileContent(t, currentMain+"-wal", "target-wal")
	assertMissing(t, currentMain)
}

// TestMigrateLegacyDatabaseRejectsDirectoryArtifact verifies an unexpected
// directory at a database artifact path is reported instead of removed.
func TestMigrateLegacyDatabaseRejectsDirectoryArtifact(t *testing.T) {
	workingDir, legacyMain := legacyLayout(t, "")

	currentMain := filepath.Join(workingDir, pandoDirName, "data", databaseFileName)
	writeFile(t, currentMain, "current")
	if err := os.MkdirAll(legacyMain+"-wal", 0o700); err != nil {
		t.Fatalf("create directory artifact: %v", err)
	}

	if _, err := MigrateLegacyProjectDatabase(workingDir, "./.pando/data"); err == nil {
		t.Fatal("expected an error for a directory artifact, got nil")
	}

	if info, err := os.Stat(legacyMain + "-wal"); err != nil || !info.IsDir() {
		t.Fatalf("directory artifact was removed: err = %v", err)
	}
}

// TestLoadMigratesLegacyDatabase verifies the startup wiring: config.Load moves
// the obsolete database before anything can open SQLite at the current path.
func TestLoadMigratesLegacyDatabase(t *testing.T) {
	isolateGlobalConfig(t)

	workingDir, legacyMain := legacyLayout(t, "", "-wal")
	configPath := filepath.Join(workingDir, ".pando.toml")
	if err := os.WriteFile(configPath, []byte("[Data]\nDirectory = './.pando/data'\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })

	loaded, err := Load(workingDir, false)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded.Data.Directory != "./.pando/data" {
		t.Fatalf("data directory = %q, want ./.pando/data", loaded.Data.Directory)
	}

	currentMain := filepath.Join(workingDir, pandoDirName, "data", databaseFileName)
	assertFileContent(t, currentMain, "legacy")
	assertFileContent(t, currentMain+"-wal", "legacy-wal")
	assertMissing(t, legacyMain)
	assertMissing(t, legacyMain+"-wal")
}
