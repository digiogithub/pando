package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ProjectTabState is the desktop shell's live view of one open project tab.
type ProjectTabState struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
}

// ShellState is the desktop shell state mirrored from the WebUI.
type ShellState struct {
	Title       string            `json:"title"`
	ActiveTabID string            `json:"activeTabId"`
	ProjectTabs []ProjectTabState `json:"projectTabs"`
}

// App holds the Wails desktop application state.
type App struct {
	ctx           context.Context
	pandoURL      string
	simpleMode    atomic.Bool
	windowFocused atomic.Bool

	// runtimeBridge holds the Wails IPC + runtime JavaScript captured from the
	// local loading page. The real UI is served from the Pando origin, where
	// Wails injects nothing, so OnDomReady replays this bundle into it.
	runtimeBridge atomic.Pointer[string]
	// trayAvailable reports whether a system tray icon is live, which decides
	// whether "minimise" hides the window into the tray or to the taskbar.
	trayAvailable atomic.Bool

	shellStateMu       sync.RWMutex
	shellState         ShellState
	shellStateListener func(ShellState)
}

// NewApp creates a new desktop App that wraps the given Pando URL in a WebView.
func NewApp(pandoURL string, startSimple bool) *App {
	a := &App{
		pandoURL: pandoURL,
	}
	a.simpleMode.Store(startSimple)
	return a
}

// Startup is called by Wails when the application starts.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.windowFocused.Store(true)

	appMenu := a.buildMenu()
	runtime.MenuSetApplicationMenu(ctx, appMenu)

	// Start listening to the Pando notification SSE stream in background.
	// Shows OS-native notifications when the window is not focused.
	go a.startNotificationListener(ctx)
}

// OnDomReady is called by Wails every time a document finishes loading.
//
// The first load is the embedded loading page, served by the Wails asset
// server with the runtime injected: it captures that runtime, hands it to Go
// and then navigates to the Pando URL. Every later load is a Pando page, where
// Wails injects nothing, so the captured runtime is replayed into it. That is
// what gives the WebUI window.runtime (window controls, drag regions) and the
// window.go bindings.
func (a *App) OnDomReady(ctx context.Context) {
	runtime.WindowExecJS(ctx, a.domReadyScript())
}

// domReadyScript builds the script run on every document load.
func (a *App) domReadyScript() string {
	mode := "advanced"
	if a.simpleMode.Load() {
		mode = "simple"
	}
	bridge := ""
	if b := a.runtimeBridge.Load(); b != nil {
		bridge = *b
	}
	urlJSON, _ := json.Marshal(a.pandoURL)

	return `
(function() {
	var url = ` + string(urlJSON) + `;
	var mode = "` + mode + `";

	if (!window.location.href.startsWith(url)) {
		// Loading page: capture the Wails runtime before leaving this origin.
		var target = mode === "simple" ? url + "/chat/simple" : url;
		var attempts = 0;
		function tryNavigate() {
			if (attempts++ > 30) { window.location.href = target; return; }
			fetch(url + "/health").then(function() {
				window.location.href = target;
			}).catch(function() {
				setTimeout(tryNavigate, 300);
			});
		}
		var capture = Promise.resolve();
		if (window.go && window.go.desktop && window.go.desktop.App && window.go.desktop.App.RegisterRuntimeBridge) {
			capture = Promise.all([
				fetch("/wails/ipc.js").then(function(r) { return r.text(); }),
				fetch("/wails/runtime.js").then(function(r) { return r.text(); })
			]).then(function(parts) {
				return window.go.desktop.App.RegisterRuntimeBridge(parts[0], parts[1]);
			}).catch(function(err) {
				console.error("pando desktop: runtime capture failed", err);
			});
		}
		capture.then(tryNavigate);
		return;
	}

	// Pando page: install the captured runtime once per document.
	// Inlined rather than eval'd, so a page CSP without 'unsafe-eval' cannot
	// block it (scripts run through ExecJS are not subject to the page CSP).
	if (!window.wails) {
		try {
` + bridge + `
		} catch (err) {
			console.error("pando desktop: runtime injection failed", err);
		}
	}
	if (!window.wails || window.__PANDO_DESKTOP_SHELL__) {
		return;
	}
	window.__PANDO_DESKTOP_SHELL__ = { frameless: true };

	// Focus tracking decides whether OS notifications are shown.
	window.addEventListener("focus", function() {
		window.go.desktop.App.SetWindowFocused(true);
	});
	window.addEventListener("blur", function() {
		window.go.desktop.App.SetWindowFocused(false);
	});

	window.dispatchEvent(new CustomEvent("pando:desktop-shell"));
})();
`
}

// RegisterRuntimeBridge stores the Wails IPC and runtime scripts captured by
// the loading page. Only the first registration is kept: it comes from the
// Wails-served page, before any remote content has loaded.
// Exposed as Wails binding.
func (a *App) RegisterRuntimeBridge(ipcJS, runtimeJS string) {
	if strings.TrimSpace(runtimeJS) == "" {
		return
	}
	bundle := ipcJS + "\n;\n" + runtimeJS
	a.runtimeBridge.CompareAndSwap(nil, &bundle)
}

