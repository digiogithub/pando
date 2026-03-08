package page

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	pandoapp "github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/tui/components/settings"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

type settingsPage struct {
	width    int
	height   int
	app      *pandoapp.App
	settings settings.SettingsCmp
}

func (p *settingsPage) Init() tea.Cmd {
	return p.settings.Init()
}

func (p *settingsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		return p, p.SetSize(msg.Width, msg.Height)
	case settings.SaveFieldMsg:
		return p, p.saveField(msg)
	}

	updated, cmd := p.settings.Update(msg)
	p.settings = updated.(settings.SettingsCmp)
	return p, cmd
}

func (p *settingsPage) View() string {
	return p.settings.View()
}

func (p *settingsPage) SetSize(width, height int) tea.Cmd {
	p.width = width
	p.height = height
	p.settings.SetSize(width, height)
	return nil
}

func (p *settingsPage) GetSize() (int, int) {
	return p.width, p.height
}

func NewSettingsPage(app *pandoapp.App) tea.Model {
	cmp := settings.NewSettingsCmp()
	cmp.SetSections(buildSections(app))
	return &settingsPage{app: app, settings: cmp}
}

func (p *settingsPage) saveField(msg settings.SaveFieldMsg) tea.Cmd {
	if err := persistSetting(p.app, msg.Field); err != nil {
		return util.ReportError(err)
	}

	p.settings.SetSections(buildSections(p.app))
	p.settings.SetSize(p.width, p.height)
	p.settings.SetActiveField(msg.SectionTitle, savedFieldKey(msg.Field))
	return util.ReportInfo("Setting saved: " + msg.Field.Label)
}

func buildSections(app *pandoapp.App) []settings.Section {
	cfg := config.Get()
	if cfg == nil {
		return nil
	}

	return []settings.Section{
		buildGeneralSection(cfg),
		buildSkillsSection(app, cfg),
		buildProvidersSection(cfg),
		buildAgentsSection(cfg),
		buildMCPServersSection(cfg),
		buildLSPSection(cfg),
		buildMesnadaSection(cfg),
	}
}

func buildGeneralSection(cfg *config.Config) settings.Section {
	currentTheme := strings.TrimSpace(cfg.TUI.Theme)
	if currentTheme == "" {
		currentTheme = theme.CurrentThemeName()
	}

	themeOptions := ensureOption(theme.AvailableThemes(), currentTheme)

	return settings.Section{
		Title: "General",
		Fields: []settings.Field{
			{
				Label:   "Theme",
				Key:     "tui.theme",
				Value:   currentTheme,
				Type:    settings.FieldSelect,
				Options: themeOptions,
			},
			{
				Label: "AutoCompact",
				Key:   "autoCompact",
				Value: boolString(cfg.AutoCompact),
				Type:  settings.FieldToggle,
			},
			{
				Label: "Debug",
				Key:   "debug",
				Value: boolString(cfg.Debug),
				Type:  settings.FieldToggle,
			},
			{
				Label: "Shell Path",
				Key:   "shell.path",
				Value: cfg.Shell.Path,
				Type:  settings.FieldText,
			},
			{
				Label: "Shell Args",
				Key:   "shell.args",
				Value: strings.Join(cfg.Shell.Args, " "),
				Type:  settings.FieldText,
			},
		},
	}
}

