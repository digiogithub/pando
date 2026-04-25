package tools

import (
	"context"
	"os"

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