// Shutdown is called by Wails when the application is closing.
func (a *App) Shutdown(ctx context.Context) {}

// buildMenu constructs the application menu with window and mode controls.
func (a *App) buildMenu() *menu.Menu {
	appMenu := menu.NewMenu()

	pandoMenu := appMenu.AddSubmenu("Pando")
	pandoMenu.AddText("Show Window", keys.CmdOrCtrl("k"), func(_ *menu.CallbackData) {
		runtime.WindowShow(a.ctx)
	})
	pandoMenu.AddText("Hide Window", keys.CmdOrCtrl("h"), func(_ *menu.CallbackData) {
		runtime.WindowHide(a.ctx)
	})
	pandoMenu.AddSeparator()

	modeItem := pandoMenu.AddCheckbox("Simple Mode", a.simpleMode.Load(), keys.CmdOrCtrl("m"), func(cd *menu.CallbackData) {
		a.toggleMode(cd.MenuItem.Checked)
	})
	_ = modeItem

	pandoMenu.AddSeparator()
	pandoMenu.AddText("Design Studio", keys.Combo("d", keys.CmdOrCtrlKey, keys.ShiftKey), func(_ *menu.CallbackData) {
		a.navigate("/design")
	})

	pandoMenu.AddSeparator()
	pandoMenu.AddText("Reload", keys.CmdOrCtrl("r"), func(_ *menu.CallbackData) {
		runtime.WindowReload(a.ctx)
	})
	pandoMenu.AddSeparator()
	pandoMenu.AddText("Quit", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) {
		runtime.Quit(a.ctx)
	})

	return appMenu
}

// navigate moves the webview to a path of the Pando UI. The desktop shell runs
// the UI on the Pando origin itself (OnDomReady sets window.location to
// pandoURL), so this is an in-origin navigation, not a new window.
//
// That is also why the Design Studio needs no CSP change here: the preview
// server sends frame-ancestors 'self', and the page framing the preview is
// served from the same origin as the preview. preview.Options.FrameAncestors
// exists for a shell that ever stops doing this.
func (a *App) navigate(path string) {
	runtime.WindowExecJS(a.ctx, `window.location.href = `+"`"+a.pandoURL+path+"`"+`;`)
}

// toggleMode switches between simple and advanced mode and reloads the URL.
func (a *App) toggleMode(simple bool) {
	a.simpleMode.Store(simple)
	// The WebUI persists the chosen mode and reopens it from "/", so leaving
	// simple mode has to say so explicitly.
	target := a.pandoURL + "/?mode=advanced"
	if simple {
		target = a.pandoURL + "/chat/simple"
	}
	runtime.WindowExecJS(a.ctx, `window.location.href = `+"`"+target+"`"+`;`)
}

// ToggleWindow shows the window, restoring it from the tray or the taskbar.
// Exposed as Wails binding.
func (a *App) ToggleWindow() {
	a.ShowWindow()
}

// ShowWindow brings the window back from the tray or the taskbar.
// Exposed as Wails binding.
func (a *App) ShowWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
}

// MinimiseToTray hides the window into the system tray when a tray icon is
// live, and falls back to a regular taskbar minimise otherwise: hiding a window
// with no tray icon would leave no way to bring it back.
// Exposed as Wails binding.
func (a *App) MinimiseToTray() {
	if a.ctx == nil {
		return
	}
	if a.trayAvailable.Load() {
		runtime.WindowHide(a.ctx)
		return
	}
	runtime.WindowMinimise(a.ctx)
}

// TrayAvailable reports whether a system tray icon is live.
// Exposed as Wails binding.
func (a *App) TrayAvailable() bool {
	return a.trayAvailable.Load()
}

// SetTrayAvailable is called by the tray integration once its icon is up (or
// known to be impossible on this desktop).
func (a *App) SetTrayAvailable(ok bool) {
	a.trayAvailable.Store(ok)
}

// SyncShellState mirrors the current WebUI shell state into the desktop
// wrapper, updating the window title and tray menu entries.
// Exposed as Wails binding.
func (a *App) SyncShellState(payload string) {
	var raw ShellState
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return
	}

	next := ShellState{
		Title:       strings.TrimSpace(raw.Title),
		ActiveTabID: strings.TrimSpace(raw.ActiveTabID),
		ProjectTabs: make([]ProjectTabState, 0, len(raw.ProjectTabs)),
	}
	if next.Title == "" {
		next.Title = "Pando"
	}
	for _, tab := range raw.ProjectTabs {
		projectID := strings.TrimSpace(tab.ProjectID)
		name := strings.TrimSpace(tab.Name)
		if projectID == "" || name == "" {
			continue
		}
		next.ProjectTabs = append(next.ProjectTabs, ProjectTabState{
			ProjectID: projectID,
			Name:      name,
		})
	}

	a.shellStateMu.Lock()
	a.shellState = next
	listener := a.shellStateListener
	a.shellStateMu.Unlock()

	if a.ctx != nil {
		runtime.WindowSetTitle(a.ctx, next.Title)
	}
	if listener != nil {
		listener(next)
	}
}

