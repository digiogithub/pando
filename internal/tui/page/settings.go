package page

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	pandoapp "github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/rag/embeddings"
	"github.com/digiogithub/pando/internal/skills/catalog"
	"github.com/digiogithub/pando/internal/tui/components/dialog"
	"github.com/digiogithub/pando/internal/tui/components/settings"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

// configExternalChangeMsg is sent to the settings page when the config file
// changes from an external source (file write or Web-UI save). This triggers a
// sections rebuild without causing a save-loop.
type configExternalChangeMsg struct{}

type skillUninstalledMsg struct {
	skillName string
	err       error
}

type skillUpdatedMsg struct {
	skillName string
	err       error
}

type lspPresetAddedMsg struct {
	presetName string
	err        error
}

type codeIndexStartedMsg struct {
	projectID string
	jobID     string
	err       error
}

type settingsPage struct {
	width          int
	height         int
	app            *pandoapp.App
	settings       settings.SettingsCmp
	catalogDialog  *dialog.SkillsCatalogDialog
	configChangeCh chan config.ConfigChangeEvent
}

// waitForConfigChange returns a blocking tea.Cmd that resolves when an
// external (non-TUI) config change event arrives on the channel.
func waitForConfigChange(ch <-chan config.ConfigChangeEvent) tea.Cmd {
	return func() tea.Msg {
		for ev := range ch {
			if ev.Source != "tui" {
				return configExternalChangeMsg{}
			}
		}
		return nil
	}
}

func (p *settingsPage) Init() tea.Cmd {
	// Subscribe to the config event bus so external changes are reflected live.
	p.configChangeCh = make(chan config.ConfigChangeEvent, 8)
	config.Bus.Subscribe(p.configChangeCh)
	return tea.Batch(p.settings.Init(), waitForConfigChange(p.configChangeCh))
}

func (p *settingsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle catalog dialog result messages at this level regardless of dialog state
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		return p, p.SetSize(msg.Width, msg.Height)
	case settings.SaveFieldMsg:
		if msg.Field.Key == "action:open_skills_catalog" {
			return p, p.openCatalogDialog()
		}
		if strings.HasPrefix(msg.Field.Key, "action:uninstall_skill:") {
			name := strings.TrimPrefix(msg.Field.Key, "action:uninstall_skill:")
			return p, p.uninstallSkill(name)
		}
		if strings.HasPrefix(msg.Field.Key, "action:update_skill:") {
			name := strings.TrimPrefix(msg.Field.Key, "action:update_skill:")
			return p, p.updateSkill(name)
		}
		if strings.HasPrefix(msg.Field.Key, "action:lsp_preset:") {
			presetName := strings.TrimPrefix(msg.Field.Key, "action:lsp_preset:")
			return p, p.addLSPPreset(presetName)
		}
		if strings.HasPrefix(msg.Field.Key, "action:delete_mcp_server:") {
			serverName := strings.TrimPrefix(msg.Field.Key, "action:delete_mcp_server:")
			return p, p.deleteMCPServer(serverName)
		}
		if msg.Field.Key == "action:remembrances_index_workdir" {
			return p, p.indexWorkingDirectory()
		}
		return p, p.saveField(msg)
	case skillUninstalledMsg:
		p.settings.SetSections(buildSections(p.app))
		p.settings.SetSize(p.width, p.height)
		if msg.err != nil {
			return p, util.ReportError(msg.err)
		}
		return p, util.ReportInfo("Skill uninstalled: " + msg.skillName)
	case skillUpdatedMsg:
		p.settings.SetSections(buildSections(p.app))
		p.settings.SetSize(p.width, p.height)
		if msg.err != nil {
			return p, util.ReportError(msg.err)
		}
		return p, util.ReportInfo("Skill updated: " + msg.skillName)
	case dialog.InstallSkillMsg:
		return p, p.installSkill(msg)
	case dialog.SkillInstalledMsg:
		p.catalogDialog = nil
		p.settings.SetSections(buildSections(p.app))
		p.settings.SetSize(p.width, p.height)
		if msg.Err != nil {
			return p, util.ReportError(msg.Err)
		}
		successMsg := fmt.Sprintf("Skill '%s' installed to %s", msg.SkillName, msg.InstallDir)
		return p, util.ReportInfo(successMsg)
	case dialog.CloseSkillsCatalogMsg:
		p.catalogDialog = nil
		return p, nil
	case lspPresetAddedMsg:
		p.settings.SetSections(buildSections(p.app))
		p.settings.SetSize(p.width, p.height)
		if msg.err != nil {
			return p, util.ReportError(msg.err)
		}
		return p, util.ReportInfo("LSP server added: " + msg.presetName)
	case codeIndexStartedMsg:
		p.settings.SetSections(buildSections(p.app))
		p.settings.SetSize(p.width, p.height)
		if msg.err != nil {
			return p, util.ReportError(msg.err)
		}
		return p, util.ReportInfo(fmt.Sprintf("Indexing started for project %q (job: %s)", msg.projectID, msg.jobID))
	case configExternalChangeMsg:
		// Config changed from outside TUI (file or Web-UI): rebuild sections and
		// re-arm the listener command so we keep receiving future events.
		p.settings.SetSections(buildSections(p.app))
		p.settings.SetSize(p.width, p.height)
		var rearm tea.Cmd
		if p.configChangeCh != nil {
			rearm = waitForConfigChange(p.configChangeCh)
		}
		return p, rearm
	}

	// Forward ALL events to catalog dialog when active (keys, ticks, search results, blinks)
	if p.catalogDialog != nil {
		updated, cmd := p.catalogDialog.Update(msg)
		d := updated.(dialog.SkillsCatalogDialog)
		p.catalogDialog = &d
		// For key messages, the dialog is the sole handler — block settings component
		if _, ok := msg.(tea.KeyMsg); ok {
			return p, cmd
		}
		// For non-key messages (ticks, search results, blink), also forward to settings
		settingsUpdated, settingsCmd := p.settings.Update(msg)
		p.settings = settingsUpdated.(settings.SettingsCmp)
		return p, tea.Batch(cmd, settingsCmd)
	}

	updated, cmd := p.settings.Update(msg)
	p.settings = updated.(settings.SettingsCmp)
	return p, cmd
}

