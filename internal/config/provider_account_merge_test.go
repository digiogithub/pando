package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// TestLoadMergesGlobalAndProjectProviderAccounts verifies that a project config
// defining its own providerAccounts does not hide the global (profile) accounts.
// Before the fix, viper's slice merge replaced the global list entirely, so the
// global account disappeared from the runtime view and its models could neither
// be removed nor distinguished from the project account's models.
func TestLoadMergesGlobalAndProjectProviderAccounts(t *testing.T) {
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	homeDir := t.TempDir()
	projectDir := t.TempDir()

	globalCfg := `{
  "providerAccounts": [
    {"id": "anthropic-global", "displayName": "Global Anthropic", "type": "anthropic", "apiKey": "sk-global"}
  ]
}`
	if err := os.WriteFile(filepath.Join(homeDir, ".pando.json"), []byte(globalCfg), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	projectCfg := `{
  "providerAccounts": [
    {"id": "anthropic-project", "displayName": "Project Anthropic", "type": "anthropic", "apiKey": "sk-project"}
  ]
}`
	if err := os.WriteFile(filepath.Join(projectDir, ".pando.json"), []byte(projectCfg), 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	loaded, err := Load(projectDir, false)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	byID := make(map[string]ProviderAccount)
	for _, acc := range loaded.ProviderAccounts {
		byID[acc.ID] = acc
	}

	project, ok := byID["anthropic-project"]
	if !ok {
		t.Fatalf("project account missing; accounts = %+v", loaded.ProviderAccounts)
	}
	global, ok := byID["anthropic-global"]
	if !ok {
		t.Fatalf("global account missing (was hidden by project config); accounts = %+v", loaded.ProviderAccounts)
	}
	if project.APIKey != "sk-project" {
		t.Fatalf("project apiKey = %q, want sk-project", project.APIKey)
	}
	if global.APIKey != "sk-global" {
		t.Fatalf("global apiKey = %q, want sk-global", global.APIKey)
	}

	// Project account must come first so it takes precedence for
	// ResolveProviderAccountForType (first-of-type wins).
	if loaded.ProviderAccounts[0].ID != "anthropic-project" {
		t.Fatalf("expected project account first, got %q", loaded.ProviderAccounts[0].ID)
	}
}

// TestLoadProjectAccountOverridesGlobalByID verifies that when both scopes define
// an account with the same ID, the project definition wins and the entry is not
// duplicated.
func TestLoadProjectAccountOverridesGlobalByID(t *testing.T) {
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	homeDir := t.TempDir()
	projectDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(homeDir, ".pando.json"),
		[]byte(`{"providerAccounts":[{"id":"anthropic","type":"anthropic","apiKey":"sk-global"}]}`), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".pando.json"),
		[]byte(`{"providerAccounts":[{"id":"anthropic","type":"anthropic","apiKey":"sk-project"}]}`), 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	loaded, err := Load(projectDir, false)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	count := 0
	for _, acc := range loaded.ProviderAccounts {
		if acc.ID == "anthropic" {
			count++
			if acc.APIKey != "sk-project" {
				t.Fatalf("same-ID account apiKey = %q, want sk-project (project should win)", acc.APIKey)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 'anthropic' account after dedup, got %d", count)
	}
}

func TestMergeProviderAccountMaps(t *testing.T) {
	global := []map[string]any{
		{"id": "a", "apiKey": "g-a"},
		{"id": "b", "apiKey": "g-b"},
	}
	local := []map[string]any{
		{"id": "b", "apiKey": "l-b"}, // overrides global b
		{"id": "c", "apiKey": "l-c"},
	}

	merged := mergeProviderAccountMaps(global, local)
	if len(merged) != 3 {
		t.Fatalf("merged len = %d, want 3 (a,b,c)", len(merged))
	}
	// Local first.
	if got := providerAccountMapID(merged[0]); got != "b" {
		t.Fatalf("merged[0] id = %q, want b", got)
	}
	if got := providerAccountMapID(merged[1]); got != "c" {
		t.Fatalf("merged[1] id = %q, want c", got)
	}
	if got := providerAccountMapID(merged[2]); got != "a" {
		t.Fatalf("merged[2] id = %q, want a", got)
	}
	// b must be the local definition.
	if merged[0]["apiKey"] != "l-b" {
		t.Fatalf("merged b apiKey = %v, want l-b", merged[0]["apiKey"])
	}

	if got := mergeProviderAccountMaps(nil, nil); got != nil {
		t.Fatalf("merge of empty scopes = %v, want nil", got)
	}
}
