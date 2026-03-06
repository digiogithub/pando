package mcpconv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// AntigravityProjectMCPConfigPath is the project-local output used by --all mode.
	AntigravityProjectMCPConfigPath = ".gemini/antigravity/mcp_config.json"
	// ZedProjectSettingsPath is the project-local output used by --all mode.
	ZedProjectSettingsPath = ".zed/settings.json"

	FormatMesnada     = "mesnada"
	FormatVSCode      = "vscode"
	FormatClaude      = "claude"
	FormatGemini      = "gemini"
	FormatOpenCode    = "opencode"
	FormatVibe        = "vibe"
	FormatZed         = "zed"
	FormatAntigravity = "antigravity"

	Version = "0.1.0"
)

// AllFormatEntry describes a single output format and where it should be written in --all mode.
type AllFormatEntry struct {
	Format string
	Path   string
}

// AllFormats is the ordered list of output formats and project-relative paths for --all mode.
var AllFormats = []AllFormatEntry{
	{Format: FormatMesnada, Path: ".github/mcp-config.json"},
	{Format: FormatVSCode, Path: ".vscode/mcp.json"},
	{Format: FormatClaude, Path: ".mcp.json"},
	{Format: FormatGemini, Path: ".gemini/settings.json"},
	{Format: FormatOpenCode, Path: "opencode.json"},
	{Format: FormatVibe, Path: ".vibe/config.toml"},
	{Format: FormatZed, Path: ".zed/settings.json"},
	{Format: FormatAntigravity, Path: ".gemini/antigravity/mcp_config.json"},
}

// CanonicalConfig is the normalized MCP model used across render targets.
type CanonicalConfig struct {
	MCPServers map[string]CanonicalServer `json:"mcpServers"`
}

// CanonicalServer is the normalized server definition.
type CanonicalServer struct {
	Type      string            `json:"type,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	URL       string            `json:"url,omitempty"`
	ServerURL string            `json:"serverUrl,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Enabled   *bool             `json:"enabled,omitempty"`
	Timeout   int               `json:"timeout,omitempty"`
	Tools     []string          `json:"tools,omitempty"`
}

// ClaudeConfig is the format accepted by claude --mcp-config.
type ClaudeConfig struct {
	MCPServers map[string]ClaudeServer `json:"mcpServers"`
}

// ClaudeServer is a Claude MCP server entry.
type ClaudeServer struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
}

// GeminiServer is a Gemini MCP server entry.
type GeminiServer struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	URL     string            `json:"url,omitempty"`
	HttpURL string            `json:"httpUrl,omitempty"`
	Trust   bool              `json:"trust,omitempty"`
}

// GeminiSettings is the format passed to GEMINI_CLI_SYSTEM_SETTINGS_PATH.
type GeminiSettings struct {
	MCPServers map[string]GeminiServer `json:"mcpServers,omitempty"`
}

// OpenCodeConfig is the OpenCode MCP config format.
type OpenCodeConfig struct {
	MCP map[string]OpenCodeServer `json:"mcp"`
}

// OpenCodeServer is an OpenCode MCP server entry.
type OpenCodeServer struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	URL         string            `json:"url,omitempty"`
	Enabled     bool              `json:"enabled,omitempty"`
	Timeout     int               `json:"timeout,omitempty"`
}

// VSCodeConfig is the VS Code MCP format (.vscode/mcp.json).
// The "inputs" section is intentionally ignored.
type VSCodeConfig struct {
	Servers map[string]VSCodeServer `json:"servers"`
	Inputs  json.RawMessage         `json:"inputs,omitempty"`
}

// VSCodeServer is a VS Code MCP server entry.
type VSCodeServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// AntigravityConfig is the Antigravity mcp_config.json format.
type AntigravityConfig struct {
	MCPServers map[string]AntigravityServer `json:"mcpServers"`
}

// AntigravityServer is an Antigravity MCP server entry.
type AntigravityServer struct {
	Type      string            `json:"type,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	ServerURL string            `json:"serverUrl,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Enabled   *bool             `json:"enabled,omitempty"`
	Timeout   int               `json:"timeout,omitempty"`
}

