package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/lsp/runtime"
	"github.com/spf13/viper"
)

// presetServer returns the resolved registry entry for a built-in preset.
func presetServer(t *testing.T, name string) config.ResolvedLSPServer {
	t.Helper()
	cfg := &config.Config{LSP: map[string]config.LSPConfig{}}
	for _, s := range cfg.LSPRegistry() {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("preset %q not found", name)
	return config.ResolvedLSPServer{}
}

// setResolveConfig installs a configuration for the resolver tests and isolates
// the staging directory so nothing on the developer's machine leaks in.
func setResolveConfig(t *testing.T, autoInstall bool) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	viper.Reset()
	config.SetForTests(&config.Config{
		LSPAutoActivate: true,
		LSPAutoInstall:  autoInstall,
		LSP:             map[string]config.LSPConfig{},
	})
	t.Cleanup(func() {
		config.SetForTests(nil)
		viper.Reset()
	})
}

func TestResolveLSPCommand_BinaryOnPath(t *testing.T) {
	setResolveConfig(t, true)
	withStubLookPath(t, true, nil)

	got := newLSPTestApp().resolveLSPCommand(presetServer(t, "gopls"))
	if got.Availability != LSPAvailable {
		t.Fatalf("availability = %v, want LSPAvailable (%q)", got.Availability, got.Reason)
	}
	if got.Command != "/usr/bin/gopls" {
		t.Fatalf("command = %q", got.Command)
	}
}

func TestResolveLSPCommand_ToolchainServerIsManual(t *testing.T) {
	setResolveConfig(t, true)
	withStubLookPath(t, false, nil)
	defer runtime.SetForTests(runtime.NodeRuntime{Manager: runtime.ManagerBun, Install: "/usr/bin/bun"})()

	got := newLSPTestApp().resolveLSPCommand(presetServer(t, "gopls"))
	if got.Availability != LSPManual {
		t.Fatalf("availability = %v, want LSPManual", got.Availability)
	}
	// Even with bun available, a Go toolchain server is never auto-installed.
	if !strings.Contains(got.Reason, "go install golang.org/x/tools/gopls@latest") {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestResolveLSPCommand_NpmServerIsInstallable(t *testing.T) {
	setResolveConfig(t, true)
	withStubLookPath(t, false, nil)
	defer runtime.SetForTests(runtime.NodeRuntime{Manager: runtime.ManagerBun, Install: "/usr/bin/bun"})()

	got := newLSPTestApp().resolveLSPCommand(presetServer(t, "pyright"))
	if got.Availability != LSPInstallable {
		t.Fatalf("availability = %v, want LSPInstallable (%q)", got.Availability, got.Reason)
	}
}

func TestResolveLSPCommand_NpmServerWithoutRuntimeIsManual(t *testing.T) {
	setResolveConfig(t, true)
	withStubLookPath(t, false, nil)
	defer runtime.SetForTests(runtime.NodeRuntime{})()

	got := newLSPTestApp().resolveLSPCommand(presetServer(t, "pyright"))
	if got.Availability != LSPManual {
		t.Fatalf("availability = %v, want LSPManual", got.Availability)
	}
	if !strings.Contains(got.Reason, "bun") || !strings.Contains(got.Reason, "npm install -g pyright") {
		t.Fatalf("reason must name the missing runtime and the manual command, got %q", got.Reason)
	}
}

func TestResolveLSPCommand_AutoInstallDisabled(t *testing.T) {
	setResolveConfig(t, false)
	withStubLookPath(t, false, nil)
	defer runtime.SetForTests(runtime.NodeRuntime{Manager: runtime.ManagerBun, Install: "/usr/bin/bun"})()

	got := newLSPTestApp().resolveLSPCommand(presetServer(t, "pyright"))
	if got.Availability != LSPManual {
		t.Fatalf("availability = %v, want LSPManual", got.Availability)
	}
	if !strings.Contains(got.Reason, "LSPAutoInstall") {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestResolveLSPCommand_RunnerOff(t *testing.T) {
	setResolveConfig(t, true)
	config.Get().LSPRunner = config.LSPRunnerOff
	withStubLookPath(t, false, nil)
	// A usable toolchain is present: only the setting keeps Pando from using it.
	defer runtime.SetForTests(runtime.NodeRuntime{Manager: runtime.ManagerBun, Install: "/usr/bin/bun"})()

	got := newLSPTestApp().resolveLSPCommand(presetServer(t, "pyright"))
	if got.Availability != LSPManual {
		t.Fatalf("availability = %v, want LSPManual (%q)", got.Availability, got.Reason)
	}
	if !strings.Contains(got.Reason, "LSPRunner") {
		t.Fatalf("reason must name the setting that blocked the install, got %q", got.Reason)
	}
}

func TestResolveLSPCommand_RunnerPinsMissingManager(t *testing.T) {
	setResolveConfig(t, true)
	config.Get().LSPRunner = config.LSPRunnerBun
	withStubLookPath(t, false, nil)
	// SetForTests pins "no toolchain at all", as if bun were absent.
	defer runtime.SetForTests(runtime.NodeRuntime{})()

	got := newLSPTestApp().resolveLSPCommand(presetServer(t, "pyright"))
	if got.Availability != LSPManual {
		t.Fatalf("availability = %v, want LSPManual", got.Availability)
	}
	if !strings.Contains(got.Reason, `LSPRunner = "bun"`) {
		t.Fatalf("reason must name the pinned manager, got %q", got.Reason)
	}
}

func TestResolveLSPCommand_UsesPreviouslyStagedBinary(t *testing.T) {
	setResolveConfig(t, true)
	withStubLookPath(t, false, nil)
	defer runtime.SetForTests(runtime.NodeRuntime{})()

	// Simulate an install from an earlier session.
	binDir := runtime.BinDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(binDir, "pyright-langserver")
	if err := os.WriteFile(staged, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := newLSPTestApp().resolveLSPCommand(presetServer(t, "pyright"))
	if got.Availability != LSPAvailable {
		t.Fatalf("availability = %v, want LSPAvailable (%q)", got.Availability, got.Reason)
	}
	if got.Command != staged {
		t.Fatalf("command = %q, want the staged binary %q", got.Command, staged)
	}
}

func TestInstallPackageMapsPresetMetadata(t *testing.T) {
	pkg, ok := installPackage(presetServer(t, "typescript-language-server"))
	if !ok {
		t.Fatal("typescript-language-server must be installable from npm")
	}
	if pkg.Spec != "typescript-language-server" || pkg.Bin != "typescript-language-server" {
		t.Fatalf("unexpected package: %+v", pkg)
	}
	// The peer dependency npm will not pull on its own.
	if len(pkg.Extra) != 1 || pkg.Extra[0] != "typescript" {
		t.Fatalf("extra packages = %v, want [typescript]", pkg.Extra)
	}

	// pyright's binary is not named like its package.
	pkg, ok = installPackage(presetServer(t, "pyright"))
	if !ok || pkg.Spec != "pyright" || pkg.Bin != "pyright-langserver" {
		t.Fatalf("unexpected pyright package: %+v (ok=%v)", pkg, ok)
	}

	if _, ok := installPackage(presetServer(t, "gopls")); ok {
		t.Fatal("gopls has no npm package and must not be installable")
	}
}
