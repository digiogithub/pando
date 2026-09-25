package api

import (
	"embed"
	"io/fs"
)

//go:embed webui/**
var embeddedWebUI embed.FS

func EmbeddedWebUI() (fs.FS, error) {
	if _, err := fs.Stat(embeddedWebUI, "webui/dist"); err == nil {
		return fs.Sub(embeddedWebUI, "webui/dist")
	}
	return fs.Sub(embeddedWebUI, "webui")
}
