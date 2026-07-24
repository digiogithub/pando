package app

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/lsp"
	"github.com/digiogithub/pando/internal/lsp/runtime"
)

// statusesByName indexes a status report for assertions.
func statusesByName(t *testing.T, app *App) map[string]LSPServerStatus {
	t.Helper()
	out := map[string]LSPServerStatus{}
	for _, st := range app.LSPServerStatuses() {
		out[st.Name] = st
	}
	return out
}

func TestLSPServerStatuses_Availability(t *testing.T) {
	setResolveConfig(t, true)
	withStubLookPath(t, false, nil)
	defer runtime.SetForTests(runtime.NodeRuntime{Manager: runtime.ManagerBun, Install: "/usr/bin/bun"})()

	app := newLSPTestApp()
	byName := statusesByName(t, app)

	pyright, ok := byName["pyright"]
	if !ok {
		t.Fatal("pyright missing from the status report")
	}
	if pyright.Availability != "installable" {
		t.Fatalf("pyright availability = %q (%q)", pyright.Availability, pyright.Reason)
	}
	if !strings.Contains(pyright.AvailabilityLabel, "bun") {
		t.Fatalf("the label must name the package manager, got %q", pyright.AvailabilityLabel)
	}

	gopls := byName["gopls"]
	if gopls.Availability != "manual" {
		t.Fatalf("gopls availability = %q", gopls.Availability)
	}
	if gopls.Hint == "" || !strings.Contains(gopls.Hint, "go install") {
		t.Fatalf("a manual server must carry an actionable hint, got %q", gopls.Hint)
	}

	// Opt-in presets are reported as opt-in, not as disabled by the user.
	biome := byName["biome"]
	if !biome.OptIn || biome.Disabled {
		t.Fatalf("biome must be reported as opt-in and not disabled, got %+v", biome)
	}
	if biome.Configured {
		t.Fatal("an undeclared preset must not be reported as configured")
	}
}

func TestLSPServerStatuses_RunStateAndInstalling(t *testing.T) {
	setResolveConfig(t, true)
	withStubLookPath(t, true, nil)
	defer runtime.SetForTests(runtime.NodeRuntime{})()

	app := newLSPTestApp()
	ready := &lsp.Client{Languages: []string{".go"}}
	ready.SetServerState(lsp.StateReady)
	app.LSPClients["gopls"] = ready
	app.lspSpawning["pyright"] = struct{}{}
	app.lspInstalling["pyright"] = struct{}{}

	byName := statusesByName(t, app)
	if got := byName["gopls"].RunState; got != "ready" {
		t.Fatalf("gopls run state = %q", got)
	}
	if got := byName["gopls"].ResolvedCommand; got != "/usr/bin/gopls" {
		t.Fatalf("resolved command = %q, want the absolute path", got)
	}
	if got := byName["gopls"].Command; got != "gopls" {
		t.Fatalf("Command must stay the configured one, got %q", got)
	}
	if st := byName["pyright"]; st.RunState != "starting" || !st.Installing {
		t.Fatalf("pyright status = %+v", st)
	}
	if got := byName["marksman"].RunState; got != "stopped" {
		t.Fatalf("an idle server must be stopped, got %q", got)
	}
}

func TestLSPServerStatuses_ConfiguredFirst(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	config.SetForTests(&config.Config{
		LSPAutoActivate: true,
		LSPAutoInstall:  true,
		LSP:             map[string]config.LSPConfig{"gopls": {Autostart: true}},
	})
	t.Cleanup(func() { config.SetForTests(nil) })
	withStubLookPath(t, true, nil)

	statuses := newLSPTestApp().LSPServerStatuses()
	if len(statuses) == 0 {
		t.Fatal("empty status report")
	}
	if statuses[0].Name != "gopls" || !statuses[0].Configured {
		t.Fatalf("the configured server must come first, got %+v", statuses[0])
	}
	if !statuses[0].Autostart {
		t.Fatal("Autostart must be reported")
	}
}

func TestLSPCatalogMirrorsStatuses(t *testing.T) {
	setResolveConfig(t, true)
	withStubLookPath(t, false, nil)
	defer runtime.SetForTests(runtime.NodeRuntime{})()

	app := newLSPTestApp()
	statuses := app.LSPServerStatuses()
	catalog := app.LSPCatalog()
	if len(catalog) != len(statuses) {
		t.Fatalf("catalog has %d entries, statuses %d", len(catalog), len(statuses))
	}
	for i := range catalog {
		if catalog[i].Name != statuses[i].Name || catalog[i].Availability != statuses[i].Availability {
			t.Fatalf("entry %d diverges: %+v vs %+v", i, catalog[i], statuses[i])
		}
	}
}
