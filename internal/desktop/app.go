package desktop

import (
	"context"
	"net/http"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App holds the Wails application state.
type App struct {
	ctx       context.Context
	serverURL string
	token     string
	server    *apiServer
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{}
}

// Startup is called when the Wails app starts. It initialises the embedded
// HTTP API server and stores the URL and auth token for later use.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx

	srv, err := startAPIServer(ctx)
	if err != nil {
		// Log but don't crash — UI will show connection error.
		println("desktop: failed to start API server:", err.Error())
		return
	}
	a.server = srv
	a.serverURL = srv.url
	a.token = srv.token
}

// OnDomReady is called by Wails when the DOM is ready. We inject the backend
// config so the React frontend knows which port and token to use.
func (a *App) OnDomReady(ctx context.Context) {
	if a.serverURL == "" {
		return
	}
	runtime.WindowExecJS(ctx, configScript(a.serverURL, a.token))
}

// Shutdown is called when the Wails app is closing.
func (a *App) Shutdown(ctx context.Context) {
	if a.server != nil {
		a.server.stop(ctx)
	}
}

// AssetsHandler returns nil — Wails serves embedded assets directly.
// Kept for compatibility with desktop/main.go.
func (a *App) AssetsHandler() http.Handler {
	return nil
}

// GetServerInfo exposes the backend URL and token to JavaScript via Wails binding.
func (a *App) GetServerInfo() map[string]string {
	return map[string]string{
		"apiBase": a.serverURL,
		"token":   a.token,
	}
}
