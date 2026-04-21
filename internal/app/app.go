package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/cronjob"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/format"
	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/prompt"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/lsp"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/mcpgateway"
	mesnadaACP "github.com/digiogithub/pando/internal/mesnada/acp"
	mesnadaConfig "github.com/digiogithub/pando/internal/mesnada/config"
	mesnadaOrch "github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/internal/mesnada/persona"
	"github.com/digiogithub/pando/internal/mesnada/persona/builtin"
	mesnadaServer "github.com/digiogithub/pando/internal/mesnada/server"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/observability"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/project"
	"github.com/digiogithub/pando/internal/pubsub"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/skills"
	"github.com/digiogithub/pando/internal/snapshot"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/version"
)

type App struct {
	Sessions    session.Service
	Messages    message.Service
	History     history.Service
	Permissions permission.Service

	CoderAgent agent.Service

	Projects            project.Service
	ProjectManager      *project.Manager
	Snapshots           snapshot.Service
	LSPClients          map[string]*lsp.Client
	SkillManager        *skills.SkillManager
	MesnadaOrchestrator *mesnadaOrch.Orchestrator
	CronService         *cronjob.Service
	MesnadaServer       *mesnadaServer.Server
	Remembrances        *rag.RemembrancesService
	LuaManager          *luaengine.FilterManager
	MCPGateway          *mcpgateway.Gateway
	Evaluator           *evaluator.EvaluatorService

	openlitShutdown func(context.Context) error

	clientsMutex sync.RWMutex

	watcherCancelFuncs []context.CancelFunc
	cancelFuncsMutex   sync.Mutex
	watcherWG          sync.WaitGroup
}

// AppOptions configures optional behaviour for New().
type AppOptions struct {
	// SkipLSP disables LSP client initialisation. Set this to true in headless
	// modes (e.g. ACP stdio) where the editor manages its own language servers.
	SkipLSP bool
}

// findFreePort returns the first available TCP port starting at preferred, trying
// up to 10 sequential candidates, then falling back to an OS-assigned port.
func findFreePort(host string, preferred int) int {
	if host == "" {
		host = "127.0.0.1"
	}
	for offset := 0; offset <= 10; offset++ {
		port := preferred + offset
		ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
		if err == nil {
			_ = ln.Close()
			return port
		}
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return preferred // give up, let the server fail with a clear error
	}
	defer ln.Close()
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return preferred
}

