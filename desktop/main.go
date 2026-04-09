package main

import (
	"embed"

	"github.com/digiogithub/pando/internal/desktop"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	app := desktop.NewApp()

	err := wails.Run(&options.App{
		Title:            "Pando",
		Width:            1280,
		Height:           800,
		MinWidth:         800,
		MinHeight:        600,
		BackgroundColour: &options.RGBA{R: 18, G: 18, B: 18, A: 1},
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: app.AssetsHandler(),
		},
		OnStartup:                app.Startup,
		OnDomReady:               app.OnDomReady,
		OnShutdown:               app.Shutdown,
		Bind:                     []interface{}{app},
		CSSDragProperty:          "widows",
		CSSDragValue:             "1",
		EnableDefaultContextMenu: false,
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