func buildProvidersSection(cfg *config.Config) settings.Section {
	providerOrder := []models.ModelProvider{
		models.ProviderAnthropic,
		models.ProviderOpenAI,
		models.ProviderOllama,
		models.ProviderGemini,
		models.ProviderGROQ,
		models.ProviderOpenRouter,
		models.ProviderXAI,
		models.ProviderCopilot,
	}

	fields := make([]settings.Field, 0, len(providerOrder)*2)
	for _, providerID := range providerOrder {
		providerCfg, ok := cfg.Providers[providerID]
		if !ok {
			providerCfg.Disabled = true
		}
		providerName := string(providerID)

		if providerID == models.ProviderCopilot {
			status := auth.GetCopilotAuthStatus().Message

			fields = append(fields,
				settings.Field{
					Label:    fmt.Sprintf("%s Auth", providerName),
					Key:      fmt.Sprintf("providers.%s.auth", providerName),
					Value:    status,
					Type:     settings.FieldText,
					ReadOnly: true,
				},
				settings.Field{
					Label: fmt.Sprintf("%s Enabled", providerName),
					Key:   fmt.Sprintf("providers.%s.enabled", providerName),
					Value: boolString(!providerCfg.Disabled),
					Type:  settings.FieldToggle,
				},
			)
			continue
		}

		if providerID == models.ProviderOllama {
			if strings.TrimSpace(providerCfg.BaseURL) == "" {
				providerCfg.BaseURL = models.ResolveOllamaBaseURL("")
			}

			fields = append(fields,
				settings.Field{
					Label: fmt.Sprintf("%s Base URL", providerName),
					Key:   fmt.Sprintf("providers.%s.baseURL", providerName),
					Value: providerCfg.BaseURL,
					Type:  settings.FieldText,
				},
				settings.Field{
					Label:  fmt.Sprintf("%s API Key", providerName),
					Key:    fmt.Sprintf("providers.%s.apiKey", providerName),
					Value:  providerCfg.APIKey,
					Type:   settings.FieldText,
					Masked: true,
				},
				settings.Field{
					Label: fmt.Sprintf("%s Enabled", providerName),
					Key:   fmt.Sprintf("providers.%s.enabled", providerName),
					Value: boolString(!providerCfg.Disabled),
					Type:  settings.FieldToggle,
				},
			)
			continue
		}

		fields = append(fields,
			settings.Field{
				Label:  fmt.Sprintf("%s API Key", providerName),
				Key:    fmt.Sprintf("providers.%s.apiKey", providerName),
				Value:  providerCfg.APIKey,
				Type:   settings.FieldText,
				Masked: true,
			},
			settings.Field{
				Label: fmt.Sprintf("%s Enabled", providerName),
				Key:   fmt.Sprintf("providers.%s.enabled", providerName),
				Value: boolString(!providerCfg.Disabled),
				Type:  settings.FieldToggle,
			},
		)
	}

	return settings.Section{
		Title:  "Providers",
		Fields: fields,
	}
}

func buildSkillsSection(app *pandoapp.App, cfg *config.Config) settings.Section {
	fields := []settings.Field{
		{
			Label: "Skills Enabled",
			Key:   "skills.enabled",
			Value: boolString(cfg.Skills.Enabled),
			Type:  settings.FieldToggle,
		},
	}

	if !cfg.Skills.Enabled || app == nil || app.SkillManager == nil {
		fields = append(fields, settings.Field{
			Label:    "Info",
			Key:      "skills.info.disabled",
			Value:    "Skills system disabled. Enable in General > Skills Enabled",
			Type:     settings.FieldText,
			ReadOnly: true,
		})
		return settings.Section{Title: "Skills", Fields: fields}
	}

	metadata := app.SkillManager.GetAllMetadata()
	if len(metadata) == 0 {
		fields = append(fields, settings.Field{
			Label:    "Info",
			Key:      "skills.info.empty",
			Value:    "No skills found. Place SKILL.md files in ~/.pando/skills/ or .pando/skills/",
			Type:     settings.FieldText,
			ReadOnly: true,
		})
		return settings.Section{Title: "Skills", Fields: fields}
	}

	for _, m := range metadata {
		displayName := m.Name
		if version := strings.TrimSpace(m.Version); version != "" {
			displayName = fmt.Sprintf("%s v%s", m.Name, version)
		}

		description := strings.TrimSpace(m.Description)
		if description == "" {
			description = "(no description)"
		}

		isLoaded := app.SkillManager.IsLoaded(m.Name)
		fields = append(fields,
			settings.Field{
				Label:    displayName,
				Key:      "skills.meta." + m.Name,
				Value:    description,
				Type:     settings.FieldText,
				ReadOnly: true,
			},
			settings.Field{
				Label:    fmt.Sprintf("%s Status", m.Name),
				Key:      "skills.status." + m.Name,
				Value:    skillLoadStatus(isLoaded),
				Type:     settings.FieldText,
				ReadOnly: true,
			},
			settings.Field{
				Label: fmt.Sprintf("%s Active", m.Name),
				Key:   "skill_" + m.Name,
				Value: boolString(isLoaded),
				Type:  settings.FieldToggle,
			},
		)
	}

	return settings.Section{
		Title:  "Skills",
		Fields: fields,
	}
}

