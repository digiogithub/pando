package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/format"
	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/lsp"
	mesnadaConfig "github.com/digiogithub/pando/internal/mesnada/config"
	mesnadaOrch "github.com/digiogithub/pando/internal/mesnada/orchestrator"
	mesnadaServer "github.com/digiogithub/pando/internal/mesnada/server"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/skills"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/version"
)

type App struct {
	Sessions    session.Service
	Messages    message.Service
	History     history.Service
	Permissions permission.Service

	CoderAgent agent.Service

	LSPClients          map[string]*lsp.Client
	SkillManager        *skills.SkillManager
	MesnadaOrchestrator *mesnadaOrch.Orchestrator
	MesnadaServer       *mesnadaServer.Server

	clientsMutex sync.RWMutex

	watcherCancelFuncs []context.CancelFunc
	cancelFuncsMutex   sync.Mutex
	watcherWG          sync.WaitGroup
}

func New(ctx context.Context, conn *sql.DB) (*App, error) {
	q := db.New(conn)
	sessions := session.NewService(q)
	messages := message.NewService(q)
	files := history.NewService(q, conn)

	app := &App{
		Sessions:    sessions,
		Messages:    messages,
		History:     files,
		Permissions: permission.NewPermissionService(),
		LSPClients:  make(map[string]*lsp.Client),
	}

	// Initialize theme based on configuration
	app.initTheme()

	if cfg := config.Get(); cfg != nil && cfg.Skills.Enabled {
		skillManager, err := newSkillManager(cfg)
		if err != nil {
			return nil, err
		}
		app.SkillManager = skillManager
	}

	// Initialize LSP clients in the background
	go app.initLSPClients(ctx)
	logging.Debug("LSP clients initialization started")

	// Refresh dynamic models from configured providers in the background
	go app.refreshDynamicModels(ctx)

	// Initialize Mesnada orchestrator if enabled
	cfg := config.Get()
	if cfg != nil && cfg.Mesnada.Enabled {
		mesnadaCfg := convertMesnadaConfig(cfg)
		orch, err := mesnadaOrch.New(mesnadaCfg)
		if err != nil {
			logging.Error("Failed to create mesnada orchestrator", "error", err)
		} else {
			app.MesnadaOrchestrator = orch

			// Create and start HTTP server
			addr := fmt.Sprintf("%s:%d", cfg.Mesnada.Server.Host, cfg.Mesnada.Server.Port)
			srv := mesnadaServer.New(mesnadaServer.Config{
				Addr:         addr,
				Orchestrator: orch,
				Version:      version.Version,
				UseStdio:     false,
				AppConfig:    mesnadaCfg.AppConfig,
			})
			app.MesnadaServer = srv

			// Start HTTP server in background
			go func() {
				if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logging.Error("Mesnada server error", "error", err)
				}
			}()
			logging.Info("Mesnada orchestrator started", "addr", addr)
		logging.Debug("Mesnada orchestrator created", "addr", addr)
		}
	}

	var err error
	app.CoderAgent, err = agent.NewAgent(
		config.AgentCoder,
		app.Sessions,
		app.Messages,
		agent.CoderAgentToolsWithMesnada(
			app.MesnadaOrchestrator,
			app.Permissions,
			app.Sessions,
			app.Messages,
			app.History,
			app.LSPClients,
			app.SkillManager,
		),
		app.SkillManager,
	)
	if err != nil {
		logging.Error("Failed to create coder agent", err)
		return nil, err
	}
	logging.Debug("Coder agent created", "model", app.CoderAgent.Model().ID)

	logging.Debug("App created", "workingDir", config.WorkingDirectory())
	return app, nil
}

func convertMesnadaConfig(cfg *config.Config) mesnadaOrch.Config {
	appCfg := convertToMesnadaAppConfig(cfg)

	return mesnadaOrch.Config{
		StorePath:        appCfg.Orchestrator.StorePath,
		LogDir:           appCfg.Orchestrator.LogDir,
		MaxParallel:      appCfg.Orchestrator.MaxParallel,
		DefaultMCPConfig: appCfg.Orchestrator.DefaultMCPConfig,
		DefaultEngine:    appCfg.Orchestrator.DefaultEngine,
		PersonaPath:      appCfg.Orchestrator.PersonaPath,
		AppConfig:        appCfg,
	}
}

