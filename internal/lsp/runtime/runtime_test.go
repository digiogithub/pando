package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// stubPath makes lookPath resolve only the given binaries, to "/usr/bin/<name>"
// unless an explicit path is provided.
func stubPath(t *testing.T, bins map[string]string) {
	t.Helper()
	original := lookPath
	lookPath = func(name string) (string, error) {
		if path, ok := bins[name]; ok {
			if path == "" {
				return filepath.Join("/usr/bin", name), nil
			}
			return path, nil
		}
		return "", exec.ErrNotFound
	}
	resetDetection()
	t.Cleanup(func() {
		lookPath = original
		resetDetection()
	})
}

func resetInstallState(t *testing.T) {
	t.Helper()
	installState.mu.Lock()
	installState.done = map[string]error{}
	installState.inFlight = map[string]chan struct{}{}
	installState.mu.Unlock()
}

func TestDetectPrefersBun(t *testing.T) {
	stubPath(t, map[string]string{"bun": "", "bunx": "", "node": "", "npm": "", "npx": ""})

	rt := Detect()
	if rt.Manager != ManagerBun {
		t.Fatalf("manager = %q, want %q", rt.Manager, ManagerBun)
	}
	if rt.Install != "/usr/bin/bun" || rt.Exec != "/usr/bin/bunx" {
		t.Fatalf("unexpected runtime: %+v", rt)
	}
	if len(rt.ExecArgs) != 0 {
		t.Fatalf("ExecArgs = %v, want none when bunx exists", rt.ExecArgs)
	}
}

func TestDetectBunWithoutBunxUsesSubcommand(t *testing.T) {
	stubPath(t, map[string]string{"bun": ""})

	rt := Detect()
	if rt.Exec != "/usr/bin/bun" || len(rt.ExecArgs) != 1 || rt.ExecArgs[0] != "x" {
		t.Fatalf("unexpected runtime: %+v", rt)
	}
}

func TestDetectNodeFallback(t *testing.T) {
	stubPath(t, map[string]string{"node": "", "npm": "", "npx": ""})

	rt := Detect()
	if rt.Manager != ManagerNpm {
		t.Fatalf("manager = %q, want %q", rt.Manager, ManagerNpm)
	}
	if rt.Install != "/usr/bin/npm" || rt.Exec != "/usr/bin/npx" {
		t.Fatalf("unexpected runtime: %+v", rt)
	}
}

func TestDetectNoRuntime(t *testing.T) {
	stubPath(t, map[string]string{"python3": ""})

	if rt := Detect(); rt.Available() {
		t.Fatalf("expected no runtime, got %+v", rt)
	}
}

func TestDetectNodeWithoutPackageManager(t *testing.T) {
	stubPath(t, map[string]string{"node": ""})

	if rt := Detect(); rt.Available() {
		t.Fatalf("node alone must not be usable, got %+v", rt)
	}
}

func TestDetectForRunnerPreference(t *testing.T) {
	stubPath(t, map[string]string{"bun": "", "bunx": "", "node": "", "npm": "", "npx": ""})

	if rt := DetectFor(config.LSPRunnerBun); rt.Manager != ManagerBun {
		t.Fatalf("bun preference = %+v", rt)
	}
	// npm is selected even though bun is installed and would win under "auto".
	npm := DetectFor(config.LSPRunnerNpm)
	if npm.Manager != ManagerNpm || npm.Install != "/usr/bin/npm" {
		t.Fatalf("npm preference = %+v", npm)
	}
	if rt := DetectFor(config.LSPRunnerOff); rt.Available() {
		t.Fatalf("off preference must yield no runtime, got %+v", rt)
	}
	if rt := DetectFor(config.LSPRunnerAuto); rt.Manager != ManagerBun {
		t.Fatalf("auto preference = %+v", rt)
	}
}

