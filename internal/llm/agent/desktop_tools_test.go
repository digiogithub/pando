package agent

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/permission"
)

// countToolByPrefix counts how many tools have a name starting with prefix.
func countToolByPrefix(toolset []tools.BaseTool, prefix string) int {
	n := 0
	for _, t := range toolset {
		name := t.Info().Name
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

// TestDesktopToolsGatedByConfig verifies that CoderAgentTools registers
// none of the 12 desktop_* tools when InternalTools.DesktopEnabled is
// false, and all 12 when it is true.
func TestDesktopToolsGatedByConfig(t *testing.T) {
	previous := config.Get()
	t.Cleanup(func() { config.SetForTests(previous) })

	perms := permission.NewPermissionService()

	config.SetForTests(&config.Config{InternalTools: config.InternalToolsConfig{DesktopEnabled: false}})
	disabled := CoderAgentTools(perms, nil, nil, nil, nil)
	if got := countToolByPrefix(disabled, "desktop_"); got != 0 {
		t.Fatalf("expected no desktop_* tools when DesktopEnabled=false, got %d", got)
	}

	config.SetForTests(&config.Config{InternalTools: config.InternalToolsConfig{
		DesktopEnabled: true,
		DesktopBackend: "null",
	}})
	enabled := CoderAgentTools(perms, nil, nil, nil, nil)
	if got := countToolByPrefix(enabled, "desktop_"); got != 12 {
		names := make([]string, 0)
		for _, tl := range enabled {
			n := tl.Info().Name
			if len(n) >= 8 && n[:8] == "desktop_" {
				names = append(names, n)
			}
		}
		t.Fatalf("expected 12 desktop_* tools when DesktopEnabled=true, got %d: %v", got, names)
	}
}
