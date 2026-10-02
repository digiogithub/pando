package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func envFunc(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

func TestDisplayEnv(t *testing.T) {
	cases := []struct {
		name      string
		env       map[string]string
		extra     []string
		noDisplay bool
	}{
		{"none", map[string]string{}, nil, true},
		{"blank", map[string]string{"DISPLAY": " "}, nil, true},
		{"x11", map[string]string{"DISPLAY": ":0"}, nil, false},
		{"both", map[string]string{"DISPLAY": ":0", "WAYLAND_DISPLAY": "wayland-0"}, nil, false},
		{"wayland only", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, []string{"GDK_BACKEND=wayland"}, false},
		{"explicit backend", map[string]string{"GDK_BACKEND": "broadway"}, nil, false},
		{"explicit backend wayland only", map[string]string{"GDK_BACKEND": "x11", "WAYLAND_DISPLAY": "wayland-0"}, nil, false},
	}
	for _, tc := range cases {
		extra, noDisplay := displayEnv(envFunc(tc.env))
		if !reflect.DeepEqual(extra, tc.extra) || noDisplay != tc.noDisplay {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", tc.name, extra, noDisplay, tc.extra, tc.noDisplay)
		}
	}
}

func TestIsWSLProcVersion(t *testing.T) {
	if !isWSLProcVersion("Linux version 5.15.90.1-microsoft-standard-WSL2 (gcc ...)") {
		t.Error("WSL2 kernel not detected")
	}
	if !isWSLProcVersion("Linux version 4.4.0-19041-Microsoft (Microsoft@Microsoft.com)") {
		t.Error("WSL1 kernel not detected")
	}
	if isWSLProcVersion("Linux version 6.8.0-generic (buildd@lcy02)") {
		t.Error("plain Linux kernel detected as WSL")
	}
}

func TestNoDisplayHelp(t *testing.T) {
	env := envFunc(map[string]string{"DISPLAY": ":0"})
	wsl := noDisplayHelp(true, env)
	for _, want := range []string{"WSLg", "wsl --update", `DISPLAY=":0"`, "pando app"} {
		if !strings.Contains(wsl, want) {
			t.Errorf("WSL help lacks %q:\n%s", want, wsl)
		}
	}
	plain := noDisplayHelp(false, env)
	if strings.Contains(plain, "WSLg") || !strings.Contains(plain, "pando app") {
		t.Errorf("unexpected non-WSL help:\n%s", plain)
	}
}

func TestDiagnoseLaunchFailureGTKInit(t *testing.T) {
	base := errors.New("exit status 2")
	err := diagnoseLaunchFailure("panic: failed to init GTK\n\ngoroutine 1 [running]:", base)
	var displayErr *NoDisplayError
	if !errors.As(err, &displayErr) {
		t.Fatalf("expected NoDisplayError, got %v", err)
	}
	if !errors.Is(err, base) {
		t.Fatal("NoDisplayError must unwrap to the exec error")
	}
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "pando-desktop")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestRunDesktopWithoutDisplay(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("display check is Linux only")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("GDK_BACKEND", "")
	marker := filepath.Join(t.TempDir(), "ran")
	err := runDesktop(writeScript(t, "touch "+marker+"\n"), "http://localhost:1", false)
	var displayErr *NoDisplayError
	if !errors.As(err, &displayErr) {
		t.Fatalf("expected NoDisplayError, got %v", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("wrapper must not be started without a display")
	}
}

func TestRunDesktopSelectsWaylandBackend(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("display check is Linux only")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("GDK_BACKEND", "")
	out := filepath.Join(t.TempDir(), "backend")
	if err := runDesktop(writeScript(t, "printf %s \"$GDK_BACKEND\" > "+out+"\n"), "http://localhost:1", false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "wayland" {
		t.Fatalf("GDK_BACKEND = %q, want wayland", got)
	}
}

func TestRunDesktopReportsGTKInitFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("display check is Linux only")
	}
	t.Setenv("DISPLAY", ":77")
	err := runDesktop(writeScript(t, "echo 'panic: failed to init GTK' >&2\nexit 2\n"), "http://localhost:1", false)
	var displayErr *NoDisplayError
	if !errors.As(err, &displayErr) {
		t.Fatalf("expected NoDisplayError, got %v", err)
	}
}
