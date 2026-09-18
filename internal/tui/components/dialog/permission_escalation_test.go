package dialog

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/permission"
)

func TestPermissionDialogRendersSandboxEscalation(t *testing.T) {
	d := NewPermissionDialogCmp().(*permissionDialogCmp)
	d.windowSize = tea.WindowSizeMsg{Width: 200, Height: 60}
	d.SetPermissions(permission.PermissionRequest{
		ID:            "p1",
		ToolName:      tools.BashToolName,
		Action:        permission.ActionExecuteUnsandboxed,
		Justification: "needs the global npm prefix",
		GrantKey:      "prefix:npm install",
		Params:        tools.BashPermissionsParams{Command: "npm install -g x", Unsandboxed: true},
	})
	view := d.View()
	for _, want := range []string{"Run outside sandbox", "needs the global npm prefix", `"npm install"`, "OUTSIDE the sandbox"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q", want)
		}
	}

	d.SetPermissions(permission.PermissionRequest{
		ID:       "p2",
		ToolName: tools.BashToolName,
		Action:   "execute",
		Params:   tools.BashPermissionsParams{Command: "ls"},
	})
	if view := d.View(); strings.Contains(view, "Run outside sandbox") || !strings.Contains(view, "Permission Required") {
		t.Error("a regular bash prompt must keep the regular title")
	}
}
