// Package config manages application configuration from various sources.
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/spf13/viper"
)

// MCPType defines the type of MCP (Model Control Protocol) server.
type MCPType string

// Supported MCP types
const (
	MCPStdio          MCPType = "stdio"
	MCPSse            MCPType = "sse"
	MCPStreamableHTTP MCPType = "streamable-http"
)

// MCPServer defines the configuration for a Model Control Protocol server.
type MCPServer struct {
	Command string            `json:"command"`
	Env     []string          `json:"env"`
	Args    []string          `json:"args"`
	Type    MCPType           `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type AgentName string

const (
	AgentCoder      AgentName = "coder"
	AgentSummarizer AgentName = "summarizer"
	AgentTask       AgentName = "task"
	AgentTitle      AgentName = "title"
)

// Agent defines configuration for different LLM models and their token limits.
type Agent struct {
	Model           models.ModelID `json:"model"`
	MaxTokens       int64          `json:"maxTokens"`
	ReasoningEffort string         `json:"reasoningEffort"` // For openai models low,medium,heigh
}

// Provider defines configuration for an LLM provider.
type Provider struct {
	APIKey   string `json:"apiKey"`
	BaseURL  string `json:"baseURL,omitempty"`
	Disabled bool   `json:"disabled"`
}

// Data defines storage configuration.
type Data struct {
	Directory string `json:"directory,omitempty"`
}

// LSPConfig defines configuration for Language Server Protocol integration.
type LSPConfig struct {
	Disabled bool     `json:"enabled"`
	Command  string   `json:"command"`
	Args     []string `json:"args"`
	Options  any      `json:"options"`
}

// TUIConfig defines the configuration for the Terminal User Interface.
type TUIConfig struct {
	Theme string `json:"theme,omitempty"`
}

// MesnadaServerConfig holds mesnada HTTP server configuration
type MesnadaServerConfig struct {
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
}

// MesnadaOrchestratorConfig holds orchestrator settings
type MesnadaOrchestratorConfig struct {
	StorePath        string `json:"storePath,omitempty"`
	LogDir           string `json:"logDir,omitempty"`
	MaxParallel      int    `json:"maxParallel,omitempty"`
	DefaultEngine    string `json:"defaultEngine,omitempty"`
	DefaultModel     string `json:"defaultModel,omitempty"`
	DefaultMCPConfig string `json:"defaultMcpConfig,omitempty"`
	PersonaPath      string `json:"personaPath,omitempty"`
}

// MesnadaACPServerConfig holds configuration for the ACP server.
type MesnadaACPServerConfig struct {
	Enabled        bool     `json:"enabled,omitempty"`
	Transports     []string `json:"transports,omitempty"` // ["stdio", "http"]
	Host           string   `json:"host,omitempty"`
	Port           int      `json:"port,omitempty"`
	MaxSessions    int      `json:"maxSessions,omitempty"`
	SessionTimeout string   `json:"sessionTimeout,omitempty"`
	RequireAuth    bool     `json:"requireAuth,omitempty"`
}

// MesnadaACPConfig holds ACP agent configuration
type MesnadaACPConfig struct {
	Enabled        bool                    `json:"enabled,omitempty"`
	DefaultAgent   string                  `json:"defaultAgent,omitempty"`
	AutoPermission bool                    `json:"autoPermission,omitempty"`
	Server         MesnadaACPServerConfig  `json:"server,omitempty"`
}

// MesnadaTUIConfig holds mesnada TUI settings
type MesnadaTUIConfig struct {
	Enabled bool `json:"enabled,omitempty"`
	WebUI   bool `json:"webui,omitempty"`
}

// MesnadaConfig holds all mesnada integration configuration
type MesnadaConfig struct {
	Enabled      bool                      `json:"enabled,omitempty"`
	Server       MesnadaServerConfig       `json:"server,omitempty"`
	Orchestrator MesnadaOrchestratorConfig `json:"orchestrator,omitempty"`
	ACP          MesnadaACPConfig          `json:"acp,omitempty"`
	TUI          MesnadaTUIConfig          `json:"tui,omitempty"`
}

// ShellConfig defines the configuration for the shell used by the bash tool.
type ShellConfig struct {
	Path string   `json:"path,omitempty"`
	Args []string `json:"args,omitempty"`
}

// SkillsConfig defines configuration for skill discovery and prompt injection.
type SkillsConfig struct {
	Enabled bool     `json:"enabled,omitempty"`
	Paths   []string `json:"paths,omitempty"`
}

// Config is the main configuration structure for the application.
type Config struct {
	Data         Data                              `json:"data"`
	WorkingDir   string                            `json:"wd,omitempty"`
	MCPServers   map[string]MCPServer              `json:"mcpServers,omitempty"`
	Providers    map[models.ModelProvider]Provider `json:"providers,omitempty"`
	LSP          map[string]LSPConfig              `json:"lsp,omitempty"`
	Agents       map[AgentName]Agent               `json:"agents,omitempty"`
	Debug        bool                              `json:"debug,omitempty"`
	LogFile      string                            `json:"logFile,omitempty"`
	DebugLSP     bool                              `json:"debugLSP,omitempty"`
	ContextPaths []string                          `json:"contextPaths,omitempty"`
	Skills       SkillsConfig                      `json:"skills,omitempty"`
	TUI          TUIConfig                         `json:"tui"`
	Mesnada      MesnadaConfig                     `json:"mesnada,omitempty"`
	Shell        ShellConfig                       `json:"shell,omitempty"`
	AutoCompact  bool                              `json:"autoCompact,omitempty"`
}

// Application constants
const (
	defaultDataDirectory = ".pando"
	defaultLogLevel      = "info"
	appName              = "pando"

	MaxTokensFallbackDefault = 4096
)

var defaultContextPaths = []string{
	".github/copilot-instructions.md",
	".cursorrules",
	".cursor/rules/",
	"CLAUDE.md",
	"CLAUDE.local.md",
	"pando.md",
	"pando.local.md",
	"Pando.md",
	"Pando.local.md",
	"PANDO.md",
	"PANDO.local.md",
}

// Global configuration instance
var cfg *Config

// Load initializes the configuration from environment variables and config files.
// If debug is true, debug mode is enabled and log level is set to debug.
// If logFile is provided, all logs are written to the specified file.
// It returns an error if configuration loading fails.
func Load(workingDir string, debug bool, logFile ...string) (*Config, error) {
	if cfg != nil {
		return cfg, nil
	}

	cfg = &Config{
		WorkingDir: workingDir,
		MCPServers: make(map[string]MCPServer),
		Providers:  make(map[models.ModelProvider]Provider),
		LSP:        make(map[string]LSPConfig),
	}

	configureViper()
	setDefaults(debug)

	// Read global config
	if err := readConfig(viper.ReadInConfig()); err != nil {
		return cfg, err
	}

	// Load and merge local config
	mergeLocalConfig(workingDir)

	setProviderDefaults()

	// Apply configuration to the struct
	if err := viper.Unmarshal(cfg); err != nil {
		return cfg, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	applyDefaultValues()

	// Apply logFile from CLI argument if provided
	if len(logFile) > 0 && logFile[0] != "" {
		cfg.LogFile = logFile[0]
		// If log file is specified, enable debug mode automatically
		cfg.Debug = true
	}

	defaultLevel := slog.LevelInfo
	if cfg.Debug {
		defaultLevel = slog.LevelDebug
	}

	if cfg.LogFile != "" {
		// Log to the specified file
		loggingFile := cfg.LogFile
		loggingDir := filepath.Dir(loggingFile)
		messagesPath := filepath.Join(loggingDir, "messages")

		// Create parent directory if needed
		if err := os.MkdirAll(loggingDir, 0o755); err != nil {
			return cfg, fmt.Errorf("failed to create log directory: %w", err)
		}

		if _, err := os.Stat(messagesPath); os.IsNotExist(err) {
			if err := os.MkdirAll(messagesPath, 0o756); err != nil {
				return cfg, fmt.Errorf("failed to create messages directory: %w", err)
			}
		}
		logging.MessageDir = messagesPath

		sloggingFileWriter, err := os.OpenFile(loggingFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
		if err != nil {
			return cfg, fmt.Errorf("failed to open log file: %w", err)
		}
		logger := slog.New(slog.NewTextHandler(sloggingFileWriter, &slog.HandlerOptions{
			Level: defaultLevel,
		}))
		slog.SetDefault(logger)
	} else if os.Getenv("PANDO_DEV_DEBUG") == "true" {
		loggingFile := fmt.Sprintf("%s/%s", cfg.Data.Directory, "debug.log")
		messagesPath := fmt.Sprintf("%s/%s", cfg.Data.Directory, "messages")

		// if file does not exist create it
		if _, err := os.Stat(loggingFile); os.IsNotExist(err) {
			if err := os.MkdirAll(cfg.Data.Directory, 0o755); err != nil {
				return cfg, fmt.Errorf("failed to create directory: %w", err)
			}
			if _, err := os.Create(loggingFile); err != nil {
				return cfg, fmt.Errorf("failed to create log file: %w", err)
			}
		}

		if _, err := os.Stat(messagesPath); os.IsNotExist(err) {
			if err := os.MkdirAll(messagesPath, 0o756); err != nil {
				return cfg, fmt.Errorf("failed to create directory: %w", err)
			}
		}
		logging.MessageDir = messagesPath

		sloggingFileWriter, err := os.OpenFile(loggingFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
		if err != nil {
			return cfg, fmt.Errorf("failed to open log file: %w", err)
		}
		logger := slog.New(slog.NewTextHandler(sloggingFileWriter, &slog.HandlerOptions{
			Level: defaultLevel,
		}))
		slog.SetDefault(logger)
	} else {
		// Configure logger
		logger := slog.New(slog.NewTextHandler(logging.NewWriter(), &slog.HandlerOptions{
			Level: defaultLevel,
		}))
		slog.SetDefault(logger)
	}

	// Validate configuration
	if err := Validate(); err != nil {
		return cfg, fmt.Errorf("config validation failed: %w", err)
	}

	if cfg.Agents == nil {
		cfg.Agents = make(map[AgentName]Agent)
	}

	// Override the max tokens for title agent
	cfg.Agents[AgentTitle] = Agent{
		Model:     cfg.Agents[AgentTitle].Model,
		MaxTokens: 80,
	}
	return cfg, nil
}

// configureViper sets up viper's configuration paths and environment variables.
// By not setting SetConfigType, Viper auto-detects the format from the file
// extension, supporting both .json and .toml (and other formats).
func configureViper() {
	viper.SetConfigName(fmt.Sprintf(".%s", appName))
	viper.AddConfigPath("$HOME")
	viper.AddConfigPath(fmt.Sprintf("$XDG_CONFIG_HOME/%s", appName))
	viper.AddConfigPath(fmt.Sprintf("$HOME/.config/%s", appName))
	viper.SetEnvPrefix(strings.ToUpper(appName))
	viper.AutomaticEnv()
}

// setDefaults configures default values for configuration options.
func setDefaults(debug bool) {
	viper.SetDefault("data.directory", defaultDataDirectory)
	viper.SetDefault("contextPaths", defaultContextPaths)
	viper.SetDefault("skills.enabled", true)
	viper.SetDefault("tui.theme", "pando")
	viper.SetDefault("mesnada.enabled", false)
	viper.SetDefault("mesnada.server.host", "127.0.0.1")
	viper.SetDefault("mesnada.server.port", 9767)
	viper.SetDefault("mesnada.orchestrator.maxParallel", 5)
	viper.SetDefault("mesnada.orchestrator.defaultEngine", "copilot")
	viper.SetDefault("mesnada.orchestrator.defaultModel", "gpt-5.4")
	viper.SetDefault("mesnada.tui.enabled", true)
	viper.SetDefault("mesnada.tui.webui", true)
	viper.SetDefault("autoCompact", true)

	// Set default shell from environment or fallback to /bin/bash
	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		shellPath = "/bin/bash"
	}
	viper.SetDefault("shell.path", shellPath)
	viper.SetDefault("shell.args", []string{"-l"})

	if debug {
		viper.SetDefault("debug", true)
		viper.Set("log.level", "debug")
	} else {
		viper.SetDefault("debug", false)
		viper.SetDefault("log.level", defaultLogLevel)
	}
}

// setProviderDefaults configures LLM provider defaults based on provider provided by
// environment variables and configuration file.
func setProviderDefaults() {
	// Set all API keys we can find in the environment
	// Note: Viper does not default if the json apiKey is ""
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.anthropic.apiKey", apiKey)
	}
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.openai.apiKey", apiKey)
	}
	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.gemini.apiKey", apiKey)
	}
	if apiKey := os.Getenv("GROQ_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.groq.apiKey", apiKey)
	}
	if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.openrouter.apiKey", apiKey)
	}
	if apiKey := os.Getenv("XAI_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.xai.apiKey", apiKey)
	}
	if apiKey := os.Getenv("AZURE_OPENAI_ENDPOINT"); apiKey != "" {
		// api-key may be empty when using Entra ID credentials – that's okay
		viper.SetDefault("providers.azure.apiKey", os.Getenv("AZURE_OPENAI_API_KEY"))
	}
	if baseURL := strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL")); baseURL != "" {
		viper.SetDefault("providers.ollama.baseURL", models.ResolveOllamaBaseURL(baseURL))
	}
	if apiKey := strings.TrimSpace(os.Getenv("OLLAMA_API_KEY")); apiKey != "" {
		viper.SetDefault("providers.ollama.apiKey", apiKey)
	}
	if hasCopilotCredentials() {
		viper.SetDefault("providers.copilot.disabled", false)
	}

	// Use this order to set the default models
	// 1. Copilot
	// 2. Anthropic
	// 3. OpenAI
	// 4. Google Gemini
	// 5. Groq
	// 6. OpenRouter
	// 7. AWS Bedrock
	// 8. Azure
	// 9. Google Cloud VertexAI

	// copilot configuration
	if key := viper.GetString("providers.copilot.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.CopilotGPT4o)
		viper.SetDefault("agents.summarizer.model", models.CopilotGPT4o)
		viper.SetDefault("agents.task.model", models.CopilotGPT4o)
		viper.SetDefault("agents.title.model", models.CopilotGPT4o)
		return
	}

	// Anthropic configuration
	if key := viper.GetString("providers.anthropic.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.Claude4Sonnet)
		viper.SetDefault("agents.summarizer.model", models.Claude4Sonnet)
		viper.SetDefault("agents.task.model", models.Claude4Sonnet)
		viper.SetDefault("agents.title.model", models.Claude4Sonnet)
		return
	}

	// OpenAI configuration
	if key := viper.GetString("providers.openai.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.GPT41)
		viper.SetDefault("agents.summarizer.model", models.GPT41)
		viper.SetDefault("agents.task.model", models.GPT41Mini)
		viper.SetDefault("agents.title.model", models.GPT41Mini)
		return
	}

	// Google Gemini configuration
	if key := viper.GetString("providers.gemini.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.Gemini25)
		viper.SetDefault("agents.summarizer.model", models.Gemini25)
		viper.SetDefault("agents.task.model", models.Gemini25Flash)
		viper.SetDefault("agents.title.model", models.Gemini25Flash)
		return
	}

	// Groq configuration
	if key := viper.GetString("providers.groq.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.QWENQwq)
		viper.SetDefault("agents.summarizer.model", models.QWENQwq)
		viper.SetDefault("agents.task.model", models.QWENQwq)
		viper.SetDefault("agents.title.model", models.QWENQwq)
		return
	}

	// OpenRouter configuration
	if key := viper.GetString("providers.openrouter.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.OpenRouterClaude37Sonnet)
		viper.SetDefault("agents.summarizer.model", models.OpenRouterClaude37Sonnet)
		viper.SetDefault("agents.task.model", models.OpenRouterClaude37Sonnet)
		viper.SetDefault("agents.title.model", models.OpenRouterClaude35Haiku)
		return
	}

	// XAI configuration
	if key := viper.GetString("providers.xai.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.XAIGrok3Beta)
		viper.SetDefault("agents.summarizer.model", models.XAIGrok3Beta)
		viper.SetDefault("agents.task.model", models.XAIGrok3Beta)
		viper.SetDefault("agents.title.model", models.XAiGrok3MiniFastBeta)
		return
	}

	// AWS Bedrock configuration
	if hasAWSCredentials() {
		viper.SetDefault("agents.coder.model", models.BedrockClaude37Sonnet)
		viper.SetDefault("agents.summarizer.model", models.BedrockClaude37Sonnet)
		viper.SetDefault("agents.task.model", models.BedrockClaude37Sonnet)
		viper.SetDefault("agents.title.model", models.BedrockClaude37Sonnet)
		return
	}

	// Azure OpenAI configuration
	if os.Getenv("AZURE_OPENAI_ENDPOINT") != "" {
		viper.SetDefault("agents.coder.model", models.AzureGPT41)
		viper.SetDefault("agents.summarizer.model", models.AzureGPT41)
		viper.SetDefault("agents.task.model", models.AzureGPT41Mini)
		viper.SetDefault("agents.title.model", models.AzureGPT41Mini)
		return
	}

	// Google Cloud VertexAI configuration
	if hasVertexAICredentials() {
		viper.SetDefault("agents.coder.model", models.VertexAIGemini25)
		viper.SetDefault("agents.summarizer.model", models.VertexAIGemini25)
		viper.SetDefault("agents.task.model", models.VertexAIGemini25Flash)
		viper.SetDefault("agents.title.model", models.VertexAIGemini25Flash)
		return
	}
}

// hasAWSCredentials checks if AWS credentials are available in the environment.
func hasAWSCredentials() bool {
	// Check for explicit AWS credentials
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		return true
	}

	// Check for AWS profile
	if os.Getenv("AWS_PROFILE") != "" || os.Getenv("AWS_DEFAULT_PROFILE") != "" {
		return true
	}

	// Check for AWS region
	if os.Getenv("AWS_REGION") != "" || os.Getenv("AWS_DEFAULT_REGION") != "" {
		return true
	}

	// Check if running on EC2 with instance profile
	if os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI") != "" ||
		os.Getenv("AWS_CONTAINER_CREDENTIALS_FULL_URI") != "" {
		return true
	}

	return false
}

// hasVertexAICredentials checks if VertexAI credentials are available in the environment.
func hasVertexAICredentials() bool {
	// Check for explicit VertexAI parameters
	if os.Getenv("VERTEXAI_PROJECT") != "" && os.Getenv("VERTEXAI_LOCATION") != "" {
		return true
	}
	// Check for Google Cloud project and location
	if os.Getenv("GOOGLE_CLOUD_PROJECT") != "" && (os.Getenv("GOOGLE_CLOUD_REGION") != "" || os.Getenv("GOOGLE_CLOUD_LOCATION") != "") {
		return true
	}
	return false
}

func hasCopilotCredentials() bool {
	// Check for explicit Copilot parameters
	if token, _ := auth.LoadGitHubOAuthToken(); token != "" {
		return true
	}
	return false
}

// readConfig handles the result of reading a configuration file.
func readConfig(err error) error {
	if err == nil {
		return nil
	}

	// It's okay if the config file doesn't exist
	if _, ok := err.(viper.ConfigFileNotFoundError); ok {
		return nil
	}

	return fmt.Errorf("failed to read config: %w", err)
}

// mergeLocalConfig loads and merges configuration from the local directory.
// Supports both JSON and TOML formats via Viper auto-detection.
func mergeLocalConfig(workingDir string) {
	local := viper.New()
	local.SetConfigName(fmt.Sprintf(".%s", appName))
	local.AddConfigPath(workingDir)

	// Merge local config if it exists
	if err := local.ReadInConfig(); err == nil {
		viper.MergeConfigMap(local.AllSettings())
	}
}

// applyDefaultValues sets default values for configuration fields that need processing.
func applyDefaultValues() {
	// Set default MCP type if not specified
	for k, v := range cfg.MCPServers {
		if v.Type == "" {
			v.Type = MCPStdio
			cfg.MCPServers[k] = v
		}
	}

	refreshConfiguredDynamicModels()
	ensureAgentDefaults()
}

// It validates model IDs and providers, ensuring they are supported.
func validateAgent(cfg *Config, name AgentName, agent Agent) error {
	// Check if model exists
	// TODO:	If a copilot model is specified, but model is not found,
	// 		 	it might be new model. The https://api.githubcopilot.com/models
	// 		 	endpoint should be queried to validate if the model is supported.
	model, modelExists := models.SupportedModels[agent.Model]
	if !modelExists {
		logging.Warn("unsupported model configured, reverting to default",
			"agent", name,
			"configured_model", agent.Model)

		// Set default model based on available providers
		if setDefaultModelForAgent(name) {
			logging.Info("set default model for agent", "agent", name, "model", cfg.Agents[name].Model)
		} else {
			logging.Warn("no valid provider available for agent, model selection required",
				"agent", name)
		}
		return nil
	}

	// Check if provider for the model is configured
	provider := model.Provider
	providerCfg, providerExists := cfg.Providers[provider]

	if !providerExists {
		if provider == models.ProviderCopilot && hasCopilotCredentials() {
			cfg.Providers[provider] = Provider{}
			logging.Info("added Copilot provider from saved login session")
		} else {
			// Provider not configured, check if we have environment variables
			apiKey := getProviderAPIKey(provider)
			if apiKey == "" {
				logging.Warn("provider not configured for model, reverting to default",
					"agent", name,
					"model", agent.Model,
					"provider", provider)

				// Set default model based on available providers
				if setDefaultModelForAgent(name) {
					logging.Info("set default model for agent", "agent", name, "model", cfg.Agents[name].Model)
				} else {
					logging.Warn("no valid provider available for agent, model selection required",
						"agent", name)
				}
			} else {
				// Add provider with API key from environment
				cfg.Providers[provider] = Provider{
					APIKey: apiKey,
				}
				logging.Info("added provider from environment", "provider", provider)
			}
		}
	} else if providerCfg.Disabled || (providerRequiresAPIKey(provider) && providerCfg.APIKey == "" && !providerCfg.Disabled) || (provider == models.ProviderCopilot && providerCfg.APIKey == "" && !hasCopilotCredentials()) {
		// Provider is disabled or has no API key
		logging.Warn("provider is disabled or has no API key, reverting to default",
			"agent", name,
			"model", agent.Model,
			"provider", provider)

		// Set default model based on available providers
		if setDefaultModelForAgent(name) {
			logging.Info("set default model for agent", "agent", name, "model", cfg.Agents[name].Model)
		} else {
			return fmt.Errorf("no valid provider available for agent %s", name)
		}
	}

	// Validate max tokens
	if agent.MaxTokens <= 0 {
		logging.Warn("invalid max tokens, setting to default",
			"agent", name,
			"model", agent.Model,
			"max_tokens", agent.MaxTokens)

		// Update the agent with default max tokens
		updatedAgent := cfg.Agents[name]
		if model.DefaultMaxTokens > 0 {
			updatedAgent.MaxTokens = model.DefaultMaxTokens
		} else {
			updatedAgent.MaxTokens = MaxTokensFallbackDefault
		}
		cfg.Agents[name] = updatedAgent
	} else if model.ContextWindow > 0 && agent.MaxTokens > model.ContextWindow/2 {
		// Ensure max tokens doesn't exceed half the context window (reasonable limit)
		logging.Warn("max tokens exceeds half the context window, adjusting",
			"agent", name,
			"model", agent.Model,
			"max_tokens", agent.MaxTokens,
			"context_window", model.ContextWindow)

		// Update the agent with adjusted max tokens
		updatedAgent := cfg.Agents[name]
		updatedAgent.MaxTokens = model.ContextWindow / 2
		cfg.Agents[name] = updatedAgent
	}

	// Validate reasoning effort for models that support reasoning
	if model.CanReason && provider == models.ProviderOpenAI || provider == models.ProviderLocal {
		if agent.ReasoningEffort == "" {
			// Set default reasoning effort for models that support it
			logging.Info("setting default reasoning effort for model that supports reasoning",
				"agent", name,
				"model", agent.Model)

			// Update the agent with default reasoning effort
			updatedAgent := cfg.Agents[name]
			updatedAgent.ReasoningEffort = "medium"
			cfg.Agents[name] = updatedAgent
		} else {
			// Check if reasoning effort is valid (low, medium, high)
			effort := strings.ToLower(agent.ReasoningEffort)
			if effort != "low" && effort != "medium" && effort != "high" {
				logging.Warn("invalid reasoning effort, setting to medium",
					"agent", name,
					"model", agent.Model,
					"reasoning_effort", agent.ReasoningEffort)

				// Update the agent with valid reasoning effort
				updatedAgent := cfg.Agents[name]
				updatedAgent.ReasoningEffort = "medium"
				cfg.Agents[name] = updatedAgent
			}
		}
	} else if !model.CanReason && agent.ReasoningEffort != "" {
		// Model doesn't support reasoning but reasoning effort is set
		logging.Warn("model doesn't support reasoning but reasoning effort is set, ignoring",
			"agent", name,
			"model", agent.Model,
			"reasoning_effort", agent.ReasoningEffort)

		// Update the agent to remove reasoning effort
		updatedAgent := cfg.Agents[name]
		updatedAgent.ReasoningEffort = ""
		cfg.Agents[name] = updatedAgent
	}

	return nil
}

// Validate checks if the configuration is valid and applies defaults where needed.
func Validate() error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	// Validate agent models
	for name, agent := range cfg.Agents {
		if err := validateAgent(cfg, name, agent); err != nil {
			return err
		}
	}

	// Validate providers
	for provider, providerCfg := range cfg.Providers {
		if provider == models.ProviderCopilot {
			if providerCfg.Disabled {
				continue
			}
			if providerCfg.APIKey == "" && hasCopilotCredentials() {
				continue
			}
		}
		if providerRequiresAPIKey(provider) && providerCfg.APIKey == "" && !providerCfg.Disabled {
			fmt.Printf("provider has no API key, marking as disabled %s", provider)
			logging.Warn("provider has no API key, marking as disabled", "provider", provider)
			providerCfg.Disabled = true
			cfg.Providers[provider] = providerCfg
		}
	}

	// Validate LSP configurations
	for language, lspConfig := range cfg.LSP {
		if lspConfig.Command == "" && !lspConfig.Disabled {
			logging.Warn("LSP configuration has no command, marking as disabled", "language", language)
			lspConfig.Disabled = true
			cfg.LSP[language] = lspConfig
		}
	}

	return nil
}

// getProviderAPIKey gets the API key for a provider from environment variables
func getProviderAPIKey(provider models.ModelProvider) string {
	switch provider {
	case models.ProviderAnthropic:
		return os.Getenv("ANTHROPIC_API_KEY")
	case models.ProviderOpenAI:
		return os.Getenv("OPENAI_API_KEY")
	case models.ProviderGemini:
		return os.Getenv("GEMINI_API_KEY")
	case models.ProviderGROQ:
		return os.Getenv("GROQ_API_KEY")
	case models.ProviderAzure:
		return os.Getenv("AZURE_OPENAI_API_KEY")
	case models.ProviderOpenRouter:
		return os.Getenv("OPENROUTER_API_KEY")
	case models.ProviderOllama:
		return os.Getenv("OLLAMA_API_KEY")
	case models.ProviderBedrock:
		if hasAWSCredentials() {
			return "aws-credentials-available"
		}
	case models.ProviderVertexAI:
		if hasVertexAICredentials() {
			return "vertex-ai-credentials-available"
		}
	}
	return ""
}

// setDefaultModelForAgent sets a default model for an agent based on available providers
func setDefaultModelForAgent(agent AgentName) bool {
	// Only use Copilot if credentials exist AND provider is not disabled
	if hasCopilotCredentials() {
		copilotDisabled := false
		if providerCfg, ok := cfg.Providers[models.ProviderCopilot]; ok && providerCfg.Disabled {
			copilotDisabled = true
		}
		if !copilotDisabled {
			maxTokens := int64(5000)
			if agent == AgentTitle {
				maxTokens = 80
			}

			cfg.Agents[agent] = Agent{
				Model:     models.CopilotGPT4o,
				MaxTokens: maxTokens,
			}
			return true
		}
	}
	// Check providers in order of preference
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		maxTokens := int64(5000)
		if agent == AgentTitle {
			maxTokens = 80
		}
		cfg.Agents[agent] = Agent{
			Model:     models.Claude4Sonnet,
			MaxTokens: maxTokens,
		}
		return true
	}

	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		var model models.ModelID
		maxTokens := int64(5000)
		reasoningEffort := ""

		switch agent {
		case AgentTitle:
			model = models.GPT41Mini
			maxTokens = 80
		case AgentTask:
			model = models.GPT41Mini
		default:
			model = models.GPT41
		}

		// Check if model supports reasoning
		if modelInfo, ok := models.SupportedModels[model]; ok && modelInfo.CanReason {
			reasoningEffort = "medium"
		}

		cfg.Agents[agent] = Agent{
			Model:           model,
			MaxTokens:       maxTokens,
			ReasoningEffort: reasoningEffort,
		}
		return true
	}

	if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
		var model models.ModelID
		maxTokens := int64(5000)

		switch agent {
		case AgentTitle:
			model = models.OpenRouterClaude35Haiku
			maxTokens = 80
		default:
			model = models.OpenRouterClaude37Sonnet
		}

		cfg.Agents[agent] = Agent{
			Model:     model,
			MaxTokens: maxTokens,
		}
		return true
	}

	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		var model models.ModelID
		maxTokens := int64(5000)

		if agent == AgentTitle {
			model = models.Gemini25Flash
			maxTokens = 80
		} else {
			model = models.Gemini25
		}

		cfg.Agents[agent] = Agent{
			Model:     model,
			MaxTokens: maxTokens,
		}
		return true
	}

	if apiKey := os.Getenv("GROQ_API_KEY"); apiKey != "" {
		maxTokens := int64(5000)
		if agent == AgentTitle {
			maxTokens = 80
		}

		cfg.Agents[agent] = Agent{
			Model:     models.QWENQwq,
			MaxTokens: maxTokens,
		}
		return true
	}

	if hasAWSCredentials() {
		maxTokens := int64(5000)
		if agent == AgentTitle {
			maxTokens = 80
		}

		cfg.Agents[agent] = Agent{
			Model:           models.BedrockClaude37Sonnet,
			MaxTokens:       maxTokens,
			ReasoningEffort: "medium", // Claude models support reasoning
		}
		return true
	}

	if hasVertexAICredentials() {
		var model models.ModelID
		maxTokens := int64(5000)

		if agent == AgentTitle {
			model = models.VertexAIGemini25Flash
			maxTokens = 80
		} else {
			model = models.VertexAIGemini25
		}

		cfg.Agents[agent] = Agent{
			Model:     model,
			MaxTokens: maxTokens,
		}
		return true
	}

	if providerCfg, ok := cfg.Providers[models.ProviderOllama]; ok && !providerCfg.Disabled {
		model, ok := firstModelForProvider(models.ProviderOllama)
		if !ok {
			return false
		}
		maxTokens := int64(5000)
		if agent == AgentTitle {
			maxTokens = 80
		}
		cfg.Agents[agent] = Agent{
			Model:     model.ID,
			MaxTokens: maxTokens,
		}
		return true
	}

	return false
}

// configFileFormat returns "toml" if the config file has a .toml extension,
// otherwise defaults to "json".
func configFileFormat(path string) string {
	if strings.HasSuffix(path, ".toml") {
		return "toml"
	}
	return "json"
}

func updateCfgFile(updateCfg func(config *Config)) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	configFile, err := ResolveConfigFilePath()
	if err != nil {
		return err
	}

	var configData []byte
	if configFile == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		configFile = filepath.Join(homeDir, fmt.Sprintf(".%s.json", appName))
	}

	data, err := os.ReadFile(configFile)
	switch {
	case err == nil:
		configData = data
	case os.IsNotExist(err):
		logging.Info("config file not found, creating new one", "path", configFile)
		configData = []byte(`{}`)
	default:
		return fmt.Errorf("failed to read config file: %w", err)
	}

	format := configFileFormat(configFile)

	// Parse the config file based on its format
	var userCfg *Config
	switch format {
	case "toml":
		if err := toml.Unmarshal(configData, &userCfg); err != nil {
			return fmt.Errorf("failed to parse TOML config file: %w", err)
		}
	default:
		if err := json.Unmarshal(configData, &userCfg); err != nil {
			return fmt.Errorf("failed to parse JSON config file: %w", err)
		}
	}

	updateCfg(userCfg)

	// Write the updated config back to file in the same format
	var updatedData []byte
	switch format {
	case "toml":
		updatedData, err = toml.Marshal(userCfg)
		if err != nil {
			return fmt.Errorf("failed to marshal TOML config: %w", err)
		}
	default:
		updatedData, err = json.MarshalIndent(userCfg, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON config: %w", err)
		}
	}

	if err := os.WriteFile(configFile, updatedData, 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ResolveConfigFilePath finds the active config file path.
func ResolveConfigFilePath() (string, error) {
	if configFile := viper.ConfigFileUsed(); configFile != "" {
		return configFile, nil
	}

	if cfg != nil && strings.TrimSpace(cfg.WorkingDir) != "" {
		for _, extension := range []string{"toml", "json"} {
			localConfig := filepath.Join(cfg.WorkingDir, fmt.Sprintf(".%s.%s", appName, extension))
			if _, err := os.Stat(localConfig); err == nil {
				return localConfig, nil
			} else if err != nil && !os.IsNotExist(err) {
				return "", fmt.Errorf("failed to stat config file: %w", err)
			}
		}
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	for _, extension := range []string{"toml", "json"} {
		defaultConfig := filepath.Join(homeDir, fmt.Sprintf(".%s.%s", appName, extension))
		if _, err := os.Stat(defaultConfig); err == nil {
			return defaultConfig, nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to stat config file: %w", err)
		}
	}

	return "", nil
}

// Reload re-reads the config file and updates the global config.
// It resets the global config and reloads from disk.
func Reload() error {
	workingDir := ""
	debug := false
	if cfg != nil {
		workingDir = cfg.WorkingDir
		debug = cfg.Debug
	}

	// Reset global state so Load() runs fresh
	cfg = nil
	viper.Reset()

	_, err := Load(workingDir, debug)
	return err
}

// ConfigFileFormat returns the format ("json" or "toml") of the active config file.
func ConfigFileFormat() string {
	path, err := ResolveConfigFilePath()
	if err != nil || path == "" {
		return "json"
	}
	return configFileFormat(path)
}

// Get returns the current configuration.
// It's safe to call this function multiple times.
func Get() *Config {
	return cfg
}

// WorkingDirectory returns the current working directory from the configuration.
func WorkingDirectory() string {
	if cfg == nil {
		panic("config not loaded")
	}
	return cfg.WorkingDir
}

func UpdateAgentModel(agentName AgentName, modelID models.ModelID) error {
	if cfg == nil {
		panic("config not loaded")
	}

	existingAgentCfg := cfg.Agents[agentName]

	model, ok := models.SupportedModels[modelID]
	if !ok {
		return fmt.Errorf("model %s not supported", modelID)
	}

	maxTokens := existingAgentCfg.MaxTokens
	if model.DefaultMaxTokens > 0 {
		maxTokens = model.DefaultMaxTokens
	}

	newAgentCfg := Agent{
		Model:           modelID,
		MaxTokens:       maxTokens,
		ReasoningEffort: existingAgentCfg.ReasoningEffort,
	}
	cfg.Agents[agentName] = newAgentCfg

	if err := validateAgent(cfg, agentName, newAgentCfg); err != nil {
		// revert config update on failure
		cfg.Agents[agentName] = existingAgentCfg
		return fmt.Errorf("failed to update agent model: %w", err)
	}

	return updateCfgFile(func(config *Config) {
		if config.Agents == nil {
			config.Agents = make(map[AgentName]Agent)
		}
		config.Agents[agentName] = newAgentCfg
	})
}

// UpdateTheme updates the theme in the configuration and writes it to the config file.
func UpdateTheme(themeName string) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	// Update the in-memory config
	cfg.TUI.Theme = themeName

	// Update the file config
	return updateCfgFile(func(config *Config) {
		config.TUI.Theme = themeName
	})
}

func UpdateShell(path string, args []string) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	oldShell := cfg.Shell
	newShell := ShellConfig{
		Path: path,
		Args: append([]string(nil), args...),
	}
	cfg.Shell = newShell

	if err := updateCfgFile(func(config *Config) {
		config.Shell.Path = path
		config.Shell.Args = append([]string(nil), args...)
	}); err != nil {
		cfg.Shell = oldShell
		return err
	}

	return nil
}

func UpdateAutoCompact(enabled bool) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	oldValue := cfg.AutoCompact
	cfg.AutoCompact = enabled

	if err := updateCfgFile(func(config *Config) {
		config.AutoCompact = enabled
	}); err != nil {
		cfg.AutoCompact = oldValue
		return err
	}

	return nil
}

func UpdateDebug(enabled bool) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	oldValue := cfg.Debug
	cfg.Debug = enabled

	if err := updateCfgFile(func(config *Config) {
		config.Debug = enabled
	}); err != nil {
		cfg.Debug = oldValue
		return err
	}

	return nil
}

func UpdateSkillsEnabled(enabled bool) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	oldValue := cfg.Skills.Enabled
	cfg.Skills.Enabled = enabled

	if err := updateCfgFile(func(config *Config) {
		config.Skills.Enabled = enabled
	}); err != nil {
		cfg.Skills.Enabled = oldValue
		return err
	}

	return nil
}

func UpdateMesnada(mesnadaCfg MesnadaConfig) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	oldMesnada := cfg.Mesnada
	cfg.Mesnada = mesnadaCfg

	if err := updateCfgFile(func(config *Config) {
		config.Mesnada = mesnadaCfg
	}); err != nil {
		cfg.Mesnada = oldMesnada
		return err
	}

	return nil
}

func UpdateProvider(name models.ModelProvider, apiKey string, baseURL string, disabled bool) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if providerRequiresAPIKey(name) && strings.TrimSpace(apiKey) == "" && !disabled {
		return fmt.Errorf("provider %s requires an API key when enabled", name)
	}
	if name == models.ProviderOllama && strings.TrimSpace(baseURL) == "" && !disabled {
		return fmt.Errorf("provider %s requires a base URL when enabled", name)
	}

	if cfg.Providers == nil {
		cfg.Providers = make(map[models.ModelProvider]Provider)
	}

	oldProvider, hadProvider := cfg.Providers[name]
	newProvider := Provider{
		APIKey:   strings.TrimSpace(apiKey),
		BaseURL:  strings.TrimSpace(baseURL),
		Disabled: disabled,
	}
	cfg.Providers[name] = newProvider

	if err := updateCfgFile(func(config *Config) {
		if config.Providers == nil {
			config.Providers = make(map[models.ModelProvider]Provider)
		}
		config.Providers[name] = newProvider
	}); err != nil {
		if hadProvider {
			cfg.Providers[name] = oldProvider
		} else {
			delete(cfg.Providers, name)
		}
		return err
	}

	return nil
}

func providerRequiresAPIKey(provider models.ModelProvider) bool {
	return provider != models.ProviderCopilot && provider != models.ProviderOllama
}

func refreshConfiguredDynamicModels() {
	if cfg == nil {
		return
	}

	// Load cached models from previous sessions so they are available before any API fetch.
	if err := models.LoadModelCache(); err != nil {
		logging.Debug("Failed to load model cache", "error", err)
	}

	providerCfg, ok := cfg.Providers[models.ProviderOllama]
	if !ok {
		// Auto-detect Ollama if not configured
		if tryAutoDetectOllama() {
			providerCfg = cfg.Providers[models.ProviderOllama]
			ok = true
		}
	}

	if !ok || providerCfg.Disabled {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := models.RefreshProviderModels(ctx, models.ProviderOllama, providerCfg.APIKey, "", providerCfg.BaseURL); err != nil {
		logging.Debug("Failed to refresh Ollama models during config load", "error", err)
	}
}

// tryAutoDetectOllama pings Ollama's native API and registers it as a provider if reachable.
func tryAutoDetectOllama() bool {
	rawBase := strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL"))
	var pingURL string
	if rawBase == "" {
		pingURL = "http://localhost:11434/api/tags"
	} else {
		rawBase = strings.TrimRight(rawBase, "/")
		rawBase = strings.TrimSuffix(rawBase, "/v1")
		rawBase = strings.TrimSuffix(rawBase, "/api")
		pingURL = strings.TrimRight(rawBase, "/") + "/api/tags"
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(pingURL) //nolint:noctx
	if err != nil {
		return false
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}

	if cfg.Providers == nil {
		cfg.Providers = make(map[models.ModelProvider]Provider)
	}
	cfg.Providers[models.ProviderOllama] = Provider{}
	logging.Info("Auto-detected Ollama", "url", pingURL)
	return true
}

func ensureAgentDefaults() {
	if cfg == nil {
		return
	}
	if cfg.Agents == nil {
		cfg.Agents = make(map[AgentName]Agent)
	}

	for _, agentName := range []AgentName{AgentCoder, AgentSummarizer, AgentTask, AgentTitle} {
		if strings.TrimSpace(string(cfg.Agents[agentName].Model)) != "" {
			continue
		}
		_ = setDefaultModelForAgent(agentName)
	}
}

func firstModelForProvider(provider models.ModelProvider) (models.Model, bool) {
	modelList := make([]models.Model, 0)
	for _, model := range models.SupportedModels {
		if model.Provider == provider {
			modelList = append(modelList, model)
		}
	}
	if len(modelList) == 0 {
		return models.Model{}, false
	}

	sort.Slice(modelList, func(i, j int) bool {
		if modelList[i].Name != modelList[j].Name {
			return modelList[i].Name < modelList[j].Name
		}
		return modelList[i].ID < modelList[j].ID
	})

	return modelList[0], true
}

func UpdateMCPServer(name string, server MCPServer) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("MCP server name cannot be empty")
	}

	if cfg.MCPServers == nil {
		cfg.MCPServers = make(map[string]MCPServer)
	}

	oldServer, hadServer := cfg.MCPServers[name]
	newServer := MCPServer{
		Command: server.Command,
		Env:     append([]string(nil), server.Env...),
		Args:    append([]string(nil), server.Args...),
		Type:    server.Type,
		URL:     server.URL,
		Headers: cloneStringMap(server.Headers),
	}
	if newServer.Type == "" {
		newServer.Type = MCPStdio
	}
	cfg.MCPServers[name] = newServer

	if err := updateCfgFile(func(config *Config) {
		if config.MCPServers == nil {
			config.MCPServers = make(map[string]MCPServer)
		}
		config.MCPServers[name] = newServer
	}); err != nil {
		if hadServer {
			cfg.MCPServers[name] = oldServer
		} else {
			delete(cfg.MCPServers, name)
		}
		return err
	}

	return nil
}

func DeleteMCPServer(name string) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	oldServer, hadServer := cfg.MCPServers[name]
	if !hadServer {
		return fmt.Errorf("MCP server %s not found", name)
	}
	delete(cfg.MCPServers, name)

	if err := updateCfgFile(func(config *Config) {
		delete(config.MCPServers, name)
	}); err != nil {
		cfg.MCPServers[name] = oldServer
		return err
	}

	return nil
}

func UpdateLSP(language string, lsp LSPConfig) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if strings.TrimSpace(language) == "" {
		return fmt.Errorf("LSP language cannot be empty")
	}

	if cfg.LSP == nil {
		cfg.LSP = make(map[string]LSPConfig)
	}

	oldLSP, hadLSP := cfg.LSP[language]
	newLSP := LSPConfig{
		Disabled: lsp.Disabled,
		Command:  lsp.Command,
		Args:     append([]string(nil), lsp.Args...),
		Options:  lsp.Options,
	}
	cfg.LSP[language] = newLSP

	if err := updateCfgFile(func(config *Config) {
		if config.LSP == nil {
			config.LSP = make(map[string]LSPConfig)
		}
		config.LSP[language] = newLSP
	}); err != nil {
		if hadLSP {
			cfg.LSP[language] = oldLSP
		} else {
			delete(cfg.LSP, language)
		}
		return err
	}

	return nil
}

func DeleteLSP(language string) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	oldLSP, hadLSP := cfg.LSP[language]
	if !hadLSP {
		return fmt.Errorf("LSP %s not found", language)
	}
	delete(cfg.LSP, language)

	if err := updateCfgFile(func(config *Config) {
		delete(config.LSP, language)
	}); err != nil {
		cfg.LSP[language] = oldLSP
		return err
	}

	return nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}

	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}

// LoadGitHubToken loads a GitHub OAuth token from the saved Copilot login,
// environment variables, or compatible external tooling.
func LoadGitHubToken() (string, error) {
	return auth.LoadGitHubOAuthToken()
}
