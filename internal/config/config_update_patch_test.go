package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
)

// These tests cover the persistence contract of the Update* write funnel
// (updateConfigFileAt): a save persists only what the mutator changed, and
// never stamps zero values for keys the file did not have. Before the fix the
// funnel re-serialised the whole Config struct, so any save (e.g. a cron edit)
// wrote `[Data] Directory = ''` into a project .pando.toml that had no [Data]
// table, and every process started afterwards failed with "data.dir is not set".

// loadProjectConfig writes body as the project .pando.toml in a fresh temp
// working dir, isolates HOME (so the developer's real global config is never
// read or written), and runs a real Load against it.
func loadProjectConfig(t *testing.T, body string) (dir, file string) {
	t.Helper()
	isolateGlobalConfig(t)
	return loadProjectConfigInIsolatedHome(t, body)
}

// loadProjectConfigInIsolatedHome is loadProjectConfig for a test that has
// already isolated HOME (and possibly written a global config into it).
func loadProjectConfigInIsolatedHome(t *testing.T, body string) (dir, file string) {
	t.Helper()
	dir = t.TempDir()
	file = filepath.Join(dir, ".pando.toml")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return dir, file
}

// readTOMLTree parses a TOML file into a generic tree.
func readTOMLTree(t *testing.T, file string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	var tree map[string]any
	if err := toml.Unmarshal(data, &tree); err != nil {
		t.Fatalf("parse %s: %v\n%s", file, err, data)
	}
	return tree
}

// reloadConfig drops the in-memory state and loads the same working dir from
// disk again, as a freshly started process would.
func reloadConfig(t *testing.T, dir string) *Config {
	t.Helper()
	cfg = nil
	viper.Reset()
	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return loaded
}

func sampleCronJobs() CronJobsConfig {
	return CronJobsConfig{
		Enabled: true,
		Jobs: []CronJob{{
			Name:     "nightly",
			Schedule: "0 3 * * *",
			Prompt:   "summarise the day",
			Enabled:  true,
		}},
	}
}

func TestUpdateCronJobsKeepsDefaultDataDirectoryInMinimalFile(t *testing.T) {
	dir, file := loadProjectConfig(t, "[TUI]\nTheme = 'dracula'\n")
	// Load itself may persist a detected default (ensureEvaluatorDefaultModel
	// picks one from whatever provider the host has), so the reference is the
	// file as Load left it, not the body written above.
	beforeSave := readTOMLTree(t, file)

	if err := UpdateCronJobs(sampleCronJobs()); err != nil {
		t.Fatalf("UpdateCronJobs: %v", err)
	}

	tree := readTOMLTree(t, file)
	if _, ok := lookupInsensitive(tree, "Data"); ok {
		data, _ := os.ReadFile(file)
		t.Fatalf("a cron save must not write a [Data] table the file did not have:\n%s", data)
	}
	// Only the changed section is new on disk, and every pre-existing key is
	// kept as it was: no zero-valued key for every other Config field.
	for key, value := range tree {
		if strings.EqualFold(key, "CronJobs") {
			continue
		}
		prev, ok := lookupInsensitive(beforeSave, key)
		if !ok {
			data, _ := os.ReadFile(file)
			t.Fatalf("unexpected top-level key %q written by a cron save:\n%s", key, data)
		}
		if !equalValues(prev, value) {
			t.Fatalf("top-level key %q changed by a cron save: %#v -> %#v", key, prev, value)
		}
	}
	if len(tree) != len(beforeSave)+1 {
		t.Fatalf("a cron save changed the set of top-level keys: before %v, after %v", beforeSave, tree)
	}

	loaded := reloadConfig(t, dir)
	if loaded.Data.Directory != defaultDataDirectory {
		t.Fatalf("Data.Directory after reload = %q, want default %q", loaded.Data.Directory, defaultDataDirectory)
	}
	if loaded.TUI.Theme != "dracula" {
		t.Fatalf("TUI.Theme after reload = %q, want dracula", loaded.TUI.Theme)
	}
	if !loaded.LLMCache.Enabled || !loaded.Skills.Enabled {
		t.Fatalf("defaults shadowed by the save: LLMCache.Enabled=%v Skills.Enabled=%v, want both true",
			loaded.LLMCache.Enabled, loaded.Skills.Enabled)
	}
	if len(loaded.CronJobs.Jobs) != 1 || loaded.CronJobs.Jobs[0].Name != "nightly" {
		t.Fatalf("CronJobs after reload = %+v, want the saved job", loaded.CronJobs)
	}
}

func TestUpdateCronJobsKeepsExplicitDataDirectory(t *testing.T) {
	dir, file := loadProjectConfig(t, "[Data]\nDirectory = './.pando/data'\n\n[TUI]\nTheme = 'dracula'\n")

	if err := UpdateCronJobs(sampleCronJobs()); err != nil {
		t.Fatalf("UpdateCronJobs: %v", err)
	}

	loaded := reloadConfig(t, dir)
	if loaded.Data.Directory != "./.pando/data" {
		data, _ := os.ReadFile(file)
		t.Fatalf("Data.Directory after reload = %q, want ./.pando/data; file:\n%s", loaded.Data.Directory, data)
	}
	if loaded.TUI.Theme != "dracula" {
		t.Fatalf("TUI.Theme after reload = %q, want dracula", loaded.TUI.Theme)
	}
}

