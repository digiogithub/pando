// Package agent handles spawning and managing CLI agent processes.
package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/digiogithub/pando/internal/mesnada/mcpconv"
)

// MesnadaMCPConfig represents the Mesnada MCP configuration format.
type MesnadaMCPConfig = mcpconv.CanonicalConfig

// MesnadaMCPServer represents a server entry in Mesnada format.
type MesnadaMCPServer = mcpconv.CanonicalServer

// ClaudeMCPConfig represents the Claude CLI MCP configuration format.
type ClaudeMCPConfig = mcpconv.ClaudeConfig

// ClaudeMCPServer represents a server entry in Claude CLI format.
type ClaudeMCPServer = mcpconv.ClaudeServer

// GeminiMCPServer represents a server entry in Gemini CLI format.
type GeminiMCPServer = mcpconv.GeminiServer

// GeminiSettings represents the Gemini CLI settings format.
type GeminiSettings = mcpconv.GeminiSettings

// OpenCodeMCPConfig represents the OpenCode.ai MCP configuration format.
type OpenCodeMCPConfig = mcpconv.OpenCodeConfig

// OpenCodeMCPServer represents a server entry in OpenCode.ai format.
type OpenCodeMCPServer = mcpconv.OpenCodeServer

// VSCodeMCPConfig represents VS Code .vscode/mcp.json format.
type VSCodeMCPConfig = mcpconv.VSCodeConfig

// VSCodeMCPServer represents a server entry in VS Code format.
type VSCodeMCPServer = mcpconv.VSCodeServer

// AntigravityMCPConfig represents Antigravity mcp_config.json format.
type AntigravityMCPConfig = mcpconv.AntigravityConfig

// AntigravityMCPServer represents a server entry in Antigravity format.
type AntigravityMCPServer = mcpconv.AntigravityServer

// AntigravityProjectMCPConfigPath is the project-local target path used by --all mode.
const AntigravityProjectMCPConfigPath = mcpconv.AntigravityProjectMCPConfigPath

const (
	MCPFormatMesnada = mcpconv.FormatMesnada
	MCPFormatVSCode  = mcpconv.FormatVSCode
)

// ConvertMCPConfigFormats converts input MCP config between supported formats (e.g. --from/--to).
func ConvertMCPConfigFormats(inputPath, outputPath, workDir, fromFormat, toFormat string) error {
	return mcpconv.ConvertFileFormatForPath(inputPath, outputPath, workDir, fromFormat, toFormat)
}

// ConvertMCPConfig converts a Mesnada MCP config file to Claude CLI format.
func ConvertMCPConfig(mcpConfigPath, tempDir, workDir string) (string, error) {
	cfg, err := mcpconv.ParseMesnadaFile(mcpConfigPath, workDir)
	if err != nil {
		return "", err
	}
	return mcpconv.WriteJSONFile(tempDir, "claude-mcp-config.json", mcpconv.RenderClaude(cfg, workDir))
}

// ConvertMCPConfigForTask converts MCP config for a specific task.
func ConvertMCPConfigForTask(mcpConfigPath, taskID, baseDir, workDir string) (string, error) {
	if mcpConfigPath == "" {
		return "", nil
	}
	tempDir := filepath.Join(baseDir, "claude-mcp", taskID)
	return ConvertMCPConfig(mcpConfigPath, tempDir, workDir)
}

// CleanupMCPConfig removes the temporary MCP config file for a task.
func CleanupMCPConfig(taskID, baseDir string) error {
	tempDir := filepath.Join(baseDir, "claude-mcp", taskID)
	return os.RemoveAll(tempDir)
}

// ConvertMCPConfigForCopilot converts Mesnada MCP config to GitHub Copilot CLI format.
// Copilot accepts the same mcpServers schema as Claude CLI (command/args/env per entry).
func ConvertMCPConfigForCopilot(mcpConfigPath, taskID, baseDir, workDir string) (string, error) {
	if mcpConfigPath == "" {
		return "", nil
	}
	cfg, err := mcpconv.ParseMesnadaFile(mcpConfigPath, workDir)
	if err != nil {
		return "", err
	}
	tempDir := filepath.Join(baseDir, "copilot-mcp", taskID)
	return mcpconv.WriteJSONFile(tempDir, "copilot-mcp-config.json", mcpconv.RenderClaude(cfg, workDir))
}

// CleanupCopilotMCPConfig removes the temporary Copilot MCP config for a task.
func CleanupCopilotMCPConfig(taskID, baseDir string) error {
	tempDir := filepath.Join(baseDir, "copilot-mcp", taskID)
	return os.RemoveAll(tempDir)
}

// CreateGeminiSettingsFile creates a temporary settings.json file with MCP configuration.
func CreateGeminiSettingsFile(mcpConfigPath, taskID, baseDir, workDir string) (string, error) {
	if mcpConfigPath == "" {
		return "", nil
	}
	cfg, err := mcpconv.ParseMesnadaFile(mcpConfigPath, workDir)
	if err != nil {
		return "", err
	}
	tempDir := filepath.Join(baseDir, "gemini-settings", taskID)
	return mcpconv.WriteJSONFile(tempDir, "settings.json", mcpconv.RenderGemini(cfg, workDir))
}

// CleanupGeminiSettingsFile removes the temporary settings file created for a task.
func CleanupGeminiSettingsFile(settingsPath string) error {
	if settingsPath == "" {
		return nil
	}
	return os.RemoveAll(filepath.Dir(settingsPath))
}

// ConvertMCPConfigForOpenCode converts Mesnada MCP config to OpenCode.ai format.
func ConvertMCPConfigForOpenCode(mcpConfigPath, taskID, baseDir, workDir string) (string, error) {
	if mcpConfigPath == "" {
		return "", nil
	}
	cfg, err := mcpconv.ParseMesnadaFile(mcpConfigPath, workDir)
	if err != nil {
		return "", err
	}
	tempDir := filepath.Join(baseDir, "opencode-mcp", taskID)
	return mcpconv.WriteJSONFile(tempDir, "opencode.json", mcpconv.RenderOpenCode(cfg))
}

// ConvertMCPConfigForAntigravity converts canonical MCP config to Antigravity mcp_config.json format.
func ConvertMCPConfigForAntigravity(mcpConfigPath, tempDir, workDir string) (string, error) {
	cfg, err := mcpconv.ParseMesnadaFile(mcpConfigPath, workDir)
	if err != nil {
		return "", err
	}
	return mcpconv.WriteJSONFile(tempDir, "mcp_config.json", mcpconv.RenderAntigravity(cfg))
}

func createVibeConfigToml(mcpConfigPath, workDir, model string) (string, error) {
	cfg := mcpconv.CanonicalConfig{MCPServers: map[string]mcpconv.CanonicalServer{}}
	if mcpConfigPath != "" {
		var err error
		cfg, err = mcpconv.ParseMesnadaFile(mcpConfigPath, workDir)
		if err != nil {
			return "", err
		}
	}
	return mcpconv.RenderMistralVibeTOML(cfg, model), nil
}

// WriteCanonicalConfigToFile serialises cfg directly to a JSON file at
// tempDir/filename without reading any input file from disk. This is used by
// the orchestrator when the MCP config is built dynamically at spawn time.
func WriteCanonicalConfigToFile(cfg mcpconv.CanonicalConfig, tempDir, filename string) (string, error) {
	return mcpconv.WriteJSONFile(tempDir, filename, cfg)
}

func writeVibeConfig(tempDir, content string) error {
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp vibe home: %w", err)
	}
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write vibe config.toml: %w", err)
	}
	return nil
}
