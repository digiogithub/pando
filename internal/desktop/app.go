package desktop

import (
	"context"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App holds the Wails desktop application state.
type App struct {
	ctx            context.Context
	pandoURL       string
	simpleMode     atomic.Bool
	windowFocused  atomic.Bool
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
		window.location.href = target;
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
	pandoMenu.AddText("Reload", keys.CmdOrCtrl("r"), func(_ *menu.CallbackData) {
		runtime.WindowReload(a.ctx)
	})
	pandoMenu.AddSeparator()
	pandoMenu.AddText("Quit", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) {
		runtime.Quit(a.ctx)
	})

	return appMenu
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

// SetWindowFocused is called from JavaScript when the window gains or loses
// focus. This controls whether OS notifications are shown.
// Exposed as Wails binding.
func (a *App) SetWindowFocused(focused bool) {
	a.windowFocused.Store(focused)
}
