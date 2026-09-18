package acp

import "testing"

// Goal mode auto-approves non-dangerous requests; a sandbox escalation must
// always reach the user.
func TestGoalModeTreatsSandboxEscalationAsDangerous(t *testing.T) {
	escalation := PermissionRequestData{ToolName: "bash", Action: actionExecuteUnsandboxed, Params: map[string]any{"command": "ls"}}
	if !isDangerousPermissionRequest(escalation) {
		t.Fatal("execute_unsandboxed must be treated as dangerous")
	}
	plain := PermissionRequestData{ToolName: "bash", Action: "execute", Params: map[string]any{"command": "ls"}}
	if isDangerousPermissionRequest(plain) {
		t.Fatal("a plain ls must not be dangerous")
	}
}
