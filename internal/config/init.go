package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// InitFlagFilename is the name of the file that indicates whether the project has been initialized
	InitFlagFilename = "init"

	// localConfigFilename is the default local config filename generated during first-run.
	localConfigFilename = ".pando.toml"

	// pandoDirName is the name of the pando data directory.
	pandoDirName = ".pando"
)

// ProjectInitFlag represents the initialization status for a project directory
type ProjectInitFlag struct {
	Initialized bool `json:"initialized"`
}

// ShouldShowInitDialog checks if the initialization dialog should be shown for the current directory
func ShouldShowInitDialog() (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("config not loaded")
	}

	// Create the flag file path
	flagFilePath := filepath.Join(cfg.Data.Directory, InitFlagFilename)

	// Check if the flag file exists
	_, err := os.Stat(flagFilePath)
	if err == nil {
		// File exists, don't show the dialog
		return false, nil
	}

	// If the error is not "file not found", return the error
	if !os.IsNotExist(err) {
		return false, fmt.Errorf("failed to check init flag file: %w", err)
	}

	// File doesn't exist, show the dialog
	return true, nil
}

// MarkProjectInitialized marks the current project as initialized
func MarkProjectInitialized() error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	// Create the flag file path
	flagFilePath := filepath.Join(cfg.Data.Directory, InitFlagFilename)

	// Create an empty file to mark the project as initialized
	file, err := os.Create(flagFilePath)
	if err != nil {
		return fmt.Errorf("failed to create init flag file: %w", err)
	}
	defer file.Close()

	return nil
}

// HasLocalConfigFile returns true if a .pando.toml or .pando.json file exists
// in the current working directory. It does NOT check profile/home locations.
func HasLocalConfigFile() bool {
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	for _, ext := range []string{"toml", "json"} {
		candidate := filepath.Join(cwd, fmt.Sprintf(".%s.%s", appName, ext))
		if _, err := os.Stat(candidate); err == nil {
			return true
		}
	}
	return false
}

// HasPandoDirectory returns true if the .pando directory already exists in
// the current working directory (meaning the project was previously initialised
// but the config file may have been deleted or never generated locally).
func HasPandoDirectory() bool {
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(cwd, pandoDirName))
	return err == nil && info.IsDir()
}

