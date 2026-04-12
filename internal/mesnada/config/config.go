// Package mesnadaconfig handles Mesnada application configuration.
package mesnadaconfig

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
)

//go:embed config.example.yaml
var defaultConfigTemplate string

// ModelConfig defines a model with its description.
type ModelConfig struct {
	ID          string `json:"id" yaml:"id"`
	Description string `json:"description" yaml:"description"`
}

// EngineConfig holds engine-specific configuration.
type EngineConfig struct {
	DefaultModel string        `json:"default_model" yaml:"default_model"`
	Models       []ModelConfig `json:"models" yaml:"models"`
}

// Config holds the application configuration.
type Config struct {
	DefaultModel string                  `json:"default_model" yaml:"default_model"`
	Models       []ModelConfig           `json:"models" yaml:"models"`
	Engines      map[string]EngineConfig `json:"engines,omitempty" yaml:"engines,omitempty"`
	ACP          ACPConfig               `json:"acp,omitempty" yaml:"acp,omitempty"`
	TUI          TUIConfig               `json:"tui,omitempty" yaml:"tui,omitempty"`
	Server       ServerConfig            `json:"server" yaml:"server"`
	Orchestrator OrchestratorConfig      `json:"orchestrator" yaml:"orchestrator"`
}

// TUIConfig holds TUI/WebUI integration settings.
type TUIConfig struct {
	Enabled                     bool `json:"enabled" yaml:"enabled"`
	WebUI                       bool `json:"webui" yaml:"webui"`
	AutoDetectTerminal          bool `json:"auto_detect_terminal" yaml:"auto_detect_terminal"`
	ConfirmQuitWithRunningTasks bool `json:"confirm_quit_with_running_tasks" yaml:"confirm_quit_with_running_tasks"`
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	Host string `json:"host" yaml:"host"`
	Port int    `json:"port" yaml:"port"`
}

// OrchestratorConfig holds orchestrator configuration.
type OrchestratorConfig struct {
	StorePath        string `json:"store_path" yaml:"store_path"`
	LogDir           string `json:"log_dir" yaml:"log_dir"`
	MaxParallel      int    `json:"max_parallel" yaml:"max_parallel"`
	DefaultMCPConfig string `json:"default_mcp_config" yaml:"default_mcp_config"`
	DefaultEngine    string `json:"default_engine" yaml:"default_engine"`
	PersonaPath      string `json:"persona_path,omitempty" yaml:"persona_path,omitempty"`
}

// ACPCapabilities defines what an ACP agent can do.
type ACPCapabilities struct {
	Terminals   bool `json:"terminals" yaml:"terminals"`
	FileAccess  bool `json:"file_access" yaml:"file_access"`
	Permissions bool `json:"permissions" yaml:"permissions"`
}

