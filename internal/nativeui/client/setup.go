package client

import (
	"context"
	"net/url"
)

// SetupStatus mirrors config.SetupStatus (GET /api/v1/setup/status).
type SetupStatus struct {
	WorkingDir              string `json:"workingDir"`
	IsHomeDir               bool   `json:"isHomeDir"`
	HasLocalConfig          bool   `json:"hasLocalConfig"`
	LocalConfigPath         string `json:"localConfigPath,omitempty"`
	HasGlobalConfig         bool   `json:"hasGlobalConfig"`
	GlobalConfigPath        string `json:"globalConfigPath,omitempty"`
	DefaultGlobalConfigPath string `json:"defaultGlobalConfigPath,omitempty"`
	ProviderAccounts        int    `json:"providerAccounts"`
	HasUsableProvider       bool   `json:"hasUsableProvider"`
	CoderModel              string `json:"coderModel,omitempty"`
	CoderModelValid         bool   `json:"coderModelValid"`
	RemembrancesEnabled     bool   `json:"remembrancesEnabled"`
	Completed               bool   `json:"completed"`
	Needed                  bool   `json:"needed"`
}

// SetupScopeResult is the answer of POST /api/v1/setup/scope.
type SetupScopeResult struct {
	Scope      string      `json:"scope"`
	ConfigPath string      `json:"configPath"`
	Status     SetupStatus `json:"status"`
}

// SuggestedModels is GET /api/v1/setup/suggested-models.
type SuggestedModels struct {
	Provider  string `json:"provider"`
	MainModel string `json:"mainModel"`
	FastModel string `json:"fastModel"`
}

// SetupProviderAccount is an entry of /api/v1/config/provider-accounts.
type SetupProviderAccount struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Type        string `json:"type"`
	APIKey      string `json:"apiKey,omitempty"`
	BaseURL     string `json:"baseUrl,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// SetupCopilotCode is the device-flow code of POST /api/v1/auth/providers/copilot/login.
type SetupCopilotCode struct {
	VerificationURI string `json:"verificationUri"`
	UserCode        string `json:"userCode"`
	ExpiresIn       int    `json:"expiresIn"`
	Interval        int    `json:"interval"`
}

// SetupOllamaOption is one way to install Ollama.
type SetupOllamaOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Command     string `json:"command,omitempty"`
	URL         string `json:"url,omitempty"`
	Recommended bool   `json:"recommended"`
	Runnable    bool   `json:"runnable"`
	Reason      string `json:"reason,omitempty"`
}

// SetupOllamaStatus is GET /api/v1/setup/ollama/status.
type SetupOllamaStatus struct {
	OS               string              `json:"os"`
	BaseURL          string              `json:"baseUrl"`
	Installed        bool                `json:"installed"`
	BinaryPath       string              `json:"binaryPath,omitempty"`
	Running          bool                `json:"running"`
	Version          string              `json:"version,omitempty"`
	Models           []string            `json:"models"`
	InstallOptions   []SetupOllamaOption `json:"installOptions"`
	CanStart         bool                `json:"canStart"`
	DocumentModel    string              `json:"documentModel"`
	CodeModel        string              `json:"codeModel"`
	HasDocumentModel bool                `json:"hasDocumentModel"`
	HasCodeModel     bool                `json:"hasCodeModel"`
}

// SetupJob is an install or pull job (GET /api/v1/setup/jobs/{id}).
type SetupJob struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Target    string   `json:"target"`
	State     string   `json:"state"` // running | done | error
	Status    string   `json:"status,omitempty"`
	Completed int64    `json:"completed"`
	Total     int64    `json:"total"`
	Output    []string `json:"output,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// SetupStatus fetches the first-run status.
func (c *Client) SetupStatus(ctx context.Context) (SetupStatus, error) {
	var s SetupStatus
	err := c.Get(ctx, "/api/v1/setup/status", &s)
	return s, err
}

// SetupScope creates the configuration file of scope ("global" | "project").
func (c *Client) SetupScope(ctx context.Context, scope string) (SetupScopeResult, error) {
	var r SetupScopeResult
	err := c.Post(ctx, "/api/v1/setup/scope", map[string]string{"scope": scope}, &r)
	return r, err
}

