package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tooldiscovery"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mcpgateway"
)

// The discovery registry is shared process-wide so that "discovered" state
// survives tool-set rebuilds between agent turns. Tool instances themselves
// are re-registered (upserted by canonical name) on every call.
var (
	discoveryMu  sync.Mutex
	discoveryReg *tooldiscovery.Registry
)

// SharedDiscoveryRegistry returns the process-wide tool discovery registry,
// creating it on first use.
func SharedDiscoveryRegistry() *tooldiscovery.Registry {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	if discoveryReg == nil {
		discoveryReg = tooldiscovery.NewRegistry()
	}
	return discoveryReg
}

// ResetSharedDiscoveryRegistry drops the shared registry (tests, config reloads).
func ResetSharedDiscoveryRegistry() {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	discoveryReg = nil
}

// ApplyToolDiscovery applies the unified tool selection policy to allTools.
// When discovery is enabled it:
//  0. Adds extension-contributed tools and applies extension tool middleware
//     (see internal/extensions.ApplyTools). This happens even when discovery is
//     disabled.
//  1. Syncs all live tools into the shared registry (upsert by name).
//  2. When gateway is non-nil, syncs the full MCP catalog as catalog-only
//     entries and wires the gateway as the remote executor so any MCP tool —
//     favorite or not — is searchable and executable through tool_search.
//  3. Creates the unified tool_search tool (search + call) backed by the registry.
//  4. Returns only the visible subset (core tools + tool_search + session-discovered).
//
// When discovery is disabled the tool set is returned as-is after step 0.
func ApplyToolDiscovery(allTools []tools.BaseTool, gateway *mcpgateway.Gateway) []tools.BaseTool {
	// Extension tools join the set before any discovery decision is taken, so
	// they are classified and possibly deferred like every other tool.
	allTools = applyExtensionTools(allTools)

	cfg := config.Get()
	if cfg == nil {
		return allTools
	}

	dc := cfg.ToolDiscovery
	if !dc.Enabled {
		return allTools
	}

	policyCfg := tooldiscovery.DefaultPolicyConfig()
	if dc.Mode != "" {
		policyCfg.Mode = tooldiscovery.PolicyMode(dc.Mode)
	}
	if dc.MaxDirectTools > 0 {
		policyCfg.MaxDirectTools = dc.MaxDirectTools
	}
	if len(dc.NonDeferredTools) > 0 {
		policyCfg.NonDeferredTools = dc.NonDeferredTools
	}
	if len(dc.DeferredSources) > 0 {
		policyCfg.DeferredSources = dc.DeferredSources
	}

	// Build the non-deferred lookup for registry construction.
	nonDeferredMap := make(map[string]bool, len(policyCfg.NonDeferredTools))
	for _, n := range policyCfg.NonDeferredTools {
		nonDeferredMap[strings.ToLower(n)] = true
	}

	reg := SharedDiscoveryRegistry()

	// Sync every live tool into the shared registry (upsert by canonical name).
	for _, t := range allTools {
		name := t.Info().Name
		source := tooldiscovery.ClassifySource(name)
		nd := nonDeferredMap[strings.ToLower(name)] || source == tooldiscovery.SourceCore
		_ = reg.Register(t, source, nd)
	}

	// Wire the MCP gateway: catalog entries become searchable and executable
	// through the same tool_search tool — no separate mcp_query_catalog /
	// mcp_call_tool needed.
	if gateway != nil {
		reg.SetRemoteExecutor(makeGatewayExecutor(gateway))
		syncGatewayCatalog(reg, gateway)
	}

	adapter := tooldiscovery.NewRegistrySearchAdapter(reg)
	searchTool := tools.NewToolSearchToolWithExecutor(adapter, reg, dc.SearchLimit)

	policy := tooldiscovery.NewSelectionPolicy(policyCfg)
	return policy.Apply(reg, searchTool)
}

// makeGatewayExecutor adapts the MCP gateway to the registry's remote
// executor signature. Tool IDs use the gateway's canonical "server/tool" form.
func makeGatewayExecutor(gw *mcpgateway.Gateway) tooldiscovery.RemoteToolExecutor {
	return func(ctx context.Context, serverName, toolName string, params map[string]interface{}) (string, error) {
		sessionID, _ := tools.GetContextValues(ctx)
		toolID := fmt.Sprintf("%s/%s", serverName, toolName)
		result, err := gw.CallTool(ctx, toolID, params, sessionID)
		if err != nil {
			return "", err
		}
		if s, ok := result.(string); ok {
			return s, nil
		}
		return fmt.Sprintf("%v", result), nil
	}
}

// syncGatewayCatalog mirrors the gateway's persisted MCP catalog into the
// discovery registry as catalog-only entries. Canonical names follow the
// "<server>_<toolname>" convention used by direct MCP tools so discovered
// favorites and deferred catalog tools share one naming scheme.
func syncGatewayCatalog(reg *tooldiscovery.Registry, gw *mcpgateway.Gateway) {
	all, err := gw.GetAllTools(context.Background())
	if err != nil {
		logging.Debug("Tool discovery: failed to sync MCP catalog", "error", err)
		return
	}
	metas := make([]tooldiscovery.ToolMetadata, 0, len(all))
	for _, t := range all {
		metas = append(metas, tooldiscovery.ToolMetadata{
			CanonicalName: fmt.Sprintf("%s_%s", t.ServerName, t.ToolName),
			ServerName:    t.ServerName,
			ToolName:      t.ToolName,
			Source:        tooldiscovery.SourceMCP,
			Description:   t.Description,
		})
	}
	reg.SyncCatalogEntries(metas)
}
