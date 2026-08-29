package config

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/spf13/viper"
)

// isolateGlobalConfig points the global-config search at an empty home directory.
// Load() looks for a global config in $HOME and $XDG_CONFIG_HOME (see the
// viper.AddConfigPath calls in setDefaults), so a test that does not do this reads
// the config of whoever runs it: the developer's own ~/.pando.toml wins over the
// defaults under test, and the suite passes or fails depending on the machine.
func isolateGlobalConfig(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
}

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
	if got := loaded.Mesnada.Orchestrator.DefaultEngine; got != "pando" {
		t.Fatalf("mesnada.orchestrator.defaultEngine = %q, want %q", got, "pando")
	}
	if !loaded.Mesnada.TUI.Enabled {
		t.Fatal("mesnada.tui.enabled = false, want true")
	}
	if !loaded.Mesnada.TUI.WebUI {
		t.Fatal("mesnada.tui.webui = false, want true")
	}
}

func TestGoalDefaults(t *testing.T) {
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

	if got := loaded.Goal.MaxIterations; got != 20 {
		t.Fatalf("goal.maxIterations = %d, want %d", got, 20)
	}
	if got := loaded.Goal.MaxDuration; got != "1h" {
		t.Fatalf("goal.maxDuration = %q, want %q", got, "1h")
	}
	if got := loaded.Goal.StallIterations; got != 3 {
		t.Fatalf("goal.stallIterations = %d, want %d", got, 3)
	}
	if !loaded.Goal.AutoApprove {
		t.Fatal("goal.autoApprove = false, want true")
	}
	if len(loaded.Goal.DangerousPatterns) != 0 {
		t.Fatalf("goal.dangerousPatterns = %v, want empty", loaded.Goal.DangerousPatterns)
	}
}

func TestDefaultConfigTemplateKeepsBuiltInContextPathsEnabled(t *testing.T) {
	if strings.Contains(DefaultConfigTemplate, "ContextPaths = []") {
		t.Fatal("DefaultConfigTemplate should not explicitly clear ContextPaths")
	}

	for _, want := range []string{"AGENTS.md", "PANDO.md", "CLAUDE.md"} {
		if !strings.Contains(DefaultConfigTemplate, want) {
			t.Fatalf("DefaultConfigTemplate should mention %s", want)
		}
	}
}

// templateSection returns the body of a [Section] of DefaultConfigTemplate, up to
// the next section header. Assertions are scoped to a section because keys like
// "Enabled" appear in many of them.
func templateSection(t *testing.T, name string) string {
	t.Helper()

	header := "[" + name + "]\n"
	start := strings.Index(DefaultConfigTemplate, header)
	if start < 0 {
		t.Fatalf("DefaultConfigTemplate has no [%s] section", name)
	}
	body := DefaultConfigTemplate[start+len(header):]
	if end := strings.Index(body, "\n["); end >= 0 {
		body = body[:end]
	}
	return body
}

