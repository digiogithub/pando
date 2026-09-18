package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpdateConfigFileAtCommentOnlyFile: a comment-only (or empty) TOML file
// decodes to no value; updating it must not dereference a nil *Config (it
// crashed `pando sandbox status` in a workspace whose .pando.toml held only
// a comment).
func TestUpdateConfigFileAtCommentOnlyFile(t *testing.T) {
	isolateGlobalConfig(t)
	prev := Get()
	SetForTests(&Config{})
	t.Cleanup(func() { SetForTests(prev) })
	for _, content := range []string{"# project config\n", ""} {
		path := filepath.Join(t.TempDir(), ".pando.toml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		err := updateConfigFileAt(func() (string, error) { return path, nil }, func(c *Config) {
			c.Sandbox.Mode = SandboxModeReadOnly
		})
		if err != nil {
			t.Fatalf("content %q: %v", content, err)
		}
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "read-only") {
			t.Fatalf("content %q: update not written:\n%s", content, data)
		}
	}
}