func buildAgentsSection(cfg *config.Config) settings.Section {
	agentOrder := []config.AgentName{
		config.AgentCoder,
		config.AgentSummarizer,
		config.AgentTask,
		config.AgentTitle,
	}

	modelOptions := supportedModelOptions(cfg)
	fields := make([]settings.Field, 0, len(agentOrder))
	for _, agentName := range agentOrder {
		agentCfg := cfg.Agents[agentName]
		modelID := string(agentCfg.Model)

		fields = append(fields, settings.Field{
			Label:   fmt.Sprintf("%s Model", string(agentName)),
			Key:     fmt.Sprintf("agents.%s.model", agentName),
			Value:   modelID,
			Type:    settings.FieldSelect,
			Options: ensureOption(modelOptions, modelID),
		})
	}

	return settings.Section{
		Title:  "Agents/Models",
		Fields: fields,
	}
}

func buildMCPServersSection(cfg *config.Config) settings.Section {
	serverNames := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)

	fields := make([]settings.Field, 0, len(serverNames)*5)
	for _, name := range serverNames {
		server := cfg.MCPServers[name]
		serverType := string(server.Type)
		if serverType == "" {
			serverType = string(config.MCPStdio)
		}

		fields = append(fields,
			settings.Field{
				Label: fmt.Sprintf("%s Name", name),
				Key:   fmt.Sprintf("mcpServers.%s.name", name),
				Value: name,
				Type:  settings.FieldText,
			},
			settings.Field{
				Label: fmt.Sprintf("%s Command", name),
				Key:   fmt.Sprintf("mcpServers.%s.command", name),
				Value: server.Command,
				Type:  settings.FieldText,
			},
			settings.Field{
				Label: fmt.Sprintf("%s Args", name),
				Key:   fmt.Sprintf("mcpServers.%s.args", name),
				Value: strings.Join(server.Args, " "),
				Type:  settings.FieldText,
			},
			settings.Field{
				Label:   fmt.Sprintf("%s Type", name),
				Key:     fmt.Sprintf("mcpServers.%s.type", name),
				Value:   serverType,
				Type:    settings.FieldSelect,
				Options: ensureOption([]string{string(config.MCPStdio), string(config.MCPSse)}, serverType),
			},
		)

		if server.Type == config.MCPSse {
			fields = append(fields, settings.Field{
				Label: fmt.Sprintf("%s URL", name),
				Key:   fmt.Sprintf("mcpServers.%s.url", name),
				Value: server.URL,
				Type:  settings.FieldText,
			})
		}
	}

	return settings.Section{
		Title:  "MCP Servers",
		Fields: fields,
	}
}

func buildLSPSection(cfg *config.Config) settings.Section {
	languages := make([]string, 0, len(cfg.LSP))
	for language := range cfg.LSP {
		languages = append(languages, language)
	}
	sort.Strings(languages)

	fields := make([]settings.Field, 0, len(languages)*3)
	for _, language := range languages {
		lspCfg := cfg.LSP[language]
		fields = append(fields,
			settings.Field{
				Label: fmt.Sprintf("%s Language", language),
				Key:   fmt.Sprintf("lsp.%s.language", language),
				Value: language,
				Type:  settings.FieldText,
			},
			settings.Field{
				Label: fmt.Sprintf("%s Command", language),
				Key:   fmt.Sprintf("lsp.%s.command", language),
				Value: lspCfg.Command,
				Type:  settings.FieldText,
			},
			settings.Field{
				Label: fmt.Sprintf("%s Enabled", language),
				Key:   fmt.Sprintf("lsp.%s.enabled", language),
				Value: boolString(!lspCfg.Disabled),
				Type:  settings.FieldToggle,
			},
		)
	}

	return settings.Section{
		Title:  "LSP",
		Fields: fields,
	}
}

