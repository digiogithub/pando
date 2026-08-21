package agent

import (
	"sync"

	"github.com/digiogithub/pando/internal/extensions"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/pkg/extension"
)

// The extension manager is injected rather than imported: internal/app owns it
// and would import this package, so the dependency has to run the other way.
// Same pattern as SetContextTrimmer and SetLuaManager.
var (
	extensionMu      sync.RWMutex
	extensionManager *extension.Manager
)

// SetExtensionManager wires the process-wide extension manager into the agent's
// tool-set builder. Called once from internal/app after extensions are loaded.
// Passing nil detaches it (tests, teardown).
func SetExtensionManager(mgr *extension.Manager) {
	extensionMu.Lock()
	defer extensionMu.Unlock()
	extensionManager = mgr
}

// ExtensionManager returns the wired manager, or nil when the build has none.
func ExtensionManager() *extension.Manager {
	extensionMu.RLock()
	defer extensionMu.RUnlock()
	return extensionManager
}

// applyExtensionTools adds extension-contributed tools and runs the extension
// tool middleware over the whole set. It is a no-op when nothing is wired, so
// standard builds pay one atomic load per tool-set build.
//
// It runs before tool discovery, deliberately: discovery classifies and may
// defer tools behind tool_search, and extension tools have to be part of that
// decision rather than bypassing it.
func applyExtensionTools(allTools []tools.BaseTool) []tools.BaseTool {
	mgr := ExtensionManager()
	if mgr == nil {
		return allTools
	}
	return extensions.ApplyTools(mgr, allTools)
}