// SetShellStateListener registers a listener for WebUI shell-state changes.
func (a *App) SetShellStateListener(listener func(ShellState)) {
	a.shellStateMu.Lock()
	a.shellStateListener = listener
	snapshot := a.shellStateLocked()
	a.shellStateMu.Unlock()

	if listener != nil {
		listener(snapshot)
	}
}

func (a *App) shellStateLocked() ShellState {
	snapshot := ShellState{
		Title:       a.shellState.Title,
		ActiveTabID: a.shellState.ActiveTabID,
		ProjectTabs: make([]ProjectTabState, len(a.shellState.ProjectTabs)),
	}
	copy(snapshot.ProjectTabs, a.shellState.ProjectTabs)
	return snapshot
}

// OpenSettings shows the window on the Settings view. The WebUI handles the
// event with its router (no reload); a page without that listener, such as the
// loading page, gets a plain navigation.
// Exposed as Wails binding.
func (a *App) OpenSettings() {
	if a.ctx == nil {
		return
	}
	a.ShowWindow()
	a.navigateInApp("/settings")
}

// FocusProjectWorkspace shows the window and focuses one project workspace tab.
func (a *App) FocusProjectWorkspace(projectID string) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || a.ctx == nil {
		return
	}
	a.ShowWindow()
	a.navigateInApp("/projects/" + url.PathEscape(projectID) + "/workspace")
}

// CloseWindow closes the shell into the tray when a tray icon is live, and
// falls back to quitting when the tray is unavailable.
// Exposed as Wails binding.
func (a *App) CloseWindow() {
	if a.ctx == nil {
		return
	}
	if a.trayAvailable.Load() {
		runtime.WindowHide(a.ctx)
		return
	}
	runtime.Quit(a.ctx)
}

// QuitApp closes the application.
// Exposed as Wails binding.
func (a *App) QuitApp() {
	if a.ctx == nil {
		return
	}
	runtime.Quit(a.ctx)
}

// navigateInApp asks the WebUI router to move to path, falling back to a full
// navigation when the WebUI is not listening.
func (a *App) navigateInApp(path string) {
	pathJSON, _ := json.Marshal(path)
	target, _ := json.Marshal(a.pandoURL + path)
	runtime.WindowExecJS(a.ctx, `(function() {
	if (window.__PANDO_DESKTOP_NAV__) {
		window.dispatchEvent(new CustomEvent("pando:desktop-navigate", { detail: `+string(pathJSON)+` }));
	} else {
		window.location.href = `+string(target)+`;
	}
})();`)
}

// GetPandoURL returns the configured Pando URL.
// Exposed as Wails binding.
func (a *App) GetPandoURL() string {
	return a.pandoURL
}

// IsSimpleMode returns whether simple mode is active.
// Exposed as Wails binding.
func (a *App) IsSimpleMode() bool {
	return a.simpleMode.Load()
}

// OpenInBrowser opens a URL in the user's real browser instead of the webview.
// Design previews and exports are the reason it exists: a preview belongs in a
// browser with devtools, and a PDF the webview cannot display should not become
// a blank panel.
// Exposed as Wails binding.
func (a *App) OpenInBrowser(url string) {
	if strings.TrimSpace(url) == "" {
		return
	}
	runtime.BrowserOpenURL(a.ctx, url)
}

// SaveFileDialog asks the user where to write a file and returns the chosen
// path, or "" when they cancel.
// Exposed as Wails binding.
func (a *App) SaveFileDialog(title, defaultFilename string) (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultFilename,
	})
}

// SaveDownload fetches a URL and writes it to a path the user picks.
//
// It exists because a webview is not a browser: an <a download> or a
// window.open on a Pando export URL has nowhere to put the file. Doing the
// fetch on the Go side also keeps the API token out of a second HTTP client.
// Returns the written path, or "" when the user cancels the dialog.
// Exposed as Wails binding.
func (a *App) SaveDownload(url, defaultFilename string) (string, error) {
	if strings.TrimSpace(url) == "" {
		return "", errors.New("desktop: empty download URL")
	}
	dest, err := a.SaveFileDialog("Save", defaultFilename)
	if err != nil || dest == "" {
		return "", err
	}

	ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("desktop: download failed with status %d", resp.StatusCode)
	}

	file, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", err
	}
	return dest, nil
}

// SetWindowFocused is called from JavaScript when the window gains or loses
// focus. This controls whether OS notifications are shown.
// Exposed as Wails binding.
func (a *App) SetWindowFocused(focused bool) {
	a.windowFocused.Store(focused)
}
