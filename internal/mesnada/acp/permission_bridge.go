package acp

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

// ACPPermissionBridge translates Pando tool permission requests into ACP
// requestPermission calls to the connected editor. It is installed as a session
// handler when the session mode is "ask".
type ACPPermissionBridge struct {
	conn      *acpsdk.AgentSideConnection
	sessionID acpsdk.SessionId
	logger    *log.Logger

	// mu serializes permission requests for the same session.
	mu sync.Mutex
}

// NewACPPermissionBridge creates a new bridge for the given session.
func NewACPPermissionBridge(conn *acpsdk.AgentSideConnection, sessionID acpsdk.SessionId, logger *log.Logger) *ACPPermissionBridge {
	if logger == nil {
		logger = log.Default()
	}

	return &ACPPermissionBridge{
		conn:      conn,
		sessionID: sessionID,
		logger:    logger,
	}
}

// Handle processes a permission request by asking the connected editor via ACP.
// Returns true if approved, false if rejected.
// Signature matches what RegisterSessionHandler expects:
// func(sessionID, toolName, description string) bool
func (b *ACPPermissionBridge) Handle(sessionID, toolName, description string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.conn == nil {
		b.logger.Printf("[ACP BRIDGE] No ACP connection available for tool=%s session=%s — denying", toolName, sessionID)
		return false
	}

	b.logger.Printf("[ACP BRIDGE] Requesting permission for tool=%s session=%s", toolName, sessionID)

	title := toolName
	if description != "" {
		title = description
	}

	toolKind := mapToolKind(toolName)
	status := acpsdk.ToolCallStatusPending

	req := acpsdk.RequestPermissionRequest{
		SessionId: b.sessionID,
		ToolCall: acpsdk.RequestPermissionToolCall{
			ToolCallId: acpsdk.ToolCallId(fmt.Sprintf("%s-%s-%d", sessionID, toolName, time.Now().UnixNano())),
			Status:     &status,
			Title:      &title,
			Kind:       &toolKind,
		},
		Options: []acpsdk.PermissionOption{
			{
				OptionId: acpsdk.PermissionOptionId("once"),
				Kind:     acpsdk.PermissionOptionKindAllowOnce,
				Name:     "Allow once",
			},
			{
				OptionId: acpsdk.PermissionOptionId("always"),
				Kind:     acpsdk.PermissionOptionKindAllowAlways,
				Name:     "Always allow",
			},
			{
				OptionId: acpsdk.PermissionOptionId("reject"),
				Kind:     acpsdk.PermissionOptionKindRejectOnce,
				Name:     "Reject",
			},
		},
	}

	resp, err := b.conn.RequestPermission(context.Background(), req)
	if err != nil {
		b.logger.Printf("[ACP BRIDGE] RequestPermission error for tool=%s session=%s: %v — denying", toolName, sessionID, err)
		return false
	}

	if resp.Outcome.Selected == nil {
		b.logger.Printf("[ACP BRIDGE] Permission cancelled for tool=%s session=%s", toolName, sessionID)
		return false
	}

	selected := string(resp.Outcome.Selected.OptionId)
	b.logger.Printf("[ACP BRIDGE] Permission outcome=%s for tool=%s session=%s", selected, toolName, sessionID)
	return selected == "once" || selected == "always"
}
