package agent

import (
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tooldiscovery"
	"github.com/digiogithub/pando/internal/llm/tools"
)

// ApplyToolDiscovery applies the tool selection policy to allTools.
// When discovery mode is active it:
//  1. Builds a registry from all tools.
//  2. Creates a tool_search tool backed by the registry.
//  3. Returns only the visible subset (core tools + tool_search + session-discovered).
//
// When discovery is disabled or the threshold is not met, allTools is returned unchanged.
func ApplyToolDiscovery(allTools []tools.BaseTool) []tools.BaseTool {
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
		nonDeferredMap[n] = true
	}

	reg := tooldiscovery.BuildRegistry(allTools, nonDeferredMap)
	adapter := tooldiscovery.NewRegistrySearchAdapter(reg)
	searchTool := tools.NewToolSearchTool(adapter, dc.SearchLimit)

	policy := tooldiscovery.NewSelectionPolicy(policyCfg)
	return policy.Apply(reg, searchTool)
}
