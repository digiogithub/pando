package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/lsp"
	"github.com/spf13/viper"
)

// newLSPTestApp builds a minimal App with just the fields ensureLSPServer needs,
// avoiding the heavy full constructor.
func newLSPTestApp() *App {
	return &App{
		LSPClients:     make(map[string]*lsp.Client),
		lspSpawning:    make(map[string]struct{}),
		lspInstalling:  make(map[string]struct{}),
		lspUnavailable: make(map[string]lspUnavailableEntry),
	}
}

func withStubLookPath(t *testing.T, found bool, calls *int) {
	t.Helper()
	prev := lspLookPath
	var mu sync.Mutex
	lspLookPath = func(file string) (string, error) {
		mu.Lock()
		if calls != nil {
			*calls++
		}
		mu.Unlock()
		if found {
			return "/usr/bin/" + file, nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lspLookPath = prev })
}

func TestEnsureLSPServer_BinaryMissingMarksBroken(t *testing.T) {
	app := newLSPTestApp()
	calls := 0
	withStubLookPath(t, false, &calls)

	s := config.ResolvedLSPServer{Name: "gopls", Command: "gopls", Languages: []string{".go"}}

	if app.ensureLSPServer(context.Background(), s) {
		t.Fatal("expected false when binary is missing")
	}
	if _, ok := app.lspUnavailable["gopls"]; !ok {
		t.Fatal("expected gopls to be marked unavailable")
	}

	// A second attempt must short-circuit on the unavailable set, not re-probe PATH.
	if app.ensureLSPServer(context.Background(), s) {
		t.Fatal("expected false on second attempt")
	}
	if calls != 1 {
		t.Fatalf("expected LookPath to be called once, got %d", calls)
	}
}

func TestEnsureLSPServer_DisabledOrNoCommand(t *testing.T) {
	app := newLSPTestApp()
	withStubLookPath(t, true, nil)

	if app.ensureLSPServer(context.Background(), config.ResolvedLSPServer{Name: "x", Command: "x", Disabled: true}) {
		t.Fatal("disabled server should not start")
	}
	if app.ensureLSPServer(context.Background(), config.ResolvedLSPServer{Name: "y"}) {
		t.Fatal("server without command should not start")
	}
	if len(app.lspSpawning) != 0 {
		t.Fatalf("nothing should be spawning, got %v", app.lspSpawning)
	}
}

func TestEnsureLSPServer_AlreadyRunningOrSpawning(t *testing.T) {
	app := newLSPTestApp()
	calls := 0
	withStubLookPath(t, true, &calls)

	app.LSPClients["gopls"] = &lsp.Client{Languages: []string{".go"}}
	if !app.ensureLSPServer(context.Background(), config.ResolvedLSPServer{Name: "gopls", Command: "gopls"}) {
		t.Fatal("already-running server should report true")
	}

	app.lspSpawning["pyright"] = struct{}{}
	if !app.ensureLSPServer(context.Background(), config.ResolvedLSPServer{Name: "pyright", Command: "pyright-langserver"}) {
		t.Fatal("spawning server should report true")
	}

	if calls != 0 {
		t.Fatalf("LookPath must not be probed for running/spawning servers, got %d", calls)
	}
}

func TestHasRunningClientForFile(t *testing.T) {
	app := newLSPTestApp()
	app.LSPClients["gopls"] = &lsp.Client{Languages: []string{".go"}}
	app.LSPClients["docker"] = &lsp.Client{
		Languages: []string{".dockerfile"},
		Filenames: []string{"Dockerfile"},
	}

	if !app.hasRunningClientForFile("main.go") {
		t.Fatal("expected main.go to be served by running gopls")
	}
	if app.hasRunningClientForFile("main.py") {
		t.Fatal("did not expect main.py to be served")
	}
	if !app.hasRunningClientForFile("build/Dockerfile.dev") {
		t.Fatal("expected the Dockerfile server to claim it by name")
	}
	// An extensionless file no server claims by name must not be handed to a
	// running server of another language.
	if app.hasRunningClientForFile("Makefile") {
		t.Fatal("Makefile must not be claimed by gopls")
	}
}

func TestClientsForFile_SnapshotFiltersByLanguage(t *testing.T) {
	app := newLSPTestApp()
	app.LSPClients["gopls"] = &lsp.Client{Languages: []string{".go"}}
	app.LSPClients["pyright"] = &lsp.Client{Languages: []string{".py"}}

	got := app.ClientsForFile("main.go")
	if len(got) != 1 {
		t.Fatalf("expected 1 client for main.go, got %d", len(got))
	}
	if _, ok := got["gopls"]; !ok {
		t.Fatalf("expected gopls in snapshot, got %v", got)
	}

	// Mutating the snapshot must not affect the app's map.
	delete(got, "gopls")
	if len(app.LSPClients) != 2 {
		t.Fatalf("snapshot must be a copy; app map changed to %d entries", len(app.LSPClients))
	}
}