// ZedSettings is the settings.json format used by Zed.
type ZedSettings struct {
	ContextServers map[string]ZedServer `json:"context_servers,omitempty"`
}

// ZedServer is a Zed context_servers entry.
type ZedServer struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Enabled *bool             `json:"enabled,omitempty"`
	Timeout int               `json:"timeout,omitempty"`
}

// ParseMesnadaFile parses Mesnada/Copilot MCP config into canonical model.
// When format is omitted, VS Code format can be auto-detected.
func ParseMesnadaFile(mcpConfigPath, workDir string) (CanonicalConfig, error) {
	sourcePath, err := resolveSourcePath(mcpConfigPath, workDir)
	if err != nil {
		return CanonicalConfig{}, err
	}

	return ParseCanonicalFileWithFormat(sourcePath, "")
}

// ParseCanonicalFile parses a JSON MCP file into canonical format, auto-detecting supported inputs.
func ParseCanonicalFile(sourcePath string) (CanonicalConfig, error) {
	return ParseCanonicalFileWithFormat(sourcePath, "")
}

// ParseCanonicalFileWithFormat parses a JSON MCP file into canonical format using explicit format when provided.
func ParseCanonicalFileWithFormat(sourcePath, fromFormat string) (CanonicalConfig, error) {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return CanonicalConfig{}, fmt.Errorf("failed to read MCP config: %w", err)
	}
	return ParseCanonicalBytesWithFormat(data, fromFormat)
}

// ParseCanonicalBytesWithFormat parses raw JSON bytes into canonical format.
func ParseCanonicalBytesWithFormat(data []byte, fromFormat string) (CanonicalConfig, error) {
	switch strings.ToLower(fromFormat) {
	case "", FormatMesnada, "copilot", FormatAntigravity:
		cfg, err := parseCanonicalBytes(data)
		if err == nil {
			return cfg, nil
		}
		if fromFormat != "" {
			return CanonicalConfig{}, fmt.Errorf("failed to parse MCP config as %s format", fromFormat)
		}
		return parseVSCodeBytes(data)
	case FormatVSCode:
		return parseVSCodeBytes(data)
	case FormatZed:
		return ParseZedSettings(data)
	default:
		return CanonicalConfig{}, fmt.Errorf("unsupported MCP input format: %s", fromFormat)
	}
}

func parseCanonicalBytes(data []byte) (CanonicalConfig, error) {
	var cfg CanonicalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return CanonicalConfig{}, fmt.Errorf("failed to parse MCP config: %w", err)
	}
	if cfg.MCPServers == nil {
		return CanonicalConfig{}, fmt.Errorf("failed to parse MCP config: missing mcpServers")
	}
	return cfg, nil
}

func parseVSCodeBytes(data []byte) (CanonicalConfig, error) {
	data = sanitizeJSONC(data)
	var cfg VSCodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return CanonicalConfig{}, fmt.Errorf("failed to parse MCP config: %w", err)
	}
	if cfg.Servers == nil {
		return CanonicalConfig{}, fmt.Errorf("failed to parse MCP config: missing servers")
	}

	out := CanonicalConfig{MCPServers: make(map[string]CanonicalServer, len(cfg.Servers))}
	for name, server := range cfg.Servers {
		item := CanonicalServer{
			Command: server.Command,
			Args:    server.Args,
			Env:     server.Env,
			Cwd:     server.Cwd,
			URL:     server.URL,
			Headers: server.Headers,
		}
		switch strings.ToLower(server.Type) {
		case "http", "remote", "sse":
			item.Type = "http"
		default:
			item.Type = "local"
		}
		out.MCPServers[name] = item
	}

	return out, nil
}