// DefaultConfigTemplate is the annotated .pando.toml written when no local
// config exists. It is kept here (rather than in cmd/) so both the CLI init
// command and the TUI first-run flow can share it without import cycles.
// API keys and provider-specific model selections are intentionally left blank.
const DefaultConfigTemplate = `# =============================================================================
# Pando Configuration File
# =============================================================================
# Pando looks for this file in (highest priority first):
#   1. Current working directory  (.pando.toml)
#   2. $HOME/.config/pando/.pando.toml
#   3. $HOME/.pando.toml
#
# Environment variables prefixed with PANDO_ override any value here.
# Example: PANDO_DEBUG=true overrides Debug = false
# =============================================================================

WorkingDir = ''
Debug      = false
LogFile    = ''
DebugLSP   = false
ContextPaths = []
AutoCompact  = true

[Data]
Directory = './.pando/data'

# =============================================================================
# MCP Servers (Model Context Protocol)
# =============================================================================
# [MCPServers]
# [MCPServers.my-tool]
# Type    = 'stdio'
# Command = 'my-mcp-server'
# Args    = []

# =============================================================================
# LLM Providers — add API keys here or use environment variables.
# =============================================================================
[Providers]

[Providers.anthropic]
APIKey   = ''
BaseURL  = ''
Disabled = false
UseOAuth = true

# [Providers.openai]
# APIKey   = ''
# Disabled = false

# [Providers.copilot]
# Disabled = false

# [Providers.ollama]
# BaseURL  = 'http://localhost:11434'
# Disabled = false

# =============================================================================
# Language Server Protocol (LSP)
# =============================================================================
[LSP]

[LSP.gopls]
Disabled  = false
Command   = 'gopls'
Args      = []
Languages = []

# =============================================================================
# Agents — configure the model for each role.
# Model format: '<provider>.<model-id>'
# =============================================================================
[Agents]

[Agents.coder]
Model     = ''
MaxTokens = 32000
AutoCompact = false

[Agents.summarizer]
Model     = ''
MaxTokens = 90000

[Agents.task]
Model     = ''
MaxTokens = 16384

# =============================================================================
# Skills
# =============================================================================
[Skills]
Enabled = true
Paths   = ['./agents/skills']

[SkillsCatalog]
Enabled    = false
BaseURL    = ''
AutoUpdate = false

# =============================================================================
# TUI
# =============================================================================
[TUI]
Theme = ''

# =============================================================================
# Permissions
# =============================================================================
[Permissions]
AutoApproveTools = false

# =============================================================================
# Mesnada — Multi-Agent Orchestrator
# =============================================================================
[Mesnada]
Enabled = true

# =============================================================================
# CronJobs — scheduled Mesnada/Pando tasks
# =============================================================================
# [CronJobs]
# Enabled = true
# [[CronJobs.Jobs]]
# Name = 'daily-review'
# Schedule = '0 9 * * 1-5'
# Prompt = 'Review today''s git log and summarize in DAILY_REPORT.md'
# Enabled = true
# Engine = 'pando'
# Tags = ['daily']
# Timeout = '10m'

[Mesnada.Server]
Host = ''
Port = 5005

[Mesnada.Orchestrator]
StorePath        = './.pando/mesnada/tasks.json'
LogDir           = './.pando/mesnada/logs'
MaxParallel      = 5
DefaultEngine    = 'pando'
DefaultModel     = 'sonnet'
# DefaultMCPConfig is optional. When unset, pando generates a dynamic MCP config
# at spawn time that includes pando itself (remembrances, mesnada, fetch, search,
# browser) plus any MCPServers defined in this config file.
# DefaultMCPConfig = '/path/to/custom-mcp-config.json'
PersonaPath      = './.pando/mesnada/personas'

[Mesnada.ACP]
Enabled        = true
DefaultAgent   = 'claude'
AutoPermission = true

[Mesnada.ACP.Server]
Enabled        = false
Transports     = ['http']
Host           = '0.0.0.0'
Port           = 8766
MaxSessions    = 100
SessionTimeout = '30m'
RequireAuth    = false

[Mesnada.TUI]
Enabled = true
WebUI   = false

# =============================================================================
# Shell & Bash
# =============================================================================
[Shell]
Path = ''
Args = []

[Bash]
BannedCommands  = []
AllowedCommands = []

# =============================================================================
# Remembrances — Semantic memory and knowledge base
# =============================================================================
[Remembrances]
Enabled = true
KBPath  = ''
KBAutoImport = true
KBWatch      = true

DocumentEmbeddingProvider = 'ollama'
DocumentEmbeddingModel    = 'nomic-embed-text'
DocumentEmbeddingBaseURL  = ''
DocumentEmbeddingAPIKey   = ''

CodeEmbeddingProvider = 'ollama'
CodeEmbeddingModel    = 'hf.co/limcheekin/CodeRankEmbed-GGUF:Q4_K_M'
CodeEmbeddingBaseURL  = ''
CodeEmbeddingAPIKey   = ''

UseSameModel = false
ChunkSize    = 800
ChunkOverlap = 100
IndexWorkers = 4

# =============================================================================
# API Server (Web UI backend)
# =============================================================================
[Server]
Enabled     = true
Host        = 'localhost'
Port        = 9999
RequireAuth = false

# =============================================================================
# Container Runtime
# =============================================================================
[Container]
Runtime         = 'host'
Image           = ''
PullPolicy      = 'if-not-present'
Socket          = ''
WorkDir         = ''
Network         = 'none'
ReadOnly        = true
User            = ''
CPULimit        = ''
MemLimit        = ''
PidsLimit       = 512
NoNewPrivileges = true
AllowEnv        = []
AllowMounts     = []
ExtraEnv        = []
ExtraMounts     = []
EmbeddedCacheDir = ''
EmbeddedGCKeepN  = 5

# =============================================================================
# Lua scripting
# =============================================================================
[Lua]
Enabled         = false
ScriptPath      = ''
Timeout         = ''
StrictMode      = false
HotReload       = false
LogFilteredData = false

# =============================================================================
# MCP Gateway
# =============================================================================
[MCPGateway]
Enabled            = true
FavoriteThreshold  = 3
MaxFavorites       = 10
FavoriteWindowDays = 7
DecayDays          = 30

# =============================================================================
# Internal Tools
# =============================================================================
[InternalTools]
FetchEnabled            = false
FetchMaxSizeMB          = 0
GoogleSearchEnabled     = false
GoogleAPIKey            = ''
GoogleSearchEngineID    = ''
BraveSearchEnabled      = false
BraveAPIKey             = ''
PerplexitySearchEnabled = false
PerplexityAPIKey        = ''
ExaSearchEnabled        = false
ExaAPIKey               = ''
Context7Enabled         = false
BrowserEnabled          = false
BrowserHeadless         = false
BrowserTimeout          = 30
BrowserUserDataDir      = ''
BrowserMaxSessions      = 3

# =============================================================================
# Snapshots
# =============================================================================
[Snapshots]
Enabled          = true
MaxSnapshots     = 50
MaxFileSize      = '10MB'
ExcludePatterns  = []
AutoCleanupDays  = 30

# =============================================================================
# Evaluator
# =============================================================================
[evaluator]
enabled              = true
model                = ''
provider             = ''
alphaWeight          = 0.8
betaWeight           = 0.2
explorationC         = 1.41
minSessionsForUCB    = 5
maxTokensBaseline    = 50
maxSkills            = 100
async                = true
judgePromptTemplate  = ''

# =============================================================================
# CLI Assist
# =============================================================================
[cliAssist]
Model   = ''
Timeout = 0

# =============================================================================
# Persona Auto-Select
# =============================================================================
[PersonaAutoSelect]
Enabled     = false
PersonaPath = ''

# =============================================================================
# ACP (Agent Client Protocol)
# =============================================================================
[acp]
enabled         = false
max_sessions    = 0
idle_timeout    = ''
log_level       = ''
auto_permission = false

# =============================================================================
# Projects — Multi-project management
# =============================================================================
[Projects]
Enabled     = true
AutoRestore = false
MaxProjects = 20
`

