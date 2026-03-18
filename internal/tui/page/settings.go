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
	"github.com/digiogithub/pando/internal/rag/embeddings"
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
		buildRemembrancesSection(cfg),
		buildInternalToolsSection(cfg),
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
			{
				Label: "Working Dir",
				Key:   "general.workingDir",
				Value: cfg.WorkingDir,
				Type:  settings.FieldText,
			},
			{
				Label: "Log File",
				Key:   "general.logFile",
				Value: cfg.LogFile,
				Type:  settings.FieldText,
			},
			{
				Label: "Debug LSP",
				Key:   "general.debugLSP",
				Value: boolString(cfg.DebugLSP),
				Type:  settings.FieldToggle,
			},
			{
				Label: "Context Paths",
				Key:   "general.contextPaths",
				Value: strings.Join(cfg.ContextPaths, ","),
				Type:  settings.FieldText,
			},
			{
				Label: "Data Directory",
				Key:   "general.data.directory",
				Value: cfg.Data.Directory,
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
		if providerID == models.ProviderAnthropic {
			fields = append(fields, settings.Field{
				Label: fmt.Sprintf("%s Use OAuth", providerName),
				Key:   fmt.Sprintf("providers.%s.useOAuth", providerName),
				Value: boolString(providerCfg.UseOAuth),
				Type:  settings.FieldToggle,
			})
		}
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

		fields = append(fields,
			settings.Field{
				Label:   fmt.Sprintf("%s Model", string(agentName)),
				Key:     fmt.Sprintf("agents.%s.model", agentName),
				Value:   modelID,
				Type:    settings.FieldSelect,
				Options: ensureOption(modelOptions, modelID),
			},
			settings.Field{
				Label: fmt.Sprintf("%s Max Tokens", string(agentName)),
				Key:   fmt.Sprintf("agents.%s.maxTokens", agentName),
				Value: fmt.Sprint(agentCfg.MaxTokens),
				Type:  settings.FieldText,
			},
			settings.Field{
				Label:   fmt.Sprintf("%s Reasoning Effort", string(agentName)),
				Key:     fmt.Sprintf("agents.%s.reasoningEffort", agentName),
				Value:   agentCfg.ReasoningEffort,
				Type:    settings.FieldSelect,
				Options: []string{"", "low", "medium", "high"},
			},
			settings.Field{
				Label: fmt.Sprintf("%s Auto Compact", string(agentName)),
				Key:   fmt.Sprintf("agents.%s.autoCompact", agentName),
				Value: boolString(agentCfg.AutoCompact),
				Type:  settings.FieldToggle,
			},
			settings.Field{
				Label:    fmt.Sprintf("%s Compact Threshold", string(agentName)),
				Key:      fmt.Sprintf("agents.%s.autoCompactThreshold", agentName),
				Value:    fmt.Sprintf("%.2f", agentCfg.AutoCompactThreshold),
				Type:     settings.FieldText,
				Disabled: !agentCfg.AutoCompact,
			},
		)
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
				Options: ensureOption([]string{string(config.MCPStdio), string(config.MCPSse), string(config.MCPStreamableHTTP)}, serverType),
			},
		)

		if server.Type == config.MCPSse || server.Type == config.MCPStreamableHTTP {
			fields = append(fields, settings.Field{
				Label: fmt.Sprintf("%s URL", name),
				Key:   fmt.Sprintf("mcpServers.%s.url", name),
				Value: server.URL,
				Type:  settings.FieldText,
			})
		}
		fields = append(fields,
			settings.Field{
				Label: fmt.Sprintf("%s Env", name),
				Key:   fmt.Sprintf("mcpServers.%s.env", name),
				Value: strings.Join(server.Env, " "),
				Type:  settings.FieldText,
			},
			settings.Field{
				Label: fmt.Sprintf("%s Headers", name),
				Key:   fmt.Sprintf("mcpServers.%s.headers", name),
				Value: headersToString(server.Headers),
				Type:  settings.FieldText,
			},
		)
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
				Label: fmt.Sprintf("%s Args", language),
				Key:   fmt.Sprintf("lsp.%s.args", language),
				Value: strings.Join(lspCfg.Args, " "),
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
		Title:  "Subagents",
		Fields: fields,
	}
}

