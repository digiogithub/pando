package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/lsp"
	"github.com/digiogithub/pando/internal/lsp/watcher"
)

// Ensure *App satisfies the LSP provider consumed by the file tools.
var _ tools.LSPProvider = (*App)(nil)

// lspLookPath resolves a server binary on PATH. It is a package variable so
// tests can stub it without spawning real processes.
var lspLookPath = exec.LookPath

// initLSPClients eagerly starts only the language servers explicitly marked
// Autostart in the configuration. Every other server (including the built-in
// presets) is activated on demand the first time a file of its language is
// touched — see EnsureLSPForFile. This avoids, for example, starting gopls in a
// project that contains no Go files.
func (app *App) initLSPClients(ctx context.Context) {
	cfg := config.Get()

	for _, s := range cfg.LSPAutostartServers() {
		app.ensureLSPServer(ctx, s)
	}

	if cfg.LSPAutoActivate {
		logging.Info("LSP on-demand activation enabled", "activateOn", cfg.LSPActivationMode())
	}
	if cfg.LSPWorkspaceWatchEnabled() {
		// A workspace-wide bootstrap watcher catches edits made outside Pando
		// (external editors, build steps) and lazily activates their servers.
		// Opt-in: it can start servers for files Pando never touches.
		app.startLSPBootstrapWatcher(ctx)
	}
	logging.Info("LSP clients initialization started in background")
}

// EnsureLSPForFile lazily activates the language server(s) for a file Pando is
// about to modify. It is shorthand for EnsureLSPForFileTrigger with the edit
// trigger.
func (app *App) EnsureLSPForFile(ctx context.Context, path string) {
	app.EnsureLSPForFileTrigger(ctx, path, config.LSPTriggerEdit)
}

// EnsureLSPForFileTrigger lazily activates the language server(s) that handle
// the given file's extension, provided the configuration allows the trigger to
// start a server (see Config.LSPActivateOn).
//
// It is safe to call frequently (e.g. on every edit): servers already running,
// currently spawning, or known-broken are skipped, and a server whose binary is
// not on PATH is recorded so it is not retried. When several preset servers
// handle the same extension only the first installed one is started, while
// servers the user configured explicitly are always honored.
func (app *App) EnsureLSPForFileTrigger(ctx context.Context, path string, trigger config.LSPTrigger) {
	cfg := config.Get()
	if !cfg.LSPActivationAllows(trigger) {
		return
	}
	candidates := cfg.LSPServersForFile(path)
	if len(candidates) == 0 {
		return
	}

	// Treat the file type as already served if a running client handles it.
	presetSatisfied := app.hasRunningClientForFile(path)
	for _, s := range candidates {
		isUserConfigured := s.Source != "preset"
		if !isUserConfigured && presetSatisfied {
			// Don't start a second preset server for the same language.
			continue
		}
		if app.ensureLSPServer(ctx, s) && !isUserConfigured {
			presetSatisfied = true
		}
	}
}

