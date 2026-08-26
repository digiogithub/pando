package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App holds the Wails desktop application state.
type App struct {
	ctx           context.Context
	pandoURL      string
	simpleMode    atomic.Bool
	windowFocused atomic.Bool
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

// OnDomReady is called by Wails when the DOM is ready.
// We navigate the webview to the Pando URL.
func (a *App) OnDomReady(ctx context.Context) {
	mode := "advanced"
	if a.simpleMode.Load() {
		mode = "simple"
	}
	script := `
(function() {
	var url = ` + "`" + a.pandoURL + "`" + `;
	var mode = "` + mode + `";
	if (window.location.href === "about:blank" || window.location.href === "" || !window.location.href.startsWith(url)) {
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
		tryNavigate();
	}
})();
`
	runtime.WindowExecJS(ctx, script)

	// Inject focus/blur tracking so the Go side knows when to show OS notifications.
	focusScript := `
(function() {
	window.addEventListener("focus", function() {
		if (window.go && window.go.desktop && window.go.desktop.App) {
			window.go.desktop.App.SetWindowFocused(true);
		}
	});
	window.addEventListener("blur", function() {
		if (window.go && window.go.desktop && window.go.desktop.App) {
			window.go.desktop.App.SetWindowFocused(false);
		}
	});
})();
`
	runtime.WindowExecJS(ctx, focusScript)
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
	target := a.pandoURL
	if simple {
		target = a.pandoURL + "/chat/simple"
	}
	runtime.WindowExecJS(a.ctx, `window.location.href = `+"`"+target+"`"+`;`)
}

// ToggleWindow shows the window if hidden, hides it if visible.
// Exposed as Wails binding.
func (a *App) ToggleWindow() {
	runtime.WindowShow(a.ctx)
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