// MCPServerRef references an MCP server for ACP agents.
type MCPServerRef struct {
	Name    string            `json:"name" yaml:"name"`
	Command string            `json:"command" yaml:"command"`
	Args    []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

// ACPAgentConfig configuration for a specific ACP agent.
type ACPAgentConfig struct {
	Name         string            `json:"name" yaml:"name"`
	Title        string            `json:"title" yaml:"title"`
	Command      string            `json:"command" yaml:"command"`
	Args         []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env          map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	MCPServers   []MCPServerRef    `json:"mcp_servers,omitempty" yaml:"mcp_servers,omitempty"`
	Capabilities ACPCapabilities   `json:"capabilities" yaml:"capabilities"`
}

// ACPServerConfig holds configuration for the ACP server.
type ACPServerConfig struct {
	Enabled        bool     `json:"enabled" yaml:"enabled"`
	Transports     []string `json:"transports" yaml:"transports"` // ["stdio", "http"]
	Host           string   `json:"host" yaml:"host"`
	Port           int      `json:"port" yaml:"port"`
	MaxSessions    int      `json:"max_sessions" yaml:"max_sessions"`
	SessionTimeout string   `json:"session_timeout" yaml:"session_timeout"`
	RequireAuth    bool     `json:"require_auth" yaml:"require_auth"`
}

// ACPConfig global ACP configuration.
type ACPConfig struct {
	Enabled        bool                      `json:"enabled" yaml:"enabled"`
	DefaultAgent   string                    `json:"default_agent,omitempty" yaml:"default_agent,omitempty"`
	DefaultMode    string                    `json:"default_mode,omitempty" yaml:"default_mode,omitempty"`
	Agents         map[string]ACPAgentConfig `json:"agents,omitempty" yaml:"agents,omitempty"`
	AutoPermission bool                      `json:"auto_permission" yaml:"auto_permission"`
	Server         ACPServerConfig           `json:"server,omitempty" yaml:"server,omitempty"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	mesnadaDir := filepath.Join(home, ".mesnada")

	return &Config{
		DefaultModel: "anthropic.claude-sonnet-4-5-20250929",
		Models: []ModelConfig{
			{ID: "anthropic.claude-sonnet-4-5-20250929", Description: "Balanced performance and speed for general tasks"},
			{ID: "anthropic.claude-opus-4-5-20251101", Description: "Highest capability for complex reasoning and analysis"},
			{ID: "anthropic.claude-haiku-4-5-20251001", Description: "Fast responses for simple tasks and quick iterations"},
			{ID: "gpt-5.1-codex-max", Description: "Advanced coding capabilities with extended context"},
			{ID: "gpt-5.1-codex", Description: "Optimized for code generation and refactoring"},
			{ID: "gpt-5.2", Description: "Latest GPT model with improved reasoning"},
			{ID: "gpt-5.1", Description: "Stable GPT model for production use"},
			{ID: "gpt-5", Description: "Base GPT-5 model"},
			{ID: "gpt-5.1-codex-mini", Description: "Lightweight coding model for quick tasks"},
			{ID: "gpt-5-mini", Description: "Fast and efficient for simple queries"},
			{ID: "gpt-4.1", Description: "Reliable GPT-4 variant"},
			{ID: "gemini-3-pro-preview", Description: "Google's latest multimodal model"},
		},
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8765,
		},
		TUI: TUIConfig{
			Enabled:                     true,
			WebUI:                       true,
			AutoDetectTerminal:          true,
			ConfirmQuitWithRunningTasks: true,
		},
		Orchestrator: OrchestratorConfig{
			StorePath:   filepath.Join(mesnadaDir, "tasks.json"),
			LogDir:      filepath.Join(mesnadaDir, "logs"),
			MaxParallel: 5,
		},
	}
}

// Load loads configuration from a file (supports JSON and YAML).
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	baseDir := ""

	if path == "" {
		home, _ := os.UserHomeDir()
		// Try YAML first, then JSON
		yamlPath := filepath.Join(home, ".mesnada", "config.yaml")
		jsonPath := filepath.Join(home, ".mesnada", "config.json")

		if _, err := os.Stat(yamlPath); err == nil {
			path = yamlPath
			baseDir = filepath.Dir(path)
		} else if _, err := os.Stat(jsonPath); err == nil {
			path = jsonPath
			baseDir = filepath.Dir(path)
		} else {
			// No config file found, return defaults
			return cfg, nil
		}
	} else {
		baseDir = filepath.Dir(path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Detect format by extension
	isYAML := strings.HasSuffix(strings.ToLower(path), ".yaml") || strings.HasSuffix(strings.ToLower(path), ".yml")

	if isYAML {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	}

	// Expand/resolve paths from config file
	// - StorePath/LogDir/PersonaPath: expand ~ and resolve relative paths relative to the config file directory
	// - DefaultMCPConfig: expand ~ (supports both "~/..." and "@~/...") but keep relative paths as-is
	cfg.Orchestrator.StorePath = resolvePath(cfg.Orchestrator.StorePath, baseDir)
	cfg.Orchestrator.LogDir = resolvePath(cfg.Orchestrator.LogDir, baseDir)
	cfg.Orchestrator.DefaultMCPConfig = expandMCPConfig(cfg.Orchestrator.DefaultMCPConfig)
	if cfg.Orchestrator.PersonaPath != "" {
		cfg.Orchestrator.PersonaPath = resolvePath(cfg.Orchestrator.PersonaPath, baseDir)
	}

	return cfg, nil
}

// Save saves configuration to a file.
func (c *Config) Save(path string) error {
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".mesnada", "config.json")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Detect format by extension
	isYAML := strings.HasSuffix(strings.ToLower(path), ".yaml") || strings.HasSuffix(strings.ToLower(path), ".yml")

	var data []byte
	var err error

	if isYAML {
		data, err = yaml.Marshal(c)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML config: %w", err)
		}
	} else {
		data, err = json.MarshalIndent(c, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON config: %w", err)
		}
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// InitConfig creates a new configuration file using the embedded template.
// If path is empty, it uses the default path (~/.mesnada/config.yaml).
func InitConfig(path string) error {
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".mesnada", "config.yaml")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(defaultConfigTemplate), 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// Address returns the server address.
func (c *Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// GetModelByID returns a model configuration by ID.
func (c *Config) GetModelByID(id string) *ModelConfig {
	for _, m := range c.Models {
		if m.ID == id {
			return &m
		}
	}
	return nil
}

// ValidateModel checks if a model ID is valid.
func (c *Config) ValidateModel(id string) bool {
	return c.GetModelByID(id) != nil
}

// GetModelsForEngine returns the list of available models for a specific engine.
// If engine-specific models are configured, returns those; otherwise returns the global models list.
func (c *Config) GetModelsForEngine(engine string) []ModelConfig {
	if c.Engines != nil {
		if engineConfig, ok := c.Engines[engine]; ok && len(engineConfig.Models) > 0 {
			return engineConfig.Models
		}
	}
	// Fallback to global models for backward compatibility
	return c.Models
}

// GetModelForEngine returns a specific model config for an engine.
func (c *Config) GetModelForEngine(engine, modelID string) *ModelConfig {
	models := c.GetModelsForEngine(engine)
	for _, m := range models {
		if m.ID == modelID {
			return &m
		}
	}
	return nil
}

// ValidateModelForEngine checks if a model ID is valid for a specific engine.
func (c *Config) ValidateModelForEngine(engine, modelID string) bool {
	return c.GetModelForEngine(engine, modelID) != nil
}

// GetDefaultModelForEngine returns the default model for an engine.
func (c *Config) GetDefaultModelForEngine(engine string) string {
	if c.Engines != nil {
		if engineConfig, ok := c.Engines[engine]; ok && engineConfig.DefaultModel != "" {
			return engineConfig.DefaultModel
		}
	}
	// Fallback to global default
	return c.DefaultModel
}

// GetModelIDsForEngine returns a list of model IDs for an engine.
func (c *Config) GetModelIDsForEngine(engine string) []string {
	models := c.GetModelsForEngine(engine)
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	return ids
}

// expandHome expands ~ to home directory in paths.
func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	// Support "~/..." (and Windows separators just in case)
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		home, _ := os.UserHomeDir()
		rest := path[2:]
		return filepath.Join(home, rest)
	}
	// We intentionally don't expand "~user/..." forms.
	return path
}

// resolvePath expands ~ and resolves relative paths against baseDir.
// If baseDir is empty, relative paths are returned unchanged.
func resolvePath(value, baseDir string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	p := expandHome(value)
	if filepath.IsAbs(p) {
		return p
	}
	if baseDir == "" {
		return p
	}
	return filepath.Clean(filepath.Join(baseDir, p))
}

// expandMCPConfig expands ~ in MCP config values.
// It supports both "~/..." and "@~/..." forms.
func expandMCPConfig(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if strings.HasPrefix(value, "@") {
		return "@" + expandHome(value[1:])
	}
	return expandHome(value)
}

// Validate validates the configuration and returns any errors.
func (c *Config) Validate() error {
	// Validate ACP configuration if enabled
	if c.ACP.Enabled {
		if err := c.validateACP(); err != nil {
			return fmt.Errorf("ACP configuration error: %w", err)
		}
	}
	return nil
}

// validateACP validates the ACP configuration.
func (c *Config) validateACP() error {
	// Check if default_agent exists in agents map
	if c.ACP.DefaultAgent != "" {
		if c.ACP.Agents == nil || len(c.ACP.Agents) == 0 {
			return fmt.Errorf("default_agent '%s' specified but no agents defined", c.ACP.DefaultAgent)
		}
		if _, exists := c.ACP.Agents[c.ACP.DefaultAgent]; !exists {
			return fmt.Errorf("default_agent '%s' not found in agents list", c.ACP.DefaultAgent)
		}
	}

	// Validate default_mode if specified
	if c.ACP.DefaultMode != "" {
		validModes := map[string]bool{"code": true, "ask": true, "architect": true}
		if !validModes[c.ACP.DefaultMode] {
			return fmt.Errorf("invalid default_mode '%s', must be one of: code, ask, architect", c.ACP.DefaultMode)
		}
	}

	// Validate each agent configuration
	for name, agent := range c.ACP.Agents {
		if agent.Name == "" {
			return fmt.Errorf("agent '%s': name is required", name)
		}
		if agent.Command == "" {
			return fmt.Errorf("agent '%s': command is required", name)
		}
	}

	return nil
}
