package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
)

// Endpoints used by the window shell: preferences, desktop settings,
// notifications, version, config bootstrap, project list and Copilot account
// commands. Each mirrors a web-ui/packages/pando-client store.

// UIPrefs is GET/PUT /api/v1/ui/preferences. PUT merges: empty strings and
// nil pointers leave the stored value alone.
type UIPrefs struct {
	ChatMode         string `json:"chatMode,omitempty"`
	Language         string `json:"language,omitempty"`
	Theme            string `json:"theme,omitempty"`
	ThemeMode        string `json:"themeMode,omitempty"`
	ThemeAccent      string `json:"themeAccent,omitempty"`
	SidebarCollapsed *bool  `json:"sidebarCollapsed,omitempty"`
}

// GetUIPrefs reads the persisted UI preferences.
func (c *Client) GetUIPrefs(ctx context.Context) (UIPrefs, error) {
	var p UIPrefs
	err := c.Get(ctx, "/api/v1/ui/preferences", &p)
	return p, err
}

// PutUIPrefs merges a patch into the persisted UI preferences.
func (c *Client) PutUIPrefs(ctx context.Context, patch UIPrefs) (UIPrefs, error) {
	var p UIPrefs
	err := c.Put(ctx, "/api/v1/ui/preferences", patch, &p)
	return p, err
}

// DesktopSettings are the fields of GET /api/v1/settings the shell reads.
type DesktopSettings struct {
	// TrayOnMinimizeClose is Desktop.TrayOnMinimizeClose.
	TrayOnMinimizeClose bool `json:"desktop_tray_on_minimize_close"`
	// DefaultModel is the configured coder model, empty when none.
	DefaultModel string `json:"default_model"`
	// Theme is the backend theme id ("family-mode"), shared with the TUI.
	Theme string `json:"theme"`
	// WorkingDirectory is the project directory relative paths resolve to.
	WorkingDirectory string `json:"working_directory"`
}

// GetDesktopSettings reads the shell-relevant settings.
func (c *Client) GetDesktopSettings(ctx context.Context) (DesktopSettings, error) {
	var s DesktopSettings
	err := c.Get(ctx, "/api/v1/settings", &s)
	return s, err
}

// ConfigEvent is a frame of GET /api/v1/config/events (a hot-reload
// notification). The initial "connected" heartbeat has Section empty and
// Connected set.
type ConfigEvent struct {
	Section     string   `json:"section"`
	Source      string   `json:"source"`
	Event       string   `json:"event"`
	ChangedKeys []string `json:"changedKeys"`
	Connected   bool     `json:"-"`
}

// SubscribeConfigEvents follows config changes until ctx ends (reconnecting).
// fn runs on the subscriber goroutine.
func (c *Client) SubscribeConfigEvents(ctx context.Context, fn func(ConfigEvent)) error {
	return c.Subscribe(ctx, "/api/v1/config/events", func(f SSEFrame) {
		if f.Event == "connected" {
			fn(ConfigEvent{Connected: true})
			return
		}
		var ev ConfigEvent
		if json.Unmarshal([]byte(f.Data), &ev) != nil {
			return
		}
		fn(ev)
	})
}

// Notification is a payload of GET /api/v1/notifications/stream.
type Notification struct {
	ID      string `json:"id"`
	Level   string `json:"level"`
	Source  string `json:"source"`
	Message string `json:"message"`
	Time    string `json:"time,omitempty"`
}

// SubscribeNotifications follows the notification stream until ctx ends.
func (c *Client) SubscribeNotifications(ctx context.Context, fn func(Notification)) error {
	return c.Subscribe(ctx, "/api/v1/notifications/stream", func(f SSEFrame) {
		if f.Event != "notification" {
			return
		}
		var n Notification
		if json.Unmarshal([]byte(f.Data), &n) != nil {
			return
		}
		fn(n)
	})
}

