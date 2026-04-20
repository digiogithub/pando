package project

import (
	"context"

	acpsdk "github.com/coder/acp-go-sdk"
)

// projectACPClient is a minimal ACP client implementation used by ProjectManager
// to maintain stdio connections with child Pando instances.
// It satisfies the acpsdk.Client interface with no-op implementations since
// the manager doesn't need tool callbacks — only session listing and process lifecycle.
type projectACPClient struct {
	project Project
}

// newProjectACPClient creates a new no-op ACP client for a project instance.
func newProjectACPClient(proj Project) *projectACPClient {
	return &projectACPClient{project: proj}
}

// ReadTextFile is a no-op implementation — project manager does not use file reads.
func (c *projectACPClient) ReadTextFile(_ context.Context, _ acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	return acpsdk.ReadTextFileResponse{}, nil
}

// WriteTextFile is a no-op implementation — project manager does not use file writes.
func (c *projectACPClient) WriteTextFile(_ context.Context, _ acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	return acpsdk.WriteTextFileResponse{}, nil
}

// RequestPermission auto-approves the first available option.
// Project manager connections are internal and do not require manual permission review.
func (c *projectACPClient) RequestPermission(_ context.Context, params acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	if len(params.Options) > 0 {
		outcome := acpsdk.NewRequestPermissionOutcomeSelected(params.Options[0].OptionId)
		return acpsdk.RequestPermissionResponse{Outcome: outcome}, nil
	}
	return acpsdk.RequestPermissionResponse{}, nil
}

// SessionUpdate discards all session notifications — project manager does not stream output.
func (c *projectACPClient) SessionUpdate(_ context.Context, _ acpsdk.SessionNotification) error {
	return nil
}

// CreateTerminal is a no-op — project manager does not manage terminals.
func (c *projectACPClient) CreateTerminal(_ context.Context, _ acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	return acpsdk.CreateTerminalResponse{}, nil
}

// KillTerminal is a no-op — project manager does not manage terminals.
func (c *projectACPClient) KillTerminal(_ context.Context, _ acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error) {
	return acpsdk.KillTerminalResponse{}, nil
}

// TerminalOutput is a no-op — project manager does not manage terminals.
func (c *projectACPClient) TerminalOutput(_ context.Context, _ acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{}, nil
}

// ReleaseTerminal is a no-op — project manager does not manage terminals.
func (c *projectACPClient) ReleaseTerminal(_ context.Context, _ acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, nil
}

// WaitForTerminalExit is a no-op — project manager does not manage terminals.
func (c *projectACPClient) WaitForTerminalExit(_ context.Context, _ acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	return acpsdk.WaitForTerminalExitResponse{}, nil
}
