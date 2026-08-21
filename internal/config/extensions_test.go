package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// TestExtensionEntriesFlattensViperNesting is the regression test for the way
// Viper handles dotted map keys: it uses "." as its key delimiter, so
// [Extensions.Entries."memory.sink.corp"] arrives as nested maps and a plain
// map[string]ExtensionEntry would silently decode to nothing.
func TestExtensionEntriesFlattensViperNesting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".pando.toml")
	content := `
[Extensions]
Disabled = ["tools.acme.legacy"]

[Extensions.Entries."memory.sink.corp"]
Enabled = true

[Extensions.Entries."memory.sink.corp".Config]
Endpoint = "https://remembrances.corp.internal"
Retries = 3

[Extensions.Entries."tools.acme.jira"]
Enabled = false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config: %v", err)
	}

	var cfg struct {
		Extensions ExtensionsConfig `mapstructure:"extensions"`
	}
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	entries := cfg.Extensions.ExtensionEntries()
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %#v", len(entries), entries)
	}

	corp, ok := entries["memory.sink.corp"]
	if !ok {
		t.Fatalf("dotted ID was not reassembled: %#v", entries)
	}
	if !corp.Enabled {
		t.Error("Enabled was not decoded")
	}
	if got := corp.Config["endpoint"]; got != "https://remembrances.corp.internal" {
		t.Errorf("Config[endpoint] = %v", got)
	}
	if got := corp.Config["retries"]; got != int64(3) {
		t.Errorf("Config[retries] = %v (%T), want int64(3)", got, got)
	}

	jira, ok := entries["tools.acme.jira"]
	if !ok {
		t.Fatal("second entry missing")
	}
	if jira.Enabled {
		t.Error("Enabled=false was decoded as true")
	}

	if len(cfg.Extensions.Disabled) != 1 || cfg.Extensions.Disabled[0] != "tools.acme.legacy" {
		t.Errorf("Disabled = %v", cfg.Extensions.Disabled)
	}
}

// TestExtensionEntriesNestedAndParentIDs checks that configuring both an ID and
// a longer ID sharing its prefix keeps both.
func TestExtensionEntriesNestedAndParentIDs(t *testing.T) {
	cfg := ExtensionsConfig{Entries: map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"enabled": true,
				"c": map[string]any{
					"enabled": true,
					"config":  map[string]any{"k": "v"},
				},
			},
		},
	}}

	entries := cfg.ExtensionEntries()
	if _, ok := entries["a.b"]; !ok {
		t.Errorf("parent ID a.b missing: %#v", entries)
	}
	child, ok := entries["a.b.c"]
	if !ok {
		t.Fatalf("child ID a.b.c missing: %#v", entries)
	}
	if child.Config["k"] != "v" {
		t.Errorf("child config not decoded: %#v", child)
	}
}

func TestExtensionEntriesEmpty(t *testing.T) {
	var cfg ExtensionsConfig
	if got := cfg.ExtensionEntries(); len(got) != 0 {
		t.Errorf("empty config produced %d entries", len(got))
	}
}