// ParseZedSettings parses Zed settings JSON and maps context_servers to canonical config.
func ParseZedSettings(data []byte) (CanonicalConfig, error) {
	var settings ZedSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return CanonicalConfig{}, fmt.Errorf("failed to parse Zed settings: %w", err)
	}

	cfg := CanonicalConfig{MCPServers: make(map[string]CanonicalServer)}
	for name, server := range settings.ContextServers {
		item := CanonicalServer{
			Enabled: server.Enabled,
			Timeout: server.Timeout,
		}
		if server.URL != "" {
			item.Type = "http"
			item.URL = server.URL
			item.Headers = server.Headers
		} else {
			item.Type = "local"
			item.Command = server.Command
			item.Args = server.Args
			item.Env = server.Env
		}
		cfg.MCPServers[name] = item
	}

	return cfg, nil
}

// ParseZedFile parses Zed settings from a file path into canonical config.
func ParseZedFile(settingsPath, workDir string) (CanonicalConfig, error) {
	sourcePath, err := resolveSourcePath(settingsPath, workDir)
	if err != nil {
		return CanonicalConfig{}, err
	}

	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return CanonicalConfig{}, fmt.Errorf("failed to read Zed settings: %w", err)
	}

	return ParseZedSettings(data)
}

// RenderClaude converts canonical config to Claude format.
func RenderClaude(cfg CanonicalConfig, workDir string) ClaudeConfig {
	absWorkDir, _ := normalizeWorkDir(workDir)
	out := ClaudeConfig{MCPServers: make(map[string]ClaudeServer, len(cfg.MCPServers))}

	for name, server := range cfg.MCPServers {
		item := ClaudeServer{}

		switch {
		case isHTTPServer(server):
			item.Command = "npx"
			item.Args = []string{"-y", "mcp-remote", remoteURL(server)}
		default:
			item.Command = server.Command
			item.Args = server.Args
			item.Env = server.Env
			item.Cwd = resolveCwd(server.Cwd, absWorkDir)
		}

		out.MCPServers[name] = item
	}

	return out
}

// RenderGemini converts canonical config to Gemini settings format.
func RenderGemini(cfg CanonicalConfig, workDir string) GeminiSettings {
	absWorkDir, _ := normalizeWorkDir(workDir)
	out := GeminiSettings{MCPServers: make(map[string]GeminiServer, len(cfg.MCPServers))}

	for name, server := range cfg.MCPServers {
		item := GeminiServer{Trust: true}

		switch {
		case isHTTPServer(server):
			item.Command = "npx"
			item.Args = []string{"-y", "mcp-remote", remoteURL(server)}
		default:
			item.Command = server.Command
			item.Args = server.Args
			item.Env = server.Env
			item.Cwd = resolveCwd(server.Cwd, absWorkDir)
		}

		out.MCPServers[name] = item
	}

	return out
}

// RenderOpenCode converts canonical config to OpenCode format.
func RenderOpenCode(cfg CanonicalConfig) OpenCodeConfig {
	out := OpenCodeConfig{MCP: make(map[string]OpenCodeServer, len(cfg.MCPServers))}

	for name, server := range cfg.MCPServers {
		item := OpenCodeServer{Enabled: true, Timeout: 5000}
		if server.Enabled != nil {
			item.Enabled = *server.Enabled
		}
		if server.Timeout > 0 {
			item.Timeout = server.Timeout
		}

		if isHTTPServer(server) {
			item.Type = "local"
			item.Command = []string{"npx", "-y", "mcp-remote", remoteURL(server)}
		} else {
			item.Type = "local"
			item.Command = append([]string{server.Command}, server.Args...)
			item.Environment = server.Env
		}
		out.MCP[name] = item
	}

	return out
}

// RenderAntigravity converts canonical config to Antigravity mcp_config.json format.
func RenderAntigravity(cfg CanonicalConfig) AntigravityConfig {
	out := AntigravityConfig{MCPServers: make(map[string]AntigravityServer, len(cfg.MCPServers))}

	for name, server := range cfg.MCPServers {
		item := AntigravityServer{
			Type:    server.Type,
			Enabled: server.Enabled,
			Timeout: server.Timeout,
			Headers: server.Headers,
		}

		if isHTTPServer(server) {
			item.ServerURL = remoteURL(server)
		} else {
			item.Command = server.Command
			item.Args = server.Args
			item.Env = server.Env
		}
		out.MCPServers[name] = item
	}

	return out
}

