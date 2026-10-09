package lsp

import (
	"encoding/json"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/lsp/protocol"
	"github.com/digiogithub/pando/internal/lsp/util"
)

// Requests

func HandleWorkspaceConfiguration(params json.RawMessage) (any, error) {
	return []map[string]any{{}}, nil
}

// HandleRegisterCapability processes client/registerCapability requests sent by
// the server of client c. File watch registrations are routed to that client's
// own handler (see Client.SetFileWatchHandler).
func HandleRegisterCapability(c *Client, params json.RawMessage) (any, error) {
	var registerParams protocol.RegistrationParams
	if err := json.Unmarshal(params, &registerParams); err != nil {
		logging.Error("Error unmarshaling registration params", "error", err)
		return nil, err
	}

	for _, reg := range registerParams.Registrations {
		switch reg.Method {
		case "workspace/didChangeWatchedFiles":
			// Parse the registration options
			optionsJSON, err := json.Marshal(reg.RegisterOptions)
			if err != nil {
				logging.Error("Error marshaling registration options", "error", err)
				continue
			}

			var options protocol.DidChangeWatchedFilesRegistrationOptions
			if err := json.Unmarshal(optionsJSON, &options); err != nil {
				logging.Error("Error unmarshaling registration options", "error", err)
				continue
			}

			// Store the file watchers registrations
			c.notifyFileWatchRegistration(reg.ID, options.Watchers)
		}
	}

	return nil, nil
}

func HandleApplyEdit(params json.RawMessage) (any, error) {
	var edit protocol.ApplyWorkspaceEditParams
	if err := json.Unmarshal(params, &edit); err != nil {
		return nil, err
	}

	err := util.ApplyWorkspaceEdit(edit.Edit)
	if err != nil {
		logging.Error("Error applying workspace edit", "error", err)
		return protocol.ApplyWorkspaceEditResult{Applied: false, FailureReason: err.Error()}, nil
	}

	return protocol.ApplyWorkspaceEditResult{Applied: true}, nil
}

// FileWatchRegistrationHandler is a function that will be called when file watch registrations are received
type FileWatchRegistrationHandler func(id string, watchers []protocol.FileSystemWatcher)

type pendingFileWatch struct {
	id       string
	watchers []protocol.FileSystemWatcher
}

// SetFileWatchHandler sets the handler that receives this client's file watch
// registrations. Registrations that arrived before a handler was set (servers
// often register during initialization) are replayed to it immediately.
func (c *Client) SetFileWatchHandler(handler FileWatchRegistrationHandler) {
	c.fileWatchMu.Lock()
	c.fileWatchHandler = handler
	pending := c.pendingFileWatch
	c.pendingFileWatch = nil
	c.fileWatchMu.Unlock()

	if handler == nil {
		return
	}
	for _, p := range pending {
		handler(p.id, p.watchers)
	}
}

// notifyFileWatchRegistration delivers a registration to this client's handler,
// or queues it until one is set.
func (c *Client) notifyFileWatchRegistration(id string, watchers []protocol.FileSystemWatcher) {
	c.fileWatchMu.Lock()
	handler := c.fileWatchHandler
	if handler == nil {
		c.pendingFileWatch = append(c.pendingFileWatch, pendingFileWatch{id: id, watchers: watchers})
	}
	c.fileWatchMu.Unlock()

	if handler != nil {
		handler(id, watchers)
	}
}

// Notifications

func HandleServerMessage(params json.RawMessage) {
	cnf := config.Get()
	var msg struct {
		Type    int    `json:"type"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(params, &msg); err == nil {
		if cnf.DebugLSP {
			logging.Debug("Server message", "type", msg.Type, "message", msg.Message)
		}
	}
}

func HandleDiagnostics(client *Client, params json.RawMessage) {
	var diagParams protocol.PublishDiagnosticsParams
	if err := json.Unmarshal(params, &diagParams); err != nil {
		logging.Error("Error unmarshaling diagnostics params", "error", err)
		return
	}

	client.diagnosticsMu.Lock()
	defer client.diagnosticsMu.Unlock()

	client.diagnostics[diagParams.URI] = diagParams.Diagnostics
}
