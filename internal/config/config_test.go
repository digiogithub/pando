package config

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/spf13/viper"
)

func TestMesnadaDefaults(t *testing.T) {
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	configureViper()
	setDefaults(false)

	var loaded Config
	if err := viper.Unmarshal(&loaded); err != nil {
		t.Fatalf("unmarshal defaults: %v", err)
	}

	if got := loaded.Mesnada.Server.Host; got != "127.0.0.1" {
		t.Fatalf("mesnada.server.host = %q, want %q", got, "127.0.0.1")
	}
	if got := loaded.Mesnada.Server.Port; got != 9767 {
		t.Fatalf("mesnada.server.port = %d, want %d", got, 9767)
	}
	if got := loaded.Mesnada.Orchestrator.MaxParallel; got != 5 {
		t.Fatalf("mesnada.orchestrator.maxParallel = %d, want %d", got, 5)
	}
	if got := loaded.Mesnada.Orchestrator.DefaultEngine; got != "copilot" {
		t.Fatalf("mesnada.orchestrator.defaultEngine = %q, want %q", got, "copilot")
	}
	if !loaded.Mesnada.TUI.Enabled {
		t.Fatal("mesnada.tui.enabled = false, want true")
	}
	if !loaded.Mesnada.TUI.WebUI {
		t.Fatal("mesnada.tui.webui = false, want true")
	}
}

func TestValidateAllowsOllamaWithoutAPIKey(t *testing.T) {
	cfg = &Config{
		Providers: map[models.ModelProvider]Provider{
			models.ProviderOllama: {
				BaseURL: "http://localhost:11434/v1",
			},
		},
		Agents: make(map[AgentName]Agent),
		LSP:    make(map[string]LSPConfig),
	}
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	if err := Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.Providers[models.ProviderOllama].Disabled {
		t.Fatal("ollama provider was disabled unexpectedly")
	}
}

func TestValidateDoesNotWriteMissingAPIKeyWarningToStdout(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
	})

	cfg = &Config{
		Providers: map[models.ModelProvider]Provider{
			models.ProviderOpenAI: {},
		},
		Agents: make(map[AgentName]Agent),
		LSP:    make(map[string]LSPConfig),
	}
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	if err := Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout pipe writer: %v", err)
	}
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	if len(output) != 0 {
		t.Fatalf("Validate() wrote to stdout: %q", string(output))
	}

	if !cfg.Providers[models.ProviderOpenAI].Disabled {
		t.Fatal("openai provider should be disabled when API key is missing")
	}
}

