package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/runtime"
)

func getWorkspaceFS(ctx context.Context) runtime.WorkspaceFS {
	if fs, ok := ctx.Value(WorkspaceFSContextKey).(runtime.WorkspaceFS); ok {
		return fs
	}
	return runtime.NewHostFS()
}

func isNotExist(err error) bool {
	return os.IsNotExist(err)
}

// resolveToolPath converts a path to an absolute path using the configured
// working directory. It handles the edge case where a model sends the working
// directory as a relative path without the leading slash — for example,
// "www/CVN/file.go" when the working directory is "/www/CVN" — which would
// otherwise produce a doubled path like "/www/CVN/www/CVN/file.go".
func resolveToolPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	wd := config.WorkingDirectory()
	// Detect if the model echoed back the working dir as a relative prefix.
	// e.g. wd="/www/CVN", path="www/CVN/docker-compose.yaml"
	relWD := strings.TrimPrefix(wd, "/")
	if relWD != "" {
		clean := filepath.Clean(path)
		if clean == relWD || strings.HasPrefix(clean, relWD+string(filepath.Separator)) {
			return "/" + clean
		}
	}
	return filepath.Join(wd, path)
}