// SetupSuggestedModels asks for the suggested main/fast models of a provider.
func (c *Client) SetupSuggestedModels(ctx context.Context, provider string, refresh bool) (SuggestedModels, error) {
	var r SuggestedModels
	p := "/api/v1/setup/suggested-models?provider=" + url.QueryEscape(provider)
	if refresh {
		p += "&refresh=1"
	}
	err := c.Get(ctx, p, &r)
	return r, err
}

// SetupSaveModels assigns the main model to the coder and the fast one to the
// secondary agents; it returns the resolved IDs.
func (c *Client) SetupSaveModels(ctx context.Context, main, fast string) (SuggestedModels, error) {
	var r SuggestedModels
	err := c.Post(ctx, "/api/v1/setup/models", map[string]string{"mainModel": main, "fastModel": fast}, &r)
	return r, err
}

// SetupComplete records the assistant as finished.
func (c *Client) SetupComplete(ctx context.Context) (SetupStatus, error) {
	var s SetupStatus
	err := c.Post(ctx, "/api/v1/setup/complete", map[string]any{}, &s)
	return s, err
}

// SetupProviderAccounts returns the configured provider accounts.
func (c *Client) SetupProviderAccounts(ctx context.Context) ([]SetupProviderAccount, error) {
	var r struct {
		ProviderAccounts []SetupProviderAccount `json:"providerAccounts"`
	}
	err := c.Get(ctx, "/api/v1/config/provider-accounts", &r)
	return r.ProviderAccounts, err
}

// SetupAddProviderAccount creates a provider account.
func (c *Client) SetupAddProviderAccount(ctx context.Context, a SetupProviderAccount) error {
	return c.Post(ctx, "/api/v1/config/provider-accounts", a, nil)
}

// SetupCopilotStatus reports whether GitHub Copilot has a valid login.
func (c *Client) SetupCopilotStatus(ctx context.Context) (bool, error) {
	var r struct {
		Authenticated bool `json:"authenticated"`
	}
	err := c.Get(ctx, "/api/v1/auth/providers/copilot/status", &r)
	return r.Authenticated, err
}

// SetupCopilotLogin starts the device flow.
func (c *Client) SetupCopilotLogin(ctx context.Context) (SetupCopilotCode, error) {
	var r SetupCopilotCode
	err := c.Post(ctx, "/api/v1/auth/providers/copilot/login", map[string]any{}, &r)
	return r, err
}

// SetupOllamaStatus detects Ollama and the embedding models.
func (c *Client) SetupOllamaStatus(ctx context.Context) (SetupOllamaStatus, error) {
	var r SetupOllamaStatus
	err := c.Get(ctx, "/api/v1/setup/ollama/status", &r)
	return r, err
}

// SetupOllamaInstall runs a fixed install option server-side.
func (c *Client) SetupOllamaInstall(ctx context.Context, option string) (SetupJob, error) {
	var r SetupJob
	err := c.Post(ctx, "/api/v1/setup/ollama/install", map[string]string{"option": option}, &r)
	return r, err
}

// SetupOllamaStart launches Ollama and waits for it; it returns whether it runs.
func (c *Client) SetupOllamaStart(ctx context.Context) (bool, error) {
	var r struct {
		Running bool `json:"running"`
	}
	err := c.Post(ctx, "/api/v1/setup/ollama/start", map[string]any{}, &r)
	return r.Running, err
}

// SetupOllamaPull starts downloading a model.
func (c *Client) SetupOllamaPull(ctx context.Context, model string) (SetupJob, error) {
	var r SetupJob
	err := c.Post(ctx, "/api/v1/setup/ollama/pull", map[string]string{"model": model}, &r)
	return r, err
}

// SetupJob fetches a job.
func (c *Client) SetupJob(ctx context.Context, id string) (SetupJob, error) {
	var r SetupJob
	err := c.Get(ctx, "/api/v1/setup/jobs/"+url.PathEscape(id), &r)
	return r, err
}

// SetupRemembrances enables Remembrances with local Ollama models.
func (c *Client) SetupRemembrances(ctx context.Context, doc, code string) error {
	return c.Post(ctx, "/api/v1/setup/remembrances", map[string]string{"documentModel": doc, "codeModel": code}, nil)
}
