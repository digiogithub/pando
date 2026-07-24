package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
)

// DefaultInstallTimeout caps a single package installation. Cold npm installs
// of heavy servers (typescript-language-server pulls the whole TypeScript
// compiler) routinely take a minute on a slow link.
const DefaultInstallTimeout = 300 * time.Second

// ErrNoRuntime is returned when neither bun nor Node.js is installed, so no
// npm-distributed language server can be provisioned.
var ErrNoRuntime = errors.New("no JavaScript runtime found (install bun or node)")

// Package describes an npm-distributed language server.
type Package struct {
	// Spec is the npm package to install, optionally versioned:
	// "pyright", "@vue/language-server", "intelephense@1.10.0".
	Spec string
	// Bin is the executable the package exposes in node_modules/.bin. It is
	// often different from the package name (pyright -> pyright-langserver).
	Bin string
	// Extra lists additional packages that must be installed alongside Spec.
	// These are peer dependencies the package manager will not pull on its own,
	// e.g. "typescript" for typescript-language-server.
	Extra []string
}

// Result reports how a language server binary was resolved.
type Result struct {
	// Command is the executable to spawn.
	Command string
	// Args are the arguments that must precede the server's own arguments.
	Args []string
	// Installed is true when Command points at a binary staged by Pando, and
	// false when the server runs through an ephemeral runner (bunx/npx), which
	// re-resolves the package on every start.
	Installed bool
}

// RootDir is the directory holding every language server Pando installed.
func RootDir() string {
	base := config.GlobalConfigDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "lsp")
}

// BinDir is the directory where staged server binaries are linked, so users can
// add a single directory to PATH to reuse them outside Pando.
func BinDir() string {
	root := RootDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "bin")
}

// packageDir is the isolated staging directory for a package. Each package gets
// its own node_modules tree so an install failure or a dependency conflict
// cannot break unrelated servers.
func packageDir(spec string) string {
	root := RootDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "packages", sanitize(spec))
}

// sanitize turns a package spec into a safe directory name on every platform.
func sanitize(spec string) string {
	var b strings.Builder
	for _, r := range spec {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Staged returns the path of an already-installed binary for the package, if
// any. It is cheap enough to call on every activation: two stat calls.
func Staged(p Package) (string, bool) {
	if p.Bin == "" {
		return "", false
	}
	candidates := []string{}
	if dir := packageDir(p.Spec); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "node_modules", ".bin", p.Bin))
	}
	if dir := BinDir(); dir != "" {
		candidates = append(candidates, filepath.Join(dir, p.Bin))
	}
	for _, c := range candidates {
		if binaryExists(c) {
			return c, true
		}
	}
	return "", false
}

func binaryExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return true
}

// installState memoizes install outcomes and deduplicates concurrent installs
// of the same package. A failed install is never retried in-process: the usual
// causes (offline, missing registry credentials, unsupported platform) do not
// resolve themselves, and retrying on every edit would stall the agent.
var installState = struct {
	mu       sync.Mutex
	done     map[string]error
	inFlight map[string]chan struct{}
}{
	done:     map[string]error{},
	inFlight: map[string]chan struct{}{},
}

// Ensure resolves an executable for the package, installing it when needed.
//
// Resolution order: an already-staged binary, then an install into Pando's
// staging directory, then an ephemeral runner (bunx/npx). It never returns a
// command that is known not to exist, so callers can spawn the result directly.
func Ensure(ctx context.Context, p Package, timeout time.Duration) (Result, error) {
	if p.Spec == "" || p.Bin == "" {
		return Result{}, errors.New("runtime: package spec and bin are required")
	}
	if path, ok := Staged(p); ok {
		return Result{Command: path, Installed: true}, nil
	}

	rt := Detect()
	if !rt.Available() {
		return Result{}, ErrNoRuntime
	}

	err := installOnce(ctx, rt, p, timeout)
	if err == nil {
		if path, ok := Staged(p); ok {
			return Result{Command: path, Installed: true}, nil
		}
		err = fmt.Errorf("runtime: installed %s but %q is missing from %s", p.Spec, p.Bin, packageDir(p.Spec))
	}

	// The staged install is the preferred path; fall back to running the package
	// straight from the registry so a broken cache still yields a usable server.
	if res, ok := Runner(rt, p); ok {
		logging.Debug("LSP package install failed; falling back to ephemeral runner",
			"package", p.Spec, "runner", res.Command, "error", err)
		return res, nil
	}
	return Result{}, err
}

