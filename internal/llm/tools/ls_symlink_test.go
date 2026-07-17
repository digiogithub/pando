package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLsTool_FollowsSymlinks checks that the ls tool sees through symlinked
// directories — both when a link sits inside the listed directory and when the
// listed path is itself a link — matching what a plain `ls` in bash shows.
func TestLsTool_FollowsSymlinks(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	proj := filepath.Join(root, "proj")

	require.NoError(t, os.MkdirAll(filepath.Join(real, "sub"), 0o755))
	require.NoError(t, os.MkdirAll(proj, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(real, "sub", "target.go"), []byte("package x"), 0o644))
	require.NoError(t, os.Symlink(real, filepath.Join(proj, "linkdir")))

	runLS := func(path string) string {
		paramsJSON, err := json.Marshal(LSParams{Path: path})
		require.NoError(t, err)
		response, err := NewLsTool().Run(context.Background(), ToolCall{
			Name:  LSToolName,
			Input: string(paramsJSON),
		})
		require.NoError(t, err)
		return response.Content
	}

	t.Run("descends into a symlinked directory", func(t *testing.T) {
		output := runLS(proj)
		assert.Contains(t, output, "linkdir")
		assert.Contains(t, output, "target.go")
	})

	t.Run("lists a symlinked directory given as the root", func(t *testing.T) {
		output := runLS(filepath.Join(proj, "linkdir"))
		assert.Contains(t, output, "target.go")
	})
}