func buildRemembrancesSection(cfg *config.Config) settings.Section {
	rem := cfg.Remembrances
	useSameModel := rem.UseSameModel

	providerOptions := []string{"ollama", "openai", "google", "anthropic", "openai-compatible"}
	docProviderModels := remembrancesModelsForProvider(rem.DocumentEmbeddingProvider)

	codeProvider := rem.CodeEmbeddingProvider
	codeModel := rem.CodeEmbeddingModel
	if useSameModel {
		codeProvider = rem.DocumentEmbeddingProvider
		codeModel = rem.DocumentEmbeddingModel
	}
	codeProviderModels := remembrancesModelsForProvider(codeProvider)

	docDimension := embeddings.GetModelDimension(rem.DocumentEmbeddingModel)
	codeDimension := embeddings.GetModelDimension(codeModel)

	fields := []settings.Field{
		{
			Label: "Enabled",
			Key:   "remembrances.enabled",
			Type:  settings.FieldToggle,
			Value: boolString(rem.Enabled),
		},
	}

	if !rem.Enabled {
		fields = append(fields, settings.Field{
			Label:    "Info",
			Key:      "remembrances.info.disabled",
			Value:    "Remembrances system is disabled. Enable it to configure embedding providers and models.",
			Type:     settings.FieldText,
			ReadOnly: true,
		})
		return settings.Section{Title: "KB & Code Index", Fields: fields}
	}

	fields = append(fields,
		settings.Field{
			Label: "Use Same Model",
			Key:   "remembrances.use_same_model",
			Type:  settings.FieldToggle,
			Value: boolString(useSameModel),
		},
		settings.Field{
			Label:    "Warning",
			Key:      "remembrances.warning.reembed",
			Type:     settings.FieldText,
			Value:    "Changing embedding providers or models requires re-embedding existing Remembrances content.",
			ReadOnly: true,
		},
	)

	docDimStr := "auto-detect"
	if docDimension > 0 {
		docDimStr = fmt.Sprintf("%d dims", docDimension)
	}
	docIsCustom := rem.DocumentEmbeddingProvider == "openai-compatible"
	fields = append(fields,
		settings.Field{
			Label:   "Doc Provider",
			Key:     "remembrances.document_embedding_provider",
			Type:    settings.FieldSelect,
			Value:   rem.DocumentEmbeddingProvider,
			Options: ensureOption(providerOptions, rem.DocumentEmbeddingProvider),
		},
		settings.Field{
			Label: "Doc Model",
			Key:   "remembrances.document_embedding_model",
			Type:  settings.FieldText,
			Value: rem.DocumentEmbeddingModel,
		},
		settings.Field{
			Label:    "Doc Base URL",
			Key:      "remembrances.document_embedding_base_url",
			Type:     settings.FieldText,
			Value:    rem.DocumentEmbeddingBaseURL,
			Disabled: !docIsCustom,
		},
		settings.Field{
			Label:    "Doc API Key",
			Key:      "remembrances.document_embedding_api_key",
			Type:     settings.FieldText,
			Value:    rem.DocumentEmbeddingAPIKey,
			Masked:   true,
			Disabled: !docIsCustom,
		},
		settings.Field{
			Label:    "Doc Suggestions",
			Key:      "remembrances.document_embedding_suggestions",
			Type:     settings.FieldText,
			Value:    remembrancesSuggestionsText(docProviderModels),
			ReadOnly: true,
		},
		settings.Field{
			Label:    "Doc Dims",
			Key:      "remembrances.doc_dims_info",
			Type:     settings.FieldText,
			Value:    docDimStr,
			ReadOnly: true,
		},
	)

	codeDimStr := "auto-detect"
	if codeDimension > 0 {
		codeDimStr = fmt.Sprintf("%d dims", codeDimension)
	}
	codeIsCustom := codeProvider == "openai-compatible"
	fields = append(fields,
		settings.Field{
			Label:    "Code Provider",
			Key:      "remembrances.code_embedding_provider",
			Type:     settings.FieldSelect,
			Value:    codeProvider,
			Options:  ensureOption(providerOptions, codeProvider),
			Disabled: useSameModel,
		},
		settings.Field{
			Label:    "Code Model",
			Key:      "remembrances.code_embedding_model",
			Type:     settings.FieldText,
			Value:    codeModel,
			Disabled: useSameModel,
		},
		settings.Field{
			Label:    "Code Base URL",
			Key:      "remembrances.code_embedding_base_url",
			Type:     settings.FieldText,
			Value:    rem.CodeEmbeddingBaseURL,
			Disabled: useSameModel || !codeIsCustom,
		},
		settings.Field{
			Label:    "Code API Key",
			Key:      "remembrances.code_embedding_api_key",
			Type:     settings.FieldText,
			Value:    rem.CodeEmbeddingAPIKey,
			Masked:   true,
			Disabled: useSameModel || !codeIsCustom,
		},
		settings.Field{
			Label:    "Code Suggestions",
			Key:      "remembrances.code_embedding_suggestions",
			Type:     settings.FieldText,
			Value:    remembrancesSuggestionsText(codeProviderModels),
			ReadOnly: true,
			Disabled: useSameModel,
		},
		settings.Field{
			Label:    "Code Dims",
			Key:      "remembrances.code_dims_info",
			Type:     settings.FieldText,
			Value:    codeDimStr,
			ReadOnly: true,
		},
	)

	fields = append(fields,
		settings.Field{
			Label: "Chunk Size",
			Key:   "remembrances.chunk_size",
			Type:  settings.FieldText,
			Value: fmt.Sprint(rem.ChunkSize),
		},
		settings.Field{
			Label: "Chunk Overlap",
			Key:   "remembrances.chunk_overlap",
			Type:  settings.FieldText,
			Value: fmt.Sprint(rem.ChunkOverlap),
		},
		settings.Field{
			Label: "Index Workers",
			Key:   "remembrances.index_workers",
			Type:  settings.FieldText,
			Value: fmt.Sprint(rem.IndexWorkers),
		},
	)

	validationMessage := "Configuration looks valid."
	if err := validateRemembrancesConfig(cfg, rem); err != nil {
		validationMessage = err.Error()
	}
	fields = append(fields, settings.Field{
		Label:    "Validation",
		Key:      "remembrances.validation",
		Type:     settings.FieldText,
		Value:    validationMessage,
		ReadOnly: true,
	})

	return settings.Section{Title: "KB & Code Index", Fields: fields}
}