// RenderZed converts canonical config to Zed settings format.
func RenderZed(cfg CanonicalConfig) ZedSettings {
	out := ZedSettings{ContextServers: make(map[string]ZedServer, len(cfg.MCPServers))}
	for name, server := range cfg.MCPServers {
		item := ZedServer{Enabled: server.Enabled, Timeout: server.Timeout}
		if isHTTPServer(server) {
			item.URL = remoteURL(server)
			item.Headers = server.Headers
		} else {
			item.Command = server.Command
			item.Args = server.Args
			item.Env = server.Env
		}
		out.ContextServers[name] = item
	}
	return out
}

// RenderVSCode converts canonical config to VS Code mcp.json format.
func RenderVSCode(cfg CanonicalConfig) VSCodeConfig {
	out := VSCodeConfig{Servers: make(map[string]VSCodeServer, len(cfg.MCPServers))}
	for name, server := range cfg.MCPServers {
		item := VSCodeServer{}
		if isHTTPServer(server) {
			item.Type = "http"
			item.URL = remoteURL(server)
			item.Headers = server.Headers
		} else {
			item.Type = "stdio"
			item.Command = server.Command
			item.Args = server.Args
			item.Env = server.Env
			item.Cwd = server.Cwd
		}
		out.Servers[name] = item
	}
	return out
}

// RenderByFormat renders canonical config to a supported output format payload.
// workDir is used for formats that resolve relative paths (claude, gemini).
// For the vibe/mistral format, a string is returned instead of a JSON-serialisable value.
func RenderByFormat(cfg CanonicalConfig, toFormat, workDir string) (any, error) {
	format := strings.ToLower(toFormat)
	renderCfg := cfg
	if format != "" && format != FormatMesnada && format != "copilot" {
		renderCfg = absolutizeRelativeServerPaths(cfg, workDir)
	}

	switch format {
	case "", FormatMesnada, "copilot":
		if cfg.MCPServers == nil {
			cfg.MCPServers = make(map[string]CanonicalServer)
		}
		return cfg, nil
	case FormatVSCode:
		return RenderVSCode(renderCfg), nil
	case FormatClaude:
		return RenderClaude(renderCfg, workDir), nil
	case FormatGemini:
		return RenderGemini(renderCfg, workDir), nil
	case FormatOpenCode:
		return RenderOpenCode(renderCfg), nil
	case FormatVibe, "mistral":
		return RenderMistralVibeTOML(renderCfg, ""), nil
	case FormatZed:
		return RenderZed(renderCfg), nil
	case FormatAntigravity:
		return RenderAntigravity(renderCfg), nil
	default:
		return nil, fmt.Errorf("unsupported MCP output format: %s", toFormat)
	}
}

func absolutizeRelativeServerPaths(cfg CanonicalConfig, workDir string) CanonicalConfig {
	absWorkDir, _ := normalizeWorkDir(workDir)
	if absWorkDir == "" {
		return cfg
	}

	out := CanonicalConfig{MCPServers: make(map[string]CanonicalServer, len(cfg.MCPServers))}
	for name, server := range cfg.MCPServers {
		item := server
		if !isHTTPServer(item) {
			item.Command = absolutizePathToken(item.Command, absWorkDir)
			item.Args = absolutizeArgs(item.Args, absWorkDir)
			item.Cwd = resolveCwd(item.Cwd, absWorkDir)
		}
		out.MCPServers[name] = item
	}
	return out
}

func absolutizeArgs(args []string, absWorkDir string) []string {
	if len(args) == 0 {
		return args
	}
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = absolutizeArgument(arg, absWorkDir)
	}
	return out
}

