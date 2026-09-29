package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadSetupTestConfig loads the configuration for a fresh working directory
// under an isolated $HOME, with no project config file.
func loadSetupTestConfig(t *testing.T) (home, workDir string) {
	t.Helper()
	isolateGlobalConfig(t)
	t.Setenv("PANDO_CONFIG_PARENT_SEARCH", "false")
	// Clear any configured local runtime URL: an env base URL registers the
	// runtime as an explicit account. A runtime auto-detected on its default
	// port is fine: it must not count as a user choice.
	t.Setenv("OLLAMA_BASE_URL", "")
	t.Setenv("LLAMACPP_BASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(os.Getenv("HOME"), ".config"))
	home = os.Getenv("HOME")
	workDir = t.TempDir()
	if _, err := Load(workDir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return home, workDir
}

func TestSetupStatusFreshDirectoryNeedsSetup(t *testing.T) {
	_, workDir := loadSetupTestConfig(t)

	status, err := GetSetupStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.HasLocalConfig {
		t.Fatalf("fresh directory reported a project config: %+v", status)
	}
	if status.WorkingDir != workDir {
		t.Fatalf("workingDir = %q, want %q", status.WorkingDir, workDir)
	}
	if !status.Needed {
		t.Fatalf("fresh directory with no provider must need setup: %+v", status)
	}
}

func TestPrepareSetupScopeGlobalWritesGlobalFile(t *testing.T) {
	home, workDir := loadSetupTestConfig(t)

	path, err := PrepareSetupScope(SetupScopeGlobal)
	if err != nil {
		t.Fatalf("PrepareSetupScope(global): %v", err)
	}
	// Load may already have persisted defaults to ~/.pando.json (it does so
	// when a model registry is cached); otherwise the assistant creates
	// ~/.config/pando/.pando.toml. Either way it must be a global file.
	if path != filepath.Join(home, ".pando.json") && path != filepath.Join(home, ".config", "pando", ".pando.toml") {
		t.Fatalf("config path = %q, want a global file under %q", path, home)
	}
	want := path
	if _, err := os.Stat(filepath.Join(workDir, ".pando.toml")); !os.IsNotExist(err) {
		t.Fatalf("global scope must not create a project file (stat err %v)", err)
	}

	if err := ApplySetupRemembrances("nomic-embed-text", DefaultSetupCodeEmbeddingModel); err != nil {
		t.Fatalf("ApplySetupRemembrances: %v", err)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "CodeRankEmbed") {
		t.Fatalf("remembrances not written to the global file:\n%s", data)
	}
}

func TestPrepareSetupScopeProjectWritesProjectFile(t *testing.T) {
	home, workDir := loadSetupTestConfig(t)

	path, err := PrepareSetupScope(SetupScopeProject)
	if err != nil {
		t.Fatalf("PrepareSetupScope(project): %v", err)
	}
	want := filepath.Join(workDir, ".pando.toml")
	if path != want {
		t.Fatalf("config path = %q, want %q", path, want)
	}

	if err := ApplySetupRemembrances("nomic-embed-text", DefaultSetupCodeEmbeddingModel); err != nil {
		t.Fatalf("ApplySetupRemembrances: %v", err)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "CodeRankEmbed") {
		t.Fatalf("remembrances not written to the project file:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "pando", ".pando.toml")); !os.IsNotExist(err) {
		t.Fatalf("project scope must not create a global file (stat err %v)", err)
	}

	// A project file now applies: global scope must be refused.
	if _, err := PrepareSetupScope(SetupScopeGlobal); err == nil {
		t.Fatal("global scope accepted although a project config applies")
	}
}

func TestMarkSetupCompletedStopsAutoOpen(t *testing.T) {
	loadSetupTestConfig(t)
	if SetupCompleted() {
		t.Fatal("fresh HOME reported setup completed")
	}
	if err := MarkSetupCompleted(); err != nil {
		t.Fatal(err)
	}
	status, err := GetSetupStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Completed || status.Needed {
		t.Fatalf("completed setup must not auto-open: %+v", status)
	}
}

func TestPrepareSetupScopeRejectsUnknownScope(t *testing.T) {
	loadSetupTestConfig(t)
	if _, err := PrepareSetupScope("team"); err == nil {
		t.Fatal("unknown scope accepted")
	}
}

func TestAutoDetectedLocalRuntimeIsNotAnExplicitAccount(t *testing.T) {
	accounts := []ProviderAccount{
		{ID: "ollama", Type: "ollama"},
		{ID: "llama-cpp", Type: "llama-cpp"},
	}
	if n := countExplicitProviderAccounts(accounts); n != 0 {
		t.Fatalf("auto-detected runtimes counted as %d explicit accounts", n)
	}
	accounts = append(accounts,
		ProviderAccount{ID: "ollama-2", Type: "ollama", BaseURL: "http://gpu-box:11434"},
		ProviderAccount{ID: "copilot", Type: "copilot"},
	)
	if n := countExplicitProviderAccounts(accounts); n != 2 {
		t.Fatalf("explicit accounts = %d, want 2", n)
	}
}

func TestRollbackSetupScopeRemovesOnlyCreatedFiles(t *testing.T) {
	_, workDir := loadSetupTestConfig(t)

	created := filepath.Join(workDir, ".pando.toml")
	if err := os.WriteFile(created, []byte(DefaultConfigTemplate), 0o644); err != nil {
		t.Fatal(err)
	}
	preExisting := filepath.Join(workDir, "keep.txt")
	if err := os.WriteFile(preExisting, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rollbackSetupScope([]string{created})

	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("created config file was not removed (stat err %v)", err)
	}
	if _, err := os.Stat(preExisting); err != nil {
		t.Fatalf("unrelated file was touched: %v", err)
	}
	if Get() == nil {
		t.Fatal("configuration not restored after rollback")
	}
	if FindLocalConfigFile(workDir) != "" {
		t.Fatal("project config still applies after rollback")
	}
}

func TestGetSetupStatusNotLoadedIsRetryable(t *testing.T) {
	isolateGlobalConfig(t)
	prev := cfg
	cfg = nil
	t.Cleanup(func() { cfg = prev })
	if _, err := GetSetupStatus(); !errors.Is(err, ErrConfigNotLoaded) {
		t.Fatalf("err = %v, want ErrConfigNotLoaded", err)
	}
}