func buildInternalToolsSection(cfg *config.Config) settings.Section {
	it := cfg.InternalTools
	fields := []settings.Field{
		{
			Label: "Fetch Enabled",
			Key:   "internalTools.fetchEnabled",
			Type:  settings.FieldToggle,
			Value: boolString(it.FetchEnabled),
		},
		{
			Label: "Fetch Max Size (MB)",
			Key:   "internalTools.fetchMaxSizeMB",
			Type:  settings.FieldText,
			Value: fmt.Sprint(it.FetchMaxSizeMB),
		},
		{
			Label: "Google Search Enabled",
			Key:   "internalTools.googleSearchEnabled",
			Type:  settings.FieldToggle,
			Value: boolString(it.GoogleSearchEnabled),
		},
		{
			Label:  "Google API Key",
			Key:    "internalTools.googleApiKey",
			Type:   settings.FieldText,
			Value:  it.GoogleAPIKey,
			Masked: true,
		},
		{
			Label: "Google Search Engine ID",
			Key:   "internalTools.googleSearchEngineId",
			Type:  settings.FieldText,
			Value: it.GoogleSearchEngineID,
		},
		{
			Label: "Brave Search Enabled",
			Key:   "internalTools.braveSearchEnabled",
			Type:  settings.FieldToggle,
			Value: boolString(it.BraveSearchEnabled),
		},
		{
			Label:  "Brave API Key",
			Key:    "internalTools.braveApiKey",
			Type:   settings.FieldText,
			Value:  it.BraveAPIKey,
			Masked: true,
		},
		{
			Label: "Perplexity Search Enabled",
			Key:   "internalTools.perplexitySearchEnabled",
			Type:  settings.FieldToggle,
			Value: boolString(it.PerplexitySearchEnabled),
		},
		{
			Label:  "Perplexity API Key",
			Key:    "internalTools.perplexityApiKey",
			Type:   settings.FieldText,
			Value:  it.PerplexityAPIKey,
			Masked: true,
		},
		{
			Label: "Context7 Enabled",
			Key:   "internalTools.context7Enabled",
			Type:  settings.FieldToggle,
			Value: boolString(it.Context7Enabled),
		},
		// --- Browser Automation ---
		{
			Label: "Browser Enabled",
			Key:   "internalTools.browserEnabled",
			Type:  settings.FieldToggle,
			Value: boolString(it.BrowserEnabled),
		},
		{
			Label: "Browser Headless",
			Key:   "internalTools.browserHeadless",
			Type:  settings.FieldToggle,
			Value: boolString(it.BrowserHeadless),
		},
		{
			Label: "Browser Timeout (s)",
			Key:   "internalTools.browserTimeout",
			Type:  settings.FieldText,
			Value: fmt.Sprint(it.BrowserTimeout),
		},
		{
			Label: "Browser User Data Dir",
			Key:   "internalTools.browserUserDataDir",
			Type:  settings.FieldText,
			Value: it.BrowserUserDataDir,
		},
		{
			Label: "Browser Max Sessions",
			Key:   "internalTools.browserMaxSessions",
			Type:  settings.FieldText,
			Value: fmt.Sprint(it.BrowserMaxSessions),
		},
		{
			Label:    "Browser Info",
			Key:      "internalTools.browserInfo",
			Type:     settings.FieldText,
			Value:    "Requires Chrome/Chromium in PATH. Tools: navigate, screenshot, get_content, evaluate, click, fill, scroll, console_logs, network, pdf",
			ReadOnly: true,
		},
		{
			Label:    "Info",
			Key:      "internalTools.info",
			Type:     settings.FieldText,
			Value:    "Search tools are only active when their API key is configured. Env vars: GOOGLE_API_KEY, BRAVE_API_KEY, PERPLEXITY_API_KEY",
			ReadOnly: true,
		},
	}
	return settings.Section{
		Title:  "Internal Tools",
		Fields: fields,
	}
}