func absolutizeArgument(arg, absWorkDir string) string {
	if eq := strings.IndexByte(arg, '='); eq > 0 && strings.HasPrefix(arg, "-") {
		left := arg[:eq+1]
		right := arg[eq+1:]
		return left + absolutizePathToken(right, absWorkDir)
	}
	return absolutizePathToken(arg, absWorkDir)
}

func absolutizePathToken(token, absWorkDir string) string {
	token = strings.TrimSpace(token)
	if !isRelativePathToken(token) || absWorkDir == "" {
		return token
	}
	return filepath.Clean(filepath.Join(absWorkDir, token))
}

func isRelativePathToken(token string) bool {
	if token == "" {
		return false
	}
	if filepath.IsAbs(token) {
		return false
	}
	if strings.Contains(token, "://") {
		return false
	}
	if strings.HasPrefix(token, "-") {
		return false
	}
	if strings.HasPrefix(token, "~") {
		return false
	}
	if strings.HasPrefix(token, "${") {
		return false
	}

	return strings.HasPrefix(token, ".") || strings.Contains(token, "/") || strings.Contains(token, "\\")
}

// WriteAllFormats writes every supported MCP format into its project-conventional path under projectDir.
// Returns the list of paths written.
func WriteAllFormats(cfg CanonicalConfig, projectDir, workDir string) ([]string, error) {
	return WriteAllFormatsSkippingSource(cfg, projectDir, workDir, "")
}

// WriteAllFormatsSkippingSource writes every supported format like WriteAllFormats,
// but never overwrites sourcePath when it points to one of the target files.
func WriteAllFormatsSkippingSource(cfg CanonicalConfig, projectDir, workDir, sourcePath string) ([]string, error) {
	var sourceAbs string
	if sourcePath != "" {
		abs, err := filepath.Abs(sourcePath)
		if err == nil {
			sourceAbs = filepath.Clean(abs)
		}
	}

	var written []string
	for _, entry := range AllFormats {
		payload, err := RenderByFormat(cfg, entry.Format, workDir)
		if err != nil {
			return written, fmt.Errorf("render %s: %w", entry.Format, err)
		}
		dest := filepath.Join(projectDir, entry.Path)
		if sourceAbs != "" {
			if destAbs, err := filepath.Abs(dest); err == nil && filepath.Clean(destAbs) == sourceAbs {
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return written, fmt.Errorf("mkdir for %s: %w", entry.Path, err)
		}

		var data []byte
		if s, ok := payload.(string); ok {
			data = []byte(s)
		} else {
			data, err = json.MarshalIndent(payload, "", "  ")
			if err != nil {
				return written, fmt.Errorf("marshal %s: %w", entry.Format, err)
			}
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return written, fmt.Errorf("write %s: %w", entry.Path, err)
		}
		written = append(written, dest)
	}
	return written, nil
}

// ConvertFileFormat converts a file between supported MCP formats.
func ConvertFileFormat(inputPath, outputPath, fromFormat, toFormat string) error {
	cfg, err := ParseCanonicalFileWithFormat(inputPath, fromFormat)
	if err != nil {
		return err
	}
	payload, err := RenderByFormat(cfg, toFormat, "")
	if err != nil {
		return err
	}
	var data []byte
	if s, ok := payload.(string); ok {
		data = []byte(s)
	} else {
		data, err = json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal converted MCP config: %w", err)
		}
	}
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write converted MCP config: %w", err)
	}
	return nil
}

// ConvertFileFormatForPath resolves source path relative to workDir before conversion.
func ConvertFileFormatForPath(inputPath, outputPath, workDir, fromFormat, toFormat string) error {
	sourcePath, err := resolveSourcePath(inputPath, workDir)
	if err != nil {
		return err
	}
	return ConvertFileFormat(sourcePath, outputPath, fromFormat, toFormat)
}

