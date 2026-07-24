package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/lsp/runtime"
)

// LSPAvailability describes whether a language server can be started.
type LSPAvailability int

const (
	// LSPAvailable means the binary was resolved and the server can start now.
	LSPAvailable LSPAvailability = iota
	// LSPInstallable means Pando can provision the binary itself before
	// starting the server.
	LSPInstallable
	// LSPManual means the binary is missing and only the user can install it.
	LSPManual
)

// LSPResolution is the outcome of resolving a server's executable.
type LSPResolution struct {
	// Command and Args are the process to spawn. They are only meaningful when
	// Availability is LSPAvailable.
	Command string
	Args    []string
	// Availability says whether the server can start, could start after an
	// install, or needs the user to act.
	Availability LSPAvailability
	// Reason explains, in a sentence a user can act on, why the server is not
	// available. Empty when Availability is LSPAvailable.
	Reason string
}

// lspUnavailableEntry records why a server cannot run, so the reason survives
// all the way to the diagnostics tool instead of being flattened into an opaque
// "broken" flag.
type lspUnavailableEntry struct {
	Availability LSPAvailability
	Reason       string
}

// resolveLSPCommand decides how (or whether) a language server can be started.
//
// Resolution order: the configured command on PATH, then a binary Pando already
// staged for it, then an install Pando could perform. It never runs the install
// itself — that happens on the spawn path, where it can block in the background
// (see installLSPServer).
func (app *App) resolveLSPCommand(s config.ResolvedLSPServer) LSPResolution {
	if s.Command == "" {
		return LSPResolution{Availability: LSPManual, Reason: fmt.Sprintf("%s has no command configured", s.Name)}
	}

	if path, err := lspLookPath(s.Command); err == nil {
		return LSPResolution{Command: path, Args: s.Args, Availability: LSPAvailable}
	}

	pkg, ok := installPackage(s)
	if !ok {
		return LSPResolution{
			Availability: LSPManual,
			Reason:       manualReason(s),
		}
	}

	// A previous session may already have staged this package.
	if path, staged := runtime.Staged(pkg); staged {
		return LSPResolution{Command: path, Args: s.Args, Availability: LSPAvailable}
	}

	cfg := config.Get()
	if runner := cfg.LSPRunnerMode(); runner == config.LSPRunnerOff {
		return LSPResolution{
			Availability: LSPManual,
			Reason: fmt.Sprintf("%s is not installed and npm-based provisioning is off (LSPRunner = \"off\"; run: %s)",
				s.Command, installHint(s)),
		}
	}
	if !cfg.LSPAutoInstallEnabled() {
		return LSPResolution{
			Availability: LSPManual,
			Reason: fmt.Sprintf("%s is not installed and automatic installation is off (set LSPAutoInstall = true, or run: %s)",
				s.Command, installHint(s)),
		}
	}

	rt := runtime.Detect()
	if !rt.Available() {
		return LSPResolution{
			Availability: LSPManual,
			Reason:       fmt.Sprintf("%s is not installed and %s (run: %s)", s.Command, noRunnerReason(cfg), installHint(s)),
		}
	}

	return LSPResolution{
		Availability: LSPInstallable,
		Reason:       fmt.Sprintf("%s is being installed with %s", pkg.Spec, rt.Manager),
	}
}

// noRunnerReason explains why no package manager is usable: either none is
// installed, or LSPRunner pinned one that is missing.
func noRunnerReason(cfg *config.Config) string {
	switch cfg.LSPRunnerMode() {
	case config.LSPRunnerBun:
		return "bun is not available to install it (LSPRunner = \"bun\")"
	case config.LSPRunnerNpm:
		return "npm is not available to install it (LSPRunner = \"npm\")"
	default:
		return "neither bun nor node is available to install it"
	}
}

// installPackage maps a server to the npm package that provides it.
func installPackage(s config.ResolvedLSPServer) (runtime.Package, bool) {
	in := s.Install
	if in.Strategy != config.LSPInstallNpm || in.Package == "" {
		return runtime.Package{}, false
	}
	bin := in.Bin
	if bin == "" {
		bin = s.Command
	}
	return runtime.Package{Spec: in.Package, Bin: bin, Extra: in.ExtraPackages}, true
}

// manualReason explains what the user has to do to get a server Pando cannot
// install on its own.
func manualReason(s config.ResolvedLSPServer) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s is not installed", s.Command)
	if hint := s.Install.Hint; hint != "" {
		fmt.Fprintf(&b, " (install it with: %s", hint)
		if s.Install.URL != "" {
			fmt.Fprintf(&b, " — %s", s.Install.URL)
		}
		b.WriteString(")")
	} else if s.Install.URL != "" {
		fmt.Fprintf(&b, " (see %s)", s.Install.URL)
	}
	return b.String()
}