func TestUpdateCronJobsDoesNotShadowGlobalDataDirectory(t *testing.T) {
	isolateGlobalConfig(t)
	globalData := filepath.Join(t.TempDir(), "global-data")
	globalFile := filepath.Join(os.Getenv("HOME"), ".pando.toml")
	if err := os.WriteFile(globalFile, []byte("[Data]\nDirectory = '"+globalData+"'\n"), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	dir, file := loadProjectConfigInIsolatedHome(t, "[TUI]\nTheme = 'dracula'\n")
	if got := Get().Data.Directory; got != globalData {
		t.Fatalf("precondition: Data.Directory = %q, want the global value %q", got, globalData)
	}

	if err := UpdateCronJobs(sampleCronJobs()); err != nil {
		t.Fatalf("UpdateCronJobs: %v", err)
	}

	if _, ok := lookupInsensitive(readTOMLTree(t, file), "Data"); ok {
		data, _ := os.ReadFile(file)
		t.Fatalf("the project file must not get a [Data] table copied from the global config:\n%s", data)
	}
	if loaded := reloadConfig(t, dir); loaded.Data.Directory != globalData {
		t.Fatalf("Data.Directory after reload = %q, want the global value %q", loaded.Data.Directory, globalData)
	}
}

// A mutator that sets a key the file does not have to its zero value, while
// the effective value (a default) is non-zero, is a real change and must be
// persisted: diffing the file struct alone would see false -> false.
func TestUpdatePersistsExplicitZeroOverNonZeroDefault(t *testing.T) {
	dir, file := loadProjectConfig(t, "[TUI]\nTheme = 'dracula'\n")
	if !Get().LLMCache.Enabled {
		t.Fatalf("precondition: LLMCache.Enabled should default to true")
	}

	if err := UpdateLLMCache(false); err != nil {
		t.Fatalf("UpdateLLMCache(false): %v", err)
	}

	if loaded := reloadConfig(t, dir); loaded.LLMCache.Enabled {
		data, _ := os.ReadFile(file)
		t.Fatalf("LLMCache.Enabled after reload = true, want the saved false; file:\n%s", data)
	}
	// And the unrelated default next to it is still untouched.
	if loaded := reloadConfig(t, dir); !loaded.Skills.Enabled {
		t.Fatalf("Skills.Enabled after reload = false, want the default true")
	}
}

func TestUpdatePreservesUnknownAndUnrelatedKeys(t *testing.T) {
	body := "AutoCompact = false\n\n[FutureSection]\nFoo = 'bar'\n\n[TUI]\nTheme = 'dracula'\n"
	dir, file := loadProjectConfig(t, body)

	if err := UpdateTheme("pando-dark"); err != nil {
		t.Fatalf("UpdateTheme: %v", err)
	}

	tree := readTOMLTree(t, file)
	future, ok := lookupInsensitive(tree, "FutureSection")
	if !ok {
		data, _ := os.ReadFile(file)
		t.Fatalf("a key unknown to this binary was dropped by the save:\n%s", data)
	}
	if m, _ := future.(map[string]any); m["Foo"] != "bar" {
		t.Fatalf("FutureSection = %#v, want Foo = bar", future)
	}

	loaded := reloadConfig(t, dir)
	if loaded.TUI.Theme != "pando-dark" {
		t.Fatalf("TUI.Theme after reload = %q, want pando-dark", loaded.TUI.Theme)
	}
	if loaded.AutoCompact {
		t.Fatalf("AutoCompact after reload = true, want the file's explicit false")
	}
}

// A plaintext secret already in the file is still encrypted by the next save
// (the whole-struct rewrite used to do that as a side effect), while a secret
// that only exists in the global config is never copied into the project file.
func TestUpdateEncryptsFileSecretsButNeverCopiesGlobalOnes(t *testing.T) {
	isolateGlobalConfig(t)
	globalFile := filepath.Join(os.Getenv("HOME"), ".pando.toml")
	if err := os.WriteFile(globalFile, []byte("[InternalTools]\nExaAPIKey = 'global-exa-key'\n"), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	_, file := loadProjectConfigInIsolatedHome(t, "[InternalTools]\nBraveAPIKey = 'plain-brave-key'\n")

	if err := UpdateTheme("pando-dark"); err != nil {
		t.Fatalf("UpdateTheme: %v", err)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "plain-brave-key") {
		t.Fatalf("the plaintext secret in the project file should have been encrypted:\n%s", text)
	}
	if !strings.Contains(text, encryptedValuePrefix) {
		t.Fatalf("expected an encrypted BraveAPIKey in the project file:\n%s", text)
	}
	if strings.Contains(strings.ToLower(text), "exaapikey") {
		t.Fatalf("a global-only secret leaked into the project file:\n%s", text)
	}
}

// Defence in depth: a file already damaged by the old funnel (an explicit
// empty [Data] Directory) loads with the default data directory instead of
// failing every later DB open with "data.dir is not set".
func TestLoadFallsBackToDefaultDataDirectoryWhenEmpty(t *testing.T) {
	_, _ = loadProjectConfig(t, "[Data]\nDirectory = ''\n")
	if got := Get().Data.Directory; got != defaultDataDirectory {
		t.Fatalf("Data.Directory = %q, want the default %q", got, defaultDataDirectory)
	}
}