func buildMesnadaSection(cfg *config.Config) settings.Section {
	fields := []settings.Field{
		{Label: "Enabled", Key: "mesnada.enabled", Type: settings.FieldToggle, Value: fmt.Sprint(cfg.Mesnada.Enabled)},
		{Label: "Server Host", Key: "mesnada.server.host", Type: settings.FieldText, Value: cfg.Mesnada.Server.Host},
		{Label: "Server Port", Key: "mesnada.server.port", Type: settings.FieldText, Value: fmt.Sprint(cfg.Mesnada.Server.Port)},
		{Label: "Max Parallel", Key: "mesnada.orchestrator.maxParallel", Type: settings.FieldText, Value: fmt.Sprint(cfg.Mesnada.Orchestrator.MaxParallel)},
		{
			Label:   "Default Engine",
			Key:     "mesnada.orchestrator.defaultEngine",
			Type:    settings.FieldSelect,
			Value:   cfg.Mesnada.Orchestrator.DefaultEngine,
			Options: []string{"copilot", "claude", "gemini", "opencode", "mistral"},
		},
		{Label: "Persona Path", Key: "mesnada.orchestrator.personaPath", Type: settings.FieldText, Value: cfg.Mesnada.Orchestrator.PersonaPath},
		{Label: "ACP Enabled", Key: "mesnada.acp.enabled", Type: settings.FieldToggle, Value: fmt.Sprint(cfg.Mesnada.ACP.Enabled)},
		{Label: "ACP Auto Permission", Key: "mesnada.acp.autoPermission", Type: settings.FieldToggle, Value: fmt.Sprint(cfg.Mesnada.ACP.AutoPermission)},
	}

	if !cfg.Mesnada.Enabled {
		for i := 1; i < len(fields); i++ {
			fields[i].Disabled = true
		}
	}

	return settings.Section{
		Title:  "Mesnada (Orchestrator)",
		Fields: fields,
	}
}

func supportedModelOptions(cfg *config.Config) []string {
	availableProviders := make(map[models.ModelProvider]struct{})
	for providerID, providerCfg := range cfg.Providers {
		if providerCfg.Disabled {
			continue
		}

		availableProviders[providerID] = struct{}{}
	}

	modelList := make([]models.Model, 0, len(models.SupportedModels))
	for _, model := range models.SupportedModels {
		if _, ok := availableProviders[model.Provider]; ok {
			modelList = append(modelList, model)
		}
	}

	sort.Slice(modelList, func(i, j int) bool {
		leftRank := providerRank(modelList[i].Provider)
		rightRank := providerRank(modelList[j].Provider)
		if leftRank != rightRank {
			return leftRank < rightRank
		}

		if modelList[i].Name != modelList[j].Name {
			return modelList[i].Name > modelList[j].Name
		}

		return modelList[i].ID < modelList[j].ID
	})

	options := make([]string, 0, len(modelList))
	for _, model := range modelList {
		options = append(options, string(model.ID))
	}

	return options
}

func providerRank(provider models.ModelProvider) int {
	rank := models.ProviderPopularity[provider]
	if rank == 0 {
		return 999
	}
	return rank
}

func ensureOption(options []string, value string) []string {
	if strings.TrimSpace(value) == "" {
		return append([]string(nil), options...)
	}

	for _, option := range options {
		if option == value {
			return append([]string(nil), options...)
		}
	}

	withValue := append([]string(nil), options...)
	return append(withValue, value)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func skillLoadStatus(loaded bool) string {
	if loaded {
		return "loaded"
	}
	return "unloaded"
}

func persistSetting(app *pandoapp.App, field settings.Field) error {
	switch {
	case field.Key == "tui.theme":
		return saveTheme(field)
	case field.Key == "autoCompact":
		value, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid AutoCompact value: %w", err)
		}
		return config.UpdateAutoCompact(value)
	case field.Key == "debug":
		value, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Debug value: %w", err)
		}
		return config.UpdateDebug(value)
	case field.Key == "skills.enabled":
		value, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Skills Enabled value: %w", err)
		}
		return config.UpdateSkillsEnabled(value)
	case strings.HasPrefix(field.Key, "skill_"):
		return saveSkill(app, field)
	case strings.HasPrefix(field.Key, "shell."):
		return saveShell(field)
	case strings.HasPrefix(field.Key, "providers."):
		return saveProvider(field)
	case strings.HasPrefix(field.Key, "agents."):
		return saveAgent(field)
	case strings.HasPrefix(field.Key, "mcpServers."):
		return saveMCPServer(field)
	case strings.HasPrefix(field.Key, "lsp."):
		return saveLSP(field)
	case strings.HasPrefix(field.Key, "mesnada."):
		return saveMesnada(field)
	default:
		return fmt.Errorf("unsupported setting %q", field.Key)
	}
}

func saveSkill(app *pandoapp.App, field settings.Field) error {
	cfg := config.Get()
	if cfg == nil || !cfg.Skills.Enabled || app == nil || app.SkillManager == nil {
		return fmt.Errorf("skills system not enabled")
	}

	enabled, err := parseBoolValue(field.Value)
	if err != nil {
		return fmt.Errorf("invalid skill activation value: %w", err)
	}

	skillName := strings.TrimPrefix(field.Key, "skill_")
	if strings.TrimSpace(skillName) == "" {
		return fmt.Errorf("invalid skill setting key %q", field.Key)
	}

	if err := app.SkillManager.SetLoaded(skillName, enabled); err != nil {
		return fmt.Errorf("update skill %s: %w", skillName, err)
	}

	return nil
}

