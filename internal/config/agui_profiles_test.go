package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
)

// PANDO-US-0012: "[AGUI.Profiles.<name>] config schema and validation".
//
// These tests cover the story's acceptance criteria directly; loadTempConfig
// (mesnada_delegation_test.go) is reused for the cases where Load is
// expected to succeed.

// TestAGUIProfileUnknownBaseRejected: a profile with an unknown Base fails
// config validation with an error naming both the profile and the
// offending base.
func TestAGUIProfileUnknownBaseRejected(t *testing.T) {
	isolateGlobalConfig(t)
	tmpDir := t.TempDir()
	body := "[AGUI.Profiles.backlog]\nBase = \"ghost-agent\"\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".pando.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })

	_, err := Load(tmpDir, false)
	if err == nil {
		t.Fatal("expected Load to fail validation for an unknown Base")
	}
	if !strings.Contains(err.Error(), "backlog") || !strings.Contains(err.Error(), "ghost-agent") {
		t.Fatalf("error %q must name both the profile (%q) and the offending base (%q)", err, "backlog", "ghost-agent")
	}
}

// TestAGUIProfileUnknownKeyRejected: an unknown key inside
// [AGUI.Profiles.<name>] is refused with a named error, not dropped -- unlike
// a stray top-level agent entry, which config load silently prunes.
func TestAGUIProfileUnknownKeyRejected(t *testing.T) {
	isolateGlobalConfig(t)
	tmpDir := t.TempDir()
	body := "[AGUI.Profiles.backlog]\nBase = \"coder\"\nPersnoa = \"oops\"\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".pando.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })

	_, err := Load(tmpDir, false)
	if err == nil {
		t.Fatal("expected Load to fail validation for an unrecognized profile key")
	}
	if !strings.Contains(err.Error(), "backlog") || !strings.Contains(strings.ToLower(err.Error()), "persnoa") {
		t.Fatalf("error %q must name both the profile (%q) and the unknown key (%q)", err, "backlog", "persnoa")
	}
}

// TestAGUIProfileNameCollisionRejected: a profile whose name equals a
// KnownAgentNames entry is refused, since the adapter resolves one
// route-name namespace against both profiles and built-in agents.
func TestAGUIProfileNameCollisionRejected(t *testing.T) {
	isolateGlobalConfig(t)
	tmpDir := t.TempDir()
	body := "[AGUI.Profiles.coder]\nBase = \"task\"\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".pando.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })

	_, err := Load(tmpDir, false)
	if err == nil {
		t.Fatal("expected Load to fail validation for a profile name colliding with a built-in agent")
	}
	if !strings.Contains(err.Error(), "coder") {
		t.Fatalf("error %q must name the colliding profile/agent %q", err, "coder")
	}
}

// TestAGUIProfilesAbsentLoadsUnchanged: a config with no Profiles block loads
// byte-identical to today -- AGUI.Profiles stays nil, not an empty map.
func TestAGUIProfilesAbsentLoadsUnchanged(t *testing.T) {
	loadTempConfig(t, "[AGUI]\nEnabled = true\n")

	if cfg.AGUI.Profiles != nil {
		t.Fatalf("AGUI.Profiles = %#v, want nil when no [AGUI.Profiles.*] block is configured", cfg.AGUI.Profiles)
	}
}

// TestAgentStructAndKnownAgentNamesUnchangedByProfileStory pins the story's
// explicit "do NOT" constraints: KnownAgentNames and the Agent struct must
// not gain profile-related fields.
func TestAgentStructAndKnownAgentNamesUnchangedByProfileStory(t *testing.T) {
	wantAgents := []AgentName{
		AgentCoder, AgentSummarizer, AgentTask, AgentTitle,
		AgentCLIAssist, AgentPersonaSelector, AgentContextEnricher,
	}
	if len(KnownAgentNames) != len(wantAgents) {
		t.Fatalf("KnownAgentNames changed length: got %d, want %d", len(KnownAgentNames), len(wantAgents))
	}
	for i, name := range wantAgents {
		if KnownAgentNames[i] != name {
			t.Fatalf("KnownAgentNames[%d] = %q, want %q", i, KnownAgentNames[i], name)
		}
	}

	wantAgentFields := []string{
		"Model", "MaxTokens", "ReasoningEffort", "ThinkingMode",
		"AutoCompact", "AutoCompactThreshold", "ContextWindowOverride",
	}
	typ := reflect.TypeOf(Agent{})
	if typ.NumField() != len(wantAgentFields) {
		t.Fatalf("config.Agent field count = %d, want %d: PANDO-US-0012 must not touch this struct", typ.NumField(), len(wantAgentFields))
	}
	for i, name := range wantAgentFields {
		if typ.Field(i).Name != name {
			t.Fatalf("config.Agent field %d = %q, want %q", i, typ.Field(i).Name, name)
		}
	}
}