func TestWaitForFile_WaitsForMatchingClient(t *testing.T) {
	app := newLSPTestApp()
	viper.Reset()
	config.SetForTests(&config.Config{LSPAutoActivate: true, LSP: map[string]config.LSPConfig{}})
	t.Cleanup(func() {
		config.SetForTests(nil)
		viper.Reset()
	})

	app.clientsMutex.Lock()
	app.lspSpawning["gopls"] = struct{}{}
	app.clientsMutex.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go func() {
		time.Sleep(100 * time.Millisecond)
		app.clientsMutex.Lock()
		delete(app.lspSpawning, "gopls")
		app.LSPClients["gopls"] = readyClient(".go")
		app.clientsMutex.Unlock()
	}()

	got := app.WaitForFile(ctx, "main.go")
	if len(got) != 1 {
		t.Fatalf("expected one client after waiting, got %d", len(got))
	}
	if _, ok := got["gopls"]; !ok {
		t.Fatalf("expected gopls after wait, got %v", got)
	}
}

func TestWaitForFile_ReturnsEmptyWhenStartupSettlesBroken(t *testing.T) {
	app := newLSPTestApp()
	viper.Reset()
	config.SetForTests(&config.Config{LSPAutoActivate: true, LSP: map[string]config.LSPConfig{}})
	t.Cleanup(func() {
		config.SetForTests(nil)
		viper.Reset()
	})

	app.clientsMutex.Lock()
	app.lspUnavailable["gopls"] = lspUnavailableEntry{Availability: LSPManual, Reason: "gopls is not installed"}
	app.clientsMutex.Unlock()

	got := app.WaitForFile(context.Background(), "main.go")
	if len(got) != 0 {
		t.Fatalf("expected no clients, got %v", got)
	}
}

// setLSPTestConfig installs a test configuration with the given activation mode.
func setLSPTestConfig(t *testing.T, activateOn string) {
	t.Helper()
	viper.Reset()
	config.SetForTests(&config.Config{
		LSPAutoActivate: true,
		LSPActivateOn:   activateOn,
		LSP:             map[string]config.LSPConfig{},
	})
	t.Cleanup(func() {
		config.SetForTests(nil)
		viper.Reset()
	})
}

// spawnedOrRunning reports whether a server was started or is being started.
func spawnedOrRunning(app *App, name string) bool {
	app.clientsMutex.RLock()
	defer app.clientsMutex.RUnlock()
	_, spawning := app.lspSpawning[name]
	_, running := app.LSPClients[name]
	return spawning || running
}

func TestEnsureLSPForFileTrigger_EditsModeIgnoresReads(t *testing.T) {
	setLSPTestConfig(t, config.LSPActivateEdits)
	calls := 0
	withStubLookPath(t, true, &calls)

	app := newLSPTestApp()
	app.EnsureLSPForFileTrigger(context.Background(), "main.go", config.LSPTriggerRead)
	if calls != 0 {
		t.Fatalf("a read must not probe PATH in %q mode, got %d probes", config.LSPActivateEdits, calls)
	}
	if spawnedOrRunning(app, "gopls") {
		t.Fatal("a read must not start a server in the default mode")
	}

	app.EnsureLSPForFileTrigger(context.Background(), "main.go", config.LSPTriggerWorkspace)
	if calls != 0 {
		t.Fatalf("an external change must not probe PATH in %q mode, got %d probes", config.LSPActivateEdits, calls)
	}
}

func TestEnsureLSPForFileTrigger_EditsAndExplicitActivate(t *testing.T) {
	for _, trigger := range []config.LSPTrigger{config.LSPTriggerEdit, config.LSPTriggerExplicit} {
		t.Run(string(trigger), func(t *testing.T) {
			setLSPTestConfig(t, config.LSPActivateEdits)
			withStubLookPath(t, true, nil)

			app := newLSPTestApp()
			app.EnsureLSPForFileTrigger(context.Background(), "main.go", trigger)
			if !spawnedOrRunning(app, "gopls") {
				t.Fatalf("trigger %q must start gopls in the default mode", trigger)
			}
		})
	}
}

func TestEnsureLSPForFileTrigger_ReadsMode(t *testing.T) {
	setLSPTestConfig(t, config.LSPActivateReads)
	withStubLookPath(t, true, nil)

	app := newLSPTestApp()
	app.EnsureLSPForFileTrigger(context.Background(), "main.go", config.LSPTriggerRead)
	if !spawnedOrRunning(app, "gopls") {
		t.Fatalf("a read must start a server in %q mode", config.LSPActivateReads)
	}
}

func TestEnsureLSPForFileTrigger_OffModeActivatesNothing(t *testing.T) {
	setLSPTestConfig(t, config.LSPActivateOff)
	calls := 0
	withStubLookPath(t, true, &calls)

	app := newLSPTestApp()
	for _, trigger := range []config.LSPTrigger{
		config.LSPTriggerEdit, config.LSPTriggerRead,
		config.LSPTriggerWorkspace, config.LSPTriggerExplicit,
	} {
		app.EnsureLSPForFileTrigger(context.Background(), "main.go", trigger)
	}
	if calls != 0 {
		t.Fatalf("no trigger may probe PATH in %q mode, got %d probes", config.LSPActivateOff, calls)
	}
}

