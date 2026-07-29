package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/viper"
)

func writeConfigFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestFindLocalConfigFilePrefersWorkingDir(t *testing.T) {
	isolateGlobalConfig(t)

	root := t.TempDir()
	nested := filepath.Join(root, "projects", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeConfigFile(t, root, ".pando.toml", "Debug = true\n")
	want := writeConfigFile(t, nested, ".pando.toml", "Debug = false\n")

	if got := FindLocalConfigFile(nested); got != want {
		t.Fatalf("FindLocalConfigFile = %q, want %q", got, want)
	}
}

func TestFindLocalConfigFileWalksUpToParent(t *testing.T) {
	isolateGlobalConfig(t)

	root := t.TempDir()
	nested := filepath.Join(root, "workspace", "projects", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	want := writeConfigFile(t, root, ".pando.toml", "Debug = true\n")

	if got := FindLocalConfigFile(nested); got != want {
		t.Fatalf("FindLocalConfigFile = %q, want %q", got, want)
	}
	if got := FindLocalConfigDir(nested); got != filepath.Clean(root) {
		t.Fatalf("FindLocalConfigDir = %q, want %q", got, root)
	}
}

func TestFindLocalConfigFilePrefersTomlOverJSON(t *testing.T) {
	isolateGlobalConfig(t)

	root := t.TempDir()
	writeConfigFile(t, root, ".pando.json", "{}")
	want := writeConfigFile(t, root, ".pando.toml", "Debug = true\n")

	if got := FindLocalConfigFile(root); got != want {
		t.Fatalf("FindLocalConfigFile = %q, want %q", got, want)
	}
}

func TestFindLocalConfigFileSkipsUnwritableCandidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file permissions required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	isolateGlobalConfig(t)

	root := t.TempDir()
	nested := filepath.Join(root, "readonly", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	want := writeConfigFile(t, root, ".pando.toml", "Debug = true\n")

	readOnly := writeConfigFile(t, filepath.Join(root, "readonly"), ".pando.toml", "Debug = false\n")
	if err := os.Chmod(readOnly, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if got := FindLocalConfigFile(nested); got != want {
		t.Fatalf("FindLocalConfigFile = %q, want %q (read-only candidate must be skipped)", got, want)
	}
}

func TestFindLocalConfigFileStopsAtHome(t *testing.T) {
	above := t.TempDir()
	home := filepath.Join(above, "home")
	nested := filepath.Join(home, "projects", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	// A config above the home directory must never be picked up as a local one.
	writeConfigFile(t, above, ".pando.toml", "Debug = true\n")

	if got := FindLocalConfigFile(nested); got != "" {
		t.Fatalf("FindLocalConfigFile = %q, want \"\" (search must stop at home)", got)
	}
}

func TestFindLocalConfigFileParentSearchDisabled(t *testing.T) {
	isolateGlobalConfig(t)
	t.Setenv("PANDO_CONFIG_PARENT_SEARCH", "false")

	root := t.TempDir()
	nested := filepath.Join(root, "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeConfigFile(t, root, ".pando.toml", "Debug = true\n")

	if got := FindLocalConfigFile(nested); got != "" {
		t.Fatalf("FindLocalConfigFile = %q, want \"\" when parent search is disabled", got)
	}
}

func TestLoadMergesParentDirectoryConfig(t *testing.T) {
	isolateGlobalConfig(t)

	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	root := t.TempDir()
	nested := filepath.Join(root, "projects", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	want := writeConfigFile(t, root, ".pando.toml", "AutoCompact = false\n")

	loaded, err := Load(nested, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AutoCompact {
		t.Fatal("AutoCompact = true, want false from the parent-directory config")
	}
	if loaded.WorkingDir != nested {
		t.Fatalf("WorkingDir = %q, want %q", loaded.WorkingDir, nested)
	}

	got, err := ResolveConfigFilePath()
	if err != nil {
		t.Fatalf("ResolveConfigFilePath: %v", err)
	}
	if got != want {
		t.Fatalf("ResolveConfigFilePath = %q, want %q", got, want)
	}
}
