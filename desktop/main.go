package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"net/url"
	"os"

	"github.com/digiogithub/pando/internal/desktop"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	pandoURL := flag.String("url", "http://localhost:8765", "Pando API URL to load in the webview")
	simpleMode := flag.Bool("simple", false, "Start in simple mode")
	flag.Parse()

	if *pandoURL == "" {
		*pandoURL = os.Getenv("PANDO_URL")
	}
	if *pandoURL == "" {
		*pandoURL = "http://localhost:8765"
	}

	// Sub into the "frontend" directory so that Wails finds index.html at the FS root.
	frontendFS, err := fs.Sub(assets, "frontend")
	if err != nil {
		println("Error: failed to sub frontend FS:", err.Error())
		os.Exit(1)
	}

	app := desktop.NewApp(*pandoURL, *simpleMode)
	tray := newTray(app)

	err = wails.Run(&options.App{
		Title:     "Pando",
		Width:     1280,
		Height:    800,
		MinWidth:  800,
		MinHeight: 600,
		// Pre-paint colour shown before the WebUI loads; matches the brand's
		// Bosque-derived dark background (~#0c1f18) so there's no flash of a
		// mismatched shade while the webview spins up.
		BackgroundColour: &options.RGBA{R: 12, G: 31, B: 24, A: 255},
		AssetServer: &assetserver.Options{
			Assets: frontendFS,
		},
		Menu:              appMenu(),
		HideWindowOnClose: false,
		// The WebUI draws its own title bar (drag region + minimise to tray,
		// maximise, close), so the native decorations are dropped.
		Frameless: true,
		OnStartup: func(ctx context.Context) {
			app.Startup(ctx)
			tray.Start()
		},
		OnDomReady: func(ctx context.Context) {
			// The window is mapped by now, so the Wayland decoration object
			// exists and the CSD announcement takes effect.
			suppressServerDecorations()
			app.OnDomReady(ctx)
		},
		OnShutdown: func(ctx context.Context) {
			tray.Stop()
			app.Shutdown(ctx)
		},
		Bind: []interface{}{app},
		// The UI is served by the Pando server, not the Wails asset server, so
		// its origin must be allowed to reach the runtime and the bindings.
		BindingsAllowedOrigins:   originOf(*pandoURL),
		CSSDragProperty:          "--wails-draggable",
		CSSDragValue:             "drag",
		EnableDefaultContextMenu: false,
		Linux: &linux.Options{
			ProgramName: "pando",
		},
	})
	if err != nil {
		println("Error:", err.Error())
		os.Exit(1)
	}
}

// originOf returns the scheme://host[:port] origin of rawURL, or "" when it
// cannot be parsed.
func originOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