func (p *settingsPage) View() string {
	base := p.settings.View()
	if p.catalogDialog != nil {
		overlay := p.catalogDialog.View()
		x := (p.width - dialog.DialogDialogWidth) / 2
		y := (p.height - dialog.DialogDialogHeight) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		return layout.PlaceOverlay(x, y, overlay, base, true)
	}
	return base
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

// HasActiveModal reports whether the settings page currently has an active modal dialog.
func (p *settingsPage) HasActiveModal() bool {
	return p.catalogDialog != nil
}

// ClearModals closes any open modal dialogs on the settings page.
func (p *settingsPage) ClearModals() {
	p.catalogDialog = nil
}

func NewSettingsPage(app *pandoapp.App) tea.Model {
	cmp := settings.NewSettingsCmp()
	cmp.SetSections(buildSections(app))
	return &settingsPage{
		app:      app,
		settings: cmp,
	}
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

func (p *settingsPage) deleteMCPServer(name string) tea.Cmd {
	name = strings.TrimSpace(name)
	if name == "" {
		return util.ReportError(fmt.Errorf("MCP server name cannot be empty"))
	}
	if err := config.DeleteMCPServer(name); err != nil {
		return util.ReportError(err)
	}
	if p.app != nil && p.app.MCPGateway != nil {
		if err := p.app.MCPGateway.DeleteServerData(context.Background(), name); err != nil {
			return util.ReportError(err)
		}
	}

	p.settings.SetSections(buildSections(p.app))
	p.settings.SetSize(p.width, p.height)
	return util.ReportInfo("MCP server deleted: " + name)
}

// indexWorkingDirectory starts a code indexing job for the current working directory.
func (p *settingsPage) indexWorkingDirectory() tea.Cmd {
	return func() tea.Msg {
		if p.app == nil || p.app.Remembrances == nil || p.app.Remembrances.Code == nil {
			return codeIndexStartedMsg{err: fmt.Errorf("remembrances code indexer not initialized")}
		}
		cwd := config.WorkingDirectory()
		projectID := sanitizeTUIProjectID(cwd)
		jobID, err := p.app.Remembrances.Code.IndexProject(context.Background(), projectID, cwd, nil)
		return codeIndexStartedMsg{projectID: projectID, jobID: jobID, err: err}
	}
}

// sanitizeTUIProjectID converts an absolute path into a safe project identifier.
func sanitizeTUIProjectID(path string) string {
	clean := strings.TrimRight(path, "/\\")
	if idx := strings.LastIndexAny(clean, "/\\"); idx >= 0 {
		clean = clean[idx+1:]
	}
	if clean == "" {
		clean = "project"
	}
	out := make([]byte, 0, len(clean))
	for _, b := range []byte(clean) {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' || b == '_' {
			out = append(out, b)
		} else {
			out = append(out, '-')
		}
	}
	return string(out)
}

func (p *settingsPage) openCatalogDialog() tea.Cmd {
	baseURL := "https://skills.sh"
	if cfg := config.Get(); cfg != nil && cfg.SkillsCatalog.BaseURL != "" {
		baseURL = cfg.SkillsCatalog.BaseURL
	}
	client := catalog.NewClient(baseURL)
	d := dialog.NewSkillsCatalogDialog(dialog.DialogDialogWidth, dialog.DialogDialogHeight, client)
	p.catalogDialog = &d
	return p.catalogDialog.Init()
}

func (p *settingsPage) installSkill(msg dialog.InstallSkillMsg) tea.Cmd {
	skill := msg.Skill
	global := msg.Global
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		content, err := catalog.FetchSkillContent(ctx, skill.Source, skill.Name)
		if err != nil {
			return dialog.SkillInstalledMsg{SkillName: skill.Name, Err: err}
		}
		targetDir := catalog.ResolveSkillsDir(!global)
		if err := catalog.InstallSkill(content, skill.Name, targetDir); err != nil {
			return dialog.SkillInstalledMsg{SkillName: skill.Name, Err: err}
		}
		scope := "project"
		if global {
			scope = "global"
		}
		entry := catalog.LockEntry{
			Name:        skill.Name,
			Source:      skill.Source,
			SkillID:     skill.SkillID,
			Scope:       scope,
			InstalledAt: time.Now(),
			Checksum:    catalog.ChecksumContent(content),
		}
		_ = catalog.AddLockEntry(targetDir, entry)
		return dialog.SkillInstalledMsg{SkillName: skill.Name, InstallDir: targetDir}
	}
}

func (p *settingsPage) uninstallSkill(name string) tea.Cmd {
	return func() tea.Msg {
		dirs := []string{
			catalog.ResolveSkillsDir(false),
			catalog.ResolveSkillsDir(true),
		}
		for _, dir := range dirs {
			if catalog.IsSkillInstalled(name, dir) {
				err := catalog.UninstallSkill(name, dir)
				if err == nil {
					_ = catalog.RemoveLockEntry(dir, name)
				}
				return skillUninstalledMsg{skillName: name, err: err}
			}
		}
		return skillUninstalledMsg{skillName: name, err: fmt.Errorf("skill %q not found in any skills directory", name)}
	}
}