func saveTheme(field settings.Field) error {
	themeName := strings.TrimSpace(field.Value)
	if themeName == "" {
		return fmt.Errorf("theme cannot be empty")
	}
	if theme.GetTheme(themeName) == nil {
		return fmt.Errorf("theme %q not found", themeName)
	}

	if err := config.UpdateTheme(themeName); err != nil {
		return err
	}

	return theme.ApplyTheme(themeName)
}

func saveShell(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	path := strings.TrimSpace(cfg.Shell.Path)
	args := append([]string(nil), cfg.Shell.Args...)
	switch field.Key {
	case "shell.path":
		path = strings.TrimSpace(field.Value)
	case "shell.args":
		args = strings.Fields(field.Value)
	default:
		return fmt.Errorf("unsupported shell setting %q", field.Key)
	}

	if _, err := exec.LookPath(path); err != nil {
		return fmt.Errorf("shell path %q not found: %w", path, err)
	}

	return config.UpdateShell(path, args)
}

func saveProvider(field settings.Field) error {
	parts := strings.Split(field.Key, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid provider setting key %q", field.Key)
	}

	providerName := models.ModelProvider(parts[1])
	providerCfg := config.Get().Providers[providerName]
	switch parts[2] {
	case "apiKey":
		providerCfg.APIKey = strings.TrimSpace(field.Value)
	case "baseURL":
		providerCfg.BaseURL = strings.TrimSpace(field.Value)
	case "enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid provider enabled value: %w", err)
		}
		providerCfg.Disabled = !enabled
	default:
		return fmt.Errorf("unsupported provider field %q", parts[2])
	}

	if !providerCfg.Disabled {
		if providerName == models.ProviderCopilot {
			return config.UpdateProvider(providerName, providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Disabled)
		}
		if providerName == models.ProviderOllama && strings.TrimSpace(providerCfg.BaseURL) == "" {
			return fmt.Errorf("provider %s requires a non-empty base URL when enabled", providerName)
		}
		if providerName != models.ProviderOllama && strings.TrimSpace(providerCfg.APIKey) == "" {
			return fmt.Errorf("provider %s requires a non-empty API key when enabled", providerName)
		}
	}

	return config.UpdateProvider(providerName, providerCfg.APIKey, providerCfg.BaseURL, providerCfg.Disabled)
}

func saveAgent(field settings.Field) error {
	parts := strings.Split(field.Key, ".")
	if len(parts) != 3 || parts[2] != "model" {
		return fmt.Errorf("invalid agent setting key %q", field.Key)
	}

	modelID := models.ModelID(strings.TrimSpace(field.Value))
	if _, ok := models.SupportedModels[modelID]; !ok {
		return fmt.Errorf("model %s not supported", modelID)
	}

	return config.UpdateAgentModel(config.AgentName(parts[1]), modelID)
}

func saveMCPServer(field settings.Field) error {
	parts := strings.Split(field.Key, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid MCP server setting key %q", field.Key)
	}

	serverName := parts[1]
	server, ok := config.Get().MCPServers[serverName]
	if !ok {
		return fmt.Errorf("MCP server %s not found", serverName)
	}

	switch parts[2] {
	case "name":
		newName := strings.TrimSpace(field.Value)
		if newName == "" {
			return fmt.Errorf("MCP server name cannot be empty")
		}
		if newName != serverName {
			if _, exists := config.Get().MCPServers[newName]; exists {
				return fmt.Errorf("MCP server %s already exists", newName)
			}
			if err := config.UpdateMCPServer(newName, server); err != nil {
				return err
			}
			if err := config.DeleteMCPServer(serverName); err != nil {
				_ = config.DeleteMCPServer(newName)
				return err
			}
		}
		return nil
	case "command":
		server.Command = strings.TrimSpace(field.Value)
	case "args":
		server.Args = strings.Fields(field.Value)
	case "type":
		serverType := config.MCPType(strings.TrimSpace(field.Value))
		if serverType != config.MCPStdio && serverType != config.MCPSse {
			return fmt.Errorf("unsupported MCP server type %q", field.Value)
		}
		server.Type = serverType
	case "url":
		server.URL = strings.TrimSpace(field.Value)
	default:
		return fmt.Errorf("unsupported MCP server field %q", parts[2])
	}

	return config.UpdateMCPServer(serverName, server)
}