// installHint is the command a user can run themselves, falling back to the
// package name when the preset carries no explicit hint.
func installHint(s config.ResolvedLSPServer) string {
	if s.Install.Hint != "" {
		return s.Install.Hint
	}
	if s.Install.Package != "" {
		return "npm install -g " + s.Install.Package
	}
	return "install " + s.Command
}

// installLSPServer provisions a server's binary and returns the command to
// spawn. It blocks for as long as the install takes, so callers must run it off
// the request path.
func (app *App) installLSPServer(ctx context.Context, s config.ResolvedLSPServer) (LSPResolution, error) {
	pkg, ok := installPackage(s)
	if !ok {
		return LSPResolution{Availability: LSPManual, Reason: manualReason(s)}, fmt.Errorf("no install recipe for %s", s.Name)
	}

	app.clientsMutex.Lock()
	app.lspInstalling[s.Name] = struct{}{}
	app.clientsMutex.Unlock()
	defer func() {
		app.clientsMutex.Lock()
		delete(app.lspInstalling, s.Name)
		app.clientsMutex.Unlock()
	}()

	res, err := runtime.Ensure(ctx, pkg, config.Get().LSPInstallWait())
	if err != nil {
		logging.Warn("Could not install language server", "name", s.Name, "package", pkg.Spec, "error", err)
		return LSPResolution{
			Availability: LSPManual,
			Reason:       fmt.Sprintf("%s could not be installed automatically (run: %s)", pkg.Spec, installHint(s)),
		}, err
	}

	// An ephemeral runner needs its own arguments before the server's.
	args := append(append([]string{}, res.Args...), s.Args...)
	return LSPResolution{Command: res.Command, Args: args, Availability: LSPAvailable}, nil
}

// lspInstallInProgress reports whether any server that handles the file is
// currently being installed, so callers can extend their wait.
func (app *App) lspInstallInProgress(path string) bool {
	cfg := config.Get()
	candidates := cfg.LSPServersForFile(path)
	if len(candidates) == 0 {
		return false
	}
	app.clientsMutex.RLock()
	defer app.clientsMutex.RUnlock()
	if len(app.lspInstalling) == 0 {
		return false
	}
	for _, s := range candidates {
		if _, ok := app.lspInstalling[s.Name]; ok {
			return true
		}
	}
	return false
}

// lspFileLabel names the file type in user-facing messages: its extension when
// it has one, otherwise its base name (servers such as dockerfile-language-server
// match by name).
func lspFileLabel(path string) string {
	if ext := strings.ToLower(filepath.Ext(path)); ext != "" {
		return ext
	}
	if base := filepath.Base(path); base != "." && base != string(filepath.Separator) {
		return base
	}
	return "these"
}

// UnavailableReason explains why no ready language server could be obtained for
// a file, in terms the user can act on: a missing binary with its install
// command, an install still running, or activation being switched off. It
// returns an empty string when a ready server exists.
func (app *App) UnavailableReason(path string) string {
	if len(app.readyClientsForFile(path)) > 0 {
		return ""
	}

	cfg := config.Get()
	if !cfg.LSPActivationAllows(config.LSPTriggerExplicit) {
		return "on-demand language server activation is disabled (LSPAutoActivate / LSPActivateOn)"
	}

	ext := lspFileLabel(path)
	candidates := cfg.LSPServersForFile(path)
	if len(candidates) == 0 {
		return fmt.Sprintf("no language server is configured for %s files (add one under [LSP] in the Pando config)", ext)
	}

	if app.lspInstallInProgress(path) {
		return fmt.Sprintf("the language server for %s files is still being installed; run diagnostics again in a moment", ext)
	}

	reasons := make([]string, 0, len(candidates))
	for _, s := range candidates {
		if s.Disabled || s.Command == "" {
			continue
		}
		app.clientsMutex.RLock()
		_, spawning := app.lspSpawning[s.Name]
		entry, unavailable := app.lspUnavailable[s.Name]
		_, running := app.LSPClients[s.Name]
		app.clientsMutex.RUnlock()

		switch {
		case spawning:
			return fmt.Sprintf("the language server for %s files is still starting; run diagnostics again in a moment", ext)
		case running:
			// Registered but not ready: initialization failed.
			reasons = append(reasons, fmt.Sprintf("%s started but did not become ready", s.Name))
		case unavailable && entry.Reason != "":
			reasons = append(reasons, entry.Reason)
		default:
			// Never attempted (activation may be off for this trigger): resolve
			// now so the answer is still actionable.
			if res := app.resolveLSPCommand(s); res.Reason != "" {
				reasons = append(reasons, res.Reason)
			}
		}
	}

	if len(reasons) == 0 {
		return fmt.Sprintf("no language server is available for %s files", ext)
	}
	return fmt.Sprintf("no language server available for %s files: %s", ext, strings.Join(reasons, "; "))
}