func (p *settingsPage) updateSkill(name string) tea.Cmd {
	return func() tea.Msg {
		dirs := []string{
			catalog.ResolveSkillsDir(false),
			catalog.ResolveSkillsDir(true),
		}
		for _, dir := range dirs {
			lock, err := catalog.ReadLock(dir)
			if err != nil || lock == nil {
				continue
			}
			entry, ok := lock.Skills[name]
			if !ok {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			content, err := catalog.FetchSkillContent(ctx, entry.Source, name)
			if err != nil {
				return skillUpdatedMsg{skillName: name, err: err}
			}
			// Force overwrite: uninstall first, then reinstall
			_ = catalog.UninstallSkill(name, dir)
			err = catalog.InstallSkill(content, name, dir)
			if err == nil {
				newEntry := catalog.LockEntry{
					Name:        name,
					Source:      entry.Source,
					SkillID:     entry.SkillID,
					Scope:       entry.Scope,
					InstalledAt: time.Now(),
					Checksum:    catalog.ChecksumContent(content),
				}
				_ = catalog.AddLockEntry(dir, newEntry)
			}
			return skillUpdatedMsg{skillName: name, err: err}
		}
		return skillUpdatedMsg{skillName: name, err: fmt.Errorf("skill %q not found in catalog lock", name)}
	}
}

func (p *settingsPage) addLSPPreset(presetName string) tea.Cmd {
	return func() tea.Msg {
		preset, ok := config.LSPPresetByName(presetName)
		if !ok {
			return lspPresetAddedMsg{presetName: presetName, err: fmt.Errorf("unknown LSP preset %q", presetName)}
		}
		err := config.UpdateLSP(preset.Name, preset.Config)
		return lspPresetAddedMsg{presetName: presetName, err: err}
	}
}

func buildSections(app *pandoapp.App) []settings.Section {
	cfg := config.Get()
	if cfg == nil {
		return nil
	}

	return []settings.Section{
		// ── Core ──
		withGroup(buildGeneralSection(cfg), "Core"),

		// ── AI ──
		withGroup(buildProvidersSection(cfg), "AI"),
		withGroup(buildAgentsSection(cfg), "AI"),
		withGroup(buildPersonaAutoSelectSection(cfg), "AI"),
		withGroup(buildEvaluatorSection(cfg), "AI"),

		// ── Extensions ──
		withGroup(buildSkillsSection(app, cfg), "Extensions"),
		withGroup(buildSkillsCatalogSection(cfg), "Extensions"),
		withGroup(buildLuaSection(cfg), "Extensions"),

		// ── Integrations ──
		withGroup(buildMCPServersSection(cfg), "Integrations"),
		withGroup(buildMCPGatewaySection(cfg), "Integrations"),
		withGroup(buildLSPSection(cfg), "Integrations"),

		// ── Tools ──
		withGroup(buildInternalToolsSection(cfg), "Tools"),
		withGroup(buildBashSection(cfg), "Tools"),

		// ── Services ──
		withGroup(buildMesnadaSection(cfg), "Services"),
		withGroup(buildRemembrancesSection(app, cfg), "Services"),
		withGroup(buildOpenLitSection(cfg), "Services"),
		withGroup(buildServerSection(cfg), "Services"),
		withGroup(buildSnapshotsSection(cfg), "Services"),
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
				Label: "Auto-Approve Tool Changes",
				Key:   "permissions.autoApproveTools",
				Value: boolString(cfg.Permissions.AutoApproveTools),
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

	if cfg.SkillsCatalog.Enabled {
		fields = append(fields, settings.Field{
			Label:    "Browse Catalog",
			Key:      "action:open_skills_catalog",
			Value:    "↵ Enter to search and install from skills.sh",
			Type:     settings.FieldAction,
			ReadOnly: true,
		})
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

	// Read lock file to get catalog source info (try project-local first, then global)
	var catalogLock *catalog.CatalogLock
	if lock, err := catalog.ReadLock(catalog.ResolveSkillsDir(false)); err == nil && len(lock.Skills) > 0 {
		catalogLock = lock
	} else if lock, err := catalog.ReadLock(catalog.ResolveSkillsDir(true)); err == nil {
		catalogLock = lock
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

		// Determine catalog lock entry if skill was installed from catalog
		sourceValue := "(local)"
		var lockEntry *catalog.LockEntry
		if catalogLock != nil {
			if entry, ok := catalogLock.Skills[m.Name]; ok {
				lockEntry = &entry
				sourceValue = entry.Source + "@" + m.Name
			}
		}

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
			settings.Field{
				Label:    fmt.Sprintf("%s Source", m.Name),
				Key:      "skills.source." + m.Name,
				Value:    sourceValue,
				Type:     settings.FieldText,
				ReadOnly: true,
			},
			settings.Field{
				Label:    fmt.Sprintf("%s Uninstall", m.Name),
				Key:      "action:uninstall_skill:" + m.Name,
				Value:    "Remove skill files",
				Type:     settings.FieldAction,
				ReadOnly: true,
			},
		)

		if lockEntry != nil {
			fields = append(fields, settings.Field{
				Label:    fmt.Sprintf("%s Update", m.Name),
				Key:      "action:update_skill:" + m.Name,
				Value:    "Re-fetch from " + lockEntry.Source,
				Type:     settings.FieldAction,
				ReadOnly: true,
			})
		}
	}

	return settings.Section{
		Title:  "Skills",
		Fields: fields,
	}
}

func buildSkillsCatalogSection(cfg *config.Config) settings.Section {
	baseURL := cfg.SkillsCatalog.BaseURL
	if baseURL == "" {
		baseURL = "https://skills.sh"
	}
	scope := cfg.SkillsCatalog.DefaultScope
	if scope == "" {
		scope = "global"
	}
	return settings.Section{
		Title: "Skills Catalog",
		Fields: []settings.Field{
			{
				Label: "Catalog Enabled",
				Key:   "skillsCatalog.enabled",
				Value: boolString(cfg.SkillsCatalog.Enabled),
				Type:  settings.FieldToggle,
			},
			{
				Label: "Catalog URL",
				Key:   "skillsCatalog.baseUrl",
				Value: baseURL,
				Type:  settings.FieldText,
			},
			{
				Label:   "Default Scope",
				Key:     "skillsCatalog.defaultScope",
				Value:   scope,
				Type:    settings.FieldSelect,
				Options: []string{"global", "project"},
			},
			{
				Label: "Auto Update",
				Key:   "skillsCatalog.autoUpdate",
				Value: boolString(cfg.SkillsCatalog.AutoUpdate),
				Type:  settings.FieldToggle,
			},
			{
				Label:    "Lock File Location",
				Key:      "skillsCatalog.lockFile.info",
				Value:    "~/.pando/skills/catalog-lock.json",
				Type:     settings.FieldText,
				ReadOnly: true,
			},
		},
	}
}

func buildAgentsSection(cfg *config.Config) settings.Section {
	agentOrder := []config.AgentName{
		config.AgentCoder,
		config.AgentSummarizer,
		config.AgentTask,
		config.AgentTitle,
		config.AgentPersonaSelector,
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
				Label:   fmt.Sprintf("%s Thinking Mode", string(agentName)),
				Key:     fmt.Sprintf("agents.%s.thinkingMode", agentName),
				Value:   string(agentCfg.ThinkingMode),
				Type:    settings.FieldSelect,
				Options: []string{"", "disabled", "low", "medium", "high"},
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
				Label: fmt.Sprintf("%s Delete", name),
				Key:   fmt.Sprintf("action:delete_mcp_server:%s", name),
				Value: "Delete server and gateway registry data",
				Type:  settings.FieldAction,
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
	names := make([]string, 0, len(cfg.LSP))
	for name := range cfg.LSP {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]settings.Field, 0, len(names)*5)
	for _, name := range names {
		lspCfg := cfg.LSP[name]
		fields = append(fields,
			settings.Field{
				Label: fmt.Sprintf("%s Name", name),
				Key:   fmt.Sprintf("lsp.%s.language", name),
				Value: name,
				Type:  settings.FieldText,
			},
			settings.Field{
				Label: fmt.Sprintf("%s Command", name),
				Key:   fmt.Sprintf("lsp.%s.command", name),
				Value: lspCfg.Command,
				Type:  settings.FieldText,
			},
			settings.Field{
				Label: fmt.Sprintf("%s Args", name),
				Key:   fmt.Sprintf("lsp.%s.args", name),
				Value: strings.Join(lspCfg.Args, " "),
				Type:  settings.FieldText,
			},
			settings.Field{
				Label: fmt.Sprintf("%s Languages", name),
				Key:   fmt.Sprintf("lsp.%s.languages", name),
				Value: strings.Join(lspCfg.Languages, " "),
				Type:  settings.FieldText,
			},
			settings.Field{
				Label: fmt.Sprintf("%s Enabled", name),
				Key:   fmt.Sprintf("lsp.%s.enabled", name),
				Value: boolString(!lspCfg.Disabled),
				Type:  settings.FieldToggle,
			},
		)
	}

	// Show available presets that are not yet configured.
	presets := config.LSPPresets()
	for _, preset := range presets {
		if _, alreadyConfigured := cfg.LSP[preset.Name]; alreadyConfigured {
			continue
		}
		fields = append(fields, settings.Field{
			Label: fmt.Sprintf("Add %s", preset.Name),
			Key:   fmt.Sprintf("action:lsp_preset:%s", preset.Name),
			Value: preset.Description,
			Type:  settings.FieldAction,
		})
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
			Options: []string{"pando", "copilot", "claude", "gemini", "opencode", "mistral", "acp", "acp-claude", "acp-codex"},
		},
		{Label: "Persona Path", Key: "mesnada.orchestrator.personaPath", Type: settings.FieldText, Value: cfg.Mesnada.Orchestrator.PersonaPath},
		{Label: "ACP Enabled", Key: "mesnada.acp.enabled", Type: settings.FieldToggle, Value: fmt.Sprint(cfg.Mesnada.ACP.Enabled)},
		{Label: "ACP Auto Permission", Key: "mesnada.acp.autoPermission", Type: settings.FieldToggle, Value: fmt.Sprint(cfg.Mesnada.ACP.AutoPermission)},
		{Label: "Orchestrator Store Path", Key: "mesnada.orchestrator.storePath", Type: settings.FieldText, Value: cfg.Mesnada.Orchestrator.StorePath},
		{Label: "Orchestrator Log Dir", Key: "mesnada.orchestrator.logDir", Type: settings.FieldText, Value: cfg.Mesnada.Orchestrator.LogDir},
		{
			Label:   "Orchestrator Default Model",
			Key:     "mesnada.orchestrator.defaultModel",
			Type:    settings.FieldSelect,
			Value:   cfg.Mesnada.Orchestrator.DefaultModel,
			Options: ensureOption(supportedModelOptions(cfg), cfg.Mesnada.Orchestrator.DefaultModel),
		},
		{Label: "Orchestrator MCP Config", Key: "mesnada.orchestrator.defaultMcpConfig", Type: settings.FieldText, Value: cfg.Mesnada.Orchestrator.DefaultMCPConfig},
		{
			Label:   "ACP Default Agent",
			Key:     "mesnada.acp.defaultAgent",
			Type:    settings.FieldSelect,
			Value:   cfg.Mesnada.ACP.DefaultAgent,
			Options: ensureOption([]string{"pando"}, cfg.Mesnada.ACP.DefaultAgent),
		},
		{Label: "ACP Server Enabled", Key: "mesnada.acp.server.enabled", Type: settings.FieldToggle, Value: boolString(cfg.Mesnada.ACP.Server.Enabled)},
		{Label: "ACP Server Host", Key: "mesnada.acp.server.host", Type: settings.FieldText, Value: cfg.Mesnada.ACP.Server.Host},
		{Label: "ACP Server Port", Key: "mesnada.acp.server.port", Type: settings.FieldText, Value: fmt.Sprint(cfg.Mesnada.ACP.Server.Port)},
		{Label: "ACP Server Max Sessions", Key: "mesnada.acp.server.maxSessions", Type: settings.FieldText, Value: fmt.Sprint(cfg.Mesnada.ACP.Server.MaxSessions)},
		{Label: "ACP Server Session Timeout", Key: "mesnada.acp.server.sessionTimeout", Type: settings.FieldText, Value: cfg.Mesnada.ACP.Server.SessionTimeout},
		{Label: "ACP Server Require Auth", Key: "mesnada.acp.server.requireAuth", Type: settings.FieldToggle, Value: boolString(cfg.Mesnada.ACP.Server.RequireAuth)},
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

func buildRemembrancesSection(app *pandoapp.App, cfg *config.Config) settings.Section {
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
			Label: "KB Path",
			Key:   "remembrances.kb_path",
			Type:  settings.FieldText,
			Value: rem.KBPath,
		},
		settings.Field{
			Label: "KB Watch",
			Key:   "remembrances.kb_watch",
			Type:  settings.FieldToggle,
			Value: boolString(rem.KBWatch),
		},
		settings.Field{
			Label: "KB Auto Import",
			Key:   "remembrances.kb_auto_import",
			Type:  settings.FieldToggle,
			Value: boolString(rem.KBAutoImport),
		},
		settings.Field{
			Label: "Auto Index Sessions",
			Key:   "remembrances.auto_index_sessions",
			Type:  settings.FieldToggle,
			Value: boolString(rem.AutoIndexSessions),
		},
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

	// ── Context Enrichment ──
	// Build the list of indexed project IDs for the selector.
	projectOptions := []string{""}
	projectIDDisplay := map[string]string{"": "(none — KB only)"}
	if app != nil && app.Remembrances != nil && app.Remembrances.Code != nil {
		if projects, err := app.Remembrances.Code.ListProjects(context.Background()); err == nil {
			for _, p := range projects {
				projectOptions = append(projectOptions, p.ProjectID)
				label := p.ProjectID
				if p.Name != "" && p.Name != p.ProjectID {
					label = p.Name + " (" + p.ProjectID + ")"
				}
				projectIDDisplay[p.ProjectID] = label
			}
		}
	}
	// Build display-friendly options (keeps values as project IDs)
	projectDisplayOptions := make([]string, len(projectOptions))
	for i, id := range projectOptions {
		if disp, ok := projectIDDisplay[id]; ok {
			projectDisplayOptions[i] = disp
		} else {
			projectDisplayOptions[i] = id
		}
	}
	_ = projectDisplayOptions // used below in select Value resolution

	currentProject := rem.ContextEnrichmentCodeProject
	enrichEnabled := rem.ContextEnrichmentEnabled

	fields = append(fields,
		settings.Field{
			Label: "Context Enrichment",
			Key:   "remembrances.context_enrichment_enabled",
			Type:  settings.FieldToggle,
			Value: boolString(enrichEnabled),
		},
	)

	if enrichEnabled {
		projectSelectValue := currentProject
		if projectSelectValue == "" {
			projectSelectValue = projectOptions[0]
		}
		fields = append(fields,
			settings.Field{
				Label:   "Code Project",
				Key:     "remembrances.context_enrichment_code_project",
				Type:    settings.FieldSelect,
				Value:   projectSelectValue,
				Options: projectOptions,
			},
			settings.Field{
				Label: "KB Results",
				Key:   "remembrances.context_enrichment_kb_results",
				Type:  settings.FieldText,
				Value: fmt.Sprint(rem.ContextEnrichmentKBResults),
			},
			settings.Field{
				Label: "Code Results",
				Key:   "remembrances.context_enrichment_code_results",
				Type:  settings.FieldText,
				Value: fmt.Sprint(rem.ContextEnrichmentCodeResults),
			},
			settings.Field{
				Label: "Index working directory",
				Key:   "action:remembrances_index_workdir",
				Type:  settings.FieldAction,
				Value: config.WorkingDirectory(),
			},
		)
	}

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
			Label: "Exa Search Enabled",
			Key:   "internalTools.exaSearchEnabled",
			Type:  settings.FieldToggle,
			Value: boolString(it.ExaSearchEnabled),
		},
		{
			Label:  "Exa API Key",
			Key:    "internalTools.exaApiKey",
			Type:   settings.FieldText,
			Value:  it.ExaAPIKey,
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
			Value:    "Search tools are only active when their API key is configured. Env vars: GOOGLE_API_KEY, BRAVE_API_KEY, PERPLEXITY_API_KEY, EXA_API_KEY",
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

func buildOpenLitSection(cfg *config.Config) settings.Section {
	ol := cfg.OpenLit
	fields := []settings.Field{
		{Label: "Enabled", Key: "openlit.enabled", Type: settings.FieldToggle, Value: boolString(ol.Enabled)},
		{Label: "Endpoint", Key: "openlit.endpoint", Type: settings.FieldText, Value: ol.Endpoint},
		{Label: "Service Name", Key: "openlit.serviceName", Type: settings.FieldText, Value: ol.ServiceName},
		{Label: "Insecure TLS", Key: "openlit.insecure", Type: settings.FieldToggle, Value: boolString(ol.Insecure)},
	}
	if !ol.Enabled {
		for i := 1; i < len(fields); i++ {
			fields[i].Disabled = true
		}
	}
	return settings.Section{
		Title:  "OpenLit Observability",
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

func buildBashSection(cfg *config.Config) settings.Section {
	bash := cfg.Bash
	bannedDefault := "alias,curl,curlie,wget,axel,aria2c,nc,telnet,lynx,w3m,links,httpie,xh,http-prompt,chrome,firefox,safari"
	bannedValue := strings.Join(bash.BannedCommands, ",")
	allowedValue := strings.Join(bash.AllowedCommands, ",")

	return settings.Section{
		Title: "Bash",
		Fields: []settings.Field{
			{
				Label: "Banned Commands",
				Key:   "bash.bannedCommands",
				Type:  settings.FieldText,
				Value: bannedValue,
			},
			{
				Label: "Allowed Commands",
				Key:   "bash.allowedCommands",
				Type:  settings.FieldText,
				Value: allowedValue,
			},
			{
				Label:    "Default Banned List",
				Key:      "bash.defaultInfo",
				Type:     settings.FieldText,
				Value:    bannedDefault,
				ReadOnly: true,
			},
		},
	}
}

func buildPersonaAutoSelectSection(cfg *config.Config) settings.Section {
	pas := cfg.PersonaAutoSelect
	return settings.Section{
		Title: "Persona Auto-Select",
		Fields: []settings.Field{
			{
				Label: "Enabled",
				Key:   "personaAutoSelect.enabled",
				Type:  settings.FieldToggle,
				Value: boolString(pas.Enabled),
			},
			{
				Label:    "Persona Path",
				Key:      "personaAutoSelect.personaPath",
				Type:     settings.FieldText,
				Value:    pas.PersonaPath,
				Disabled: !pas.Enabled,
			},
			{
				Label:    "Info",
				Key:      "personaAutoSelect.info",
				Type:     settings.FieldText,
				Value:    "Uses agents[\"persona-selector\"] model. Falls back to mesnada.orchestrator.personaPath when empty.",
				Disabled: true,
			},
		},
	}
}

func buildEvaluatorSection(cfg *config.Config) settings.Section {
	// Apply recommended defaults for any zero/unset values so the TUI always
	// shows meaningful values even on a fresh installation.
	eval := config.EvaluatorWithDefaults(cfg.Evaluator)

	judgeTemplateValue := eval.JudgePromptTemplate
	judgeTemplateHint := "(built-in default — leave empty to use it)"
	if eval.JudgePromptTemplate != "" {
		judgeTemplateHint = "custom template path"
	}

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
			Label: "Alpha Weight",
			Key:   "evaluator.alphaWeight",
			Type:  settings.FieldText,
			Value: fmt.Sprintf("%.2f", eval.AlphaWeight),
			Hint:  "recommended: 0.80 (accuracy importance)",
		},
		{
			Label: "Beta Weight",
			Key:   "evaluator.betaWeight",
			Type:  settings.FieldText,
			Value: fmt.Sprintf("%.2f", eval.BetaWeight),
			Hint:  "recommended: 0.20 (token efficiency importance; α+β should sum to 1.0)",
		},
		{
			Label: "Exploration C",
			Key:   "evaluator.explorationC",
			Type:  settings.FieldText,
			Value: fmt.Sprintf("%.4f", eval.ExplorationC),
			Hint:  "recommended: 1.4142 (UCB1 exploration factor, √2)",
		},
		{
			Label: "Min Sessions UCB",
			Key:   "evaluator.minSessionsForUCB",
			Type:  settings.FieldText,
			Value: fmt.Sprint(eval.MinSessionsForUCB),
			Hint:  "recommended: 5 (min sessions before UCB selection activates)",
		},
		{
			Label: "Max Tokens Baseline",
			Key:   "evaluator.maxTokensBaseline",
			Type:  settings.FieldText,
			Value: fmt.Sprint(eval.MaxTokensBaseline),
			Hint:  "recommended: 50 (rolling window size for token efficiency)",
		},
		{
			Label: "Max Skills",
			Key:   "evaluator.maxSkills",
			Type:  settings.FieldText,
			Value: fmt.Sprint(eval.MaxSkills),
			Hint:  "recommended: 100 (max active skills in the library)",
		},
		{
			Label: "Judge Prompt Template",
			Key:   "evaluator.judgePromptTemplate",
			Type:  settings.FieldText,
			Value: judgeTemplateValue,
			Hint:  judgeTemplateHint,
		},
		{
			Label: "Async Evaluation",
			Key:   "evaluator.async",
			Type:  settings.FieldToggle,
			Value: boolString(eval.Async),
			Hint:  "recommended: true (run evaluation in background after session end)",
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

func withGroup(s settings.Section, group string) settings.Section {
	s.Group = group
	return s
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

// headersToString formats a headers map as sorted "Key:Value" pairs joined by spaces.
func headersToString(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+":"+headers[k])
	}
	return strings.Join(parts, " ")
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
	case strings.HasPrefix(field.Key, "openlit."):
		return saveOpenLit(field)
	case strings.HasPrefix(field.Key, "internalTools."):
		return saveInternalTools(field)
	case strings.HasPrefix(field.Key, "general."):
		return saveGeneral(field)
	case strings.HasPrefix(field.Key, "permissions."):
		return savePermissions(field)
	case strings.HasPrefix(field.Key, "server."):
		return saveServer(field)
	case strings.HasPrefix(field.Key, "lua."):
		return saveLua(field)
	case strings.HasPrefix(field.Key, "mcpGateway."):
		return saveMCPGateway(field)
	case strings.HasPrefix(field.Key, "snapshots."):
		return saveSnapshots(field)
	case strings.HasPrefix(field.Key, "bash."):
		return saveBash(field)
	case strings.HasPrefix(field.Key, "evaluator."):
		return saveEvaluator(field)
	case strings.HasPrefix(field.Key, "skillsCatalog."):
		return saveSkillsCatalog(field)
	case strings.HasPrefix(field.Key, "personaAutoSelect."):
		return savePersonaAutoSelect(field)
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
	case "useOAuth":
		useOAuth, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid useOAuth value: %w", err)
		}
		if providerName != models.ProviderAnthropic {
			return fmt.Errorf("useOAuth is only supported for the anthropic provider")
		}
		return config.UpdateProviderOAuth(providerName, useOAuth)
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
	if len(parts) != 3 {
		return fmt.Errorf("invalid agent setting key %q", field.Key)
	}

	agentName := config.AgentName(parts[1])
	agentCfg := config.Get().Agents[agentName]

	switch parts[2] {
	case "model":
		modelID := models.ModelID(strings.TrimSpace(field.Value))
		if _, ok := models.SupportedModels[modelID]; !ok {
			return fmt.Errorf("model %s not supported", modelID)
		}
		return config.UpdateAgentModel(agentName, modelID)
	case "maxTokens":
		v, err := strconv.ParseInt(strings.TrimSpace(field.Value), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid max tokens value: %w", err)
		}
		if v <= 0 {
			return fmt.Errorf("max tokens must be greater than zero")
		}
		agentCfg.MaxTokens = v
	case "reasoningEffort":
		effort := strings.TrimSpace(field.Value)
		switch effort {
		case "", "low", "medium", "high":
			// valid
		default:
			return fmt.Errorf("reasoning effort must be one of: empty, low, medium, high")
		}
		agentCfg.ReasoningEffort = effort
	case "thinkingMode":
		mode := config.ThinkingMode(strings.TrimSpace(field.Value))
		switch mode {
		case "", config.ThinkingDisabled, config.ThinkingLow, config.ThinkingMedium, config.ThinkingHigh:
			// valid
		default:
			return fmt.Errorf("thinking mode must be one of: empty, disabled, low, medium, high")
		}
		agentCfg.ThinkingMode = mode
	case "autoCompact":
		v, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid auto compact value: %w", err)
		}
		agentCfg.AutoCompact = v
	case "autoCompactThreshold":
		v, err := strconv.ParseFloat(strings.TrimSpace(field.Value), 64)
		if err != nil {
			return fmt.Errorf("invalid auto compact threshold value: %w", err)
		}
		if v < 0.0 || v > 1.0 {
			return fmt.Errorf("auto compact threshold must be between 0.0 and 1.0")
		}
		agentCfg.AutoCompactThreshold = v
	default:
		return fmt.Errorf("unsupported agent field %q", parts[2])
	}

	return config.UpdateAgent(agentName, agentCfg)
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
	case "env":
		server.Env = strings.Fields(field.Value)
	case "headers":
		headers, err := parseHeaders(field.Value)
		if err != nil {
			return fmt.Errorf("invalid headers format: %w", err)
		}
		server.Headers = headers
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
	case "args":
		lspCfg.Args = strings.Fields(field.Value)
	case "languages":
		lspCfg.Languages = strings.Fields(field.Value)
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
	case "mesnada.orchestrator.storePath":
		mesnadaCfg.Orchestrator.StorePath = strings.TrimSpace(field.Value)
	case "mesnada.orchestrator.logDir":
		mesnadaCfg.Orchestrator.LogDir = strings.TrimSpace(field.Value)
	case "mesnada.orchestrator.defaultModel":
		mesnadaCfg.Orchestrator.DefaultModel = strings.TrimSpace(field.Value)
	case "mesnada.orchestrator.defaultMcpConfig":
		mesnadaCfg.Orchestrator.DefaultMCPConfig = strings.TrimSpace(field.Value)
	case "mesnada.acp.defaultAgent":
		mesnadaCfg.ACP.DefaultAgent = strings.TrimSpace(field.Value)
	case "mesnada.acp.server.enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid ACP server enabled value: %w", err)
		}
		mesnadaCfg.ACP.Server.Enabled = enabled
	case "mesnada.acp.server.host":
		host := strings.TrimSpace(field.Value)
		if host == "" {
			return fmt.Errorf("ACP server host cannot be empty")
		}
		mesnadaCfg.ACP.Server.Host = host
	case "mesnada.acp.server.port":
		port, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid ACP server port: %w", err)
		}
		if port < 1 || port > 65535 {
			return fmt.Errorf("ACP server port must be between 1 and 65535")
		}
		mesnadaCfg.ACP.Server.Port = port
	case "mesnada.acp.server.maxSessions":
		maxSessions, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid ACP server max sessions: %w", err)
		}
		if maxSessions < 1 {
			return fmt.Errorf("ACP server max sessions must be greater than zero")
		}
		mesnadaCfg.ACP.Server.MaxSessions = maxSessions
	case "mesnada.acp.server.sessionTimeout":
		timeout := strings.TrimSpace(field.Value)
		if timeout == "" {
			return fmt.Errorf("ACP server session timeout cannot be empty")
		}
		mesnadaCfg.ACP.Server.SessionTimeout = timeout
	case "mesnada.acp.server.requireAuth":
		requireAuth, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid ACP server require auth value: %w", err)
		}
		mesnadaCfg.ACP.Server.RequireAuth = requireAuth
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
	case "remembrances.kb_path":
		remCfg.KBPath = strings.TrimSpace(field.Value)
	case "remembrances.kb_watch":
		watch, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid KB watch value: %w", err)
		}
		remCfg.KBWatch = watch
	case "remembrances.kb_auto_import":
		autoImport, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid KB auto import value: %w", err)
		}
		remCfg.KBAutoImport = autoImport
	case "remembrances.auto_index_sessions":
		autoIndex, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid auto index sessions value: %w", err)
		}
		remCfg.AutoIndexSessions = autoIndex
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
	case "remembrances.context_enrichment_enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid context enrichment enabled value: %w", err)
		}
		remCfg.ContextEnrichmentEnabled = enabled
	case "remembrances.context_enrichment_code_project":
		remCfg.ContextEnrichmentCodeProject = strings.TrimSpace(field.Value)
	case "remembrances.context_enrichment_kb_results":
		n, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid KB results value: %w", err)
		}
		if n < 1 || n > 20 {
			return fmt.Errorf("KB results must be between 1 and 20")
		}
		remCfg.ContextEnrichmentKBResults = n
	case "remembrances.context_enrichment_code_results":
		n, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid code results value: %w", err)
		}
		if n < 1 || n > 20 {
			return fmt.Errorf("code results must be between 1 and 20")
		}
		remCfg.ContextEnrichmentCodeResults = n
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
	case "internalTools.exaSearchEnabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Exa Search Enabled value: %w", err)
		}
		itCfg.ExaSearchEnabled = enabled
	case "internalTools.exaApiKey":
		itCfg.ExaAPIKey = strings.TrimSpace(field.Value)
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