func buildServerSection(cfg *config.Config) settings.Section {
	fields := []settings.Field{
		{Label: "Enabled", Key: "server.enabled", Type: settings.FieldToggle, Value: boolString(cfg.Server.Enabled)},
		{Label: "Host", Key: "server.host", Type: settings.FieldText, Value: cfg.Server.Host},
		{Label: "Port", Key: "server.port", Type: settings.FieldText, Value: fmt.Sprint(cfg.Server.Port)},
		{Label: "Require Auth", Key: "server.requireAuth", Type: settings.FieldToggle, Value: boolString(cfg.Server.RequireAuth)},
	}

	if !cfg.Server.Enabled {
		fields = append(fields, settings.Field{
			Label:    "Info",
			Key:      "server.info.disabled",
			Type:     settings.FieldText,
			Value:    "API server is disabled.",
			ReadOnly: true,
		})
		for i := 1; i < len(fields)-1; i++ {
			fields[i].Disabled = true
		}
	}

	return settings.Section{
		Title:  "API Server",
		Fields: fields,
	}
}

func buildLuaSection(cfg *config.Config) settings.Section {
	fields := []settings.Field{
		{Label: "Enabled", Key: "lua.enabled", Type: settings.FieldToggle, Value: boolString(cfg.Lua.Enabled)},
		{Label: "Script Path", Key: "lua.scriptPath", Type: settings.FieldText, Value: cfg.Lua.ScriptPath},
		{Label: "Timeout", Key: "lua.timeout", Type: settings.FieldText, Value: cfg.Lua.Timeout},
		{Label: "Strict Mode", Key: "lua.strictMode", Type: settings.FieldToggle, Value: boolString(cfg.Lua.StrictMode)},
		{Label: "Hot Reload", Key: "lua.hotReload", Type: settings.FieldToggle, Value: boolString(cfg.Lua.HotReload)},
		{Label: "Log Filtered Data", Key: "lua.logFilteredData", Type: settings.FieldToggle, Value: boolString(cfg.Lua.LogFilteredData)},
	}

	if !cfg.Lua.Enabled {
		fields = append(fields, settings.Field{
			Label:    "Info",
			Key:      "lua.info.disabled",
			Type:     settings.FieldText,
			Value:    "Lua engine disabled.",
			ReadOnly: true,
		})
		for i := 1; i < len(fields)-1; i++ {
			fields[i].Disabled = true
		}
	}

	return settings.Section{
		Title:  "Lua Engine",
		Fields: fields,
	}
}

