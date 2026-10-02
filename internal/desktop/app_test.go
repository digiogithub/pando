package desktop

import (
	"strings"
	"testing"
)

func TestDomReadyScriptInjectsBridgeOnlyAfterRegistration(t *testing.T) {
	a := NewApp("http://127.0.0.1:8765", false)

	if s := a.domReadyScript(); strings.Contains(s, "BRIDGE_MARKER") {
		t.Fatal("script contains a bridge before one was registered")
	}

	a.RegisterRuntimeBridge("/*ipc*/", "window.BRIDGE_MARKER = 1;")
	s := a.domReadyScript()
	if !strings.Contains(s, "window.BRIDGE_MARKER = 1;") {
		t.Fatal("registered bridge is not inlined into the DOM-ready script")
	}
	if !strings.Contains(s, `var url = "http://127.0.0.1:8765";`) {
		t.Fatalf("pando URL not embedded as a JS string literal:\n%s", s)
	}
	if !strings.Contains(s, "__PANDO_DESKTOP_SHELL__") || !strings.Contains(s, "pando:desktop-shell") {
		t.Fatal("script does not announce the desktop shell to the WebUI")
	}
}

func TestRegisterRuntimeBridgeKeepsFirstRegistration(t *testing.T) {
	a := NewApp("http://localhost:8765", false)

	a.RegisterRuntimeBridge("", "   ") // empty runtime is ignored
	if a.runtimeBridge.Load() != nil {
		t.Fatal("empty runtime must not be registered")
	}

	a.RegisterRuntimeBridge("ipc1", "rt1")
	a.RegisterRuntimeBridge("ipc2", "rt2") // a later page cannot replace it
	got := *a.runtimeBridge.Load()
	if !strings.Contains(got, "rt1") || strings.Contains(got, "rt2") {
		t.Fatalf("bridge = %q, want the first registration", got)
	}
}

func TestDomReadyScriptSimpleModeTarget(t *testing.T) {
	a := NewApp("http://localhost:8765", true)
	if !strings.Contains(a.domReadyScript(), `var mode = "simple";`) {
		t.Fatal("simple mode not reflected in the navigation script")
	}
}

func TestMinimiseToTrayWithoutContextIsNoop(t *testing.T) {
	a := NewApp("http://localhost:8765", false)
	a.SetTrayAvailable(true)
	// Must not panic before Startup has provided a Wails context.
	a.MinimiseToTray()
	a.CloseWindow()
	a.ShowWindow()
	a.OpenSettings()
	a.QuitApp()
	if !a.TrayAvailable() {
		t.Fatal("TrayAvailable should reflect SetTrayAvailable")
	}
}

func TestSyncShellStateStoresTrayProjects(t *testing.T) {
	a := NewApp("http://localhost:8765", false)
	var got ShellState
	a.SetShellStateListener(func(state ShellState) {
		got = state
	})

	a.SyncShellState(`{"title":"Pando — Project One","activeTabId":"proj-1","projectTabs":[{"projectId":"proj-1","name":"Project One"},{"projectId":" ","name":"ignored"}]}`)

	if got.Title != "Pando — Project One" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.ActiveTabID != "proj-1" {
		t.Fatalf("active tab = %q", got.ActiveTabID)
	}
	if len(got.ProjectTabs) != 1 || got.ProjectTabs[0].ProjectID != "proj-1" || got.ProjectTabs[0].Name != "Project One" {
		t.Fatalf("project tabs = %#v", got.ProjectTabs)
	}
}

func TestSyncShellStateFallsBackToDefaultTitle(t *testing.T) {
	a := NewApp("http://localhost:8765", false)
	var got ShellState
	a.SetShellStateListener(func(state ShellState) {
		got = state
	})

	a.SyncShellState(`{"title":" ","activeTabId":"main","projectTabs":[]}`)

	if got.Title != "Pando" {
		t.Fatalf("title = %q, want Pando", got.Title)
	}
}