func saveLSP(field settings.Field) error {
	parts := strings.Split(field.Key, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid LSP setting key %q", field.Key)
	}

	language := parts[1]
	lspCfg, ok := config.Get().LSP[language]
	if !ok {
		return fmt.Errorf("LSP %s not found", language)
	}

	switch parts[2] {
	case "language":
		newLanguage := strings.TrimSpace(field.Value)
		if newLanguage == "" {
			return fmt.Errorf("LSP language cannot be empty")
		}
		if newLanguage != language {
			if _, exists := config.Get().LSP[newLanguage]; exists {
				return fmt.Errorf("LSP %s already exists", newLanguage)
			}
			if err := config.UpdateLSP(newLanguage, lspCfg); err != nil {
				return err
			}
			if err := config.DeleteLSP(language); err != nil {
				_ = config.DeleteLSP(newLanguage)
				return err
			}
		}
		return nil
	case "command":
		lspCfg.Command = strings.TrimSpace(field.Value)
	case "enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid LSP enabled value: %w", err)
		}
		lspCfg.Disabled = !enabled
	default:
		return fmt.Errorf("unsupported LSP field %q", parts[2])
	}

	return config.UpdateLSP(language, lspCfg)
}

func saveMesnada(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	mesnadaCfg := cfg.Mesnada
	switch field.Key {
	case "mesnada.enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Mesnada enabled value: %w", err)
		}
		mesnadaCfg.Enabled = enabled
	case "mesnada.server.host":
		host := strings.TrimSpace(field.Value)
		if host == "" {
			return fmt.Errorf("Mesnada server host cannot be empty")
		}
		mesnadaCfg.Server.Host = host
	case "mesnada.server.port":
		port, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Mesnada server port: %w", err)
		}
		if port < 1 || port > 65535 {
			return fmt.Errorf("Mesnada server port must be between 1 and 65535")
		}
		mesnadaCfg.Server.Port = port
	case "mesnada.orchestrator.maxParallel":
		maxParallel, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Mesnada max parallel value: %w", err)
		}
		if maxParallel < 1 {
			return fmt.Errorf("Mesnada max parallel must be greater than zero")
		}
		mesnadaCfg.Orchestrator.MaxParallel = maxParallel
	case "mesnada.orchestrator.defaultEngine":
		engine := strings.TrimSpace(field.Value)
		if !isAllowedMesnadaEngine(engine) {
			return fmt.Errorf("unsupported Mesnada default engine %q", engine)
		}
		mesnadaCfg.Orchestrator.DefaultEngine = engine
	case "mesnada.orchestrator.personaPath":
		mesnadaCfg.Orchestrator.PersonaPath = strings.TrimSpace(field.Value)
	case "mesnada.acp.enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Mesnada ACP enabled value: %w", err)
		}
		mesnadaCfg.ACP.Enabled = enabled
	case "mesnada.acp.autoPermission":
		autoPermission, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Mesnada ACP auto permission value: %w", err)
		}
		mesnadaCfg.ACP.AutoPermission = autoPermission
	default:
		return fmt.Errorf("unsupported Mesnada setting %q", field.Key)
	}

	return config.UpdateMesnada(mesnadaCfg)
}

func parseIntValue(value string) (int, error) {
	parsedValue, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	return parsedValue, nil
}

func isAllowedMesnadaEngine(engine string) bool {
	switch engine {
	case "copilot", "claude", "gemini", "opencode", "mistral":
		return true
	default:
		return false
	}
}

func parseBoolValue(value string) (bool, error) {
	parsedValue, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, err
	}
	return parsedValue, nil
}

func savedFieldKey(field settings.Field) string {
	if !strings.HasPrefix(field.Key, "mcpServers.") && !strings.HasPrefix(field.Key, "lsp.") {
		return field.Key
	}

	parts := strings.Split(field.Key, ".")
	if len(parts) != 3 || (parts[2] != "name" && parts[2] != "language") {
		return field.Key
	}

	return strings.Join([]string{parts[0], strings.TrimSpace(field.Value), parts[2]}, ".")
}

var _ layout.Sizeable = (*settingsPage)(nil)