func buildMCPGatewaySection(cfg *config.Config) settings.Section {
	fields := []settings.Field{
		{Label: "Enabled", Key: "mcpGateway.enabled", Type: settings.FieldToggle, Value: boolString(cfg.MCPGateway.Enabled)},
		{Label: "Favorite Threshold", Key: "mcpGateway.favoriteThreshold", Type: settings.FieldText, Value: fmt.Sprint(cfg.MCPGateway.FavoriteThreshold)},
		{Label: "Max Favorites", Key: "mcpGateway.maxFavorites", Type: settings.FieldText, Value: fmt.Sprint(cfg.MCPGateway.MaxFavorites)},
		{Label: "Favorite Window Days", Key: "mcpGateway.favoriteWindowDays", Type: settings.FieldText, Value: fmt.Sprint(cfg.MCPGateway.FavoriteWindowDays)},
		{Label: "Decay Days", Key: "mcpGateway.decayDays", Type: settings.FieldText, Value: fmt.Sprint(cfg.MCPGateway.DecayDays)},
		{
			Label:    "Info",
			Key:      "mcpGateway.info",
			Type:     settings.FieldText,
			Value:    "Tracks MCP tool usage frequency to surface favorites.",
			ReadOnly: true,
		},
	}

	if !cfg.MCPGateway.Enabled {
		for i := 1; i < len(fields); i++ {
			fields[i].Disabled = true
		}
	}

	return settings.Section{
		Title:  "MCP Gateway",
		Fields: fields,
	}
}

func buildSnapshotsSection(cfg *config.Config) settings.Section {
	snap := cfg.Snapshots
	fields := []settings.Field{
		{
			Label: "Enabled",
			Key:   "snapshots.enabled",
			Type:  settings.FieldToggle,
			Value: boolString(snap.Enabled),
		},
		{
			Label: "Max Snapshots",
			Key:   "snapshots.maxSnapshots",
			Type:  settings.FieldText,
			Value: fmt.Sprint(snap.MaxSnapshots),
		},
		{
			Label: "Max File Size",
			Key:   "snapshots.maxFileSize",
			Type:  settings.FieldText,
			Value: snap.MaxFileSize,
		},
		{
			Label: "Exclude Patterns",
			Key:   "snapshots.excludePatterns",
			Type:  settings.FieldText,
			Value: strings.Join(snap.ExcludePatterns, ","),
		},
		{
			Label: "Auto Cleanup Days",
			Key:   "snapshots.autoCleanupDays",
			Type:  settings.FieldText,
			Value: fmt.Sprint(snap.AutoCleanupDays),
		},
		{
			Label:    "Info",
			Key:      "snapshots.info",
			Type:     settings.FieldText,
			Value:    "Session file snapshots. Excluded patterns use glob syntax (comma-separated).",
			ReadOnly: true,
		},
	}

	if !snap.Enabled {
		for i := 1; i < len(fields); i++ {
			fields[i].Disabled = true
		}
	}

	return settings.Section{
		Title:  "Snapshots",
		Fields: fields,
	}
}