// readyClient builds an LSP client that has finished initialization.
func readyClient(exts ...string) *lsp.Client {
	c := &lsp.Client{Languages: exts}
	c.SetServerState(lsp.StateReady)
	return c
}

func TestWaitForFile_IgnoresClientsThatFailedToInitialize(t *testing.T) {
	app := newLSPTestApp()
	viper.Reset()
	config.SetForTests(&config.Config{
		LSPAutoActivate:   true,
		LSPStartupTimeout: "150ms",
		LSP:               map[string]config.LSPConfig{},
	})
	t.Cleanup(func() {
		config.SetForTests(nil)
		viper.Reset()
	})

	failed := &lsp.Client{Languages: []string{".go"}}
	failed.SetServerState(lsp.StateError)
	app.clientsMutex.Lock()
	app.LSPClients["gopls"] = failed
	app.clientsMutex.Unlock()

	// The client handles the file but never became ready, so the tool must be
	// told nothing is available instead of querying a dead server.
	if got := app.WaitForFile(context.Background(), "main.go"); len(got) != 0 {
		t.Fatalf("expected no ready clients, got %v", got)
	}
	if len(app.ClientsForFile("main.go")) != 1 {
		t.Fatal("ClientsForFile must still expose the registered client")
	}
}

func TestWaitForFile_ExtendsBudgetWhileInstalling(t *testing.T) {
	app := newLSPTestApp()
	viper.Reset()
	config.SetForTests(&config.Config{
		LSPAutoActivate:   true,
		LSPStartupTimeout: "100ms",
		LSPInstallTimeout: "3s",
		LSP:               map[string]config.LSPConfig{},
	})
	t.Cleanup(func() {
		config.SetForTests(nil)
		viper.Reset()
	})

	app.clientsMutex.Lock()
	app.lspSpawning["gopls"] = struct{}{}
	app.lspInstalling["gopls"] = struct{}{}
	app.clientsMutex.Unlock()

	// The server becomes ready well after the startup budget but inside the
	// install budget.
	go func() {
		time.Sleep(400 * time.Millisecond)
		app.clientsMutex.Lock()
		delete(app.lspSpawning, "gopls")
		delete(app.lspInstalling, "gopls")
		app.LSPClients["gopls"] = readyClient(".go")
		app.clientsMutex.Unlock()
	}()

	got := app.WaitForFile(context.Background(), "main.go")
	if len(got) != 1 {
		t.Fatalf("expected the wait to be extended for the install, got %d clients", len(got))
	}
}

func TestUnavailableReason(t *testing.T) {
	t.Run("activation disabled", func(t *testing.T) {
		viper.Reset()
		config.SetForTests(&config.Config{LSPAutoActivate: false, LSP: map[string]config.LSPConfig{}})
		t.Cleanup(func() { config.SetForTests(nil); viper.Reset() })

		got := newLSPTestApp().UnavailableReason("main.go")
		if !strings.Contains(got, "disabled") {
			t.Fatalf("reason = %q", got)
		}
	})

	t.Run("no server for the extension", func(t *testing.T) {
		setLSPTestConfig(t, config.LSPActivateEdits)

		got := newLSPTestApp().UnavailableReason("notes.unknownext")
		if !strings.Contains(got, ".unknownext") {
			t.Fatalf("reason = %q", got)
		}
	})

	t.Run("manual install carries the hint", func(t *testing.T) {
		setLSPTestConfig(t, config.LSPActivateEdits)
		withStubLookPath(t, false, nil)

		app := newLSPTestApp()
		app.EnsureLSPForFileTrigger(context.Background(), "main.go", config.LSPTriggerEdit)

		got := app.UnavailableReason("main.go")
		if !strings.Contains(got, "go install golang.org/x/tools/gopls@latest") {
			t.Fatalf("reason must tell the user how to install gopls, got %q", got)
		}
	})

	t.Run("still starting", func(t *testing.T) {
		setLSPTestConfig(t, config.LSPActivateEdits)

		app := newLSPTestApp()
		app.clientsMutex.Lock()
		app.lspSpawning["gopls"] = struct{}{}
		app.clientsMutex.Unlock()

		if got := app.UnavailableReason("main.go"); !strings.Contains(got, "still starting") {
			t.Fatalf("reason = %q", got)
		}
	})

	t.Run("installing", func(t *testing.T) {
		setLSPTestConfig(t, config.LSPActivateEdits)

		app := newLSPTestApp()
		app.clientsMutex.Lock()
		app.lspInstalling["typescript-language-server"] = struct{}{}
		app.clientsMutex.Unlock()

		if got := app.UnavailableReason("main.ts"); !strings.Contains(got, "still being installed") {
			t.Fatalf("reason = %q", got)
		}
	})

	t.Run("empty when a ready client exists", func(t *testing.T) {
		setLSPTestConfig(t, config.LSPActivateEdits)

		app := newLSPTestApp()
		app.LSPClients["gopls"] = readyClient(".go")
		if got := app.UnavailableReason("main.go"); got != "" {
			t.Fatalf("reason = %q, want empty", got)
		}
	})
}
