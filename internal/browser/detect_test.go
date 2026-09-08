package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizeBrowserType(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"obscura lowercase", "obscura", "obscura"},
		{"obscura uppercase", "OBSCURA", "obscura"},
		{"obscura alias", "obscura-browser", "obscura"},
		{"lightpanda alias", "light-panda", "lightpanda"},
		{"chrome", "chrome", "chrome"},
		{"edge", "edge", "msedge"},
		{"unknown passthrough", "some-unknown-browser", "some-unknown-browser"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeBrowserType(tt.input); got != tt.want {
				t.Errorf("NormalizeBrowserType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsRemoteBrowserType(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"lightpanda is remote", "lightpanda", true},
		{"obscura is remote", "obscura", true},
		{"chrome is not remote", "chrome", false},
		{"chromium is not remote", "chromium", false},
		{"msedge is not remote", "msedge", false},
		{"opera is not remote", "opera", false},
		{"empty defaults to chrome, not remote", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRemoteBrowserType(tt.input); got != tt.want {
				t.Errorf("IsRemoteBrowserType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestDetectObscuraOnPath places a fake obscura executable in a temporary
// directory prepended to PATH and verifies both DetectInstalledBrowsers and
// ResolveBrowserInstall find it via exec.LookPath.
func TestDetectObscuraOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH-based executable lookup test is skipped on windows")
	}

	dir := t.TempDir()
	execName := "obscura"
	execPath := filepath.Join(dir, execName)
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("failed to write fake obscura executable: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+origPath)

	installs := DetectInstalledBrowsers()
	found := false
	for _, install := range installs {
		if install.Type == "obscura" {
			found = true
			if install.Label != "Obscura" {
				t.Errorf("obscura install Label = %q, want %q", install.Label, "Obscura")
			}
		}
	}
	if !found {
		t.Errorf("DetectInstalledBrowsers() did not find obscura; got %+v", installs)
	}

	install, ok := ResolveBrowserInstall("obscura", "")
	if !ok {
		t.Fatal("ResolveBrowserInstall(\"obscura\", \"\") returned ok=false")
	}
	if install.Label != "Obscura" {
		t.Errorf("ResolveBrowserInstall label = %q, want %q", install.Label, "Obscura")
	}
	if install.Type != "obscura" {
		t.Errorf("ResolveBrowserInstall type = %q, want %q", install.Type, "obscura")
	}
}
