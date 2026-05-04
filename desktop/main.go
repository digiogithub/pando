package main

import (
	"embed"
	"flag"
	"io/fs"
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
	url := flag.String("url", "http://localhost:8765", "Pando API URL to load in the webview")
	simpleMode := flag.Bool("simple", false, "Start in simple mode")
	flag.Parse()

	if *url == "" {
		*url = os.Getenv("PANDO_URL")
	}
	if *url == "" {
		*url = "http://localhost:8765"
	}

	// Sub into the "frontend" directory so that Wails finds index.html at the FS root.
	frontendFS, err := fs.Sub(assets, "frontend")
	if err != nil {
		println("Error: failed to sub frontend FS:", err.Error())
		os.Exit(1)
	}

	app := desktop.NewApp(*url, *simpleMode)

	err = wails.Run(&options.App{
		Title:            "Pando",
		Width:            1280,
		Height:           800,
		MinWidth:         800,
		MinHeight:        600,
		BackgroundColour: &options.RGBA{R: 18, G: 18, B: 18, A: 1},
		AssetServer: &assetserver.Options{
			Assets: frontendFS,
		},
		HideWindowOnClose:        false,
		OnStartup:                app.Startup,
		OnDomReady:               app.OnDomReady,
		OnShutdown:               app.Shutdown,
		Bind:                     []interface{}{app},
		CSSDragProperty:          "widows",
		CSSDragValue:             "1",
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
