package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeSandboxConfigs writes an optional global ~/.pando.toml and an optional
// project .pando.toml, returning the project directory. HOME must already be
// isolated (isolateGlobalConfig).
func writeSandboxConfigs(t *testing.T, global, project string) string {
	t.Helper()
	if global != "" {
		if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".pando.toml"), []byte(global), 0o600); err != nil {
			t.Fatalf("write global config: %v", err)
		}
	}
	dir := t.TempDir()
	if project != "" {
		if err := os.WriteFile(filepath.Join(dir, ".pando.toml"), []byte(project), 0o600); err != nil {
			t.Fatalf("write project config: %v", err)
		}
	}
	return dir
}

func TestSandboxEmptyConfigIsZeroValue(t *testing.T) {
	isolateGlobalConfig(t)
	dir := writeSandboxConfigs(t, "", "")

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.Sandbox, SandboxConfig{}) {
		t.Fatalf("Sandbox = %+v, want the zero value (sandbox on, defaults)", loaded.Sandbox)
	}
}

func TestSandboxGlobalConfigIsLoaded(t *testing.T) {
	isolateGlobalConfig(t)
	dir := writeSandboxConfigs(t, `
[Sandbox]
Mode = "strict"
Network = "restricted"
DenyPaths = ["~/.ssh"]
UseBwrap = "never"
[Sandbox.Env]
Inherit = "core"
`, "")

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sb := loaded.Sandbox
	if sb.Mode != "strict" || sb.Network != "restricted" || sb.UseBwrap != "never" || sb.Env.Inherit != "core" {
		t.Fatalf("Sandbox = %+v, want the global values", sb)
	}
	if !reflect.DeepEqual(sb.DenyPaths, []string{"~/.ssh"}) {
		t.Fatalf("DenyPaths = %v", sb.DenyPaths)
	}
}

func TestSandboxProjectCannotLoosen(t *testing.T) {
	isolateGlobalConfig(t)
	dir := writeSandboxConfigs(t, `
[Sandbox]
Mode = "strict"
Network = "restricted"
AutoAllowBashDisabled = true
WritableRoots = ["/opt/global-cache"]
UseBwrap = "always"
[Sandbox.Env]
Inherit = "core"
`, `
[Sandbox]
Disabled = true
Mode = "workspace-write"
Network = "allowed"
WritableRoots = ["/etc", "sub", "../escape", "~/outside"]
ReadOnlyRoots = ["/home/other/.ssh"]
DenyPaths = ["**/.env"]
UseBwrap = "never"
AllowAutoEscalation = true
[Sandbox.Env]
Inherit = "all"
KeepSecrets = true
Keep = ["GITHUB_TOKEN"]
`)

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sb := loaded.Sandbox
	if sb.Disabled {
		t.Fatal("project Disabled=true was honoured")
	}
	if sb.Mode != "strict" {
		t.Fatalf("Mode = %q, want strict (project cannot loosen)", sb.Mode)
	}
	if sb.Network != "restricted" {
		t.Fatalf("Network = %q, want restricted", sb.Network)
	}
	if !sb.AutoAllowBashDisabled {
		t.Fatal("AutoAllowBashDisabled lost")
	}
	if sb.UseBwrap != "always" {
		t.Fatalf("UseBwrap = %q, want always", sb.UseBwrap)
	}
	if sb.AllowAutoEscalation {
		t.Fatal("project AllowAutoEscalation=true was honoured")
	}
	if sb.Env.Inherit != "core" || sb.Env.KeepSecrets || len(sb.Env.Keep) != 0 {
		t.Fatalf("Env = %+v, want the global env policy untouched", sb.Env)
	}
	wantRoots := []string{"/opt/global-cache", filepath.Join(dir, "sub")}
	if !reflect.DeepEqual(sb.WritableRoots, wantRoots) {
		t.Fatalf("WritableRoots = %v, want %v (only in-workspace project roots)", sb.WritableRoots, wantRoots)
	}
	if len(sb.ReadOnlyRoots) != 0 {
		t.Fatalf("ReadOnlyRoots = %v, want none (project root outside the workspace)", sb.ReadOnlyRoots)
	}
	if !reflect.DeepEqual(sb.DenyPaths, []string{"**/.env"}) {
		t.Fatalf("DenyPaths = %v, want the project deny path added", sb.DenyPaths)
	}
}

