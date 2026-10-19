package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// TestLoadDoesNotRewriteConfigFile guards the rule that starting a command
// (Load) never writes the user's config file. Two processes can share one
// file (a read-only mount of rendered config, or a volume shared between
// containers), and a startup rewrite would re-encode it and encrypt plaintext
// secrets with an identity only the first process owns. The scenario seeds
// everything that used to trigger a write: an environment provider, a coder
// model with no evaluator model, and a plaintext MCP header with AgeKeys empty.
func TestLoadDoesNotRewriteConfigFile(t *testing.T) {
	isolateGlobalConfig(t)
	t.Setenv("GEMINI_API_KEY", "dummy")

	projectDir := t.TempDir()
	path := filepath.Join(projectDir, ".pando.toml")
	body := "AgeKeys = ''\n\n[Agents.coder]\nModel = 'gemini.gemini-2.5-flash'\n\n" +
		"[MCPServers.radar]\nType = 'streamable-http'\nURL = 'http://127.0.0.1:1/mcp'\n\n" +
		"[MCPServers.radar.Headers]\nAuthorization = 'Bearer secret-token'\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	viper.Reset()
	cfg = nil
	t.Cleanup(func() { cfg = nil; viper.Reset() })
	if _, err := Load(projectDir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != body {
		t.Fatalf("Load rewrote the config file:\n--- before ---\n%s--- after ---\n%s", body, after)
	}
	if cfg.Evaluator.Model == "" {
		t.Fatal("evaluator model should still be seeded in memory")
	}
}
