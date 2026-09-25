package mcpgateway

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
)

// Session-scoped exclusion of Xcode mcpbridge servers (PANDO-US-0068).
//
// An Xcode ACP session hands Pando its own mcpbridge ("xcode-tools") as a
// per-session MCP server. A globally configured mcpbridge server would then be
// a second, redundant route to the same Xcode, and every connection it opens
// raises another "Allow ... to access Xcode?" prompt. For such sessions the
// gateway hides global mcpbridge servers from the catalog and refuses calls to
// them, pointing the model at the session's own tools instead.
//
// The agent package owns the per-session MCP registry, so it installs the
// predicate here (mcpgateway cannot import agent).

var sessionHasXcodeBridge atomic.Pointer[func(sessionID string) bool]

// SetSessionXcodeBridgeHook installs the predicate that reports whether a
// session has its own Xcode mcpbridge attached. nil removes it.
func SetSessionXcodeBridgeHook(fn func(sessionID string) bool) {
	if fn == nil {
		sessionHasXcodeBridge.Store(nil)
		return
	}
	sessionHasXcodeBridge.Store(&fn)
}

// excludedServers returns the configured server names the run behind ctx must
// not reach through the gateway, sorted; nil when nothing is excluded.
func excludedServers(ctx context.Context) []string {
	hook := sessionHasXcodeBridge.Load()
	if hook == nil {
		return nil
	}
	sessionID, _ := tools.GetContextValues(ctx)
	if sessionID == "" || !(*hook)(sessionID) {
		return nil
	}
	cfg := config.Get()
	if cfg == nil {
		return nil
	}
	var names []string
	for name, srv := range cfg.MCPServers {
		if srv.IsXcodeMCPBridge() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func isExcluded(excluded []string, serverName string) bool {
	for _, name := range excluded {
		if name == serverName {
			return true
		}
	}
	return false
}

// supersededServerError is returned for a call to a global mcpbridge server
// from a session that attached its own bridge.
func supersededServerError(serverName string) error {
	return fmt.Errorf("MCP server %q is not available in this session: the ACP client attached its own Xcode bridge, use its tools (xcode-tools_*) directly instead", serverName)
}