func TestSandboxProjectCanTighten(t *testing.T) {
	isolateGlobalConfig(t)
	dir := writeSandboxConfigs(t, `
[Sandbox]
DenyPaths = ["~/.aws"]
`, `
[Sandbox]
Mode = "read-only"
Network = "restricted"
AutoAllowBashDisabled = true
CacheDirsDisabled = true
DenyPaths = ["**/.env"]
ExtendTo = ["mcp"]
[Sandbox.Env]
Inherit = "none"
Exclude = ["MY_*"]
`)

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sb := loaded.Sandbox
	if sb.Mode != "read-only" || sb.Network != "restricted" || !sb.AutoAllowBashDisabled || !sb.CacheDirsDisabled {
		t.Fatalf("Sandbox = %+v, want the tighter project values", sb)
	}
	if !reflect.DeepEqual(sb.DenyPaths, []string{"~/.aws", "**/.env"}) {
		t.Fatalf("DenyPaths = %v, want the union", sb.DenyPaths)
	}
	if !reflect.DeepEqual(sb.ExtendTo, []string{"mcp"}) || sb.Env.Inherit != "none" ||
		!reflect.DeepEqual(sb.Env.Exclude, []string{"MY_*"}) {
		t.Fatalf("Sandbox = %+v, want project extendTo/env tightening", sb)
	}
}

func TestSandboxProjectCanEnableGloballyDisabledSandbox(t *testing.T) {
	isolateGlobalConfig(t)
	dir := writeSandboxConfigs(t, "[Sandbox]\nDisabled = true\n", "[Sandbox]\nMode = \"strict\"\n")

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Sandbox.Disabled || loaded.Sandbox.Mode != "strict" {
		t.Fatalf("Sandbox = %+v, want enabled strict (tightening is allowed)", loaded.Sandbox)
	}
}

func TestSandboxProjectCannotSwapReadOnlyForStrict(t *testing.T) {
	isolateGlobalConfig(t)
	dir := writeSandboxConfigs(t, "[Sandbox]\nMode = \"read-only\"\n", "[Sandbox]\nMode = \"strict\"\n")

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Sandbox.Mode != "read-only" {
		t.Fatalf("Mode = %q, want read-only (strict is not a superset of read-only)", loaded.Sandbox.Mode)
	}
}

// PANDO_SANDBOX is a mode override applied by sandbox.Resolve. Left to viper's
// AutomaticEnv it would shadow every sandbox.* key from the files.
func TestSandboxEnvVarDoesNotShadowFileConfig(t *testing.T) {
	isolateGlobalConfig(t)
	t.Setenv(SandboxEnvVar, "off")
	dir := writeSandboxConfigs(t, `
[Sandbox]
Mode = "strict"
DenyPaths = ["~/.ssh"]
`, "")

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Sandbox.Mode != "strict" || !reflect.DeepEqual(loaded.Sandbox.DenyPaths, []string{"~/.ssh"}) {
		t.Fatalf("Sandbox = %+v, want the file values (env is applied later by sandbox.Resolve)", loaded.Sandbox)
	}
}