// ConfigInitStatus is GET /api/v1/config/init-status.
type ConfigInitStatus struct {
	HasLocalConfig bool `json:"hasLocalConfig"`
	HasPandoDir    bool `json:"hasPandoDir"`
	ShouldGenerate bool `json:"shouldGenerate"`
}

// ConfigInitStatus reports whether the working directory lacks .pando.toml.
func (c *Client) ConfigInitStatus(ctx context.Context) (ConfigInitStatus, error) {
	var s ConfigInitStatus
	err := c.Get(ctx, "/api/v1/config/init-status", &s)
	return s, err
}

// GenerateConfig writes a local .pando.toml (POST /api/v1/config/generate).
func (c *Client) GenerateConfig(ctx context.Context) error {
	return c.Post(ctx, "/api/v1/config/generate", map[string]any{}, nil)
}

// ProjectOpen reports whether the project has a live workspace: its project
// WebUI child runs (what the Web UI shows as an open project tab).
func ProjectOpen(p Project) bool { return p.WebState == "running" || p.WebState == "starting" }

// AuthProviderType is a provider type of GET /api/v1/config/provider-types.
type AuthProviderType struct {
	Type          string `json:"type"`
	DisplayName   string `json:"displayName"`
	SupportsOAuth bool   `json:"supportsOAuth"`
}

// AuthProviderTypes lists the provider types (the quick menu offers account
// commands for Copilot).
func (c *Client) AuthProviderTypes(ctx context.Context) ([]AuthProviderType, error) {
	var out struct {
		ProviderTypes []AuthProviderType `json:"providerTypes"`
	}
	if err := c.Get(ctx, "/api/v1/config/provider-types", &out); err != nil {
		return nil, err
	}
	return out.ProviderTypes, nil
}

// AuthStatus is GET /api/v1/auth/providers/{type}/status.
type AuthStatus struct {
	Provider      string `json:"provider"`
	Authenticated bool   `json:"authenticated"`
	Source        string `json:"source"`
	Message       string `json:"message"`
	EnterpriseURL string `json:"enterpriseUrl"`
}

// ProviderAuthStatus reports whether a provider has saved credentials.
func (c *Client) ProviderAuthStatus(ctx context.Context, provider string) (AuthStatus, error) {
	var s AuthStatus
	err := c.Get(ctx, "/api/v1/auth/providers/"+url.PathEscape(provider)+"/status", &s)
	return s, err
}

// CopilotLogin is the device-flow start of POST /api/v1/auth/providers/copilot/login.
type CopilotLogin struct {
	VerificationURI string `json:"verificationUri"`
	UserCode        string `json:"userCode"`
	Message         string `json:"message"`
}

// StartCopilotLogin starts the GitHub device login.
func (c *Client) StartCopilotLogin(ctx context.Context) (CopilotLogin, error) {
	var l CopilotLogin
	err := c.Post(ctx, "/api/v1/auth/providers/copilot/login", map[string]any{}, &l)
	return l, err
}

// CopilotLogout removes the saved Copilot credentials and returns the
// server's message.
func (c *Client) CopilotLogout(ctx context.Context) (string, error) {
	var out struct {
		Message string `json:"message"`
	}
	err := c.Post(ctx, "/api/v1/auth/providers/copilot/logout", map[string]any{}, &out)
	return out.Message, err
}

// DescribeError turns an API error into a short message for the user;
// authorization failures are named explicitly (the token was rejected even
// after a refresh, or an exposed server wants basic auth).
func DescribeError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrBasicAuthRequired):
		return "The server requires a username and password (basic auth)."
	case errors.Is(err, ErrUnauthorized):
		return "The server rejected the access token (401). Restart Pando or sign in again."
	}
	return err.Error()
}

// IsAuthError reports a 401-class failure (ErrUnauthorized or
// ErrBasicAuthRequired).
func IsAuthError(err error) bool {
	return errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrBasicAuthRequired)
}