// TestAGUIProfilesRoundTripThroughConfigAPI: read-modify-write (the same
// updateCfgFile path every config Update* function uses) must preserve every
// AGUIProfile field, including the distinction between an absent Tools list
// and an explicit empty one.
func TestAGUIProfilesRoundTripThroughConfigAPI(t *testing.T) {
	body := `
[AGUI.Profiles.backlog]
Base = "coder"
Model = "claude-sonnet-4-20250514"
Persona = "backlog-bot"
Prompt = "Stay terse."
Tools = ["gintrack__*"]
DenyTools = ["bash"]
Mesnada = false

[AGUI.Profiles.docs]
Base = "task"
Tools = []

[AGUI.Profiles.plain]
Base = "task"
`
	configPath := loadTempConfig(t, body)

	// A no-op read-modify-write: exercises the exact persistence path every
	// config Update* function uses, without touching AGUI.Profiles itself.
	if err := updateCfgFile(func(c *Config) {}); err != nil {
		t.Fatalf("updateCfgFile: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read back persisted config: %v", err)
	}
	var reloaded Config
	if err := toml.Unmarshal(data, &reloaded); err != nil {
		t.Fatalf("unmarshal persisted config: %v", err)
	}

	backlog, ok := reloaded.AGUI.Profiles["backlog"]
	if !ok {
		t.Fatal("backlog profile missing after round-trip")
	}
	if backlog.Base != AgentCoder {
		t.Fatalf("backlog.Base = %q, want %q", backlog.Base, AgentCoder)
	}
	if string(backlog.Model) != "claude-sonnet-4-20250514" {
		t.Fatalf("backlog.Model = %q, want claude-sonnet-4-20250514", backlog.Model)
	}
	if backlog.Persona != "backlog-bot" {
		t.Fatalf("backlog.Persona = %q, want backlog-bot", backlog.Persona)
	}
	if backlog.Prompt != "Stay terse." {
		t.Fatalf("backlog.Prompt = %q, want %q", backlog.Prompt, "Stay terse.")
	}
	if backlog.Tools == nil || len(*backlog.Tools) != 1 || (*backlog.Tools)[0] != "gintrack__*" {
		t.Fatalf("backlog.Tools = %v, want [gintrack__*]", backlog.Tools)
	}
	if backlog.DenyTools == nil || len(*backlog.DenyTools) != 1 || (*backlog.DenyTools)[0] != "bash" {
		t.Fatalf("backlog.DenyTools = %v, want [bash]", backlog.DenyTools)
	}
	if backlog.Mesnada == nil || *backlog.Mesnada != false {
		t.Fatalf("backlog.Mesnada = %v, want false", backlog.Mesnada)
	}

	docs, ok := reloaded.AGUI.Profiles["docs"]
	if !ok {
		t.Fatal("docs profile missing after round-trip")
	}
	if docs.Tools == nil {
		t.Fatal("docs.Tools must round-trip as a non-nil empty list (explicit []), distinct from an absent one")
	}
	if len(*docs.Tools) != 0 {
		t.Fatalf("docs.Tools = %v, want empty", *docs.Tools)
	}

	plain, ok := reloaded.AGUI.Profiles["plain"]
	if !ok {
		t.Fatal("plain profile missing after round-trip")
	}
	if plain.Tools != nil {
		t.Fatalf("plain.Tools = %v, want nil (key was never set): must stay distinct from docs' explicit empty list", plain.Tools)
	}
	if plain.DenyTools != nil {
		t.Fatalf("plain.DenyTools = %v, want nil", plain.DenyTools)
	}
	if plain.Mesnada != nil {
		t.Fatalf("plain.Mesnada = %v, want nil (inherit default)", plain.Mesnada)
	}
}