func TestSandboxOverlayBeatsProject(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)
	dir := writeSandboxConfigs(t, "", "[Sandbox]\nMode = \"read-only\"\nNetwork = \"restricted\"\n")

	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		return Overlay{
			Source: "test",
			Values: map[string]any{"sandbox": map[string]any{"mode": "workspace-write"}},
			Locked: []string{"sandbox.mode"},
		}, nil
	}))

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Sandbox.Mode != "workspace-write" {
		t.Fatalf("Mode = %q, want the locked overlay value", loaded.Sandbox.Mode)
	}
	if loaded.Sandbox.Network != "restricted" {
		t.Fatalf("Network = %q, want the project tightening kept for untouched fields", loaded.Sandbox.Network)
	}
}

func TestUpdateSandboxPersistsGlobally(t *testing.T) {
	isolateGlobalConfig(t)
	globalPath := filepath.Join(os.Getenv("HOME"), ".pando.toml")
	dir := writeSandboxConfigs(t, "Debug = false\n", "[Sandbox]\nNetwork = \"restricted\"\n")

	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Snapshot after Load: Load itself may normalise the project file.
	projectBefore, _ := os.ReadFile(filepath.Join(dir, ".pando.toml"))
	if err := UpdateSandbox(SandboxConfig{Mode: " Strict ", WritableRoots: []string{"/a", " /a ", ""}}); err != nil {
		t.Fatalf("UpdateSandbox: %v", err)
	}

	got := Get().Sandbox
	if got.Mode != "strict" || !reflect.DeepEqual(got.WritableRoots, []string{"/a"}) {
		t.Fatalf("in-memory Sandbox = %+v, want the normalized value", got)
	}
	if got.Network != "restricted" {
		t.Fatalf("in-memory Network = %q, want the project tightening re-applied", got.Network)
	}

	globalData, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	if !strings.Contains(string(globalData), "strict") {
		t.Fatalf("global config does not contain the new mode:\n%s", globalData)
	}
	projectAfter, _ := os.ReadFile(filepath.Join(dir, ".pando.toml"))
	if string(projectAfter) != string(projectBefore) {
		t.Fatalf("project config was modified:\n%s", projectAfter)
	}

	if err := UpdateSandbox(SandboxConfig{Mode: "bogus"}); err == nil {
		t.Fatal("UpdateSandbox accepted an invalid mode")
	}
	if Get().Sandbox.Mode != "strict" {
		t.Fatal("a rejected update changed the in-memory config")
	}
}

func TestUpdateSandboxRefusesLockedChange(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)
	dir := writeSandboxConfigs(t, "Debug = false\n", "")

	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		return Overlay{Source: "test", Locked: []string{"sandbox.disabled"}}, nil
	}))
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := UpdateSandbox(SandboxConfig{Disabled: true}); err == nil {
		t.Fatal("UpdateSandbox changed a locked sandbox.disabled")
	}
	if Get().Sandbox.Disabled {
		t.Fatal("a refused update changed the in-memory config")
	}
}

func TestNormalizeSandboxConfig(t *testing.T) {
	got, err := NormalizeSandboxConfig(SandboxConfig{
		Mode: " READ-ONLY ", Network: "Restricted", UseBwrap: "Auto",
		ExtendTo: []string{"MCP", "mcp", ""}, Env: SandboxEnvConfig{Inherit: "Core"},
	})
	if err != nil {
		t.Fatalf("NormalizeSandboxConfig: %v", err)
	}
	if got.Mode != "read-only" || got.Network != "restricted" || got.UseBwrap != "auto" ||
		!reflect.DeepEqual(got.ExtendTo, []string{"mcp"}) || got.Env.Inherit != "core" {
		t.Fatalf("normalized = %+v", got)
	}
	for _, bad := range []SandboxConfig{
		{Mode: "loose"}, {Network: "open"}, {UseBwrap: "sometimes"},
		{ExtendTo: []string{"lua"}}, {Env: SandboxEnvConfig{Inherit: "some"}},
	} {
		if _, err := NormalizeSandboxConfig(bad); err == nil {
			t.Fatalf("NormalizeSandboxConfig(%+v) accepted an invalid value", bad)
		}
	}
}
