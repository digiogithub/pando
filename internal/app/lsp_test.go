package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/lsp"
)

// newLSPTestApp builds a minimal App with just the fields ensureLSPServer needs,
// avoiding the heavy full constructor.
func newLSPTestApp() *App {
	return &App{
		LSPClients:  make(map[string]*lsp.Client),
		lspSpawning: make(map[string]struct{}),
		lspBroken:   make(map[string]struct{}),
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
	if _, ok := app.lspBroken["gopls"]; !ok {
		t.Fatal("expected gopls to be marked broken")
	}

	// A second attempt must short-circuit on the broken set, not re-probe PATH.
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

func TestHasRunningClientForExt(t *testing.T) {
	app := newLSPTestApp()
	app.LSPClients["gopls"] = &lsp.Client{Languages: []string{".go"}}

	if !app.hasRunningClientForExt(".go") {
		t.Fatal("expected .go to be served by running gopls")
	}
	if app.hasRunningClientForExt(".py") {
		t.Fatal("did not expect .py to be served")
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