func TestDetectForPinnedManagerMissing(t *testing.T) {
	// Only npm is installed: pinning bun must not silently fall back to it.
	stubPath(t, map[string]string{"node": "", "npm": "", "npx": ""})

	if rt := DetectFor(config.LSPRunnerBun); rt.Available() {
		t.Fatalf("bun preference must not fall back to npm, got %+v", rt)
	}
	if rt := DetectFor(config.LSPRunnerAuto); rt.Manager != ManagerNpm {
		t.Fatalf("auto preference = %+v", rt)
	}
}

func TestDetectHonorsConfiguredRunner(t *testing.T) {
	stubPath(t, map[string]string{"bun": "", "bunx": "", "node": "", "npm": "", "npx": ""})
	config.SetForTests(&config.Config{LSPRunner: config.LSPRunnerNpm})
	t.Cleanup(func() { config.SetForTests(nil) })

	if rt := Detect(); rt.Manager != ManagerNpm {
		t.Fatalf("Detect must honor LSPRunner, got %+v", rt)
	}
}

func TestRunnerNpxNamesPackageAndBinary(t *testing.T) {
	rt := NodeRuntime{Manager: ManagerNpm, Exec: "/usr/bin/npx"}

	res, ok := Runner(rt, Package{Spec: "pyright", Bin: "pyright-langserver"})
	if !ok {
		t.Fatal("npx runner must handle a package/binary name mismatch")
	}
	want := "-y --package pyright -- pyright-langserver"
	if got := strings.Join(res.Args, " "); got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
	if res.Installed {
		t.Fatal("an ephemeral runner is not an install")
	}
}

func TestRunnerBunRejectsMismatchedBinary(t *testing.T) {
	rt := NodeRuntime{Manager: ManagerBun, Exec: "/usr/bin/bunx"}

	if _, ok := Runner(rt, Package{Spec: "pyright", Bin: "pyright-langserver"}); ok {
		t.Fatal("bunx cannot resolve a binary that is not named like its package")
	}
	res, ok := Runner(rt, Package{Spec: "@vue/language-server", Bin: "language-server"})
	if !ok {
		t.Fatal("bunx must handle a scoped package whose binary matches its name")
	}
	if len(res.Args) != 1 || res.Args[0] != "language-server" {
		t.Fatalf("args = %v", res.Args)
	}
}

func TestPackageNameHelpers(t *testing.T) {
	cases := []struct{ spec, name, unscoped string }{
		{"pyright", "pyright", "pyright"},
		{"intelephense@1.10.0", "intelephense", "intelephense"},
		{"@vue/language-server", "@vue/language-server", "language-server"},
		{"@vue/language-server@2.0.0", "@vue/language-server", "language-server"},
	}
	for _, c := range cases {
		if got := packageName(c.spec); got != c.name {
			t.Errorf("packageName(%q) = %q, want %q", c.spec, got, c.name)
		}
		if got := unscopedName(c.spec); got != c.unscoped {
			t.Errorf("unscopedName(%q) = %q, want %q", c.spec, got, c.unscoped)
		}
	}
}

func TestSanitizeSpec(t *testing.T) {
	if got := sanitize("@vue/language-server@2.0.0"); got != "_vue_language-server_2.0.0" {
		t.Fatalf("sanitize = %q", got)
	}
}