// GenerateLocalConfigFile writes the annotated default .pando.toml template
// into the current working directory. It returns an error if the file already
// exists to prevent accidental overwrites.
func GenerateLocalConfigFile(template string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	dest := filepath.Join(cwd, localConfigFilename)
	if _, statErr := os.Stat(dest); statErr == nil {
		return fmt.Errorf("config file already exists at %s", dest)
	}
	if err := os.WriteFile(dest, []byte(template), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// ShouldGenerateLocalConfig returns true when pando is running in a directory
// that has no local .pando.toml/.pando.json but does have (or should have) one.
// The rules are:
//   - If a local config already exists → false (nothing to do).
//   - Otherwise → true (we should offer/auto-generate one).
func ShouldGenerateLocalConfig() bool {
	return !HasLocalConfigFile()
}

// DefaultPersonaTemplate is the starter persona written to personas/default.md
// during project initialisation. It is exported here so both cmd/init.go and
// the headless InitializeProjectAt function can share a single source of truth
// without creating import cycles.
const DefaultPersonaTemplate = `# Default Persona

## Role
You are a helpful, senior software engineer with deep knowledge of the codebase.

## Behaviour
- Prefer minimal, readable, idiomatic code over clever one-liners.
- Always explain *why* before *how* when introducing non-obvious solutions.
- Ask clarifying questions before starting work on ambiguous tasks.
- Point out potential security issues even when not explicitly asked.

## Communication style
- Be concise but thorough — no unnecessary filler phrases.
- Use code blocks for all code snippets, commands, and file paths.
- Prefer bullet lists over long paragraphs for multi-step explanations.
`

// HasConfigFileAt returns true if a .pando.toml or .pando.json file exists
// in the given directory. It does NOT search parent directories or profiles.
func HasConfigFileAt(dir string) bool {
	for _, name := range []string{".pando.toml", ".pando.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// HasPandoDirectoryAt returns true if the .pando/ subdirectory exists in dir.
func HasPandoDirectoryAt(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, pandoDirName))
	return err == nil && info.IsDir()
}

// InitializeProjectAt creates the full Pando directory structure and writes
// the default configuration at the given path. It is the path-aware equivalent
// of the "pando init --target project" command, suitable for use by the
// ProjectManager when a new project directory has no existing configuration.
//
// Created layout:
//
//	<dir>/.pando.toml
//	<dir>/.pando/data/
//	<dir>/.pando/data/init          (init flag)
//	<dir>/.pando/mesnada/logs/
//	<dir>/.pando/mesnada/personas/
//	<dir>/.pando/mesnada/personas/default.md
//	<dir>/agents/skills/
//
// Returns an error if any step fails. It is idempotent: existing files/dirs
// are not overwritten (same behaviour as "pando init" without --force).
func InitializeProjectAt(dir string) error {
	dataDir := filepath.Join(dir, pandoDirName, "data")
	mesnadaDir := filepath.Join(dir, pandoDirName, "mesnada")
	logsDir := filepath.Join(mesnadaDir, "logs")
	personasDir := filepath.Join(mesnadaDir, "personas")
	skillsDir := filepath.Join(dir, "agents", "skills")

	// Create all required directories.
	for _, d := range []string{dataDir, logsDir, personasDir, skillsDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("InitializeProjectAt: create directory %s: %w", d, err)
		}
	}

	// Write .pando.toml only when it does not already exist.
	configPath := filepath.Join(dir, localConfigFilename)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(DefaultConfigTemplate), 0644); err != nil {
			return fmt.Errorf("InitializeProjectAt: write config file: %w", err)
		}
	}

	// Write the default persona only when it does not already exist.
	personaPath := filepath.Join(personasDir, "default.md")
	if _, err := os.Stat(personaPath); os.IsNotExist(err) {
		if err := os.WriteFile(personaPath, []byte(DefaultPersonaTemplate), 0644); err != nil {
			return fmt.Errorf("InitializeProjectAt: write persona file: %w", err)
		}
	}

	// Touch the init flag file to mark the project as initialised.
	initFlag := filepath.Join(dataDir, InitFlagFilename)
	f, err := os.OpenFile(initFlag, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("InitializeProjectAt: write init flag: %w", err)
	}
	_ = f.Close()

	return nil
}
