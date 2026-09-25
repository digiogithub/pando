package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mcpclient"
	"github.com/digiogithub/pando/internal/mcpgateway"

	"github.com/mark3labs/mcp-go/mcp"
)

// Fewer connections to Xcode's mcpbridge (PANDO-US-0068).
//
// Xcode asks "Allow ... to access Xcode?" on every NEW connection to its
// mcpbridge and cannot remember the answer, so Pando has to keep the number of
// connections it opens to a bridge as low as possible:
//
//   - a globally configured bridge server keeps ONE persistent, initialized
//     client per process (keyed by server name + config fingerprint) that is
//     shared by direct-mode discovery and every tool call, instead of the
//     client-per-call used for other servers;
//   - a session that attached its own bridge through ACP (Xcode's
//     `xcode-tools`) does not see global bridge servers at all, neither as
//     direct tools (toolsForSession, mcpTool.Run) nor through the gateway
//     (mcpgateway.SetSessionXcodeBridgeHook).

func init() {
	mcpgateway.SetSessionXcodeBridgeHook(sessionHasXcodeBridge)
}

// globalBridgeEntry is the persistent client of one global bridge server.
type globalBridgeEntry struct {
	fingerprint string
	server      *sessionMCPServer
}

var (
	globalBridgeMu      sync.Mutex
	globalBridgeClients = map[string]*globalBridgeEntry{} // server name -> entry
)

// globalBridgeServer returns the process-wide persistent connection holder for
// a global bridge server, replacing (and closing) one built from a different
// configuration. The connection itself opens lazily on first use.
func globalBridgeServer(name string, cfg config.MCPServer) *sessionMCPServer {
	fp := mcpgateway.ConfigFingerprint(cfg)
	globalBridgeMu.Lock()
	defer globalBridgeMu.Unlock()
	if entry, ok := globalBridgeClients[name]; ok {
		if entry.fingerprint == fp {
			return entry.server
		}
		logging.Info("MCP bridge: configuration changed, closing persistent client", "server", name)
		entry.server.close()
	}
	srv := &sessionMCPServer{name: name, cfg: cfg}
	globalBridgeClients[name] = &globalBridgeEntry{fingerprint: fp, server: srv}
	return srv
}

// closeGlobalBridgeClients closes every persistent bridge client. It runs with
// ResetMcpToolsCache, i.e. whenever the MCP configuration changes.
func closeGlobalBridgeClients() {
	globalBridgeMu.Lock()
	entries := globalBridgeClients
	globalBridgeClients = map[string]*globalBridgeEntry{}
	globalBridgeMu.Unlock()
	for name, entry := range entries {
		entry.server.close()
		logging.Debug("MCP bridge: persistent client closed", "server", name)
	}
}

// discoverBridgeTools lists a global bridge server's tools over its persistent
// client, so the connection discovery opens is the one later tool calls reuse.
func discoverBridgeTools(ctx context.Context, name string, cfg config.MCPServer) []mcpToolDescriptor {
	listed, err := globalBridgeServer(name, cfg).listTools(ctx)
	if err != nil {
		logging.Error("error listing tools", "server", name, "error", err)
		mcpclient.PublishWarn(name, mcpclient.BuildCallError(name, "", "list tools during discovery", err).Error())
		return nil
	}
	descriptors := make([]mcpToolDescriptor, 0, len(listed))
	for _, t := range listed {
		descriptors = append(descriptors, mcpToolDescriptor{serverName: name, tool: t, mcpConfig: cfg})
	}
	logging.Debug("MCP server tools listed", "serverName", name, "toolCount", len(listed), "persistent", true)
	return descriptors
}

// listTools lists the server's tools on the live client, opening it if needed.
func (s *sessionMCPServer) listTools(ctx context.Context) ([]mcp.Tool, error) {
	c, err := s.activeClient(ctx)
	if err != nil {
		return nil, err
	}
	listCtx, cancel := mcpclient.WithTimeout(ctx, s.discoveryTimeout())
	result, err := c.ListTools(listCtx, mcp.ListToolsRequest{})
	cancel()
	if err != nil {
		s.invalidate(c)
		return nil, fmt.Errorf("list tools: %w", err)
	}
	return result.Tools, nil
}

// sessionHasXcodeBridge reports whether the session attached an Xcode
// mcpbridge of its own.
func sessionHasXcodeBridge(sessionID string) bool {
	value, ok := sessionMCPRegistry.Load(sessionID)
	if !ok {
		return false
	}
	set, ok := value.(*sessionMCPSet)
	if !ok {
		return false
	}
	for _, srv := range set.servers {
		if srv.cfg.IsXcodeMCPBridge() {
			return true
		}
	}
	return false
}

// withoutGlobalXcodeBridgeTools drops direct tools of global bridge servers.
// base is never mutated.
func withoutGlobalXcodeBridgeTools(base []tools.BaseTool) []tools.BaseTool {
	filtered := make([]tools.BaseTool, 0, len(base))
	for _, t := range base {
		if mt, ok := t.(*mcpTool); ok && mt.mcpConfig.IsXcodeMCPBridge() {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered
}