// fakeManager writes a shell script that mimics a package manager: it appends
// its arguments to a log and creates the expected binary under node_modules.
func fakeManager(t *testing.T, dir, bin string, fail bool) string {
	t.Helper()
	script := filepath.Join(dir, "fake-npm")
	body := `#!/bin/sh
echo "$@" >> "$PWD/calls.log"
`
	if fail {
		body += "echo 'E404 not found' >&2\nexit 1\n"
	} else {
		body += `mkdir -p "$PWD/node_modules/.bin"
printf '#!/bin/sh\necho fake-server\n' > "$PWD/node_modules/.bin/` + bin + `"
chmod +x "$PWD/node_modules/.bin/` + bin + `"
`
	}
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestEnsureInstallsAndLinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	resetInstallState(t)

	manager := fakeManager(t, t.TempDir(), "pyright-langserver", false)
	stubPath(t, map[string]string{"node": "", "npm": manager})

	pkg := Package{Spec: "pyright", Bin: "pyright-langserver"}
	res, err := Ensure(context.Background(), pkg, 30*time.Second)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !res.Installed {
		t.Fatalf("expected a staged install, got %+v", res)
	}
	if _, err := os.Stat(res.Command); err != nil {
		t.Fatalf("staged binary missing: %v", err)
	}

	// The binary is linked into the shared bin directory.
	link := filepath.Join(BinDir(), pkg.Bin)
	if _, err := os.Stat(link); err != nil {
		t.Fatalf("bin link missing: %v", err)
	}

	// The extra package and the isolation flags reached the package manager.
	calls, err := os.ReadFile(filepath.Join(packageDir(pkg.Spec), "calls.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "--prefix") || !strings.Contains(string(calls), "pyright") {
		t.Fatalf("unexpected manager invocation: %s", calls)
	}

	// A second call is served from the staging directory without reinstalling.
	if _, err := Ensure(context.Background(), pkg, 30*time.Second); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(packageDir(pkg.Spec), "calls.log"))
	if len(after) != len(calls) {
		t.Fatalf("package manager ran twice:\n%s", after)
	}
}

func TestEnsureDeduplicatesConcurrentInstalls(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	resetInstallState(t)

	manager := fakeManager(t, t.TempDir(), "yaml-language-server", false)
	stubPath(t, map[string]string{"node": "", "npm": manager})

	pkg := Package{Spec: "yaml-language-server", Bin: "yaml-language-server"}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Ensure(context.Background(), pkg, 30*time.Second); err != nil {
				t.Errorf("Ensure: %v", err)
			}
		}()
	}
	wg.Wait()

	calls, err := os.ReadFile(filepath.Join(packageDir(pkg.Spec), "calls.log"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(strings.TrimSpace(string(calls)), "\n") + 1; lines != 1 {
		t.Fatalf("package manager ran %d times, want 1:\n%s", lines, calls)
	}
}

func TestEnsureFallsBackToRunnerOnInstallFailure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	resetInstallState(t)

	manager := fakeManager(t, t.TempDir(), "bash-language-server", true)
	stubPath(t, map[string]string{"node": "", "npm": manager, "npx": ""})

	res, err := Ensure(context.Background(), Package{Spec: "bash-language-server", Bin: "bash-language-server"}, 30*time.Second)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if res.Installed || res.Command != "/usr/bin/npx" {
		t.Fatalf("expected an npx fallback, got %+v", res)
	}
}

func TestEnsureFailsWithoutRuntime(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	resetInstallState(t)
	stubPath(t, map[string]string{})

	_, err := Ensure(context.Background(), Package{Spec: "pyright", Bin: "pyright-langserver"}, time.Second)
	if !errors.Is(err, ErrNoRuntime) {
		t.Fatalf("err = %v, want ErrNoRuntime", err)
	}
}

func TestEnsureDoesNotRetryFailedInstall(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	resetInstallState(t)

	manager := fakeManager(t, t.TempDir(), "gopls", true)
	// No npx: the failure must surface instead of falling back.
	stubPath(t, map[string]string{"node": "", "npm": manager})

	pkg := Package{Spec: "vscode-langservers-extracted", Bin: "vscode-json-language-server"}
	if _, err := Ensure(context.Background(), pkg, 30*time.Second); err == nil {
		t.Fatal("expected the install failure to surface")
	}
	if _, err := Ensure(context.Background(), pkg, 30*time.Second); err == nil {
		t.Fatal("expected the memoized failure to surface")
	}

	calls, err := os.ReadFile(filepath.Join(packageDir(pkg.Spec), "calls.log"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(strings.TrimSpace(string(calls)), "\n") + 1; lines != 1 {
		t.Fatalf("package manager ran %d times, want 1:\n%s", lines, calls)
	}
}
