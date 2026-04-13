package api

import (
	"embed"
	"io/fs"
)

//go:embed webui/dist/**
var embeddedWebUI embed.FS

func EmbeddedWebUI() (fs.FS, error) {
	return fs.Sub(embeddedWebUI, "webui/dist")
}
