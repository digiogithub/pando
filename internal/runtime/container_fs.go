package runtime

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
)

type containerFSMode string

const (
	containerFSModeBindMount containerFSMode = "bind-mount"
	containerFSModeCopy      containerFSMode = "copy"
)

var errContainerCopyModeNotImplemented = errors.New("container copy workspace filesystem is not implemented")

// containerFS exposes workspace files for container runtimes.
// In bind-mount mode the host workspace path is mounted into the container at the
// same absolute path, so filesystem operations can safely delegate to the host.
type containerFS struct {
	mode containerFSMode
	host WorkspaceFS
}

func NewContainerFS() WorkspaceFS {
	return NewBindMountedContainerFS()
}

func NewBindMountedContainerFS() WorkspaceFS {
	return &containerFS{
		mode: containerFSModeBindMount,
		host: NewHostFS(),
	}
}

// NewCopyContainerFS is reserved for runtimes that need to move files in and out
// of an isolated container filesystem. The copy-based implementation is deferred
// until the runtime/session APIs expose the required container copy primitives.
func NewCopyContainerFS() WorkspaceFS {
	return &containerFS{
		mode: containerFSModeCopy,
		host: NewHostFS(),
	}
}

func (c *containerFS) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if c.Mounted() {
		return c.host.ReadFile(ctx, path)
	}
	return nil, fmt.Errorf("read %q: %w", path, errContainerCopyModeNotImplemented)
}

func (c *containerFS) WriteFile(ctx context.Context, path string, data []byte, perm fs.FileMode) error {
	if c.Mounted() {
		return c.host.WriteFile(ctx, path, data, perm)
	}
	return fmt.Errorf("write %q: %w", path, errContainerCopyModeNotImplemented)
}

func (c *containerFS) Stat(ctx context.Context, path string) (fs.FileInfo, error) {
	if c.Mounted() {
		return c.host.Stat(ctx, path)
	}
	return nil, fmt.Errorf("stat %q: %w", path, errContainerCopyModeNotImplemented)
}

func (c *containerFS) MkdirAll(ctx context.Context, path string, perm fs.FileMode) error {
	if c.Mounted() {
		return c.host.MkdirAll(ctx, path, perm)
	}
	return fmt.Errorf("mkdir %q: %w", path, errContainerCopyModeNotImplemented)
}

func (c *containerFS) Remove(ctx context.Context, path string) error {
	if c.Mounted() {
		return c.host.Remove(ctx, path)
	}
	return fmt.Errorf("remove %q: %w", path, errContainerCopyModeNotImplemented)
}

func (c *containerFS) List(ctx context.Context, path string) ([]fs.DirEntry, error) {
	if c.Mounted() {
		return c.host.List(ctx, path)
	}
	return nil, fmt.Errorf("list %q: %w", path, errContainerCopyModeNotImplemented)
}

func (c *containerFS) Mounted() bool {
	return c.mode == containerFSModeBindMount
}
