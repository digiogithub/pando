package runtime

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// hostFS implements WorkspaceFS using the local filesystem.
// It is the default WorkspaceFS when runtime=host.
type hostFS struct{}

// NewHostFS returns a WorkspaceFS backed by the local OS filesystem.
func NewHostFS() WorkspaceFS {
	return &hostFS{}
}

func (h *hostFS) ReadFile(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (h *hostFS) ReadFileRange(_ context.Context, path string, offset, length int64) ([]byte, error) {
	if offset < 0 {
		return nil, fmt.Errorf("invalid offset %d", offset)
	}
	if length < 0 {
		return nil, fmt.Errorf("invalid length %d", length)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if _, err := file.Seek(offset, 0); err != nil {
		return nil, err
	}

	buf := make([]byte, length)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

func (h *hostFS) WriteFile(_ context.Context, path string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (h *hostFS) Stat(_ context.Context, path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

func (h *hostFS) MkdirAll(_ context.Context, path string, perm fs.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (h *hostFS) Remove(_ context.Context, path string) error {
	return os.Remove(path)
}

func (h *hostFS) List(_ context.Context, path string) ([]fs.DirEntry, error) {
	return os.ReadDir(path)
}

func (h *hostFS) Mounted() bool {
	return false
}