// parseHeaders parses a "Key1:Value1 Key2:Value2" formatted string into a map.
func parseHeaders(s string) (map[string]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	result := make(map[string]string)
	for _, pair := range strings.Fields(s) {
		idx := strings.IndexByte(pair, ':')
		if idx < 0 {
			return nil, fmt.Errorf("invalid header pair %q: expected Key:Value format", pair)
		}
		key := strings.TrimSpace(pair[:idx])
		value := strings.TrimSpace(pair[idx+1:])
		if key == "" {
			return nil, fmt.Errorf("invalid header pair %q: key cannot be empty", pair)
		}
		result[key] = value
	}
	return result, nil
}

func saveGeneral(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	workingDir := cfg.WorkingDir
	logFile := cfg.LogFile
	debugLSP := cfg.DebugLSP
	contextPaths := append([]string(nil), cfg.ContextPaths...)
	dataDir := cfg.Data.Directory

	switch field.Key {
	case "general.workingDir":
		workingDir = strings.TrimSpace(field.Value)
	case "general.logFile":
		logFile = strings.TrimSpace(field.Value)
	case "general.debugLSP":
		v, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Debug LSP value: %w", err)
		}
		debugLSP = v
	case "general.contextPaths":
		parts := strings.Split(field.Value, ",")
		contextPaths = make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				contextPaths = append(contextPaths, trimmed)
			}
		}
	case "general.data.directory":
		dataDir = strings.TrimSpace(field.Value)
	default:
		return fmt.Errorf("unsupported general setting %q", field.Key)
	}

	return config.UpdateGeneral(workingDir, logFile, debugLSP, contextPaths, dataDir)
}