func buildEvaluatorSection(cfg *config.Config) settings.Section {
	eval := cfg.Evaluator
	fields := []settings.Field{
		{
			Label: "Enabled",
			Key:   "evaluator.enabled",
			Type:  settings.FieldToggle,
			Value: boolString(eval.Enabled),
		},
		{
			Label:   "Judge Model",
			Key:     "evaluator.model",
			Type:    settings.FieldSelect,
			Value:   string(eval.Model),
			Options: ensureOption(supportedModelOptions(cfg), string(eval.Model)),
		},
		{
			Label: "Judge Provider",
			Key:   "evaluator.provider",
			Type:  settings.FieldText,
			Value: eval.Provider,
		},
		{
			Label: "Alpha Weight",
			Key:   "evaluator.alphaWeight",
			Type:  settings.FieldText,
			Value: fmt.Sprintf("%.2f", eval.AlphaWeight),
		},
		{
			Label: "Beta Weight",
			Key:   "evaluator.betaWeight",
			Type:  settings.FieldText,
			Value: fmt.Sprintf("%.2f", eval.BetaWeight),
		},
		{
			Label: "Exploration C",
			Key:   "evaluator.explorationC",
			Type:  settings.FieldText,
			Value: fmt.Sprintf("%.4f", eval.ExplorationC),
		},
		{
			Label: "Min Sessions UCB",
			Key:   "evaluator.minSessionsForUCB",
			Type:  settings.FieldText,
			Value: fmt.Sprint(eval.MinSessionsForUCB),
		},
		{
			Label: "Max Tokens Baseline",
			Key:   "evaluator.maxTokensBaseline",
			Type:  settings.FieldText,
			Value: fmt.Sprint(eval.MaxTokensBaseline),
		},
		{
			Label: "Max Skills",
			Key:   "evaluator.maxSkills",
			Type:  settings.FieldText,
			Value: fmt.Sprint(eval.MaxSkills),
		},
		{
			Label: "Judge Prompt Template",
			Key:   "evaluator.judgePromptTemplate",
			Type:  settings.FieldText,
			Value: eval.JudgePromptTemplate,
		},
		{
			Label: "Async Evaluation",
			Key:   "evaluator.async",
			Type:  settings.FieldToggle,
			Value: boolString(eval.Async),
		},
		{
			Label: "Corrections Patterns",
			Key:   "evaluator.correctionsPatterns",
			Type:  settings.FieldText,
			Value: strings.Join(eval.CorrectionsPatterns, ","),
		},
		{
			Label:    "Info",
			Key:      "evaluator.info",
			Type:     settings.FieldText,
			Value:    "LLM-as-Judge self-improvement with UCB1 prompt selection. Requires a cheap/fast judge model.",
			ReadOnly: true,
		},
	}

	if !eval.Enabled {
		for i := 1; i < len(fields); i++ {
			fields[i].Disabled = true
		}
	}

	return settings.Section{
		Title:  "Self-Improvement",
		Fields: fields,
	}
}

func remembrancesModelsForProvider(provider string) []string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "ollama":
		return []string{"nomic-embed-text", "mxbai-embed-large", "all-minilm"}
	case "openai":
		return []string{"text-embedding-3-small", "text-embedding-3-large", "text-embedding-ada-002"}
	case "openai-compatible":
		// Common models for OpenAI-compatible endpoints (LM Studio, LocalAI, vLLM, etc.)
		return []string{"text-embedding-3-small", "text-embedding-ada-002", "nomic-embed-text", "bge-m3"}
	case "google", "gemini":
		return []string{"text-embedding-004"}
	case "anthropic", "voyage":
		return []string{"voyage-3", "voyage-3-large", "voyage-code-3"}
	default:
		return []string{}
	}
}

func remembrancesSuggestionsText(models []string) string {
	if len(models) == 0 {
		return "No suggested models available for this provider."
	}

	return strings.Join(models, ", ")
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
	case strings.HasPrefix(field.Key, "remembrances."):
		return saveRemembrances(field)
	case strings.HasPrefix(field.Key, "internalTools."):
		return saveInternalTools(field)
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
		if serverType != config.MCPStdio && serverType != config.MCPSse && serverType != config.MCPStreamableHTTP {
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

func saveRemembrances(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	remCfg := cfg.Remembrances
	switch field.Key {
	case "remembrances.enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Remembrances enabled value: %w", err)
		}
		remCfg.Enabled = enabled
	case "remembrances.use_same_model":
		useSameModel, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Remembrances same-model value: %w", err)
		}
		remCfg.UseSameModel = useSameModel
	case "remembrances.document_embedding_provider":
		remCfg.DocumentEmbeddingProvider = strings.TrimSpace(field.Value)
		if defaultModel := embeddings.GetDefaultModel(remCfg.DocumentEmbeddingProvider); defaultModel != "" {
			remCfg.DocumentEmbeddingModel = defaultModel
		}
	case "remembrances.document_embedding_model":
		remCfg.DocumentEmbeddingModel = strings.TrimSpace(field.Value)
	case "remembrances.code_embedding_provider":
		remCfg.CodeEmbeddingProvider = strings.TrimSpace(field.Value)
		if defaultModel := embeddings.GetDefaultModel(remCfg.CodeEmbeddingProvider); defaultModel != "" {
			remCfg.CodeEmbeddingModel = defaultModel
		}
	case "remembrances.code_embedding_model":
		remCfg.CodeEmbeddingModel = strings.TrimSpace(field.Value)
	case "remembrances.document_embedding_base_url":
		remCfg.DocumentEmbeddingBaseURL = strings.TrimSpace(field.Value)
	case "remembrances.document_embedding_api_key":
		remCfg.DocumentEmbeddingAPIKey = strings.TrimSpace(field.Value)
	case "remembrances.code_embedding_base_url":
		remCfg.CodeEmbeddingBaseURL = strings.TrimSpace(field.Value)
	case "remembrances.code_embedding_api_key":
		remCfg.CodeEmbeddingAPIKey = strings.TrimSpace(field.Value)
	case "remembrances.chunk_size":
		size, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid chunk size: %w", err)
		}
		if size < 100 || size > 10000 {
			return fmt.Errorf("chunk size must be between 100 and 10000")
		}
		remCfg.ChunkSize = size
	case "remembrances.chunk_overlap":
		overlap, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid chunk overlap: %w", err)
		}
		if overlap < 0 || overlap > 1000 {
			return fmt.Errorf("chunk overlap must be between 0 and 1000")
		}
		remCfg.ChunkOverlap = overlap
	case "remembrances.index_workers":
		workers, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid index workers: %w", err)
		}
		if workers < 1 || workers > 32 {
			return fmt.Errorf("index workers must be between 1 and 32")
		}
		remCfg.IndexWorkers = workers
	default:
		return fmt.Errorf("unsupported Remembrances setting %q", field.Key)
	}

	remCfg = normalizeRemembrancesConfig(remCfg)
	if err := validateRemembrancesConfig(cfg, remCfg); err != nil {
		return err
	}

	return config.UpdateRemembrances(remCfg)
}