// ensureLSPServer starts the given server unless it is already running,
// currently spawning, or known to be unavailable. It returns true when the
// server is now running or being started (i.e. it satisfies its language), and
// false when the binary is missing and Pando cannot provision it.
//
// When the binary is missing but the server ships as an npm package, the
// install runs in the background and the server starts as soon as it finishes;
// the server counts as satisfied in the meantime so no second preset is started
// for the same language.
func (app *App) ensureLSPServer(ctx context.Context, s config.ResolvedLSPServer) bool {
	if s.Disabled || s.Command == "" {
		return false
	}

	app.clientsMutex.Lock()
	if _, ok := app.LSPClients[s.Name]; ok {
		app.clientsMutex.Unlock()
		return true
	}
	if _, ok := app.lspSpawning[s.Name]; ok {
		app.clientsMutex.Unlock()
		return true
	}
	if _, ok := app.lspUnavailable[s.Name]; ok {
		app.clientsMutex.Unlock()
		return false
	}
	app.clientsMutex.Unlock()

	// Resolving is cheap (a PATH lookup plus two stat calls) and must not run
	// under the lock: the install it may schedule can take minutes.
	res := app.resolveLSPCommand(s)
	if res.Availability == LSPManual {
		app.markLSPUnavailable(s.Name, res)
		logging.Debug("LSP server unavailable; skipping", "name", s.Name, "reason", res.Reason)
		return false
	}

	app.clientsMutex.Lock()
	// Another goroutine may have won the race while we were resolving.
	if _, ok := app.lspSpawning[s.Name]; ok {
		app.clientsMutex.Unlock()
		return true
	}
	if _, ok := app.LSPClients[s.Name]; ok {
		app.clientsMutex.Unlock()
		return true
	}
	app.lspSpawning[s.Name] = struct{}{}
	app.clientsMutex.Unlock()

	logging.Info("Activating LSP server on demand",
		"name", s.Name, "command", s.Command, "installing", res.Availability == LSPInstallable)

	go func() {
		start := res
		if res.Availability == LSPInstallable {
			installed, err := app.installLSPServer(ctx, s)
			if err != nil {
				app.clientsMutex.Lock()
				delete(app.lspSpawning, s.Name)
				app.clientsMutex.Unlock()
				app.markLSPUnavailable(s.Name, installed)
				return
			}
			start = installed
		}

		app.createAndStartLSPClient(ctx, s.Name, start.Command, start.Args...)

		app.clientsMutex.Lock()
		delete(app.lspSpawning, s.Name)
		// createAndStartLSPClient only registers the client on success; if it is
		// absent here the start failed, so record it to avoid retrying on every
		// keystroke.
		_, started := app.LSPClients[s.Name]
		app.clientsMutex.Unlock()
		if !started {
			app.markLSPUnavailable(s.Name, LSPResolution{
				Availability: LSPManual,
				Reason:       fmt.Sprintf("%s failed to start (see the Pando log for the server's output)", s.Name),
			})
		}
	}()
	return true
}

// markLSPUnavailable records why a server cannot be used, so the diagnostics
// tool can tell the user instead of reporting a bare absence.
func (app *App) markLSPUnavailable(name string, res LSPResolution) {
	app.clientsMutex.Lock()
	defer app.clientsMutex.Unlock()
	app.lspUnavailable[name] = lspUnavailableEntry{Availability: res.Availability, Reason: res.Reason}
}

// hasRunningClientForFile reports whether a running LSP client already handles
// the given file.
func (app *App) hasRunningClientForFile(path string) bool {
	app.clientsMutex.RLock()
	defer app.clientsMutex.RUnlock()
	for _, c := range app.LSPClients {
		if c.HandlesFile(path) {
			return true
		}
	}
	return false
}

// EnsureForFile implements the lazy-activation half of the LSP provider used by
// the tools. It is a thin wrapper around EnsureLSPForFile.
func (app *App) EnsureForFile(ctx context.Context, path string) {
	app.EnsureLSPForFile(ctx, path)
}

// EnsureForFileTrigger implements the trigger-aware half of the LSP provider,
// letting each tool declare why it wants a language server.
func (app *App) EnsureForFileTrigger(ctx context.Context, path string, trigger config.LSPTrigger) {
	app.EnsureLSPForFileTrigger(ctx, path, trigger)
}

