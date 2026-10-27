package client

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// FileEntry is one entry of a directory listing (GET /api/v1/files).
type FileEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// maxRawBytes bounds what ReadRaw reads (images, blobs).
const maxRawBytes = 64 << 20

// escapePath escapes each segment of a relative file path.
func escapePath(p string) string {
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// ListFiles lists directory dir of the server's working directory ("." for
// the root). hidden overrides the server's show_hidden_files preference when
// not nil.
func (c *Client) ListFiles(ctx context.Context, dir string, hidden *bool) ([]FileEntry, error) {
	if dir == "" {
		dir = "."
	}
	q := url.Values{"path": {dir}}
	if hidden != nil {
		if *hidden {
			q.Set("hidden", "1")
		} else {
			q.Set("hidden", "0")
		}
	}
	var out struct {
		Files []FileEntry `json:"files"`
	}
	if err := c.Get(ctx, "/api/v1/files?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Files, nil
}

// ReadFile returns the text of a file of the working directory.
func (c *Client) ReadFile(ctx context.Context, path string) (string, error) {
	var out struct {
		Content string `json:"content"`
	}
	if err := c.Get(ctx, "/api/v1/files/"+escapePath(path), &out); err != nil {
		return "", err
	}
	return out.Content, nil
}

// WriteFile replaces the content of a file (PUT /api/v1/files/{path}).
func (c *Client) WriteFile(ctx context.Context, path, content string) error {
	return c.Put(ctx, "/api/v1/files/"+escapePath(path), map[string]string{"content": content}, nil)
}

// CreateFile creates an empty file, or a directory when isDir.
func (c *Client) CreateFile(ctx context.Context, path string, isDir bool) error {
	body := map[string]any{"path": path, "isDir": isDir}
	if isDir {
		body["content"] = nil
	} else {
		body["content"] = ""
	}
	return c.Post(ctx, "/api/v1/files", body, nil)
}

// RenameFile moves a file or directory.
func (c *Client) RenameFile(ctx context.Context, oldPath, newPath string) error {
	return c.Post(ctx, "/api/v1/files/rename", map[string]string{"oldPath": oldPath, "newPath": newPath}, nil)
}

// DeleteFile removes a file or a directory tree.
func (c *Client) DeleteFile(ctx context.Context, path string) error {
	return c.Delete(ctx, "/api/v1/files/"+escapePath(path), nil)
}

// ReadRaw fetches a path of the API as bytes (no JSON decoding): the raw
// file endpoint for images, agentvcs blobs.
func (c *Client) ReadRaw(ctx context.Context, apiPath string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.send(ctx, http.MethodGet, apiPath, nil, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, maxRawBytes))
}

// ReadRawFile returns the bytes of a working-directory file
// (GET /api/v1/files/raw/{path}).
func (c *Client) ReadRawFile(ctx context.Context, path string) ([]byte, error) {
	return c.ReadRaw(ctx, "/api/v1/files/raw/"+escapePath(path))
}

// GitBranch returns the current git branch of the working directory the way
// the Web UI does (terminal/exec), or "" when unknown.
func (c *Client) GitBranch(ctx context.Context) string {
	var out struct {
		Stdout string `json:"stdout"`
	}
	if err := c.Post(ctx, "/api/v1/terminal/exec", map[string]string{"command": "git rev-parse --abbrev-ref HEAD"}, &out); err != nil {
		return ""
	}
	b := strings.TrimSpace(out.Stdout)
	if b == "HEAD" {
		return ""
	}
	return b
}
