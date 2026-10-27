package client

import "context"

// LSPConfig is a configured language server.
type LSPConfig struct {
	Language  string   `json:"language"`
	Disabled  bool     `json:"disabled"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Languages []string `json:"languages"`
	Filenames []string `json:"filenames"`
	Autostart bool     `json:"autostart"`
}

// LSPActivation holds the global on-demand activation knobs.
type LSPActivation struct {
	AutoActivate   bool   `json:"autoActivate"`
	ActivateOn     string `json:"activateOn"`
	AutoInstall    bool   `json:"autoInstall"`
	Runner         string `json:"runner"`
	StartupTimeout string `json:"startupTimeout"`
	InstallTimeout string `json:"installTimeout"`
}

// LSPServerStatus is a catalogue entry with its live status.
type LSPServerStatus struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Command           string   `json:"command"`
	ResolvedCommand   string   `json:"resolvedCommand"`
	Args              []string `json:"args"`
	Languages         []string `json:"languages"`
	Filenames         []string `json:"filenames"`
	Configured        bool     `json:"configured"`
	OptIn             bool     `json:"optIn"`
	Disabled          bool     `json:"disabled"`
	Autostart         bool     `json:"autostart"`
	Availability      string   `json:"availability"`
	AvailabilityLabel string   `json:"availabilityLabel"`
	Reason            string   `json:"reason"`
	Hint              string   `json:"hint"`
	URL               string   `json:"url"`
	RunState          string   `json:"runState"`
	Installing        bool     `json:"installing"`
}

// LSPConfigs reads GET /api/v1/config/lsp.
func (c *Client) LSPConfigs(ctx context.Context) ([]LSPConfig, *LSPActivation, error) {
	var out struct {
		LSP        []LSPConfig    `json:"lsp"`
		Activation *LSPActivation `json:"activation"`
	}
	err := c.Get(ctx, "/api/v1/config/lsp", &out)
	return out.LSP, out.Activation, err
}

// LSPCatalog reads the whole catalogue with availability.
func (c *Client) LSPCatalog(ctx context.Context) ([]LSPServerStatus, error) {
	var out struct {
		Servers []LSPServerStatus `json:"servers"`
	}
	err := c.Get(ctx, "/api/v1/config/lsp/catalog", &out)
	return out.Servers, err
}

// SaveLSP creates or updates a language server.
func (c *Client) SaveLSP(ctx context.Context, cfg LSPConfig) error {
	return c.Put(ctx, "/api/v1/config/lsp", cfg, nil)
}

// DeleteLSP removes a language server.
func (c *Client) DeleteLSP(ctx context.Context, language string) error {
	return c.Delete(ctx, "/api/v1/config/lsp/"+Esc(language), nil)
}

// SaveLSPActivation stores the activation knobs.
func (c *Client) SaveLSPActivation(ctx context.Context, a LSPActivation) (LSPActivation, error) {
	out := a
	err := c.Put(ctx, "/api/v1/config/lsp/activation", a, &out)
	return out, err
}