// Runner builds the command that runs a package without installing it. It
// reports false when the runtime cannot express this invocation.
func Runner(rt NodeRuntime, p Package) (Result, bool) {
	if rt.Exec == "" {
		return Result{}, false
	}
	args := append([]string{}, rt.ExecArgs...)
	switch rt.Manager {
	case ManagerBun:
		// bunx resolves the package from the executable name, so it can only run
		// binaries named like their package. Anything else (pyright exposes
		// pyright-langserver) has to go through a staged install.
		if p.Bin != unscopedName(p.Spec) {
			return Result{}, false
		}
		args = append(args, p.Bin)
	default:
		// npx can name package and binary independently.
		args = append(args, "-y", "--package", p.Spec, "--", p.Bin)
	}
	return Result{Command: rt.Exec, Args: args}, true
}

// installOnce runs the installation at most once per package per process,
// making concurrent callers wait for the winner instead of racing npm.
func installOnce(ctx context.Context, rt NodeRuntime, p Package, timeout time.Duration) error {
	for {
		installState.mu.Lock()
		if err, ok := installState.done[p.Spec]; ok {
			installState.mu.Unlock()
			return err
		}
		if ch, ok := installState.inFlight[p.Spec]; ok {
			installState.mu.Unlock()
			select {
			case <-ch:
				continue // the winner recorded an outcome; read it above
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		ch := make(chan struct{})
		installState.inFlight[p.Spec] = ch
		installState.mu.Unlock()

		err := install(ctx, rt, p, timeout)

		installState.mu.Lock()
		delete(installState.inFlight, p.Spec)
		installState.done[p.Spec] = err
		installState.mu.Unlock()
		close(ch)
		return err
	}
}

func install(ctx context.Context, rt NodeRuntime, p Package, timeout time.Duration) error {
	if rt.Install == "" {
		return fmt.Errorf("runtime: no package manager available to install %s", p.Spec)
	}
	dir := packageDir(p.Spec)
	if dir == "" {
		return errors.New("runtime: no global config directory available")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("runtime: creating %s: %w", dir, err)
	}
	// A manifest of our own keeps bun/npm from walking up and installing into
	// the user's project (or refusing to run outside a package).
	if err := writeStubManifest(dir, p.Spec); err != nil {
		return err
	}

	specs := append([]string{p.Spec}, p.Extra...)
	var args []string
	switch rt.Manager {
	case ManagerBun:
		args = append([]string{"add", "--cwd", dir}, specs...)
	default:
		args = append([]string{"install", "--prefix", dir, "--no-audit", "--no-fund", "--loglevel", "error"}, specs...)
	}

	if timeout <= 0 {
		timeout = DefaultInstallTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	logging.Info("Installing language server package", "package", p.Spec, "manager", rt.Manager, "dir", dir)
	cmd := exec.CommandContext(runCtx, rt.Install, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"NO_UPDATE_NOTIFIER=1",
		"npm_config_audit=false",
		"npm_config_fund=false",
		"npm_config_update_notifier=false",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("runtime: installing %s with %s failed: %w: %s", p.Spec, rt.Manager, err, tail(out))
	}

	linkBin(dir, p.Bin)
	logging.Info("Language server package installed", "package", p.Spec, "bin", p.Bin)
	return nil
}

// writeStubManifest creates a minimal package.json when the staging directory
// has none, so the package manager treats it as an isolated project.
func writeStubManifest(dir, spec string) error {
	manifest := filepath.Join(dir, "package.json")
	if _, err := os.Stat(manifest); err == nil {
		return nil
	}
	content := fmt.Sprintf(`{
  "name": "pando-lsp-%s",
  "version": "0.0.0",
  "private": true,
  "description": "Language server staged by Pando for %s"
}
`, sanitize(spec), spec)
	if err := os.WriteFile(manifest, []byte(content), 0o644); err != nil {
		return fmt.Errorf("runtime: writing %s: %w", manifest, err)
	}
	return nil
}

// linkBin exposes a staged binary under the shared bin directory. Failures are
// not fatal: Staged also looks inside the package directory itself.
func linkBin(dir, bin string) {
	binDir := BinDir()
	if binDir == "" {
		return
	}
	src := filepath.Join(dir, "node_modules", ".bin", bin)
	if !binaryExists(src) {
		return
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		logging.Debug("Could not create LSP bin directory", "dir", binDir, "error", err)
		return
	}
	dst := filepath.Join(binDir, bin)
	_ = os.Remove(dst)
	if err := os.Symlink(src, dst); err != nil {
		logging.Debug("Could not link staged LSP binary", "src", src, "dst", dst, "error", err)
	}
}

// packageName strips a version suffix from a package spec, keeping any scope.
func packageName(spec string) string {
	s := spec
	scoped := strings.HasPrefix(s, "@")
	if scoped {
		s = s[1:]
	}
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[:i]
	}
	if scoped {
		s = "@" + s
	}
	return s
}

// unscopedName returns the package name without scope or version.
func unscopedName(spec string) string {
	name := packageName(spec)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// tail keeps the end of a command's output, where the actual error lives.
func tail(out []byte) string {
	const max = 400
	s := strings.TrimSpace(string(out))
	if len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max:]
}
