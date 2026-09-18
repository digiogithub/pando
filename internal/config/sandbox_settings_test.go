package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The settings view starts from the global file, so the project-local
// tightening never leaks into the global file on a UI save.
func TestSandboxSettingsViewIgnoresProjectTightening(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)
	dir := writeSandboxConfigs(t, "[Sandbox]\nNetwork = \"allowed\"\n", "[Sandbox]\nMode = \"read-only\"\nDenyPaths = [\"secret\"]\n")
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if Get().Sandbox.Mode != SandboxModeReadOnly {
		t.Fatalf("effective mode = %q, want the project's read-only", Get().Sandbox.Mode)
	}
	view := SandboxSettingsView()
	if view.Mode != "" || len(view.DenyPaths) != 0 || view.Network != SandboxNetworkAllowed {
		t.Fatalf("view = %+v, want the global section only", view)
	}

	view.WritableRoots = []string{"/opt/extra"}
	if err := UpdateSandbox(view); err != nil {
		t.Fatalf("UpdateSandbox: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".pando.toml"))
	if err != nil {
		t.Fatalf("read global: %v", err)
	}
	if strings.Contains(string(data), "read-only") || strings.Contains(string(data), "secret") {
		t.Fatalf("project tightening leaked into the global file:\n%s", data)
	}
	if !reflect.DeepEqual(GlobalSandboxConfig().WritableRoots, []string{"/opt/extra"}) {
		t.Fatalf("global writableRoots = %v", GlobalSandboxConfig().WritableRoots)
	}
}

// A locked field keeps its enforced value in memory and its own value in the
// file, and an explicit change of it is reported as a locked-key error.
func TestSandboxLockedFieldsSurviveUpdate(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)
	dir := writeSandboxConfigs(t, "", "")
	RegisterOverlayProvider(OverlayProviderFunc(func(context.Context) (Overlay, error) {
		return Overlay{
			Values: map[string]any{"sandbox": map[string]any{"mode": "strict"}},
			Locked: []string{"sandbox.mode"},
		}, nil
	}))
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := LockedSandboxFields(); !reflect.DeepEqual(got, []string{"sandbox.disabled", "sandbox.mode"}) {
		t.Fatalf("LockedSandboxFields = %v", got)
	}

	view := SandboxSettingsView()
	if view.Mode != SandboxModeStrict {
		t.Fatalf("view mode = %q, want the enforced strict", view.Mode)
	}

	changed := view
	changed.Mode = SandboxModeOff
	if err := ErrIfSandboxLockedChange(view, changed); !errors.Is(err, ErrKeyLocked) {
		t.Fatalf("ErrIfSandboxLockedChange = %v, want ErrKeyLocked", err)
	}

	// Echoing the managed value back while changing another field is accepted.
	echo := view
	echo.Network = SandboxNetworkRestricted
	if err := ErrIfSandboxLockedChange(view, echo); err != nil {
		t.Fatalf("echo refused: %v", err)
	}
	if err := UpdateSandbox(echo); err != nil {
		t.Fatalf("UpdateSandbox: %v", err)
	}
	if Get().Sandbox.Mode != SandboxModeStrict || Get().Sandbox.Network != SandboxNetworkRestricted {
		t.Fatalf("in-memory sandbox = %+v, want strict + restricted", Get().Sandbox)
	}
	if g := GlobalSandboxConfig(); g.Mode != "" || g.Network != SandboxNetworkRestricted {
		t.Fatalf("global file sandbox = %+v, want no mode (the lock's value stays out) and restricted", g)
	}
}

// Locking the mode also locks the on/off switch (and vice versa): turning the
// sandbox off would otherwise bypass a locked mode.
func TestSandboxModeLockCouplesDisabled(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)
	dir := writeSandboxConfigs(t, "", "")
	RegisterOverlayProvider(OverlayProviderFunc(func(context.Context) (Overlay, error) {
		return Overlay{Locked: []string{"sandbox.mode"}}, nil
	}))
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := LockedSandboxFields(); !reflect.DeepEqual(got, []string{"sandbox.disabled", "sandbox.mode"}) {
		t.Fatalf("LockedSandboxFields = %v", got)
	}
	if err := UpdateSandbox(SandboxConfig{Disabled: true}); !errors.Is(err, ErrKeyLocked) {
		t.Fatalf("UpdateSandbox(Disabled) = %v, want ErrKeyLocked", err)
	}
	if Get().Sandbox.Disabled {
		t.Fatal("a refused update disabled the sandbox")
	}
}