func TestOverrideAgentModelUpdatesMemoryOnly(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".pando.toml")
	originalConfig := "[Agents]\n[Agents.coder]\nModel = 'openai.gpt-4.1'\n"
	if err := os.WriteFile(configPath, []byte(originalConfig), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg = &Config{
		WorkingDir: tmpDir,
		Agents: map[AgentName]Agent{
			AgentCoder: {
				Model:     models.AzureGPT41,
				MaxTokens: 1234,
			},
		},
		Providers: map[models.ModelProvider]Provider{
			models.ProviderAzure: {
				APIKey: "test-key",
			},
		},
		LSP: make(map[string]LSPConfig),
	}
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	if err := OverrideAgentModel(AgentCoder, models.AzureGPT41Mini); err != nil {
		t.Fatalf("OverrideAgentModel() error = %v", err)
	}

	if got := cfg.Agents[AgentCoder].Model; got != models.AzureGPT41Mini {
		t.Fatalf("coder model = %q, want %q", got, models.AzureGPT41Mini)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if string(content) != originalConfig {
		t.Fatalf("config file was modified unexpectedly\n got: %q\nwant: %q", string(content), originalConfig)
	}
}

func TestOverrideAgentModelRejectsUnavailableProvider(t *testing.T) {
	cfg = &Config{
		Agents: map[AgentName]Agent{
			AgentCoder: {
				Model: models.AzureGPT41,
			},
		},
		Providers: map[models.ModelProvider]Provider{
			models.ProviderAzure: {
				Disabled: true,
			},
		},
		LSP: make(map[string]LSPConfig),
	}
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	err := OverrideAgentModel(AgentCoder, models.AzureGPT41Mini)
	if err == nil {
		t.Fatal("OverrideAgentModel() error = nil, want provider validation error")
	}
}

func TestResolveProjectInitializationContextPath(t *testing.T) {
	tmpDir := t.TempDir()

	if got := ResolveProjectInitializationContextPath(tmpDir); got != "AGENTS.md" {
		t.Fatalf("default initialization file = %q, want %q", got, "AGENTS.md")
	}

	for _, entry := range []struct {
		name string
		want string
	}{
		{name: "CLAUDE.md", want: "CLAUDE.md"},
		{name: "PANDO.md", want: "PANDO.md"},
		{name: "AGENTS.md", want: "AGENTS.md"},
	} {
		path := filepath.Join(tmpDir, entry.name)
		if err := os.WriteFile(path, []byte(entry.name), 0o644); err != nil {
			t.Fatalf("write %s: %v", entry.name, err)
		}
		if got := ResolveProjectInitializationContextPath(tmpDir); got != entry.want {
			t.Fatalf("after creating %s, ResolveProjectInitializationContextPath() = %q, want %q", entry.name, got, entry.want)
		}
	}
}

func TestLoadSupportsLegacyGlobalConfigYAML(t *testing.T) {
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config", "pando")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	content := []byte(`providers:
  gemini:
    apiKey: test-gemini-key
    disabled: false
agents:
  coder:
    model: gemini.gemini-2.5-pro-preview-05-06
    maxTokens: 4096
`)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write legacy config file: %v", err)
	}

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GEMINI_API_KEY", "")

	loaded, err := Load(t.TempDir(), false)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	providerCfg, ok := loaded.Providers[models.ProviderGemini]
	if !ok {
		t.Fatalf("gemini provider not loaded from legacy config")
	}
	if providerCfg.APIKey != "test-gemini-key" {
		t.Fatalf("gemini api key = %q, want %q", providerCfg.APIKey, "test-gemini-key")
	}

	if got := viper.ConfigFileUsed(); got != configPath {
		t.Fatalf("ConfigFileUsed() = %q, want %q", got, configPath)
	}
}

func TestResolveConfigFilePathPrefersLegacyGlobalConfigYAML(t *testing.T) {
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config", "pando")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("providers: {}\n"), 0o644); err != nil {
		t.Fatalf("write legacy config file: %v", err)
	}

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	if _, err := Load(t.TempDir(), false); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	resolved, err := ResolveConfigFilePath()
	if err != nil {
		t.Fatalf("ResolveConfigFilePath() error = %v", err)
	}
	if resolved != configPath {
		t.Fatalf("ResolveConfigFilePath() = %q, want %q", resolved, configPath)
	}
}

func TestLoadSupportsLegacyConfigYAMLWithTOMLContent(t *testing.T) {
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config", "pando")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	content := []byte(`[Providers.gemini]
APIKey = 'toml-in-yaml-file-key'
Disabled = false

[Agents.coder]
Model = 'gemini.gemini-2.5-pro-preview-05-06'
MaxTokens = 4096
`)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write legacy config file: %v", err)
	}

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GEMINI_API_KEY", "")

	loaded, err := Load(t.TempDir(), false)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	providerCfg, ok := loaded.Providers[models.ProviderGemini]
	if !ok {
		t.Fatalf("gemini provider not loaded from TOML content in config.yaml")
	}
	if providerCfg.APIKey != "toml-in-yaml-file-key" {
		t.Fatalf("gemini api key = %q, want %q", providerCfg.APIKey, "toml-in-yaml-file-key")
	}
}