func saveInternalTools(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	itCfg := cfg.InternalTools
	switch field.Key {
	case "internalTools.fetchEnabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Fetch Enabled value: %w", err)
		}
		itCfg.FetchEnabled = enabled
	case "internalTools.fetchMaxSizeMB":
		size, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Fetch Max Size value: %w", err)
		}
		if size < 1 {
			return fmt.Errorf("fetch max size must be greater than zero")
		}
		itCfg.FetchMaxSizeMB = size
	case "internalTools.googleSearchEnabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Google Search Enabled value: %w", err)
		}
		itCfg.GoogleSearchEnabled = enabled
	case "internalTools.googleApiKey":
		itCfg.GoogleAPIKey = strings.TrimSpace(field.Value)
	case "internalTools.googleSearchEngineId":
		itCfg.GoogleSearchEngineID = strings.TrimSpace(field.Value)
	case "internalTools.braveSearchEnabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Brave Search Enabled value: %w", err)
		}
		itCfg.BraveSearchEnabled = enabled
	case "internalTools.braveApiKey":
		itCfg.BraveAPIKey = strings.TrimSpace(field.Value)
	case "internalTools.perplexitySearchEnabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Perplexity Search Enabled value: %w", err)
		}
		itCfg.PerplexitySearchEnabled = enabled
	case "internalTools.perplexityApiKey":
		itCfg.PerplexityAPIKey = strings.TrimSpace(field.Value)
	case "internalTools.context7Enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Context7 Enabled value: %w", err)
		}
		itCfg.Context7Enabled = enabled
	case "internalTools.browserEnabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Browser Enabled value: %w", err)
		}
		itCfg.BrowserEnabled = enabled
	case "internalTools.browserHeadless":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Browser Headless value: %w", err)
		}
		itCfg.BrowserHeadless = enabled
	case "internalTools.browserTimeout":
		timeout, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Browser Timeout value: %w", err)
		}
		if timeout < 5 || timeout > 300 {
			return fmt.Errorf("browser timeout must be between 5 and 300 seconds")
		}
		itCfg.BrowserTimeout = timeout
	case "internalTools.browserUserDataDir":
		itCfg.BrowserUserDataDir = strings.TrimSpace(field.Value)
	case "internalTools.browserMaxSessions":
		maxSessions, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Browser Max Sessions value: %w", err)
		}
		if maxSessions < 1 || maxSessions > 10 {
			return fmt.Errorf("browser max sessions must be between 1 and 10")
		}
		itCfg.BrowserMaxSessions = maxSessions
	case "internalTools.browserInfo":
		// read-only informational field, nothing to save
		return nil
	case "internalTools.info":
		// read-only informational field, nothing to save
		return nil
	default:
		return fmt.Errorf("unsupported InternalTools setting %q", field.Key)
	}

	return config.UpdateInternalTools(itCfg)
}

