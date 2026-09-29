package desktop

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/procgroup"
)

// LaunchedFromAppEnv is set by the macOS Pando.app bundle wrapper so the CLI
// knows it was opened from Finder/Dock/Launchpad rather than a terminal.
const LaunchedFromAppEnv = "PANDO_LAUNCHED_FROM_APP"

// LaunchedFromApp reports whether this process was started by a GUI launcher
// (the Pando.app wrapper) instead of a terminal. Older bundles do not set
// LaunchedFromAppEnv, so on macOS a process re-parented to launchd (PPID 1) is
// treated the same way: that is how LaunchServices starts app executables.
func LaunchedFromApp() bool {
	if os.Getenv(LaunchedFromAppEnv) == "1" {
		return true
	}
	return runtime.GOOS == "darwin" && os.Getppid() == 1
}

// DefaultWorkingDir returns the directory `pando desktop` works in when no
// --cwd flag is given. A GUI launch starts with the filesystem root as its
// working directory, which is read-only on macOS and never a sensible
// workspace, so it falls back to the user's home directory (Pando's general,
// project-less workspace).
func DefaultWorkingDir() (string, error) {
	cwd, err := os.Getwd()
	if err == nil && !LaunchedFromApp() && filepath.Clean(cwd) != string(filepath.Separator) {
		return cwd, nil
	}
	home, homeErr := os.UserHomeDir()
	if homeErr != nil || home == "" {
		if err != nil {
			return "", fmt.Errorf("failed to get current working directory: %w", err)
		}
		return cwd, nil
	}
	return home, nil
}

const loginPathMarker = "__PANDO_LOGIN_PATH__="

// ImportLoginShellPath prepends the PATH of the user's login shell to the
// process PATH. Apps opened from Finder inherit launchd's minimal PATH
// (/usr/bin:/bin:/usr/sbin:/sbin), which hides Homebrew, language toolchains,
// LSP servers and agent CLIs that the tools and delegated agents rely on. It is
// a no-op off macOS or when the process was started from a terminal.
func ImportLoginShellPath() {
	if runtime.GOOS != "darwin" || !LaunchedFromApp() {
		return
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Interactive login shell so both .zprofile and .zshrc style files run;
	// the marker isolates PATH from anything the rc files print.
	cmd := exec.CommandContext(ctx, shell, "-l", "-i", "-c", `printf '\n`+loginPathMarker+`%s\n' "$PATH"`)
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return
	}
	loginPath := parseLoginPath(out)
	if loginPath == "" {
		return
	}
	_ = os.Setenv("PATH", mergePathLists(loginPath, os.Getenv("PATH")))
}

func parseLoginPath(out []byte) string {
	idx := bytes.LastIndex(out, []byte(loginPathMarker))
	if idx < 0 {
		return ""
	}
	rest := out[idx+len(loginPathMarker):]
	if nl := bytes.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	return strings.TrimSpace(string(rest))
}

// mergePathLists returns primary followed by the entries of secondary that are
// not already present, preserving order.
func mergePathLists(primary, secondary string) string {
	seen := map[string]bool{}
	var merged []string
	for _, list := range []string{primary, secondary} {
		for _, entry := range filepath.SplitList(list) {
			if entry == "" || seen[entry] {
				continue
			}
			seen[entry] = true
			merged = append(merged, entry)
		}
	}
	return strings.Join(merged, string(os.PathListSeparator))
}

// SpawnInstance starts an independent `pando desktop` process working in dir.
// The child runs its own API server and window, in its own process group, so
// closing the window that spawned it does not take it down.
func SpawnInstance(dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve pando executable: %w", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	cmd := exec.Command(exe, "desktop", "--cwd", dir)
	cmd.Dir = dir
	// The parent already imported the login PATH; drop the GUI-launch marker so
	// the child does not repeat the shell probe.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, LaunchedFromAppEnv+"=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	procgroup.Ensure(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start pando desktop for %s: %w", dir, err)
	}
	return cmd.Process.Release()
}