func convertToMesnadaAppConfig(cfg *config.Config) *mesnadaConfig.Config {
	mesnadaCfg := mesnadaConfig.DefaultConfig()
	if cfg == nil {
		return mesnadaCfg
	}

	mesnadaCfg.Server.Host = cfg.Mesnada.Server.Host
	mesnadaCfg.Server.Port = cfg.Mesnada.Server.Port

	if storePath := expandMesnadaPath(cfg.Mesnada.Orchestrator.StorePath); storePath != "" {
		mesnadaCfg.Orchestrator.StorePath = storePath
	}
	if logDir := expandMesnadaPath(cfg.Mesnada.Orchestrator.LogDir); logDir != "" {
		mesnadaCfg.Orchestrator.LogDir = logDir
	}
	if cfg.Mesnada.Orchestrator.MaxParallel > 0 {
		mesnadaCfg.Orchestrator.MaxParallel = cfg.Mesnada.Orchestrator.MaxParallel
	}
	if cfg.Mesnada.Orchestrator.DefaultEngine != "" {
		mesnadaCfg.Orchestrator.DefaultEngine = cfg.Mesnada.Orchestrator.DefaultEngine
	}
	if defaultMCPConfig := expandMesnadaMCPConfig(cfg.Mesnada.Orchestrator.DefaultMCPConfig); defaultMCPConfig != "" {
		mesnadaCfg.Orchestrator.DefaultMCPConfig = defaultMCPConfig
	}
	if personaPath := expandMesnadaPath(cfg.Mesnada.Orchestrator.PersonaPath); personaPath != "" {
		mesnadaCfg.Orchestrator.PersonaPath = personaPath
	}

	mesnadaCfg.ACP.Enabled = cfg.Mesnada.ACP.Enabled
	mesnadaCfg.ACP.DefaultAgent = cfg.Mesnada.ACP.DefaultAgent
	mesnadaCfg.ACP.AutoPermission = cfg.Mesnada.ACP.AutoPermission

	mesnadaCfg.TUI.Enabled = cfg.Mesnada.TUI.Enabled
	mesnadaCfg.TUI.WebUI = cfg.Mesnada.TUI.WebUI

	return mesnadaCfg
}

func expandMesnadaPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if value == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return value
		}
		return home
	}
	if strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return value
		}
		return filepath.Join(home, value[2:])
	}
	return value
}

func expandMesnadaMCPConfig(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if strings.HasPrefix(value, "@") {
		return "@" + expandMesnadaPath(value[1:])
	}
	return expandMesnadaPath(value)
}

func newSkillManager(cfg *config.Config) (*skills.SkillManager, error) {
	discoveryPaths := append([]string{}, skills.DiscoveryPaths(config.WorkingDirectory())...)
	for _, skillPath := range cfg.Skills.Paths {
		if strings.TrimSpace(skillPath) == "" {
			continue
		}
		if !filepath.IsAbs(skillPath) {
			skillPath = filepath.Join(cfg.WorkingDir, skillPath)
		}
		discoveryPaths = append(discoveryPaths, filepath.Clean(skillPath))
	}

	discoveredSkills, err := skills.DiscoverSkills(discoveryPaths)
	if err != nil {
		return nil, fmt.Errorf("discover skills: %w", err)
	}

	skillManager := skills.NewSkillManager(20)
	if len(discoveredSkills) > 0 {
		skillPaths := make([]string, 0, len(discoveredSkills))
		for _, skill := range discoveredSkills {
			skillPaths = append(skillPaths, skill.SourcePath)
		}
		if err := skillManager.LoadAll(skillPaths); err != nil {
			return nil, fmt.Errorf("load skills: %w", err)
		}
	}

	logging.Info("Loaded skills", "count", len(discoveredSkills), "search_paths", discoveryPaths)
	logging.Debug("Skill manager initialized", "skillCount", len(discoveredSkills))
	return skillManager, nil
}

// initTheme sets the application theme based on the configuration
func (app *App) initTheme() {
	cfg := config.Get()
	if cfg == nil || cfg.TUI.Theme == "" {
		return // Use default theme
	}

	// Try to set the theme from config
	err := theme.ApplyTheme(cfg.TUI.Theme)
	if err != nil {
		logging.Warn("Failed to set theme from config, using default theme", "theme", cfg.TUI.Theme, "error", err)
	} else {
		logging.Debug("Set theme from config", "theme", cfg.TUI.Theme)
	}
}