func TestDefaultConfigTemplateEnablesPandoPreferredDefaults(t *testing.T) {
	checks := []struct{ section, key, want string }{
		{"SkillsCatalog", "Enabled", "true"},
		{"SkillsCatalog", "BaseURL", "''"},
		{"SkillsCatalog", "AutoUpdate", "false"},
		{"SkillsCatalog", "DefaultScope", "'global'"},
		{"TUI", "Theme", "'pando-nobg'"},
		{"TUI", "ShowHiddenFiles", "true"},
		{"TUI", "NerdFonts", "true"},
		{"Permissions", "AutoApproveTools", "true"},
		{"Mesnada.Delegation", "Enabled", "true"},
		{"Remembrances", "ContextEnrichmentEnabled", "true"},
		{"Remembrances", "MemoryEnabled", "true"},
		{"Remembrances", "MemoryAutoCapture", "true"},
		{"Remembrances", "KBWikiLinks", "true"},
		{"LLMCache", "Enabled", "true"},
		{"ToolDiscovery", "Enabled", "true"},
		{"InternalTools", "FetchEnabled", "true"},
		{"InternalTools", "BrowserEnabled", "true"},
		{"Evaluator", "Enabled", "true"},
	}

	for _, c := range checks {
		// Some sections align their '=' into a column, so match the assignment
		// rather than an exact substring: what matters is the value, not the
		// whitespace someone used to line the section up.
		assignment := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(c.key) + `\s*=\s*` + regexp.QuoteMeta(c.want) + `\s*$`)
		if !assignment.MatchString(templateSection(t, c.section)) {
			t.Errorf("DefaultConfigTemplate: [%s] should set %s = %s", c.section, c.key, c.want)
		}
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

func TestValidateRejectsInvalidGoalDuration(t *testing.T) {
	cfg = &Config{
		Goal: GoalConfig{
			MaxIterations:   20,
			MaxDuration:     "not-a-duration",
			StallIterations: 3,
			AutoApprove:     true,
		},
		Agents:    make(map[AgentName]Agent),
		LSP:       make(map[string]LSPConfig),
		Providers: make(map[models.ModelProvider]Provider),
	}
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	err := Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want invalid goal duration error")
	}
	if !strings.Contains(err.Error(), "goal.maxDuration") {
		t.Fatalf("Validate() error = %v, want goal.maxDuration error", err)
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

func TestEffectiveContextPathsPrependsPreferredProjectMemory(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("memory"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	got := EffectiveContextPaths(tmpDir, []string{"docs/"})
	want := []string{"AGENTS.md", "docs/"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("EffectiveContextPaths() = %v, want %v", got, want)
	}
}

func TestEffectiveContextPathsKeepsPreferredProjectMemoryWhenAlreadyConfigured(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("memory"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	got := EffectiveContextPaths(tmpDir, []string{"AGENTS.md", "docs/"})
	want := []string{"AGENTS.md", "docs/"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("EffectiveContextPaths() = %v, want %v", got, want)
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

func TestMigrateProvidersToAccounts(t *testing.T) {
	t.Cleanup(func() { cfg = nil; viper.Reset() })

	cfg = &Config{
		Providers: map[models.ModelProvider]Provider{
			models.ProviderAnthropic: {APIKey: "sk-ant-test"},
			models.ProviderOpenAI:    {APIKey: "sk-openai-test", BaseURL: "https://api.openai.com"},
		},
		Agents: make(map[AgentName]Agent),
		LSP:    make(map[string]LSPConfig),
	}

	migrateProvidersToAccounts(cfg)

	if len(cfg.ProviderAccounts) != 2 {
		t.Fatalf("expected 2 accounts after migration, got %d", len(cfg.ProviderAccounts))
	}

	var anthAcc *ProviderAccount
	for i := range cfg.ProviderAccounts {
		if cfg.ProviderAccounts[i].ID == "anthropic" {
			anthAcc = &cfg.ProviderAccounts[i]
		}
	}
	if anthAcc == nil {
		t.Fatal("expected account with ID 'anthropic' after migration")
	}
	if anthAcc.APIKey != "sk-ant-test" {
		t.Fatalf("expected APIKey 'sk-ant-test', got %q", anthAcc.APIKey)
	}
	if anthAcc.Type != models.ProviderAnthropic {
		t.Fatalf("expected Type %q, got %q", models.ProviderAnthropic, anthAcc.Type)
	}
}

func TestMigrateProvidersToAccountsSkipsIfAlreadyMigrated(t *testing.T) {
	t.Cleanup(func() { cfg = nil; viper.Reset() })

	cfg = &Config{
		Providers: map[models.ModelProvider]Provider{
			models.ProviderAnthropic: {APIKey: "sk-old"},
		},
		ProviderAccounts: []ProviderAccount{
			{ID: "my-account", Type: models.ProviderAnthropic, APIKey: "sk-new"},
		},
		Agents: make(map[AgentName]Agent),
		LSP:    make(map[string]LSPConfig),
	}

	migrateProvidersToAccounts(cfg)

	if len(cfg.ProviderAccounts) != 1 {
		t.Fatalf("expected 1 account (no migration), got %d", len(cfg.ProviderAccounts))
	}
	if cfg.ProviderAccounts[0].APIKey != "sk-new" {
		t.Fatal("migration incorrectly overwrote existing ProviderAccounts")
	}
}

func TestGetProviderAccountCRUD(t *testing.T) {
	t.Cleanup(func() { cfg = nil; viper.Reset() })

	cfg = &Config{
		ProviderAccounts: []ProviderAccount{},
		Agents:           make(map[AgentName]Agent),
		LSP:              make(map[string]LSPConfig),
	}

	// Directly add to bypass disk writes in tests
	cfg.ProviderAccounts = append(cfg.ProviderAccounts, ProviderAccount{
		ID:          "test-anthropic",
		DisplayName: "Test Anthropic",
		Type:        models.ProviderAnthropic,
		APIKey:      "sk-ant-xxx",
	})

	found, ok := GetProviderAccount("test-anthropic")
	if !ok {
		t.Fatal("expected to find account 'test-anthropic'")
	}
	if found.APIKey != "sk-ant-xxx" {
		t.Fatalf("expected APIKey 'sk-ant-xxx', got %q", found.APIKey)
	}

	all := GetProviderAccounts()
	if len(all) != 1 {
		t.Fatalf("expected 1 account, got %d", len(all))
	}

	byType := AccountsForProviderType(models.ProviderAnthropic)
	if len(byType) != 1 {
		t.Fatalf("expected 1 anthropic account, got %d", len(byType))
	}

	_, notFound := GetProviderAccount("nonexistent")
	if notFound {
		t.Fatal("expected not to find account 'nonexistent'")
	}
}

func configTestSetGlobalConfig(c *Config) {
	cfg = c
}

func TestProviderOpenAICompatibleExists(t *testing.T) {
	if models.ProviderOpenAICompatible != "openai-compatible" {
		t.Fatalf("expected ProviderOpenAICompatible = 'openai-compatible', got %q", models.ProviderOpenAICompatible)
	}
}

func TestProviderAntigravityExists(t *testing.T) {
	if models.ProviderAntigravity != "antigravity" {
		t.Fatalf("expected ProviderAntigravity = 'antigravity', got %q", models.ProviderAntigravity)
	}
}

func TestProviderAccountRequiresAPIKeyAntigravity(t *testing.T) {
	if providerAccountRequiresAPIKey(models.ProviderAntigravity) {
		t.Fatal("expected antigravity provider accounts not to require an API key")
	}
}

func TestProviderRequiresAPIKeyAntigravity(t *testing.T) {
	if providerRequiresAPIKey(models.ProviderAntigravity) {
		t.Fatal("expected antigravity provider not to require an API key")
	}
}

func TestLLMCacheConfigDefault(t *testing.T) {
	// Test that LLMCacheConfig has correct zero value behavior.
	// Zero value of bool is false, so without initialization it should be false.
	c := &Config{}
	if c.LLMCache.Enabled {
		t.Error("expected LLMCacheConfig.Enabled to be false by default (zero value)")
	}
}

func TestUpdateLLMCache(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()

	cfg = &Config{
		LLMCache: LLMCacheConfig{Enabled: true},
	}

	// Test disabling
	cfg.LLMCache.Enabled = false
	if cfg.LLMCache.Enabled {
		t.Error("expected Enabled to be false after setting to false")
	}

	// Test enabling
	cfg.LLMCache.Enabled = true
	if !cfg.LLMCache.Enabled {
		t.Error("expected Enabled to be true after setting to true")
	}
}

// TestChatSidebarEnabled verifies the accessor returns false only for "off" and
// true for "auto", empty string, or unknown values.
func TestChatSidebarEnabled(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()

	tests := []struct {
		name     string
		sidebar  string
		wantTrue bool
	}{
		{"off-lowercase", "off", false},
		{"off-uppercase", "OFF", false},
		{"off-mixed", "Off", false},
		{"auto", "auto", true},
		{"empty", "", true},
		{"unknown", "always", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg = &Config{TUI: TUIConfig{ChatSidebar: tc.sidebar}}
			if got := ChatSidebarEnabled(); got != tc.wantTrue {
				t.Fatalf("ChatSidebarEnabled() = %v, want %v (ChatSidebar=%q)", got, tc.wantTrue, tc.sidebar)
			}
		})
	}
}

// TestChatSidebarEnabledNilConfig verifies the function returns true (enabled)
// when the config singleton has not been loaded.
func TestChatSidebarEnabledNilConfig(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()

	cfg = nil
	if !ChatSidebarEnabled() {
		t.Fatal("ChatSidebarEnabled() = false, want true when config is nil")
	}
}

// TestChatSidebarMinWidthDefault verifies the resolver falls back to 120 when
// the config value is 0 or the config is nil.
func TestChatSidebarMinWidthDefault(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()

	// Unset (zero) value must resolve to default.
	cfg = &Config{TUI: TUIConfig{ChatSidebarMinWidth: 0}}
	if got := ChatSidebarMinWidth(); got != defaultChatSidebarMinWidth {
		t.Fatalf("ChatSidebarMinWidth() = %d, want %d (zero value)", got, defaultChatSidebarMinWidth)
	}

	// Nil config must also resolve to default.
	cfg = nil
	if got := ChatSidebarMinWidth(); got != defaultChatSidebarMinWidth {
		t.Fatalf("ChatSidebarMinWidth() = %d, want %d (nil config)", got, defaultChatSidebarMinWidth)
	}
}

// TestChatSidebarMinWidthExplicit verifies the resolver returns the configured
// value when it is a positive integer.
func TestChatSidebarMinWidthExplicit(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()

	cfg = &Config{TUI: TUIConfig{ChatSidebarMinWidth: 160}}
	if got := ChatSidebarMinWidth(); got != 160 {
		t.Fatalf("ChatSidebarMinWidth() = %d, want 160", got)
	}
}

// TestValidateDisablesEvaluatorWithoutModel is the regression guard for the
// "pando init generates a config that fails to start" report. The generated
// default template enables the evaluator, and on a fresh install with no provider
// configured no coder model can be resolved to seed the evaluator model. Validate
// must NOT abort in that state (which made Pando fail to start): it degrades by
// disabling the evaluator in memory until a model becomes available.
func TestValidateDisablesEvaluatorWithoutModel(t *testing.T) {
	cfg = &Config{
		Providers: make(map[models.ModelProvider]Provider),
		Agents:    make(map[AgentName]Agent),
		LSP:       make(map[string]LSPConfig),
		Evaluator: EvaluatorConfig{Enabled: true},
	}
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	if err := Validate(); err != nil {
		t.Fatalf("Validate() must not error when evaluator has no model, got: %v", err)
	}
	if cfg.Evaluator.Enabled {
		t.Fatal("evaluator should be disabled at runtime when no model is available")
	}
}

// TestValidateAgentWithoutProviderDoesNotError guards the sibling hard-error path:
// an agent whose model resolves to a provider that is present but unusable (no key,
// no credentials) and with no fallback provider available must not abort Validate.
func TestValidateAgentWithoutProviderDoesNotError(t *testing.T) {
	isolateGlobalConfig(t)
	for _, k := range []string{
		"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "OPENAI_API_KEY", "GEMINI_API_KEY",
		"GROQ_API_KEY", "OPENROUTER_API_KEY", "XAI_API_KEY",
	} {
		t.Setenv(k, "")
	}

	cfg = &Config{
		Providers: map[models.ModelProvider]Provider{
			models.ProviderAnthropic: {APIKey: "", Disabled: true},
		},
		Agents: map[AgentName]Agent{
			AgentCoder: {Model: models.Claude4Sonnet},
		},
		LSP: make(map[string]LSPConfig),
	}
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	if err := Validate(); err != nil {
		t.Fatalf("Validate() must not error when an agent has no usable provider, got: %v", err)
	}
}

// ---- R3: partial [InternalTools] table config-merge regression tests ----
//
// Phase 8 of the Desktop Controller feature (pando/changes/
// uiauto_exposure_docs_phase8.md) recorded an "incidental finding": a
// project .pando.toml [InternalTools] table that sets only DesktopEnabled
// observably lost the DesktopAllowPhysicalInput=true default, attributed to
// mergeLocalConfig's viper.MergeConfigMap not "reliably preserving
// unrelated global defaults for sibling keys in that same nested table".
//
// Investigating for Block R (2026-08-30): the suspected mechanism is
// viper's flattenAndMergeMap prefix-shadowing (used by both AllKeys() and
// Unmarshal(), which calls v.getSettings(v.AllKeys())) -- a higher-priority
// layer can shadow an entire subtree for a lower-priority layer IF it holds
// an IMMEDIATE (non-map) value at the exact parent key path. A partial TOML
// table under a single map (e.g. internalTools.desktopEnabled without
// internalTools.desktopAllowPhysicalInput) never creates that condition:
// the parent key ("internaltools") is always a map at every layer that
// defines it, never a scalar, so nothing shadows the defaults layer's
// leaves. This test reproduces the exact documented scenario against the
// currently vendored github.com/spf13/viper (go.mod pins v1.20.0) through
// the real Load() entry point end to end, including mergeLocalConfig.
//
// It currently PASSES: DesktopAllowPhysicalInput correctly reads back true.
// This is kept as a permanent regression test (not deleted just because it
// passes) so a future change to mergeLocalConfig/viper's merge behavior
// that reintroduces the defect fails loudly, and so the finding is backed
// by an executable check rather than only a KB note.
func TestPartialInternalToolsTablePreservesDesktopDefaults(t *testing.T) {
	isolateGlobalConfig(t)
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	dir := t.TempDir()
	toml := "[InternalTools]\nDesktopEnabled = true\n"
	if err := os.WriteFile(filepath.Join(dir, ".pando.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	it := loaded.InternalTools
	if !it.DesktopEnabled {
		t.Fatal("DesktopEnabled should be true (explicitly set in the project config)")
	}
	if !it.DesktopAllowPhysicalInput {
		t.Fatal("DesktopAllowPhysicalInput lost its documented true default when only DesktopEnabled was set in a partial [InternalTools] table")
	}
	if it.DesktopBackend != "auto" {
		t.Fatalf("DesktopBackend = %q, want the documented default %q", it.DesktopBackend, "auto")
	}
	if it.DesktopMaxNodes != 500 {
		t.Fatalf("DesktopMaxNodes = %d, want the documented default 500", it.DesktopMaxNodes)
	}
	if it.DesktopDefaultDepth != 3 {
		t.Fatalf("DesktopDefaultDepth = %d, want the documented default 3", it.DesktopDefaultDepth)
	}
	if it.DesktopActionTimeout != 10 {
		t.Fatalf("DesktopActionTimeout = %d, want the documented default 10", it.DesktopActionTimeout)
	}
	if it.DesktopSnapshotTTL != 60 {
		t.Fatalf("DesktopSnapshotTTL = %d, want the documented default 60", it.DesktopSnapshotTTL)
	}
	if it.DesktopScreenshotScale != 1.0 {
		t.Fatalf("DesktopScreenshotScale = %v, want the documented default 1.0", it.DesktopScreenshotScale)
	}
}

// TestPartialInternalToolsTableExplicitFalseOverridesDefault guards the
// other direction of the same mechanism: an explicit false in the partial
// table must NOT be shadowed back to the true default -- i.e. the fix for
// R3 (in this case: confirming no fix was needed, see above) must not
// accidentally make DesktopAllowPhysicalInput un-overridable.
func TestPartialInternalToolsTableExplicitFalseOverridesDefault(t *testing.T) {
	isolateGlobalConfig(t)
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	dir := t.TempDir()
	toml := "[InternalTools]\nDesktopEnabled = true\nDesktopAllowPhysicalInput = false\n"
	if err := os.WriteFile(filepath.Join(dir, ".pando.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.InternalTools.DesktopAllowPhysicalInput {
		t.Fatal("an explicit DesktopAllowPhysicalInput = false in the project config must not be overridden back to the default")
	}
}

// TestPartialMesnadaDelegationTablePreservesDefaults checks the other
// partial-table scenario this codebase already suspected the same class of
// bug for (see the normalizeMesnadaDelegationDefaults doc comment): a
// [Mesnada] table that sets a sibling key (Enabled) without a
// [Mesnada.Delegation] sub-table. This is the two-level-nesting case
// (mesnada -> delegation -> field), a stronger version of R3's single-level
// InternalTools/Desktop* case. It also currently passes against the
// vendored viper -- normalizeMesnadaDelegationDefaults's own zero-value
// workaround is not even required for this to come back correct via a
// fresh Load(), which confirms R3's finding generalizes: this class of
// "sibling key without the nested table" partial-config defect is not
// currently reproducible at the viper-merge level with this dependency
// version, in either the one-level or two-level nesting shape.
func TestPartialMesnadaDelegationTablePreservesDefaults(t *testing.T) {
	isolateGlobalConfig(t)
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	dir := t.TempDir()
	toml := "[Mesnada]\nEnabled = true\n"
	if err := os.WriteFile(filepath.Join(dir, ".pando.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.Mesnada.Enabled {
		t.Fatal("Mesnada.Enabled should be true (explicitly set)")
	}
	if loaded.Mesnada.Delegation.MaxResurrections != defaultDelegationMaxResurrections {
		t.Fatalf("Mesnada.Delegation.MaxResurrections = %d, want the documented default %d",
			loaded.Mesnada.Delegation.MaxResurrections, defaultDelegationMaxResurrections)
	}
	if loaded.Mesnada.Delegation.MaxDepth != defaultDelegationMaxDepth {
		t.Fatalf("Mesnada.Delegation.MaxDepth = %d, want the documented default %d",
			loaded.Mesnada.Delegation.MaxDepth, defaultDelegationMaxDepth)
	}
}