func New(ctx context.Context, conn *sql.DB, opts ...AppOptions) (*App, error) {
	opt := AppOptions{}
	if len(opts) > 0 {
		opt = opts[0]
	}

	q := db.New(conn)
	sessions := session.NewService(q)
	messages := message.NewService(q)
	files := history.NewService(q, conn)
	projects := project.NewService(q)

	app := &App{
		Sessions:    sessions,
		Messages:    messages,
		History:     files,
		Permissions: permission.NewPermissionService(),
		Projects:    projects,
		LSPClients:  make(map[string]*lsp.Client),
	}

	// Initialize project manager (Phase 2).
	mgr, mgrErr := project.NewManager(ctx, projects)
	if mgrErr != nil {
		return nil, fmt.Errorf("failed to initialize project manager: %w", mgrErr)
	}
	app.ProjectManager = mgr

	// Initialize theme based on configuration
	app.initTheme()

	if cfg := config.Get(); cfg != nil && cfg.Skills.Enabled {
		skillManager, err := newSkillManager(cfg)
		if err != nil {
			return nil, err
		}
		app.SkillManager = skillManager
	}

	// Initialize LSP clients in the background (skipped in headless/ACP mode).
	if !opt.SkipLSP {
		go app.initLSPClients(ctx)
		logging.Debug("LSP clients initialization started")
	}

	// Refresh dynamic models from configured providers in the background
	go app.refreshDynamicModels(ctx)

	// Initialize Remembrances service if enabled
	cfg := config.Get()
	if cfg != nil {
		// --- OpenLit Observability Init ---
		{
			shutdownFn, err := observability.Init(cfg.OpenLit, version.Version)
			if err != nil {
				logging.Warn("OpenLit observability init failed", "error", err)
				shutdownFn = func(ctx context.Context) error { return nil }
			}
			app.openlitShutdown = shutdownFn
		}

		remembrances, err := rag.NewRemembrancesService(conn, &cfg.Remembrances)
		if err != nil {
			logging.Error("Failed to create remembrances service", "error", err)
		} else {
			app.Remembrances = remembrances
			if remembrances != nil {
				logging.Info("Remembrances service initialized")
				app.initRemembrancesKBSync(ctx, remembrances, &cfg.Remembrances)
				app.initRemembrancesSessionIndexing(ctx, remembrances, &cfg.Remembrances)

				// Initialize context enricher if enabled: searches KB and code index
				// before every user prompt and prepends relevant context.
				if cfg.Remembrances.ContextEnrichmentEnabled {
					enricher := rag.NewContextEnricher(
						remembrances,
						cfg.Remembrances.ContextEnrichmentKBResults,
						cfg.Remembrances.ContextEnrichmentCodeResults,
						cfg.Remembrances.ContextEnrichmentCodeProject,
					)
					agent.SetContextEnricher(enricher)
					logging.Info("remembrances: context enricher enabled",
						"kb_results", cfg.Remembrances.ContextEnrichmentKBResults,
						"code_results", cfg.Remembrances.ContextEnrichmentCodeResults,
						"code_project", cfg.Remembrances.ContextEnrichmentCodeProject,
					)
				}
			}
		}
	}

	// Initialize Lua filter manager if enabled
	cfg = config.Get()
	if cfg != nil && cfg.Lua.Enabled && cfg.Lua.ScriptPath != "" {
		luaTimeout := 5 * time.Second
		if cfg.Lua.Timeout != "" {
			if d, err := time.ParseDuration(cfg.Lua.Timeout); err == nil {
				luaTimeout = d
			}
		}
		luaMgr, err := luaengine.NewFilterManager(cfg.Lua.ScriptPath, luaTimeout, cfg.Lua.StrictMode)
		if err != nil {
			logging.Error("Failed to create Lua filter manager", "error", err)
		} else {
			app.LuaManager = luaMgr
			agent.SetLuaManager(luaMgr)
			session.SetLuaManager(luaMgr)
			logging.Info("Lua filter manager initialized", "script", cfg.Lua.ScriptPath)
		}
	}

	// Initialize Snapshot service if enabled
	cfg = config.Get()
	if cfg != nil && cfg.Snapshots.Enabled {
		snapshotSvc, err := snapshot.NewService()
		if err != nil {
			logging.Error("Failed to create snapshot service", "error", err)
		} else {
			app.Snapshots = snapshotSvc
			session.SetSnapshotCreator(snapshot.NewAdapter(snapshotSvc))
			logging.Info("Snapshot service initialized")
		}
	}

	// Initialize Evaluator service (self-improvement loop, disabled by default).
	cfg = config.Get()
	if cfg != nil && cfg.Evaluator.Enabled {
		evalSvc, err := evaluator.New(cfg.Evaluator, q, messages)
		if err != nil {
			logging.Warn("evaluator: failed to initialize, continuing without it", "err", err)
		} else if evalSvc != nil {
			app.Evaluator = evalSvc
			// Wire evaluator into session (triggers EvaluateSession on EndSession).
			session.SetEvaluator(evalSvc)
			// Wire evaluator into prompt builder via adapter (UCB template selection + skill injection).
			prompt.SetGlobalEvaluator(&evaluatorPromptAdapter{svc: evalSvc})
			logging.Info("evaluator: self-improvement system initialized", "model", cfg.Evaluator.Model)
		}
	}

	// Seed default prompt templates so UCB selection has a baseline to compare against.
	if app.Evaluator != nil {
		if err := seedEvaluatorTemplates(context.Background(), q); err != nil {
			logging.Warn("evaluator: template seeding failed", "err", err)
		}
	}

	// Initialize browser registry if enabled
	cfg = config.Get()
	if cfg != nil && cfg.InternalTools.BrowserEnabled {
		tools.InitBrowserRegistry(&cfg.InternalTools)
		logging.Info("Browser registry initialized")
	}

	// Initialize MCP Gateway if enabled
	cfg = config.Get()
	if cfg != nil && cfg.MCPGateway.Enabled {
		favCfg := mcpgateway.FavoriteConfig{
			Threshold:    cfg.MCPGateway.FavoriteThreshold,
			MaxFavorites: cfg.MCPGateway.MaxFavorites,
			WindowDays:   cfg.MCPGateway.FavoriteWindowDays,
			DecayDays:    cfg.MCPGateway.DecayDays,
		}
		gw := mcpgateway.NewGateway(conn, favCfg)
		go func() {
			if err := gw.Initialize(ctx, cfg.MCPServers); err != nil {
				logging.Error("MCP Gateway initialization failed", "error", err)
			}
		}()
		app.MCPGateway = gw
		logging.Info("MCP Gateway initialized")
	}

	// Initialize Mesnada orchestrator if enabled
	cfg = config.Get()
	if cfg != nil && cfg.Mesnada.Enabled {
		mesnadaCfg := convertMesnadaConfig(cfg)
		orch, err := mesnadaOrch.New(mesnadaCfg)
		if err != nil {
			logging.Error("Failed to create mesnada orchestrator", "error", err)
		} else {
			app.MesnadaOrchestrator = orch
			app.CronService = cronjob.NewService(orch, cfg.WorkingDir, nil)
			if err := app.CronService.Start(ctx, cfg.CronJobs); err != nil {
				logging.Error("Failed to start cronjob service", "error", err)
			}

			// Initialize ACP handler if ACP server is enabled
			var acpHandler *mesnadaServer.ACPHandler
			if mesnadaCfg.AppConfig != nil && mesnadaCfg.AppConfig.ACP.Server.Enabled {
				// Build ACP agent adapters so PandoACPAgent can use the live app services
				// without causing import cycles (the ACP package defines narrow interfaces).
				agentAdapter := &appACPAgentAdapter{svc: app.CoderAgent}
				sessionAdapter := &appACPSessionAdapter{svc: app.Sessions, msgSvc: app.Messages}
				permAdapter := &appACPPermissionAdapter{svc: app.Permissions}

				cwd, _ := os.Getwd()
				acpAgent := mesnadaACP.NewPandoACPAgent(
					version.Version,
					cwd,
					nil,
					agentAdapter,
					sessionAdapter,
					permAdapter,
				)

				// Parse session timeout
				sessionTimeout := 30 * time.Minute
				if mesnadaCfg.AppConfig.ACP.Server.SessionTimeout != "" {
					if timeout, err := time.ParseDuration(mesnadaCfg.AppConfig.ACP.Server.SessionTimeout); err == nil {
						sessionTimeout = timeout
					}
				}

				// Create HTTP transport with configuration
				transportCfg := mesnadaACP.HTTPTransportConfig{
					MaxSessions:  mesnadaCfg.AppConfig.ACP.Server.MaxSessions,
					IdleTimeout:  sessionTimeout,
					EventBufSize: 100,
				}
				transport := mesnadaACP.NewHTTPTransport(acpAgent, nil, transportCfg)

				// Create ACP handler
				acpHandler = mesnadaServer.NewACPHandler(mesnadaServer.ACPHandlerConfig{
					Agent:     acpAgent,
					Logger:    nil,
					Transport: transport,
				})

				logging.Info("ACP server enabled", "host", mesnadaCfg.AppConfig.ACP.Server.Host, "port", mesnadaCfg.AppConfig.ACP.Server.Port)
			}

			// Find a free port for the mesnada server; fall back if preferred is busy.
			mesnadaPort := findFreePort(cfg.Mesnada.Server.Host, cfg.Mesnada.Server.Port)
			if mesnadaPort != cfg.Mesnada.Server.Port {
				logging.Warn("Mesnada preferred port unavailable, using alternative",
					"preferred", cfg.Mesnada.Server.Port, "actual", mesnadaPort)
				cfg.Mesnada.Server.Port = mesnadaPort
				if mesnadaCfg.AppConfig != nil {
					mesnadaCfg.AppConfig.Server.Port = mesnadaPort
				}
			}
			addr := fmt.Sprintf("%s:%d", cfg.Mesnada.Server.Host, mesnadaPort)
			srv := mesnadaServer.New(mesnadaServer.Config{
				Addr:         addr,
				Orchestrator: orch,
				Version:      version.Version,
				UseStdio:     false,
				AppConfig:    mesnadaCfg.AppConfig,
				ACPHandler:   acpHandler,
				Remembrances: app.Remembrances,
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
			app.Remembrances,
			app.MCPGateway,
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
	// Wire Lua manager into agent for lifecycle hooks
	if app.LuaManager != nil {
		app.CoderAgent.SetLuaManager(app.LuaManager)
	}
	logging.Debug("Coder agent created", "model", app.CoderAgent.Model().ID)

	// Initialize the global persona manager with built-in personas, then overlay
	// any user-defined personas from the configured path. This is always done so
	// that built-in personas are available even without auto-selection configured.
	{
		userPersonaPath := cfg.PersonaAutoSelect.PersonaPath
		if userPersonaPath == "" {
			userPersonaPath = expandMesnadaPath(cfg.Mesnada.Orchestrator.PersonaPath)
		}
		personaMgr, pmErr := persona.NewManagerWithBuiltins(builtin.FS, userPersonaPath)
		if pmErr != nil {
			logging.Warn("Failed to initialize persona manager", "reason", pmErr)
		} else {
			agent.SetPersonaManager(personaMgr)
			logging.Debug("Persona manager initialized", "userPath", userPersonaPath, "count", len(personaMgr.ListPersonas()))
		}
	}

	// Initialize automatic persona selector for the main session when enabled.
	if cfg.PersonaAutoSelect.Enabled {
		personaPath := cfg.PersonaAutoSelect.PersonaPath
		if personaPath == "" {
			personaPath = expandMesnadaPath(cfg.Mesnada.Orchestrator.PersonaPath)
		}
		if personaPath != "" {
			ps, psErr := agent.NewPersonaSelector(personaPath)
			if psErr != nil {
				logging.Warn("Auto persona selector disabled", "reason", psErr)
			} else {
				agent.SetPersonaSelector(ps)
				logging.Debug("Auto persona selector enabled", "personaPath", personaPath)
			}
		} else {
			logging.Warn("Auto persona selector enabled but no personaPath configured")
		}
	}

	// Default active persona to "assistant" if none is configured.
	if agent.GetActivePersona() == "" {
		_ = agent.SetActivePersona("assistant")
	}

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

	// ACP Server configuration
	mesnadaCfg.ACP.Server.Enabled = cfg.Mesnada.ACP.Server.Enabled
	if cfg.Mesnada.ACP.Server.Host != "" {
		mesnadaCfg.ACP.Server.Host = cfg.Mesnada.ACP.Server.Host
	} else if mesnadaCfg.ACP.Server.Enabled {
		mesnadaCfg.ACP.Server.Host = "0.0.0.0"
	}
	if cfg.Mesnada.ACP.Server.Port > 0 {
		mesnadaCfg.ACP.Server.Port = cfg.Mesnada.ACP.Server.Port
	} else if mesnadaCfg.ACP.Server.Enabled {
		mesnadaCfg.ACP.Server.Port = 8766
	}
	if cfg.Mesnada.ACP.Server.MaxSessions > 0 {
		mesnadaCfg.ACP.Server.MaxSessions = cfg.Mesnada.ACP.Server.MaxSessions
	} else if mesnadaCfg.ACP.Server.Enabled {
		mesnadaCfg.ACP.Server.MaxSessions = 100
	}
	if cfg.Mesnada.ACP.Server.SessionTimeout != "" {
		mesnadaCfg.ACP.Server.SessionTimeout = cfg.Mesnada.ACP.Server.SessionTimeout
	} else if mesnadaCfg.ACP.Server.Enabled {
		mesnadaCfg.ACP.Server.SessionTimeout = "30m"
	}
	if len(cfg.Mesnada.ACP.Server.Transports) > 0 {
		mesnadaCfg.ACP.Server.Transports = cfg.Mesnada.ACP.Server.Transports
	} else if mesnadaCfg.ACP.Server.Enabled {
		mesnadaCfg.ACP.Server.Transports = []string{"http"}
	}
	mesnadaCfg.ACP.Server.RequireAuth = cfg.Mesnada.ACP.Server.RequireAuth

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

// seedEvaluatorTemplates inserts the embedded prompt templates into the DB
// as default (is_default=1) entries so UCB selection has a baseline.
// Uses INSERT OR IGNORE so re-runs are safe.
func seedEvaluatorTemplates(ctx context.Context, q db.Querier) error {
	// Key template sections to seed.
	sections := []struct {
		name    string
		section string
	}{
		{"base/identity", "base"},
		{"base/environment", "base"},
		{"base/workflow", "base"},
		{"base/conventions", "base"},
		{"agents/coder", "agents"},
		{"agents/task", "agents"},
		{"capabilities/remembrances", "capabilities"},
		{"capabilities/web_search", "capabilities"},
		{"capabilities/code_indexing", "capabilities"},
	}

	registry := prompt.NewTemplateRegistry()
	seeded := 0
	for _, s := range sections {
		content, err := registry.Render(s.name, nil)
		if err != nil {
			// Template might not exist — skip silently.
			continue
		}
		if content == "" {
			continue
		}
		_, err = q.InsertPromptTemplate(ctx, db.InsertPromptTemplateParams{
			ID:        uuid.New().String(),
			Name:      s.name,
			Section:   s.section,
			Content:   content,
			Version:   1,
			IsActive:  1,
			IsDefault: 1,
		})
		if err != nil {
			// UNIQUE constraint violation means it's already seeded — skip.
			continue
		}
		seeded++
	}
	if seeded > 0 {
		logging.Info("evaluator: seeded default prompt templates", "count", seeded)
	}
	return nil
}

// evaluatorPromptAdapter adapts *evaluator.EvaluatorService to prompt.PromptEvaluator.
// It translates between the evaluator types and the prompt package types to avoid
// import cycles between internal/llm/prompt and internal/evaluator.
type evaluatorPromptAdapter struct {
	svc *evaluator.EvaluatorService
}

func (a *evaluatorPromptAdapter) SelectTemplate(ctx context.Context, sectionName string) (*prompt.PromptEvaluatorTemplate, error) {
	tmpl, err := a.svc.SelectTemplate(ctx, sectionName)
	if err != nil || tmpl == nil {
		return nil, err
	}
	return &prompt.PromptEvaluatorTemplate{
		ID:      tmpl.ID,
		Content: tmpl.Content,
		Version: tmpl.Version,
	}, nil
}

func (a *evaluatorPromptAdapter) GetActiveSkills(ctx context.Context, taskType string) ([]prompt.PromptEvaluatorSkill, error) {
	skills, err := a.svc.GetActiveSkills(ctx, taskType)
	if err != nil || len(skills) == 0 {
		return nil, err
	}
	result := make([]prompt.PromptEvaluatorSkill, len(skills))
	for i, sk := range skills {
		result[i] = prompt.PromptEvaluatorSkill{Content: sk.Content}
	}
	return result, nil
}

func (a *evaluatorPromptAdapter) RecordTemplateSelection(ctx context.Context, sessionID, templateID string) {
	a.svc.RecordTemplateSelection(ctx, sessionID, templateID)
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

type assistantTextStreamer struct {
	output               io.Writer
	sessionID            string
	currentSection       string
	lastEndedWithNewline bool
	wrote                bool
	wroteContent         bool
}

func newAssistantTextStreamer(output io.Writer, sessionID string) *assistantTextStreamer {
	return &assistantTextStreamer{
		output:    output,
		sessionID: sessionID,
	}
}

func (s *assistantTextStreamer) Consume(event pubsub.Event[agent.AgentEvent]) error {
	if event.Type != pubsub.CreatedEvent {
		return nil
	}

	return s.ConsumeAgentEvent(event.Payload)
}

func (s *assistantTextStreamer) ConsumeAgentEvent(event agent.AgentEvent) error {
	if event.SessionID != s.sessionID {
		return nil
	}

	switch event.Type {
	case agent.AgentEventTypeContentDelta:
		return s.consumeContentDelta(event.Delta)
	case agent.AgentEventTypeToolCall:
		if event.ToolCall == nil {
			return nil
		}
		return s.consumeToolCall(*event.ToolCall)
	case agent.AgentEventTypeToolResult:
		if event.ToolResult == nil {
			return nil
		}
		return s.consumeToolResult(*event.ToolResult)
	default:
		return nil
	}
}

func (s *assistantTextStreamer) consumeContentDelta(delta string) error {
	if delta == "" {
		return nil
	}

	if err := s.startSection("assistant"); err != nil {
		return err
	}

	if err := s.writeString(delta); err != nil {
		return err
	}

	s.wrote = true
	s.wroteContent = true
	return nil
}

func (s *assistantTextStreamer) consumeToolCall(toolCall message.ToolCall) error {
	if err := s.startSection("tool"); err != nil {
		return err
	}

	line := fmt.Sprintf("🔧 %s", toolCall.Name)
	if compactInput := compactSingleLine(toolCall.Input, 180); compactInput != "" {
		line += " " + compactInput
	}

	if err := s.writeString(line + "\n"); err != nil {
		return err
	}

	s.wrote = true
	return nil
}

func (s *assistantTextStreamer) consumeToolResult(result message.ToolResult) error {
	if err := s.startSection("tool"); err != nil {
		return err
	}

	toolName := result.Name
	if toolName == "" {
		toolName = result.ToolCallID
	}

	status := "✓"
	line := fmt.Sprintf("%s %s completed", status, toolName)
	if result.IsError {
		status = "✗"
		line = fmt.Sprintf("%s %s failed", status, toolName)
		if preview := compactSingleLine(result.Content, 200); preview != "" {
			line += ": " + preview
		}
	}

	if err := s.writeString(line + "\n"); err != nil {
		return err
	}

	s.wrote = true
	return nil
}

func (s *assistantTextStreamer) startSection(section string) error {
	if s.currentSection == section {
		return nil
	}

	if err := s.writeSeparator(); err != nil {
		return err
	}

	s.currentSection = section
	return nil
}

func (s *assistantTextStreamer) writeSeparator() error {
	if !s.wrote {
		return nil
	}

	separator := "\n\n"
	if s.lastEndedWithNewline {
		separator = "\n"
	}
	return s.writeString(separator)
}

func (s *assistantTextStreamer) PrintFinalContent(content string) error {
	content = strings.TrimSpace(content)
	if content == "" || s.wroteContent {
		return nil
	}

	if err := s.startSection("assistant"); err != nil {
		return err
	}
	if err := s.writeString(content); err != nil {
		return err
	}

	s.wrote = true
	s.wroteContent = true
	return nil
}

func compactSingleLine(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	var compacted bytes.Buffer
	if err := json.Compact(&compacted, []byte(value)); err == nil {
		value = compacted.String()
	}

	value = strings.Join(strings.Fields(value), " ")
	if limit > 0 && len(value) > limit {
		value = value[:limit-3] + "..."
	}

	return value
}

func (s *assistantTextStreamer) CloseLine() error {
	if !s.wrote {
		return nil
	}

	if s.lastEndedWithNewline {
		return nil
	}

	return s.writeString("\n")
}

func (s *assistantTextStreamer) writeString(value string) error {
	if value == "" {
		return nil
	}

	if _, err := io.WriteString(s.output, value); err != nil {
		return err
	}

	s.lastEndedWithNewline = strings.HasSuffix(value, "\n")
	return nil
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

	var (
		streamer     *assistantTextStreamer
		streamCancel context.CancelFunc
		streamWG     sync.WaitGroup
	)
	if outputFormat == format.Text.String() {
		streamer = newAssistantTextStreamer(os.Stdout, sess.ID)
		streamCtx, cancel := context.WithCancel(ctx)
		streamCancel = cancel
		agentEvents := a.CoderAgent.Subscribe(streamCtx)

		streamWG.Add(1)
		go func() {
			defer streamWG.Done()
			for event := range agentEvents {
				if err := streamer.Consume(event); err != nil {
					logging.Warn("Failed to stream non-interactive response", "session_id", sess.ID, "error", err)
					return
				}
			}
		}()
	}

	done, err := a.CoderAgent.Run(ctx, sess.ID, prompt)
	if err != nil {
		if streamCancel != nil {
			streamCancel()
			streamWG.Wait()
		}
		return fmt.Errorf("failed to start agent processing stream: %w", err)
	}

	var result agent.AgentEvent
	for event := range done {
		result = event
	}
	if streamCancel != nil {
		streamCancel()
		streamWG.Wait()
	}
	logging.Debug("Non-interactive agent completed", "sessionID", sess.ID, "hasError", result.Error != nil)
	if result.Error != nil {
		if streamer != nil {
			_ = streamer.CloseLine()
		}
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

	if streamer != nil {
		if err := streamer.PrintFinalContent(content); err != nil {
			return fmt.Errorf("failed to render final response: %w", err)
		}
		if streamer.wrote {
			if err := streamer.CloseLine(); err != nil {
				return fmt.Errorf("failed to finalize streamed response: %w", err)
			}
		} else {
			fmt.Println(content)
		}
	} else {
		fmt.Println(format.FormatOutput(content, outputFormat))
	}

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
	if app.CronService != nil {
		app.CronService.Stop()
	}
	if app.MesnadaOrchestrator != nil {
		if err := app.MesnadaOrchestrator.Shutdown(); err != nil {
			logging.Error("Failed to shutdown Mesnada orchestrator", "error", err)
		}
	}

	// Cleanup old snapshots
	if app.Snapshots != nil {
		cfg := config.Get()
		if cfg != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := app.Snapshots.Cleanup(cleanupCtx, cfg.Snapshots.AutoCleanupDays, cfg.Snapshots.MaxSnapshots); err != nil {
				logging.Error("Failed to cleanup snapshots", "error", err)
			}
			cancel()
		}
	}

	// Close all browser sessions
	tools.CloseAllBrowserSessions()

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
	// Shutdown OpenLit OTLP exporter (flush traces before exit)
	if app.openlitShutdown != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.openlitShutdown(ctx)
	}
	// Shutdown project manager and all child project ACP processes.
	if app.ProjectManager != nil {
		app.ProjectManager.Shutdown()
	}
	logging.Debug("App shutdown completed")
}

// ---------------------------------------------------------------------------
// ACP adapter types — bridge app services to the narrow interfaces that
// PandoACPAgent requires, avoiding import cycles between mesnada/acp and
// internal/llm/agent, internal/session, and internal/permission.
// ---------------------------------------------------------------------------

type appACPAgentAdapter struct{ svc agent.Service }

func (a *appACPAgentAdapter) Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan mesnadaACP.AgentEvent, error) {
	realCh, err := a.svc.Run(ctx, sessionID, content, attachments...)
	if err != nil {
		return nil, err
	}
	// Buffered to decouple the event-drain goroutine from the ACP handler.
	// Without a buffer, each SendUpdate (RPC write) stalls the goroutine and
	// prevents it from draining agent.eventCh, causing overflow and lost events.
	acpCh := make(chan mesnadaACP.AgentEvent, 512)
	go func() {
		defer close(acpCh)
		for ev := range realCh {
			var acpEv mesnadaACP.AgentEvent
			switch ev.Type {
			case agent.AgentEventTypeError:
				acpEv.Type = mesnadaACP.AgentEventTypeError
				acpEv.Error = ev.Error
			case agent.AgentEventTypeResponse:
				acpEv.Type = mesnadaACP.AgentEventTypeResponse
				acpEv.Message = ev.Message
			case agent.AgentEventTypeSummarize:
				acpEv.Type = mesnadaACP.AgentEventTypeSummarize
			case agent.AgentEventTypeContentDelta:
				acpEv.Type = mesnadaACP.AgentEventTypeContentDelta
				acpEv.Delta = ev.Delta
			case agent.AgentEventTypeThinkingDelta:
				acpEv.Type = mesnadaACP.AgentEventTypeThinkingDelta
				acpEv.Delta = ev.Delta
			case agent.AgentEventTypeToolCall:
				acpEv.Type = mesnadaACP.AgentEventTypeToolCall
				acpEv.ToolCall = ev.ToolCall
			case agent.AgentEventTypeToolResult:
				acpEv.Type = mesnadaACP.AgentEventTypeToolResult
				acpEv.ToolResult = ev.ToolResult
			default:
				continue
			}
			select {
			case acpCh <- acpEv:
			case <-ctx.Done():
				return
			}
		}
	}()
	return acpCh, nil
}

func (a *appACPAgentAdapter) Cancel(sessionID string) { a.svc.Cancel(sessionID) }

func (a *appACPAgentAdapter) CurrentModelID() string { return string(a.svc.Model().ID) }

func (a *appACPAgentAdapter) AvailableModels() []mesnadaACP.ACPModelInfo {
	all := models.GetAllModels()
	result := make([]mesnadaACP.ACPModelInfo, 0, len(all))
	for _, m := range all {
		provider := string(m.Provider)
		displayName := m.Name
		if provider != "" && provider != "__mock" {
			providerLabel := strings.ToUpper(provider[:1]) + provider[1:]
			displayName = providerLabel + ": " + m.Name
		}
		result = append(result, mesnadaACP.ACPModelInfo{ID: string(m.ID), Name: displayName})
	}
	return result
}

func (a *appACPAgentAdapter) SetModelOverride(modelID string) error {
	if modelID == "" {
		return nil
	}
	return config.OverrideAgentModel(config.AgentCoder, models.ModelID(modelID))
}

func (a *appACPAgentAdapter) ListPersonas() []string             { return agent.ListAvailablePersonas() }
func (a *appACPAgentAdapter) GetActivePersona() string           { return agent.GetActivePersona() }
func (a *appACPAgentAdapter) SetActivePersona(name string) error { return agent.SetActivePersona(name) }

func (a *appACPAgentAdapter) ListAvailableTools() []mesnadaACP.ACPToolInfo {
	baseTools := a.svc.GetTools()
	result := make([]mesnadaACP.ACPToolInfo, 0, len(baseTools))
	for _, t := range baseTools {
		info := t.Info()
		result = append(result, mesnadaACP.ACPToolInfo{
			Name:        info.Name,
			Description: info.Description,
		})
	}
	return result
}

func (a *appACPAgentAdapter) OpenCopilotUsage() error {
	status := auth.GetCopilotAuthStatus()
	if !status.Authenticated {
		return fmt.Errorf("/copilot-usage is only available when the copilot provider is authenticated")
	}
	return auth.OpenBrowser("https://github.com/settings/copilot/features")
}

func (a *appACPAgentAdapter) OpenClaudeUsage() error {
	status, err := auth.GetClaudeAuthStatus()
	if err != nil || status == nil || !status.Authenticated || status.Source == "env" {
		return fmt.Errorf("/claude-usage is only available when Claude OAuth is authenticated")
	}
	return auth.OpenBrowser("https://claude.ai/settings/usage")
}

// ---------------------------------------------------------------------------

type appACPSessionAdapter struct {
	svc    session.Service
	msgSvc message.Service
}

func (a *appACPSessionAdapter) CreateSession(ctx context.Context, title string) (string, error) {
	sess, err := a.svc.Create(ctx, title)
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func (a *appACPSessionAdapter) GetSession(ctx context.Context, id string) (mesnadaACP.ACPSessionInfo, error) {
	sess, err := a.svc.Get(ctx, id)
	if err != nil {
		return mesnadaACP.ACPSessionInfo{}, err
	}
	return mesnadaACP.ACPSessionInfo{ID: sess.ID, Title: sess.Title, UpdatedAt: sess.UpdatedAt}, nil
}

func (a *appACPSessionAdapter) ListSessions(ctx context.Context) ([]mesnadaACP.ACPSessionInfo, error) {
	sessions, err := a.svc.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]mesnadaACP.ACPSessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = mesnadaACP.ACPSessionInfo{ID: s.ID, Title: s.Title, UpdatedAt: s.UpdatedAt}
	}
	return result, nil
}

func (a *appACPSessionAdapter) GetMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	return a.msgSvc.List(ctx, sessionID)
}

// ---------------------------------------------------------------------------

type appACPPermissionAdapter struct{ svc permission.Service }

func (a *appACPPermissionAdapter) AutoApproveSession(sessionID string) {
	a.svc.AutoApproveSession(sessionID)
}

func (a *appACPPermissionAdapter) RemoveAutoApproveSession(sessionID string) {
	a.svc.RemoveAutoApproveSession(sessionID)
}

func (a *appACPPermissionAdapter) RegisterSessionHandler(sessionID string, handler func(req mesnadaACP.PermissionRequestData) bool) {
	a.svc.RegisterSessionHandler(sessionID, func(req permission.CreatePermissionRequest) bool {
		return handler(mesnadaACP.PermissionRequestData{
			SessionID:   req.SessionID,
			ToolName:    req.ToolName,
			Description: req.Description,
			Action:      req.Action,
			Path:        req.Path,
			Params:      req.Params,
		})
	})
}

func (a *appACPPermissionAdapter) UnregisterSessionHandler(sessionID string) {
	a.svc.UnregisterSessionHandler(sessionID)
}