func savePermissions(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	perms := cfg.Permissions
	switch field.Key {
	case "permissions.autoApproveTools":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid permissions auto approve tools value: %w", err)
		}
		perms.AutoApproveTools = enabled
	default:
		return fmt.Errorf("unsupported permissions setting %q", field.Key)
	}

	return config.UpdatePermissions(perms)
}

func saveServer(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	serverCfg := cfg.Server
	switch field.Key {
	case "server.enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid server enabled value: %w", err)
		}
		serverCfg.Enabled = enabled
	case "server.host":
		host := strings.TrimSpace(field.Value)
		if host == "" {
			return fmt.Errorf("server host cannot be empty")
		}
		serverCfg.Host = host
	case "server.port":
		port, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid server port: %w", err)
		}
		if port < 1 || port > 65535 {
			return fmt.Errorf("server port must be between 1 and 65535")
		}
		serverCfg.Port = port
	case "server.requireAuth":
		requireAuth, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid server require auth value: %w", err)
		}
		serverCfg.RequireAuth = requireAuth
	case "server.info.disabled":
		// read-only informational field, nothing to save
		return nil
	default:
		return fmt.Errorf("unsupported server setting %q", field.Key)
	}

	return config.UpdateServer(serverCfg)
}

func saveOpenLit(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	ol := cfg.OpenLit
	switch field.Key {
	case "openlit.enabled":
		v, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid OpenLit enabled value: %w", err)
		}
		ol.Enabled = v
	case "openlit.endpoint":
		ol.Endpoint = strings.TrimSpace(field.Value)
	case "openlit.serviceName":
		ol.ServiceName = strings.TrimSpace(field.Value)
	case "openlit.insecure":
		v, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid OpenLit insecure value: %w", err)
		}
		ol.Insecure = v
	default:
		return fmt.Errorf("unsupported OpenLit setting %q", field.Key)
	}
	return config.UpdateOpenLit(ol)
}

