package app

import (
	"context"
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
		logging.Info("LSP on-demand activation enabled; servers start when matching files are edited")
		// A workspace-wide bootstrap watcher catches edits made outside Pando
		// (external editors, build steps) and lazily activates their servers.
		app.startLSPBootstrapWatcher(ctx)
	}
	logging.Info("LSP clients initialization started in background")
}

// EnsureLSPForFile lazily activates the language server(s) that handle the given
// file's extension. It is safe to call frequently (e.g. on every edit/open):
// servers already running, currently spawning, or known-broken are skipped, and
// a server whose binary is not on PATH is recorded so it is not retried. When
// several preset servers handle the same extension only the first installed one
// is started, while servers the user configured explicitly are always honored.
func (app *App) EnsureLSPForFile(ctx context.Context, path string) {
	cfg := config.Get()
	if cfg == nil || !cfg.LSPAutoActivate {
		return
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return
	}
	candidates := cfg.LSPServersForExt(ext)
	if len(candidates) == 0 {
		return
	}

	// Treat the extension as already served if a running client handles it.
	presetSatisfied := app.hasRunningClientForExt(ext)
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
// currently spawning, or known-broken. It returns true when the server is now
// running or being started (i.e. it satisfies its language), and false when the
// binary is missing or the server is marked broken.
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
	if _, ok := app.lspBroken[s.Name]; ok {
		app.clientsMutex.Unlock()
		return false
	}
	// Only commit to a spawn if the binary is actually available.
	if _, err := lspLookPath(s.Command); err != nil {
		app.lspBroken[s.Name] = struct{}{}
		app.clientsMutex.Unlock()
		logging.Debug("LSP server binary not found on PATH; skipping", "name", s.Name, "command", s.Command)
		return false
	}
	app.lspSpawning[s.Name] = struct{}{}
	app.clientsMutex.Unlock()

	logging.Info("Activating LSP server on demand", "name", s.Name, "command", s.Command)

	go func() {
		app.createAndStartLSPClient(ctx, s.Name, s.Command, s.Args...)

		app.clientsMutex.Lock()
		delete(app.lspSpawning, s.Name)
		// createAndStartLSPClient only registers the client on success; if it is
		// absent here the start failed, so mark it broken to avoid retrying it on
		// every keystroke.
		if _, ok := app.LSPClients[s.Name]; !ok {
			app.lspBroken[s.Name] = struct{}{}
		}
		app.clientsMutex.Unlock()
	}()
	return true
}

// hasRunningClientForExt reports whether a running LSP client already handles
// the given file extension.
func (app *App) hasRunningClientForExt(ext string) bool {
	probe := "probe" + ext
	app.clientsMutex.RLock()
	defer app.clientsMutex.RUnlock()
	for _, c := range app.LSPClients {
		if c.HandlesFile(probe) {
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
		logging.Error("Failed to create LSP client for", name, err)
		return
	}

	// Propagate the language filter from the resolved registry (presets + user
	// config), so on-demand servers not present in cfg.LSP are still filtered.
	for _, rs := range config.Get().LSPRegistry() {
		if rs.Name == name {
			lspClient.Languages = rs.Languages
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
	app.createAndStartLSPClient(ctx, name, server.Command, server.Args...)
	logging.Info("Successfully restarted LSP client", "client", name)
}
