package client

import (
	"context"
	"net/url"
	"sort"
)

// ExtensionPanel is one panel a compiled-in extension contributes
// (GET /api/v1/extensions/ui).
type ExtensionPanel struct {
	ID        string `json:"id"`
	Extension string `json:"extension"`
	Title     string `json:"title"`
	Slot      string `json:"slot"`
	Entry     string `json:"entry"`
	Icon      string `json:"icon,omitempty"`
	Order     int    `json:"order"`
}

// Extension panel slots the shell reserves.
const (
	SlotSidebar   = "sidebar"
	SlotSettings  = "settings"
	SlotChatSide  = "chat-side"
	SlotStatusBar = "status-bar"
)

// ExtensionPanels fetches the panel manifest sorted by order then id. A
// standard build returns no panels.
func (c *Client) ExtensionPanels(ctx context.Context) ([]ExtensionPanel, error) {
	var out struct {
		Panels []ExtensionPanel `json:"panels"`
	}
	if err := c.Get(ctx, "/api/v1/extensions/ui", &out); err != nil {
		return nil, err
	}
	sort.SliceStable(out.Panels, func(i, j int) bool {
		if out.Panels[i].Order != out.Panels[j].Order {
			return out.Panels[i].Order < out.Panels[j].Order
		}
		return out.Panels[i].ID < out.Panels[j].ID
	})
	return out.Panels, nil
}

// ExtensionHostPath is the path of the server-served host page that mounts
// one extension panel with the window.__PANDO_UI__ v1 contract.
func ExtensionHostPath(panelID string) string {
	return "/api/v1/extensions/host?panel=" + url.QueryEscape(panelID)
}