func saveLua(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	luaCfg := cfg.Lua
	switch field.Key {
	case "lua.enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Lua enabled value: %w", err)
		}
		luaCfg.Enabled = enabled
	case "lua.scriptPath":
		luaCfg.ScriptPath = strings.TrimSpace(field.Value)
	case "lua.timeout":
		luaCfg.Timeout = strings.TrimSpace(field.Value)
	case "lua.strictMode":
		v, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Lua strict mode value: %w", err)
		}
		luaCfg.StrictMode = v
	case "lua.hotReload":
		v, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Lua hot reload value: %w", err)
		}
		luaCfg.HotReload = v
	case "lua.logFilteredData":
		v, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Lua log filtered data value: %w", err)
		}
		luaCfg.LogFilteredData = v
	case "lua.info.disabled":
		// read-only informational field, nothing to save
		return nil
	default:
		return fmt.Errorf("unsupported Lua setting %q", field.Key)
	}

	return config.UpdateLua(luaCfg)
}

func saveMCPGateway(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	gwCfg := cfg.MCPGateway
	switch field.Key {
	case "mcpGateway.enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid MCP Gateway enabled value: %w", err)
		}
		gwCfg.Enabled = enabled
	case "mcpGateway.favoriteThreshold":
		v, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid favorite threshold: %w", err)
		}
		if v < 0 {
			return fmt.Errorf("favorite threshold must be >= 0")
		}
		gwCfg.FavoriteThreshold = v
	case "mcpGateway.maxFavorites":
		v, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid max favorites: %w", err)
		}
		if v < 0 {
			return fmt.Errorf("max favorites must be >= 0")
		}
		gwCfg.MaxFavorites = v
	case "mcpGateway.favoriteWindowDays":
		v, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid favorite window days: %w", err)
		}
		if v < 0 {
			return fmt.Errorf("favorite window days must be >= 0")
		}
		gwCfg.FavoriteWindowDays = v
	case "mcpGateway.decayDays":
		v, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid decay days: %w", err)
		}
		if v < 0 {
			return fmt.Errorf("decay days must be >= 0")
		}
		gwCfg.DecayDays = v
	case "mcpGateway.info":
		// read-only informational field, nothing to save
		return nil
	default:
		return fmt.Errorf("unsupported MCP Gateway setting %q", field.Key)
	}

	return config.UpdateMCPGateway(gwCfg)
}