// RenderMistralVibeTOML converts canonical config to Vibe config.toml content.
func RenderMistralVibeTOML(cfg CanonicalConfig, model string) string {
	var buf bytes.Buffer
	if model != "" {
		fmt.Fprintf(&buf, "active_model = %q\n", model)
	}
	buf.WriteString("enable_telemetry = false\n")
	buf.WriteString("enable_auto_update = false\n")

	for name, server := range cfg.MCPServers {
		buf.WriteString("\n[[mcp_servers]]\n")
		fmt.Fprintf(&buf, "name = %q\n", name)

		if isHTTPServer(server) {
			fmt.Fprintf(&buf, "transport = %q\n", "http")
			fmt.Fprintf(&buf, "url = %q\n", remoteURL(server))
		} else {
			fmt.Fprintf(&buf, "transport = %q\n", "stdio")
			fmt.Fprintf(&buf, "command = %q\n", server.Command)
			if len(server.Args) > 0 {
				buf.WriteString("args = [")
				for i, arg := range server.Args {
					if i > 0 {
						buf.WriteString(", ")
					}
					fmt.Fprintf(&buf, "%q", arg)
				}
				buf.WriteString("]\n")
			}
		}
	}

	return buf.String()
}

// WriteJSONFile marshals data as pretty JSON into a file under tempDir.
func WriteJSONFile(tempDir, fileName string, payload any) (string, error) {
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	path := filepath.Join(tempDir, fileName)
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal converted MCP config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write converted MCP config: %w", err)
	}
	return path, nil
}

func resolveSourcePath(mcpConfigPath, workDir string) (string, error) {
	sourcePath := mcpConfigPath
	if strings.HasPrefix(sourcePath, "@") {
		sourcePath = sourcePath[1:]
	}
	absWorkDir, err := normalizeWorkDir(workDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workDir to absolute path: %w", err)
	}
	if !filepath.IsAbs(sourcePath) && absWorkDir != "" {
		sourcePath = filepath.Join(absWorkDir, sourcePath)
	}
	return sourcePath, nil
}

func normalizeWorkDir(workDir string) (string, error) {
	if workDir == "" || filepath.IsAbs(workDir) {
		return workDir, nil
	}
	return filepath.Abs(workDir)
}

func resolveCwd(cwd, absWorkDir string) string {
	if cwd == "" {
		return ""
	}
	if filepath.IsAbs(cwd) || absWorkDir == "" {
		return cwd
	}
	return filepath.Join(absWorkDir, cwd)
}

func remoteURL(server CanonicalServer) string {
	if server.ServerURL != "" {
		return server.ServerURL
	}
	return server.URL
}

func isHTTPServer(server CanonicalServer) bool {
	t := strings.ToLower(server.Type)
	return t == "http" || t == "remote" || t == "sse" || server.URL != "" || server.ServerURL != ""
}

func sanitizeJSONC(data []byte) []byte {
	data = stripUTF8BOM(data)
	data = stripJSONComments(data)
	data = stripTrailingCommas(data)
	return data
}

func stripUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func stripJSONComments(data []byte) []byte {
	var out bytes.Buffer
	inString := false
	escaped := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if inLineComment {
			if c == '\n' {
				inLineComment = false
				out.WriteByte(c)
			}
			continue
		}

		if inBlockComment {
			if c == '*' && i+1 < len(data) && data[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inString {
			out.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}

		if c == '"' {
			inString = true
			out.WriteByte(c)
			continue
		}

		if c == '/' && i+1 < len(data) {
			next := data[i+1]
			if next == '/' {
				inLineComment = true
				i++
				continue
			}
			if next == '*' {
				inBlockComment = true
				i++
				continue
			}
		}

		out.WriteByte(c)
	}

	return out.Bytes()
}

func stripTrailingCommas(data []byte) []byte {
	var out bytes.Buffer
	inString := false
	escaped := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if inString {
			out.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}

		if c == '"' {
			inString = true
			out.WriteByte(c)
			continue
		}

		if c == ',' {
			j := i + 1
			for j < len(data) {
				if data[j] == ' ' || data[j] == '\t' || data[j] == '\n' || data[j] == '\r' {
					j++
					continue
				}
				break
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue
			}
		}

		out.WriteByte(c)
	}

	return out.Bytes()
}