func normalizeRemembrancesConfig(remCfg config.RemembrancesConfig) config.RemembrancesConfig {
	remCfg.DocumentEmbeddingProvider = strings.ToLower(strings.TrimSpace(remCfg.DocumentEmbeddingProvider))
	remCfg.DocumentEmbeddingModel = strings.TrimSpace(remCfg.DocumentEmbeddingModel)
	remCfg.CodeEmbeddingProvider = strings.ToLower(strings.TrimSpace(remCfg.CodeEmbeddingProvider))
	remCfg.CodeEmbeddingModel = strings.TrimSpace(remCfg.CodeEmbeddingModel)

	if remCfg.UseSameModel {
		remCfg.CodeEmbeddingProvider = remCfg.DocumentEmbeddingProvider
		remCfg.CodeEmbeddingModel = remCfg.DocumentEmbeddingModel
		remCfg.CodeEmbeddingBaseURL = remCfg.DocumentEmbeddingBaseURL
		remCfg.CodeEmbeddingAPIKey = remCfg.DocumentEmbeddingAPIKey
	}

	return remCfg
}

func validateRemembrancesConfig(cfg *config.Config, remCfg config.RemembrancesConfig) error {
	if !remCfg.Enabled {
		return nil
	}

	if remCfg.ChunkSize < 100 || remCfg.ChunkSize > 10000 {
		return fmt.Errorf("chunk size must be between 100 and 10000")
	}
	if remCfg.ChunkOverlap < 0 || remCfg.ChunkOverlap > 1000 {
		return fmt.Errorf("chunk overlap must be between 0 and 1000")
	}

	if err := validateRemembrancesEmbedding(cfg, remCfg.DocumentEmbeddingProvider, remCfg.DocumentEmbeddingModel, remCfg.DocumentEmbeddingAPIKey, remCfg.DocumentEmbeddingBaseURL); err != nil {
		return fmt.Errorf("document embedding validation failed: %w", err)
	}

	if err := validateRemembrancesEmbedding(cfg, remCfg.CodeEmbeddingProvider, remCfg.CodeEmbeddingModel, remCfg.CodeEmbeddingAPIKey, remCfg.CodeEmbeddingBaseURL); err != nil {
		return fmt.Errorf("code embedding validation failed: %w", err)
	}

	return nil
}

func validateRemembrancesEmbedding(cfg *config.Config, provider, model, customAPIKey, customBaseURL string) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)
	if provider == "" {
		return fmt.Errorf("provider cannot be empty")
	}
	if model == "" {
		return fmt.Errorf("model cannot be empty")
	}

	var apiKey, baseURL string
	if provider == "openai-compatible" {
		apiKey = strings.TrimSpace(customAPIKey)
		baseURL = strings.TrimSpace(customBaseURL)
		if baseURL == "" {
			return fmt.Errorf("base URL is required for openai-compatible provider")
		}
	} else {
		apiKey, baseURL = remembrancesProviderCredentials(cfg, provider)
	}

	if _, err := embeddings.NewEmbedder(provider, model, apiKey, baseURL); err != nil {
		return err
	}

	return nil
}

func remembrancesProviderCredentials(cfg *config.Config, provider string) (apiKey string, baseURL string) {
	if cfg == nil {
		return "", ""
	}

	switch provider {
	case "openai":
		if providerCfg, ok := cfg.Providers[models.ProviderOpenAI]; ok {
			return strings.TrimSpace(providerCfg.APIKey), ""
		}
	case "google", "gemini":
		if providerCfg, ok := cfg.Providers[models.ProviderGemini]; ok {
			return strings.TrimSpace(providerCfg.APIKey), ""
		}
	case "anthropic", "voyage":
		if providerCfg, ok := cfg.Providers[models.ProviderAnthropic]; ok {
			return strings.TrimSpace(providerCfg.APIKey), ""
		}
	case "ollama":
		baseURL = models.ResolveOllamaBaseURL("")
		if providerCfg, ok := cfg.Providers[models.ProviderOllama]; ok {
			baseURL = models.ResolveOllamaBaseURL(providerCfg.BaseURL)
		}
		return "", baseURL
	}

	return "", ""
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
