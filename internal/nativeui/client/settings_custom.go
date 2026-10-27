package client

import (
	"context"
)

func objList(o Obj, key string) []Obj {
	arr, _ := o[key].([]any)
	out := make([]Obj, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// ObjList returns the objects stored under key (nil-safe helper for views).
func ObjList(o Obj, key string) []Obj { return objList(o, key) }

// ProviderAccounts is GET /api/v1/config/provider-accounts.
func (c *Client) ProviderAccounts(ctx context.Context) ([]Obj, error) {
	o, err := c.GetObj(ctx, "/api/v1/config/provider-accounts")
	return objList(o, "providerAccounts"), err
}

// SaveProviderAccount creates (POST) or updates (PUT) an account.
func (c *Client) SaveProviderAccount(ctx context.Context, editID string, payload Obj) error {
	if editID != "" {
		_, err := c.SendObj(ctx, "PUT", "/api/v1/config/provider-accounts/"+Esc(editID), payload)
		return err
	}
	_, err := c.SendObj(ctx, "POST", "/api/v1/config/provider-accounts", payload)
	return err
}

// DeleteProviderAccount removes an account.
func (c *Client) DeleteProviderAccount(ctx context.Context, id string) error {
	return c.Delete(ctx, "/api/v1/config/provider-accounts/"+Esc(id), nil)
}

// ProviderTestResult is the answer of an account connection test.
type ProviderTestResult struct {
	OK         bool   `json:"ok"`
	ModelCount int    `json:"modelCount"`
	Error      string `json:"error"`
}

// TestProviderAccount probes an account's credentials.
func (c *Client) TestProviderAccount(ctx context.Context, id string) (ProviderTestResult, error) {
	var r ProviderTestResult
	err := c.Post(ctx, "/api/v1/config/provider-accounts/"+Esc(id)+"/test", map[string]any{}, &r)
	return r, err
}

// CopilotLoginInfo is the device-code answer of the Copilot login.
type CopilotLoginInfo struct {
	VerificationURI string `json:"verificationUri"`
	UserCode        string `json:"userCode"`
}

// CopilotLogin starts the GitHub device flow.
func (c *Client) CopilotLogin(ctx context.Context) (CopilotLoginInfo, error) {
	var r CopilotLoginInfo
	err := c.Post(ctx, "/api/v1/auth/providers/copilot/login", map[string]any{}, &r)
	return r, err
}

// Agents is GET /api/v1/config/agents.
func (c *Client) Agents(ctx context.Context) ([]Obj, error) {
	o, err := c.GetObj(ctx, PathConfigAgents)
	return objList(o, "agents"), err
}

// SaveAgents is PUT /api/v1/config/agents and returns the stored agents.
func (c *Client) SaveAgents(ctx context.Context, agents []Obj) ([]Obj, error) {
	o, err := c.SendObj(ctx, "PUT", PathConfigAgents, Obj{"agents": agents})
	return objList(o, "agents"), err
}

// DecisionModelRouter reads provider and model of the shared decision model.
func (c *Client) DecisionModelRouter(ctx context.Context) (provider, model string, err error) {
	o, err := c.GetObj(ctx, "/api/v1/config/decision-model")
	if err != nil {
		return "", "", err
	}
	r, _ := o["router"].(map[string]any)
	provider, _ = r["provider"].(string)
	model, _ = r["model"].(string)
	return provider, model, nil
}

// BasicAuthStatus is the WebUI access (HTTP basic auth) state.
type BasicAuthStatus struct {
	Enabled bool `json:"enabled"`
	Users   []struct {
		Username string `json:"username"`
	} `json:"users"`
	Enforced bool   `json:"enforced"`
	BindHost string `json:"bindHost"`
}

const basicAuthPath = "/api/v1/config/api-server/basic-auth"

// BasicAuth reads the status.
func (c *Client) BasicAuth(ctx context.Context) (BasicAuthStatus, error) {
	var s BasicAuthStatus
	err := c.Get(ctx, basicAuthPath, &s)
	return s, err
}

// SetBasicAuthEnabled toggles basic auth.
func (c *Client) SetBasicAuthEnabled(ctx context.Context, on bool) (BasicAuthStatus, error) {
	var s BasicAuthStatus
	err := c.Put(ctx, basicAuthPath, map[string]bool{"enabled": on}, &s)
	return s, err
}

// AddBasicAuthUser creates or updates a user.
func (c *Client) AddBasicAuthUser(ctx context.Context, user, pass string) (BasicAuthStatus, error) {
	var s BasicAuthStatus
	err := c.Post(ctx, basicAuthPath+"/users", map[string]string{"username": user, "password": pass}, &s)
	return s, err
}

// DeleteBasicAuthUser removes a user.
func (c *Client) DeleteBasicAuthUser(ctx context.Context, user string) (BasicAuthStatus, error) {
	var s BasicAuthStatus
	err := c.Delete(ctx, basicAuthPath+"/users/"+Esc(user), &s)
	return s, err
}

// RevealBasicAuthPassword returns a user's stored password.
func (c *Client) RevealBasicAuthPassword(ctx context.Context, user string) (string, error) {
	var r struct {
		Password string `json:"password"`
	}
	err := c.Post(ctx, basicAuthPath+"/users/"+Esc(user)+"/reveal", map[string]any{}, &r)
	return r.Password, err
}

// DesignSystemPayload is GET /api/v1/design/system.
type DesignSystemPayload struct {
	System struct {
		Name   string                       `json:"name"`
		Tokens map[string]map[string]string `json:"tokens"`
	} `json:"system"`
	Exists         bool   `json:"exists"`
	TokensPath     string `json:"tokens_path"`
	StylesheetPath string `json:"stylesheet_path"`
	ContractPath   string `json:"contract_path"`
}

// DesignSystem reads the project design system.
func (c *Client) DesignSystem(ctx context.Context) (DesignSystemPayload, error) {
	var p DesignSystemPayload
	err := c.Get(ctx, "/api/v1/design/system", &p)
	return p, err
}

// SaveDesignSystem stores name and tokens.
func (c *Client) SaveDesignSystem(ctx context.Context, name string, tokens map[string]map[string]string) (DesignSystemPayload, error) {
	var p DesignSystemPayload
	err := c.Put(ctx, "/api/v1/design/system", map[string]any{"name": name, "tokens": tokens, "fonts": []string{}, "replace_fonts": false}, &p)
	return p, err
}

// DesignExample is a bundled style-guide example.
type DesignExample struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// DesignSystemExamples lists the bundled examples.
func (c *Client) DesignSystemExamples(ctx context.Context) ([]DesignExample, error) {
	var r struct {
		Examples []DesignExample `json:"examples"`
	}
	err := c.Get(ctx, "/api/v1/design/system/examples", &r)
	return r.Examples, err
}

// DesignExtractResult is the answer of an extraction.
type DesignExtractResult struct {
	Result struct {
		Notes []string `json:"notes"`
	} `json:"result"`
	Saved       bool                 `json:"saved"`
	System      *DesignSystemPayload `json:"system"`
	Mirrored    string               `json:"mirrored"`
	MirrorError string               `json:"mirror_error"`
}

// ExtractDesignSystem extracts a system from code/url/image/text.
func (c *Client) ExtractDesignSystem(ctx context.Context, source, target, name string, dryRun bool) (DesignExtractResult, error) {
	var r DesignExtractResult
	err := c.Post(ctx, "/api/v1/design/system/extract", map[string]any{"source": source, "target": target, "name": name, "dry_run": dryRun}, &r)
	return r, err
}