// WaitForFile waits for a lazily spawned LSP client to become *ready* for the
// requested file, so a tool call never queries a server that is still starting
// up. It returns early when startup settles (ready or unavailable).
//
// The wait is bounded by LSPStartupTimeout, extended once to LSPInstallTimeout
// when the matching server is still being downloaded — a first-run install is
// an order of magnitude slower than a cold start.
func (app *App) WaitForFile(ctx context.Context, path string) map[string]*lsp.Client {
	if clients := app.readyClientsForFile(path); len(clients) > 0 {
		return clients
	}

	cfg := config.Get()
	budget := cfg.LSPStartupWait()
	start := time.Now()

	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	extended := false
	for {
		select {
		case <-ctx.Done():
			return app.readyClientsForFile(path)
		case <-deadline.C:
			return app.readyClientsForFile(path)
		case <-ticker.C:
			if clients := app.readyClientsForFile(path); len(clients) > 0 {
				return clients
			}
			if !extended && app.lspInstallInProgress(path) {
				extended = true
				if remaining := cfg.LSPInstallWait() - time.Since(start); remaining > 0 {
					deadline.Reset(remaining)
				}
			}
			if app.lspStartupSettled(path) {
				return app.readyClientsForFile(path)
			}
		}
	}
}

// readyClientsForFile returns the clients that handle the file and have
// completed initialization. A server that failed to initialize is excluded: it
// answers no requests, and reporting its absence lets the caller explain why.
func (app *App) readyClientsForFile(path string) map[string]*lsp.Client {
	app.clientsMutex.RLock()
	defer app.clientsMutex.RUnlock()
	out := make(map[string]*lsp.Client)
	for name, c := range app.LSPClients {
		if c.HandlesFile(path) && c.GetServerState() == lsp.StateReady {
			out[name] = c
		}
	}
	return out
}

func (app *App) lspStartupSettled(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return true
	}
	cfg := config.Get()
	// A caller waiting for a file always did so through an explicit request, so
	// only a fully disabled activation makes the wait pointless.
	if !cfg.LSPActivationAllows(config.LSPTriggerExplicit) {
		return true
	}
	candidates := cfg.LSPServersForExt(ext)
	if len(candidates) == 0 {
		return true
	}

	app.clientsMutex.RLock()
	defer app.clientsMutex.RUnlock()
	for _, s := range candidates {
		if s.Disabled || s.Command == "" {
			continue
		}
		if _, ok := app.LSPClients[s.Name]; ok {
			return true
		}
		if _, ok := app.lspSpawning[s.Name]; ok {
			return false
		}
		if _, ok := app.lspUnavailable[s.Name]; ok {
			continue
		}
		return false
	}
	return true
}

// ClientsForFile returns a snapshot of the running LSP clients that handle the
// given file. The returned map is a copy, safe to iterate without holding the
// app lock while clients are added or removed concurrently.
func (app *App) ClientsForFile(path string) map[string]*lsp.Client {
	app.clientsMutex.RLock()
	defer app.clientsMutex.RUnlock()
	out := make(map[string]*lsp.Client)
	for name, c := range app.LSPClients {
		if c.HandlesFile(path) {
			out[name] = c
		}
	}
	return out
}

// Clients returns a snapshot copy of all running LSP clients.
func (app *App) Clients() map[string]*lsp.Client {
	app.clientsMutex.RLock()
	defer app.clientsMutex.RUnlock()
	out := make(map[string]*lsp.Client, len(app.LSPClients))
	maps.Copy(out, app.LSPClients)
	return out
}

