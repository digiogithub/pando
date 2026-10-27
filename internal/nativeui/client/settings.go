package client

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
)

// Obj is a decoded JSON object whose numbers keep their literal form
// (json.Number), so a value read from a config endpoint and written back is
// never rewritten as 1e+06. Settings forms edit Obj documents and PUT them
// back, which preserves every field the form does not know about.
type Obj = map[string]any

// Settings endpoints shared with the Web UI (web-ui/packages/pando-client
// stores). Each is read with GetObj and written with SendObj.
const (
	PathSettings         = "/api/v1/settings"
	PathUIPreferences    = "/api/v1/ui/preferences"
	PathUIPolicy         = "/api/v1/config/ui-policy"
	PathConfigTools      = "/api/v1/config/tools"
	PathConfigBash       = "/api/v1/config/bash"
	PathConfigSandbox    = "/api/v1/config/sandbox"
	PathConfigTokenOpt   = "/api/v1/config/token-optimization"
	PathConfigMCPGateway = "/api/v1/config/mcp-gateway"
	PathConfigServices   = "/api/v1/config/services"
	PathConfigExtensions = "/api/v1/config/extensions"
	PathConfigEvaluator  = "/api/v1/config/evaluator"
	PathContainerConfig  = "/api/v1/container/config"
	PathConfigAgents     = "/api/v1/config/agents"
)

// DecodeObj decodes data into an Obj keeping numbers as json.Number.
func DecodeObj(data []byte) (Obj, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var o Obj
	if err := dec.Decode(&o); err != nil {
		return nil, err
	}
	if o == nil {
		o = Obj{}
	}
	return o, nil
}

// GetObj reads a JSON object.
func (c *Client) GetObj(ctx context.Context, path string) (Obj, error) {
	return c.SendObj(ctx, "GET", path, nil)
}

// SendObj sends body (nil for none) and decodes the JSON object answer
// (empty object when the answer has no body).
func (c *Client) SendObj(ctx context.Context, method, path string, body any) (Obj, error) {
	var raw json.RawMessage
	if err := c.Do(ctx, method, path, body, &raw); err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return Obj{}, nil
	}
	return DecodeObj(raw)
}

// UIPreferences is GET /api/v1/ui/preferences (user-level UI choices).
type UIPreferences struct {
	Language string `json:"language,omitempty"`
	ChatMode string `json:"chatMode,omitempty"`
}

// GetUIPreferences reads the user-level UI preferences.
func (c *Client) GetUIPreferences(ctx context.Context) (UIPreferences, error) {
	var p UIPreferences
	err := c.Get(ctx, PathUIPreferences, &p)
	return p, err
}

// SetLanguagePreference stores the UI language user-level.
func (c *Client) SetLanguagePreference(ctx context.Context, lang string) error {
	return c.Put(ctx, PathUIPreferences, map[string]string{"language": lang}, nil)
}

// RegenerateTelemetryID asks the server for a new telemetry debug id with
// the minimal request the Web UI sends and returns the new id.
func (c *Client) RegenerateTelemetryID(ctx context.Context) (string, error) {
	o, err := c.SendObj(ctx, "PUT", PathSettings, map[string]bool{"telemetry_regenerate_id": true})
	if err != nil {
		return "", err
	}
	s, _ := o["telemetry_debug_id"].(string)
	return s, nil
}

// RegenerateAPIToken is POST /api/v1/config/api-server/regenerate-token.
func (c *Client) RegenerateAPIToken(ctx context.Context) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	err := c.Post(ctx, "/api/v1/config/api-server/regenerate-token", map[string]any{}, &out)
	return out.Token, err
}

// VersionStatus is GET /api/v1/version.
type VersionStatus struct {
	Version         string `json:"version"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	UpdateCommand   string `json:"update_command"`
	ReleaseURL      string `json:"release_url"`
	Checkable       bool   `json:"checkable"`
}

// Version reads the running version and update availability.
func (c *Client) Version(ctx context.Context) (VersionStatus, error) {
	var v VersionStatus
	err := c.Get(ctx, "/api/v1/version", &v)
	return v, err
}

// Esc escapes a path segment.
func Esc(s string) string { return url.PathEscape(s) }

// UIPolicyBanner is the notice an extension may show above the settings.
type UIPolicyBanner struct {
	Text string `json:"text"`
	Link string `json:"link"`
}

// UIPolicy is GET /api/v1/config/ui-policy: sections an extension hides or
// makes read-only. A standalone Pando answers empty lists.
type UIPolicy struct {
	HiddenSections   []string       `json:"hiddenSections"`
	ReadOnlySections []string       `json:"readOnlySections"`
	ReadOnlyLabel    string         `json:"readOnlyLabel"`
	Banner           UIPolicyBanner `json:"banner"`
}

// UIPolicy reads the settings UI policy.
func (c *Client) UIPolicy(ctx context.Context) (UIPolicy, error) {
	var p UIPolicy
	err := c.Get(ctx, PathUIPolicy, &p)
	return p, err
}