func saveSnapshots(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	snapCfg := cfg.Snapshots
	switch field.Key {
	case "snapshots.enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid snapshots enabled value: %w", err)
		}
		snapCfg.Enabled = enabled
	case "snapshots.maxSnapshots":
		v, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid max snapshots: %w", err)
		}
		if v < 1 {
			return fmt.Errorf("max snapshots must be greater than zero")
		}
		snapCfg.MaxSnapshots = v
	case "snapshots.maxFileSize":
		size := strings.TrimSpace(field.Value)
		if size == "" {
			return fmt.Errorf("max file size cannot be empty")
		}
		snapCfg.MaxFileSize = size
	case "snapshots.excludePatterns":
		parts := strings.Split(field.Value, ",")
		patterns := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				patterns = append(patterns, trimmed)
			}
		}
		snapCfg.ExcludePatterns = patterns
	case "snapshots.autoCleanupDays":
		v, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid auto cleanup days: %w", err)
		}
		if v < 0 {
			return fmt.Errorf("auto cleanup days must be >= 0")
		}
		snapCfg.AutoCleanupDays = v
	case "snapshots.info":
		// read-only informational field, nothing to save
		return nil
	default:
		return fmt.Errorf("unsupported snapshots setting %q", field.Key)
	}

	return config.UpdateSnapshots(snapCfg)
}