// createAndStartLSPClient creates a new LSP client, initializes it, and starts its workspace watcher
func (app *App) createAndStartLSPClient(ctx context.Context, name string, command string, args ...string) {
	// Create a specific context for initialization with a timeout
	logging.Info("Creating LSP client", "name", name, "command", command, "args", args)

	// Create the LSP client
	lspClient, err := lsp.NewClient(ctx, command, args...)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			logging.Error("LSP binary not found while starting client", "name", name, "command", command, "error", err)
		} else {
			logging.Error("Failed to create LSP client", "name", name, "command", command, "error", err)
		}
		return
	}

	// Propagate the language filter from the resolved registry (presets + user
	// config), so on-demand servers not present in cfg.LSP are still filtered.
	for _, rs := range config.Get().LSPRegistry() {
		if rs.Name == name {
			lspClient.Languages = rs.Languages
			lspClient.Filenames = rs.Filenames
			break
		}
	}

	// Create a longer timeout for initialization (some servers take time to start)
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Initialize with the initialization context
	_, err = lspClient.InitializeLSPClient(initCtx, config.WorkingDirectory())
	if err != nil {
		logging.Error("Initialize failed", "name", name, "error", err)
		// Clean up the client to prevent resource leaks
		lspClient.Close()
		return
	}

	// Wait for the server to be ready
	if err := lspClient.WaitForServerReady(initCtx); err != nil {
		logging.Error("Server failed to become ready", "name", name, "error", err)
		// We'll continue anyway, as some functionality might still work
		lspClient.SetServerState(lsp.StateError)
	} else {
		logging.Info("LSP server is ready", "name", name)
		lspClient.SetServerState(lsp.StateReady)
	}

	logging.Info("LSP client initialized", "name", name)

	// Create a child context that can be canceled when the app is shutting down
	watchCtx, cancelFunc := context.WithCancel(ctx)

	// Create a context with the server name for better identification
	watchCtx = context.WithValue(watchCtx, "serverName", name)

	// Create the workspace watcher
	workspaceWatcher := watcher.NewWorkspaceWatcher(lspClient)

	// Store the cancel function to be called during cleanup
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancelFunc)
	app.cancelFuncsMutex.Unlock()

	// Add the watcher to a WaitGroup to track active goroutines
	app.watcherWG.Add(1)

	// Add to map with mutex protection before starting goroutine
	app.clientsMutex.Lock()
	app.LSPClients[name] = lspClient
	app.clientsMutex.Unlock()

	go app.runWorkspaceWatcher(watchCtx, name, workspaceWatcher)
}

// runWorkspaceWatcher executes the workspace watcher for an LSP client
func (app *App) runWorkspaceWatcher(ctx context.Context, name string, workspaceWatcher *watcher.WorkspaceWatcher) {
	defer app.watcherWG.Done()
	defer logging.RecoverPanic("LSP-"+name, func() {
		// Try to restart the client
		app.restartLSPClient(ctx, name)
	})

	workspaceWatcher.WatchWorkspace(ctx, config.WorkingDirectory())
	logging.Info("Workspace watcher stopped", "client", name)
}

// restartLSPClient attempts to restart a crashed or failed LSP client
func (app *App) restartLSPClient(ctx context.Context, name string) {
	// Resolve the server configuration from the registry (presets + user config)
	// so on-demand servers not present in cfg.LSP can also be restarted.
	var server config.ResolvedLSPServer
	found := false
	for _, rs := range config.Get().LSPRegistry() {
		if rs.Name == name {
			server = rs
			found = true
			break
		}
	}
	if !found || server.Command == "" {
		logging.Error("Cannot restart client, configuration not found", "client", name)
		return
	}

	// Resolve the executable again: a server Pando installed itself lives in the
	// staging directory, not on PATH, so the raw configured command would fail.
	res := app.resolveLSPCommand(server)
	if res.Availability != LSPAvailable {
		logging.Error("Cannot restart client, its binary is no longer available", "client", name, "reason", res.Reason)
		app.markLSPUnavailable(name, res)
		return
	}

	// Clean up the old client if it exists
	app.clientsMutex.Lock()
	oldClient, exists := app.LSPClients[name]
	if exists {
		delete(app.LSPClients, name) // Remove from map before potentially slow shutdown
	}
	app.clientsMutex.Unlock()

	if exists && oldClient != nil {
		// Try to shut it down gracefully, but don't block on errors
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = oldClient.Shutdown(shutdownCtx)
		cancel()
	}

	// Create a new client using the shared function
	app.createAndStartLSPClient(ctx, name, res.Command, res.Args...)
	logging.Info("Successfully restarted LSP client", "client", name)
}