// RunNonInteractive handles the execution flow when a prompt is provided via CLI flag.
func (a *App) RunNonInteractive(ctx context.Context, prompt string, outputFormat string, quiet bool, yoloMode bool) error {
	logging.Info("Running in non-interactive mode")
	logging.Debug("Non-interactive mode started", "promptLength", len(prompt), "outputFormat", outputFormat, "yoloMode", yoloMode)

	if yoloMode {
		a.Permissions.SetGlobalAutoApprove(true)
	}

	// Start spinner if not in quiet mode
	var spinner *format.Spinner
	if !quiet {
		spinner = format.NewSpinner("Thinking...")
		spinner.Start()
		defer spinner.Stop()
	}

	const maxPromptLengthForTitle = 100
	titlePrefix := "Non-interactive: "
	var titleSuffix string

	if len(prompt) > maxPromptLengthForTitle {
		titleSuffix = prompt[:maxPromptLengthForTitle] + "..."
	} else {
		titleSuffix = prompt
	}
	title := titlePrefix + titleSuffix

	sess, err := a.Sessions.Create(ctx, title)
	if err != nil {
		return fmt.Errorf("failed to create session for non-interactive mode: %w", err)
	}
	logging.Info("Created session for non-interactive run", "session_id", sess.ID)

	if !yoloMode {
		// Automatically approve all permission requests for this non-interactive session
		a.Permissions.AutoApproveSession(sess.ID)
	}

	done, err := a.CoderAgent.Run(ctx, sess.ID, prompt)
	if err != nil {
		return fmt.Errorf("failed to start agent processing stream: %w", err)
	}

	result := <-done
	logging.Debug("Non-interactive agent completed", "sessionID", sess.ID, "hasError", result.Error != nil)
	if result.Error != nil {
		if errors.Is(result.Error, context.Canceled) || errors.Is(result.Error, agent.ErrRequestCancelled) {
			logging.Info("Agent processing cancelled", "session_id", sess.ID)
			return nil
		}
		return fmt.Errorf("agent processing failed: %w", result.Error)
	}

	// Stop spinner before printing output
	if !quiet && spinner != nil {
		spinner.Stop()
	}

	// Get the text content from the response
	content := "No content available"
	if result.Message.Content().String() != "" {
		content = result.Message.Content().String()
	}

	fmt.Println(format.FormatOutput(content, outputFormat))

	logging.Info("Non-interactive run completed", "session_id", sess.ID)

	return nil
}

// refreshDynamicModels fetches model lists from configured providers asynchronously.
func (app *App) refreshDynamicModels(ctx context.Context) {
	cfg := config.Get()
	if cfg == nil {
		return
	}
	logging.Debug("Refreshing dynamic models", "providerCount", len(cfg.Providers))

	for providerID, providerCfg := range cfg.Providers {
		if providerCfg.Disabled {
			continue
		}

		apiKey := providerCfg.APIKey
		var bearerToken string
		if providerID == models.ProviderCopilot {
			token, err := auth.LoadGitHubOAuthToken()
			if err != nil || token == "" {
				continue
			}
			bearerToken = token
			apiKey = ""
		} else if providerID != models.ProviderOllama && apiKey == "" {
			continue
		}

		if err := models.RefreshProviderModels(ctx, providerID, apiKey, bearerToken, providerCfg.BaseURL); err != nil {
			logging.Debug("Failed to refresh models from provider", "provider", providerID, "error", err)
		} else {
			logging.Debug("Refreshed models from provider", "provider", providerID)
			if err := models.SaveModelCache(); err != nil {
				logging.Debug("Failed to save model cache", "error", err)
			}
		}
	}
}

// Shutdown performs a clean shutdown of the application
func (app *App) Shutdown() {
	logging.Debug("App shutdown started")
	if app.MesnadaServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := app.MesnadaServer.Shutdown(shutdownCtx); err != nil {
			logging.Error("Failed to shutdown Mesnada server", "error", err)
		}
		cancel()
	}
	if app.MesnadaOrchestrator != nil {
		if err := app.MesnadaOrchestrator.Shutdown(); err != nil {
			logging.Error("Failed to shutdown Mesnada orchestrator", "error", err)
		}
	}

	// Cancel all watcher goroutines
	app.cancelFuncsMutex.Lock()
	for _, cancel := range app.watcherCancelFuncs {
		cancel()
	}
	app.cancelFuncsMutex.Unlock()
	app.watcherWG.Wait()

	// Perform additional cleanup for LSP clients
	app.clientsMutex.RLock()
	clients := make(map[string]*lsp.Client, len(app.LSPClients))
	maps.Copy(clients, app.LSPClients)
	app.clientsMutex.RUnlock()

	for name, client := range clients {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := client.Shutdown(shutdownCtx); err != nil {
			logging.Error("Failed to shutdown LSP client", "name", name, "error", err)
		}
		cancel()
	}
	logging.Debug("App shutdown completed")
}