func saveEvaluator(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	evalCfg := cfg.Evaluator
	switch field.Key {
	case "evaluator.enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid evaluator enabled value: %w", err)
		}
		evalCfg.Enabled = enabled
	case "evaluator.model":
		modelID := models.ModelID(strings.TrimSpace(field.Value))
		evalCfg.Model = modelID
		if model, ok := models.SupportedModels[modelID]; ok {
			evalCfg.Provider = string(model.Provider)
		}
	case "evaluator.alphaWeight":
		v, err := strconv.ParseFloat(strings.TrimSpace(field.Value), 64)
		if err != nil {
			return fmt.Errorf("invalid alpha weight: %w", err)
		}
		if v < 0.0 || v > 1.0 {
			return fmt.Errorf("alpha weight must be between 0.0 and 1.0")
		}
		evalCfg.AlphaWeight = v
	case "evaluator.betaWeight":
		v, err := strconv.ParseFloat(strings.TrimSpace(field.Value), 64)
		if err != nil {
			return fmt.Errorf("invalid beta weight: %w", err)
		}
		if v < 0.0 || v > 1.0 {
			return fmt.Errorf("beta weight must be between 0.0 and 1.0")
		}
		evalCfg.BetaWeight = v
	case "evaluator.explorationC":
		v, err := strconv.ParseFloat(strings.TrimSpace(field.Value), 64)
		if err != nil {
			return fmt.Errorf("invalid exploration C: %w", err)
		}
		if v < 0.0 {
			return fmt.Errorf("exploration C must be >= 0")
		}
		evalCfg.ExplorationC = v
	case "evaluator.minSessionsForUCB":
		v, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid min sessions for UCB: %w", err)
		}
		if v < 1 {
			return fmt.Errorf("min sessions for UCB must be >= 1")
		}
		evalCfg.MinSessionsForUCB = v
	case "evaluator.maxTokensBaseline":
		v, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid max tokens baseline: %w", err)
		}
		if v < 1 {
			return fmt.Errorf("max tokens baseline must be >= 1")
		}
		evalCfg.MaxTokensBaseline = v
	case "evaluator.maxSkills":
		v, err := parseIntValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid max skills: %w", err)
		}
		if v < 1 {
			return fmt.Errorf("max skills must be >= 1")
		}
		evalCfg.MaxSkills = v
	case "evaluator.judgePromptTemplate":
		evalCfg.JudgePromptTemplate = strings.TrimSpace(field.Value)
	case "evaluator.async":
		v, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid evaluator async value: %w", err)
		}
		evalCfg.Async = v
	case "evaluator.correctionsPatterns":
		parts := strings.Split(field.Value, ",")
		patterns := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				patterns = append(patterns, trimmed)
			}
		}
		evalCfg.CorrectionsPatterns = patterns
	case "evaluator.info":
		// read-only informational field, nothing to save
		return nil
	default:
		return fmt.Errorf("unsupported evaluator setting %q", field.Key)
	}

	return config.UpdateEvaluator(evalCfg)
}

func savePersonaAutoSelect(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	pasCfg := cfg.PersonaAutoSelect
	switch field.Key {
	case "personaAutoSelect.enabled":
		v, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid personaAutoSelect enabled value: %w", err)
		}
		pasCfg.Enabled = v
	case "personaAutoSelect.personaPath":
		pasCfg.PersonaPath = strings.TrimSpace(field.Value)
	case "personaAutoSelect.info":
		return nil
	default:
		return fmt.Errorf("unsupported personaAutoSelect setting %q", field.Key)
	}

	return config.UpdatePersonaAutoSelect(pasCfg)
}

func saveSkillsCatalog(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	scCfg := cfg.SkillsCatalog
	switch field.Key {
	case "skillsCatalog.enabled":
		v, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid catalog enabled value: %w", err)
		}
		scCfg.Enabled = v
	case "skillsCatalog.baseUrl":
		url := strings.TrimSpace(field.Value)
		if url == "" {
			return fmt.Errorf("catalog URL cannot be empty")
		}
		scCfg.BaseURL = url
	case "skillsCatalog.defaultScope":
		scope := strings.TrimSpace(field.Value)
		if scope != "global" && scope != "project" {
			return fmt.Errorf("default scope must be \"global\" or \"project\"")
		}
		scCfg.DefaultScope = scope
	case "skillsCatalog.autoUpdate":
		v, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid auto update value: %w", err)
		}
		scCfg.AutoUpdate = v
	case "skillsCatalog.lockFile.info":
		// read-only informational field, nothing to save
		return nil
	default:
		return fmt.Errorf("unsupported skillsCatalog setting %q", field.Key)
	}

	return config.UpdateSkillsCatalog(scCfg)
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

func saveBash(field settings.Field) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	bashCfg := cfg.Bash
	splitCommands := func(val string) []string {
		parts := strings.Split(val, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	}

	switch field.Key {
	case "bash.bannedCommands":
		bashCfg.BannedCommands = splitCommands(field.Value)
	case "bash.allowedCommands":
		bashCfg.AllowedCommands = splitCommands(field.Value)
	case "bash.defaultInfo":
		// read-only informational field, nothing to save
		return nil
	default:
		return fmt.Errorf("unsupported bash setting %q", field.Key)
	}

	return config.UpdateBash(bashCfg)
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
