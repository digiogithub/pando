// Package agent handles spawning and managing CLI agent processes.
package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/mesnada/mcpconv"
)

// PandoMCPServerEntry represents an MCP server configured in pando that can be
// forwarded to subagents.
type PandoMCPServerEntry struct {
	Name    string
	Command string
	Args    []string
	Env     []string // KEY=VALUE pairs (same format as pando config)
	Type    string   // "stdio", "sse", "streamable-http"
	URL     string
	Headers map[string]string
}

// BuildSubagentMCPConfig builds the canonical MCP config to inject into subagents.
//
// It always adds pando itself as a stdio MCP server, which exposes:
// remembrances, mesnada orchestration, fetch, web search (Google/Brave/Perplexity/Exa),
// and browser/Chrome DevTools tools.
//
// On top of that every MCP server configured in pando is forwarded to the
// subagent UNLESS gatewayExposeEnabled is true, meaning those servers are
// already proxied through pando's own MCP-gateway (so the subagent already
// has access to them via the "pando" entry).
func BuildSubagentMCPConfig(pandoBin string, pandoMCPServers []PandoMCPServerEntry, gatewayExposeEnabled bool) mcpconv.CanonicalConfig {
	if pandoBin == "" {
		pandoBin = resolveSelfBinary()
	}

	servers := make(map[string]mcpconv.CanonicalServer)

	// Always include pando itself as a stdio MCP server.
	servers["pando"] = mcpconv.CanonicalServer{
		Type:    "stdio",
		Command: pandoBin,
		Args:    []string{"mcp-server", "--no-http"},
	}

	// Forward pando-configured MCP servers to the subagent only when they are
	// not already re-exported via the gateway.
	if !gatewayExposeEnabled {
		for _, srv := range pandoMCPServers {
			canonical := mcpconv.CanonicalServer{
				Command: srv.Command,
				Args:    srv.Args,
				Env:     parseEnvPairs(srv.Env),
				URL:     srv.URL,
				Headers: srv.Headers,
			}
			// Normalise type field.
			switch strings.ToLower(srv.Type) {
			case "sse", "streamable-http":
				canonical.Type = srv.Type
			case "stdio", "":
				if canonical.URL != "" {
					canonical.Type = "http"
				} else {
					canonical.Type = "stdio"
				}
			default:
				canonical.Type = srv.Type
			}

			name := srv.Name
			if name == "" {
				continue
			}
			// Avoid overwriting the built-in "pando" entry.
			if name == "pando" {
				name = "pando-ext"
			}
			servers[name] = canonical
		}
	}

	return mcpconv.CanonicalConfig{MCPServers: servers}
}

// parseEnvPairs converts a KEY=VALUE string slice into a map.
func parseEnvPairs(env []string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	m := make(map[string]string, len(env))
	for _, e := range env {
		idx := strings.Index(e, "=")
		if idx > 0 {
			m[e[:idx]] = e[idx+1:]
		}
	}
	return m
}

// resolveSelfBinary returns the absolute path to the currently running binary,
// falling back to "pando" if the path cannot be determined.
func resolveSelfBinary() string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		if abs, err := filepath.Abs(exe); err == nil {
			return abs
		}
		return exe
	}
	return "pando"
}
