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

	"github.com/digiogithub/pando/internal/agentvcs"
	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/caveman"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/cronjob"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/format"
	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/ipc/failover"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/prompt"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/lsp"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/mcpgateway"
	mesnadaACP "github.com/digiogithub/pando/internal/mesnada/acp"
	mesnadaAgent "github.com/digiogithub/pando/internal/mesnada/agent"
	"github.com/digiogithub/pando/internal/mesnada/conclusion"
	mesnadaConfig "github.com/digiogithub/pando/internal/mesnada/config"
	mesnadaOrch "github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/internal/mesnada/persona"
	"github.com/digiogithub/pando/internal/mesnada/persona/builtin"
	mesnadaServer "github.com/digiogithub/pando/internal/mesnada/server"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/observability"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/ponytail"
	"github.com/digiogithub/pando/internal/project"
	"github.com/digiogithub/pando/internal/pubsub"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/kb"
	ragproxy "github.com/digiogithub/pando/internal/rag/proxy"
	"github.com/digiogithub/pando/internal/savings"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/skills"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/userinput"
	"github.com/digiogithub/pando/internal/version"
)

type App struct {
	Sessions    session.Service
	Messages    message.Service
	History     history.Service
	Permissions permission.Service
	UserInput   userinput.Service
	DBQuerier   db.Querier

	// rwConn is the read-write SQLite connection on the primary instance (the one
	// db.Connect() opened, or the one acquired on failover promotion). It is used
	// for whole-database maintenance that bypasses the query layer, notably
	// CompactDatabase (VACUUM). On a secondary it holds the local RO/RW-secondary
	// connection and is not used directly — compaction is routed to the primary.
	rwConn *sql.DB

	CoderAgent agent.Service

	Projects            project.Service
	ProjectManager      *project.Manager
	AgentVCS            agentvcs.Service
	LSPClients          map[string]*lsp.Client
	SkillManager        *skills.SkillManager
	MesnadaOrchestrator *mesnadaOrch.Orchestrator
	CronService         *cronjob.Service
	MesnadaServer       *mesnadaServer.Server
	Remembrances        *rag.RemembrancesService
	ContextEnricher     *rag.ContextEnricher
	LuaManager          *luaengine.FilterManager
	MCPGateway          *mcpgateway.Gateway
	Evaluator           *evaluator.EvaluatorService

	// delegationSupervisor implements Case A of the delegated-conclusion protocol
	// (inject a completed subagent's conclusion into a still-running parent loop).
	// It is nil when delegation is disabled (default-off).
	delegationSupervisor *delegationSupervisor

	// IPCBus is set on the primary instance after calling SetupIPC.
	// Secondary instances leave this nil.
	IPCBus *ipc.Bus
	// IPCIsPrimary is true when this instance holds the IPC lock.
	IPCIsPrimary bool

	// ipcWatcher is the failover watcher. Non-nil when IPC is active.
	ipcWatcher *failover.Watcher

	// ---- Secondary-only IPC context (set via SetIPCSecondaryContext) ----

	// ipcClient is the ZMQ client used by secondary instances.
	ipcClient *ipc.Client
	// ipcROConn is the read-only SQLite connection held by the secondary.
	// Closed and replaced with a RW connection on promotion.
	ipcROConn *sql.DB
	// ipcWorkdir is the working directory used to derive IPC ports and lock path.
	ipcWorkdir string
	// ipcInstanceID is the instance ID used in lock acquisition and IPC messaging.
	ipcInstanceID string
	// ipcPubPort and ipcRPCPort are the deterministic ports derived from ipcWorkdir.
	ipcPubPort int
	ipcRPCPort int
	// ipcBusSetupFunc is provided by cmd/root.go and performs the full primary wiring
	// (writecoordinator, changepub, bridge registration) once the RW DB and Bus are
	// available after promotion.
	ipcBusSetupFunc func(ctx context.Context, bus *ipc.Bus, rwConn *sql.DB) error

	openlitShutdown func(context.Context) error

	clientsMutex sync.RWMutex
	// lspSpawning tracks LSP servers whose lazy startup is in flight, and
	// lspBroken tracks servers that failed to start or whose binary was not
	// found on PATH. Both are guarded by clientsMutex and prevent re-attempting
	// the same server on every file edit. See EnsureLSPForFile.
	lspSpawning map[string]struct{}
	lspBroken   map[string]struct{}

	watcherCancelFuncs []context.CancelFunc
	cancelFuncsMutex   sync.Mutex
	watcherWG          sync.WaitGroup
}

// AppOptions configures optional behaviour for New().
type AppOptions struct {
	// SkipLSP disables LSP client initialisation. Set this to true in headless
	// modes (e.g. ACP stdio) where the editor manages its own language servers.
	SkipLSP bool
	// SkipMesnadaServer avoids starting the embedded Mesnada HTTP server while
	// still allowing the orchestrator and related tools to be initialized.
	SkipMesnadaServer bool
	// StartupMode identifies the mode in which Pando is starting so background
	// remembrances behaviors can be aligned consistently across entrypoints.
	StartupMode string
	// DBQuerier overrides the db.Querier used for sessions, messages, and projects.
	// When non-nil this querier is used instead of db.New(conn).
	// Primary instances leave this nil; secondary instances pass a dbproxy.DBProxy.
	DBQuerier db.Querier
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

	// Use the provided querier override (secondary instances pass a dbproxy.DBProxy),
	// or fall back to the standard direct querier for primary instances.
	var q db.Querier
	rawQ := db.New(conn) // always needed for history (uses WithTx)
	if opt.DBQuerier != nil {
		q = opt.DBQuerier
	} else {
		q = rawQ
	}
	sessions := session.NewService(q)
	messages := message.NewService(q)
	files := history.NewService(rawQ, conn)
	// project.NewService uses *db.Queries directly (UpdateProjectName is not in db.Querier).
	// Secondary instances will have project writes go through rawQ (read-only conn) which
	// will fail gracefully; project management on secondaries is a Phase 5+ concern.
	projects := project.NewService(rawQ)

	app := &App{
		Sessions:    sessions,
		Messages:    messages,
		History:     files,
		Permissions: permission.NewPermissionService(),
		UserInput:   userinput.NewService(),
		DBQuerier:   q,
		rwConn:      conn,
		Projects:    projects,
		LSPClients:  make(map[string]*lsp.Client),
		lspSpawning: make(map[string]struct{}),
		lspBroken:   make(map[string]struct{}),
	}

	// Initialize project manager (Phase 2).
	mgr, mgrErr := project.NewManager(ctx, projects)
	if mgrErr != nil {
		return nil, fmt.Errorf("failed to initialize project manager: %w", mgrErr)
	}
	app.ProjectManager = mgr

	// Auto-register the current working directory as a known global project so
	// other Pando instances can discover it.  Only register when there is a
	// config file present (i.e. the directory is a real Pando project).
	go func() {
		cwd := config.WorkingDirectory()
		if cwd != "" && config.HasConfigFileAt(cwd) {
			name := filepath.Base(cwd)
			if err := config.RegisterSelfAsGlobalProject(cwd, name); err != nil {
				logging.Warn("failed to register self as global project", "error", err)
			}
		}
		// Seed local DB with all projects from the global registry so this
		// instance can see projects registered by other Pando instances.
		mgr.SeedFromGlobal(ctx)
	}()

	// Initialize theme based on configuration
	app.initTheme()

	// Select the TUI icon set (Nerd Font glyphs vs. plain fallback) based on
	// configuration and the PANDO_NERD_FONTS environment override.
	app.initIcons()

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
	agent.ResetMcpToolsCache()

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

		var remembrancesProxy *dbproxy.DBProxy
		if p, ok := q.(*dbproxy.DBProxy); ok {
			remembrancesProxy = p
		}
		remembrances, err := rag.NewRemembrancesServiceWithProxy(conn, &cfg.Remembrances, remembrancesProxy)
		if err != nil {
			logging.Error("Failed to create remembrances service", "error", err)
		} else {
			app.Remembrances = remembrances
			if remembrances != nil {
				logging.Info("Remembrances service initialized", "startup_mode", opt.StartupMode)
				app.initRemembrancesProjectIndexing(ctx, remembrances, &cfg.Remembrances, opt.StartupMode)
				app.initRemembrancesKBSync(ctx, remembrances, &cfg.Remembrances)
				app.initKBLinkBackfill(ctx, remembrances)
				app.initRemembrancesSessionIndexing(ctx, remembrances, &cfg.Remembrances)

				// Initialize context enricher if enabled: searches KB and code index
				// before every user prompt and prepends relevant context.
				if cfg.Remembrances.ContextEnrichmentEnabled {
					enricher := rag.NewContextEnricher(
						remembrances,
						rag.EnricherConfig{
							KBResults:      cfg.Remembrances.ContextEnrichmentKBResults,
							CodeResults:    cfg.Remembrances.ContextEnrichmentCodeResults,
							CodeProject:    cfg.Remembrances.ContextEnrichmentCodeProject,
							EventsResults:  cfg.Remembrances.ContextEnrichmentEventsResults,
							EventsSubject:  cfg.Remembrances.ContextEnrichmentEventsSubject,
							EventsLastDays: cfg.Remembrances.ContextEnrichmentEventsLastDays,
							MinScore:       cfg.Remembrances.ContextEnrichmentMinScore,
							KBMaxChars:     cfg.Remembrances.ContextEnrichmentKBMaxChars,
							CodeMaxChars:   cfg.Remembrances.ContextEnrichmentCodeMaxChars,
							EventsMaxChars: cfg.Remembrances.ContextEnrichmentEventsMaxChars,
							TotalMaxChars:  cfg.Remembrances.ContextEnrichmentTotalMaxChars,
						},
					)

					// Phase 2: optionally replace the heuristic planner with an LLM-based one.
					plannerMode := "heuristic"
					if cfg.Remembrances.ContextEnrichmentUseAgentPlanner {
						primaryProv, primaryErr := agent.CreateAgentProvider(ctx, config.AgentContextEnricher)
						if primaryErr == nil {
							var secondary rag.PlannerProvider
							if cfg.Remembrances.ContextEnrichmentPlannerFallbackToCoder {
								coderProv, coderErr := agent.CreateAgentProvider(ctx, config.AgentCoder)
								if coderErr == nil {
									secondary = &plannerProviderAdapter{p: coderProv}
								} else {
									logging.Debug("context enricher: coder fallback provider unavailable", "error", coderErr)
								}
							}
							promptFn := func(providerName string) string {
								return prompt.ContextEnricherPrompt(models.ModelProvider(providerName))
							}
							llmPlanner := rag.NewLLMPlanner(
								&plannerProviderAdapter{p: primaryProv},
								secondary,
								promptFn,
							)
							enricher.SetPlanner(llmPlanner)
							plannerMode = "llm"
						} else {
							logging.Warn("context enricher: agent planner requested but provider unavailable; falling back to heuristic",
								"error", primaryErr)
						}
					}

					app.ContextEnricher = enricher
					agent.SetContextEnricher(enricher)
					logging.Info("remembrances: context enricher enabled",
						"planner", plannerMode,
						"kb_results", cfg.Remembrances.ContextEnrichmentKBResults,
						"code_results", cfg.Remembrances.ContextEnrichmentCodeResults,
						"code_project", cfg.Remembrances.ContextEnrichmentCodeProject,
						"events_results", cfg.Remembrances.ContextEnrichmentEventsResults,
						"events_subject", cfg.Remembrances.ContextEnrichmentEventsSubject,
						"events_last_days", cfg.Remembrances.ContextEnrichmentEventsLastDays,
						"min_score", cfg.Remembrances.ContextEnrichmentMinScore,
					)
				}

				// Memory system — inject stored memories into the system prompt when enabled.
				if cfg.Remembrances.MemoryEnabled && cfg.Remembrances.MemoryContextEnrichmentEnabled && remembrances.KB != nil {
					injector := &memoryInjectorAdapter{
						store: remembrances.KB,
						cfg:   cfg.Remembrances,
					}
					agent.SetMemoryInjector(injector)
					logging.Info("remembrances: memory context injection enabled",
						"max_items", cfg.Remembrances.MemoryContextMaxItems,
						"max_chars", cfg.Remembrances.MemoryContextMaxChars,
					)
				}

				// Memory GC service — periodically marks expired memories as outdated.
				if cfg.Remembrances.MemoryEnabled && remembrances.KB != nil {
					gcInterval := time.Hour
					if cfg.Remembrances.MemoryGCInterval != "" {
						if d, err := time.ParseDuration(cfg.Remembrances.MemoryGCInterval); err == nil && d > 0 {
							gcInterval = d
						}
					}
					gcSvc := kb.NewMemoryGCService(remembrances.KB, gcInterval)
					gcCtx, gcCancel := context.WithCancel(ctx)
					app.cancelFuncsMutex.Lock()
					app.watcherCancelFuncs = append(app.watcherCancelFuncs, gcCancel)
					app.cancelFuncsMutex.Unlock()
					app.watcherWG.Add(1)
					go func() {
						defer app.watcherWG.Done()
						gcSvc.Start(gcCtx)
					}()
					logging.Info("remembrances: memory GC service started", "interval", gcInterval)
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

	// Initialize agent-vcs (replaces legacy snapshot service).
	// When snapshots are enabled, agent-vcs provides a jj-inspired commit chain
	// per session instead of isolated snapshot captures.
	cfg = config.Get()
	if cfg != nil && cfg.Snapshots.Enabled {
		vcsSvc, err := agentvcs.NewService()
		if err != nil {
			logging.Error("Failed to create agent-vcs service", "error", err)
		} else {
			app.AgentVCS = vcsSvc
			session.SetSnapshotCreator(agentvcs.NewAdapter(vcsSvc))
			logging.Info("Agent-VCS service initialized (replaces snapshots)")
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
			// Wire ContextTrimmer into agent for pre-session tool filtering (Phase 3).
			if ct := evalSvc.NewContextTrimmer(); ct != nil {
				agent.SetContextTrimmer(&agentContextTrimmerAdapter{trimmer: ct})
				logging.Info("evaluator: context trimmer initialized")
			}
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
		agent.ResetMcpToolsCache()
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
		// Wire the delegation conclusion enricher's project resolver to the live
		// project registry so a delegated task's project id/name are resolved from
		// its canonical path. Degrades gracefully to filepath.Base when the path is
		// not a registered project. Only consulted when delegation is enabled.
		mesnadaCfg.ProjectResolver = makeProjectResolver(ctx, app.Projects)
		// Wire warm-target routing (Phase 7.3): a delegated task whose project is
		// known is run inside an already-running per-project ACP instance instead of
		// cold-spawning a CLI. nil when ReuseWarmInstances is off → cold path always.
		mesnadaCfg.WarmTargetResolver = makeWarmTargetResolver(app.ProjectManager, cfg)
		// Wire external-delegation re-attach recovery (item A2): after a parent
		// restart, recover the result of an interrupted external (editor-launched)
		// peer delegation instead of failing it. Gated on the same caller opt-in as
		// routing external warm targets; nil otherwise → today's mark-failed path.
		if app.ProjectManager != nil && cfg.Mesnada.Delegation.AllowExternalWarmTargets {
			mesnadaCfg.ExternalDelegationRecoverer = makeExternalDelegationRecoverer(app.ProjectManager)
		}
		// Wire project-reference resolution (item B1): lets the spawn tool target a
		// registered project by id/name/path so its delegated task is routed to that
		// project's warm instance. nil when no project service is available.
		mesnadaCfg.ProjectRefResolver = makeProjectRefResolver(app.Projects)
		// Idle auto-GC (item C1): stop router-auto-started warm instances that go
		// idle, so they don't leak. Gated on warm reuse being enabled and a
		// positive WarmInstanceIdleTimeout; "0"/empty leaves warm instances to
		// persist until stopped from the Projects panel.
		if app.ProjectManager != nil && cfg.Mesnada.Delegation.ReuseWarmInstances {
			if idle, derr := time.ParseDuration(cfg.Mesnada.Delegation.WarmInstanceIdleTimeout); derr == nil && idle > 0 {
				app.ProjectManager.StartIdleGC(idle)
				logging.Info("delegation warm-instance idle auto-GC enabled", "idle_timeout", idle.String())
			}
		}
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
				agentAdapter := &appACPAgentAdapter{
					svc:        app.CoderAgent,
					goalRunner: agent.NewGoalRunner(app.CoderAgent, app.DBQuerier),
				}
				sessionAdapter := &appACPSessionAdapter{svc: app.Sessions, msgSvc: app.Messages, q: app.DBQuerier}
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
				acpAgent.SetDBCompactor(app.ACPDBCompactor())

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

			if !opt.SkipMesnadaServer {
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
			} else {
				logging.Info("Mesnada orchestrator initialized without embedded HTTP server")
			}
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
			app.History,
			app,
			app.UserInput,
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

	// Delegation supervisor: re-enters the parent loop when a delegated subagent
	// completes — Case A (inject into a still-running loop) and/or Case B (resurrect
	// an idle loop). Default-off — only started when Enabled and at least one
	// re-entry mode is set, otherwise it never subscribes (no overhead).
	if app.MesnadaOrchestrator != nil && app.CoderAgent != nil {
		delCfg := cfg.Mesnada.Delegation
		if delCfg.Enabled && (delCfg.InjectIntoLiveLoop || delCfg.ResurrectIdleLoop) {
			resurrectionTimeout := 10 * time.Minute
			if delCfg.ResurrectionTimeout != "" {
				if d, perr := time.ParseDuration(delCfg.ResurrectionTimeout); perr == nil && d > 0 {
					resurrectionTimeout = d
				} else if perr != nil {
					logging.Warn("Delegation: invalid resurrectionTimeout, using default 10m",
						"value", delCfg.ResurrectionTimeout, "error", perr)
				}
			}
			app.delegationSupervisor = newDelegationSupervisor(
				app.MesnadaOrchestrator,
				app.CoderAgent,
				delegationSupervisorOptions{
					Enabled:             delCfg.Enabled,
					InjectIntoLiveLoop:  delCfg.InjectIntoLiveLoop,
					ResurrectIdleLoop:   delCfg.ResurrectIdleLoop,
					MaxResurrections:    delCfg.MaxResurrections,
					MaxDepth:            delCfg.MaxDepth,
					ResurrectionTimeout: resurrectionTimeout,
				},
			)
			app.delegationSupervisor.Start(ctx)
		}
	}

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

	orchCfg := mesnadaOrch.Config{
		StorePath:        appCfg.Orchestrator.StorePath,
		LogDir:           appCfg.Orchestrator.LogDir,
		MaxParallel:      appCfg.Orchestrator.MaxParallel,
		DefaultMCPConfig: appCfg.Orchestrator.DefaultMCPConfig,
		DefaultEngine:    appCfg.Orchestrator.DefaultEngine,
		PersonaPath:      appCfg.Orchestrator.PersonaPath,
		AppConfig:        appCfg,
		ModelResolver:    makePandoModelResolver(cfg),
	}

	// Populate MCP servers from the pando application config so the orchestrator
	// can forward them dynamically to subagents at spawn time.
	if cfg != nil {
		orchCfg.MCPServers = convertPandoMCPServers(cfg.MCPServers)
		orchCfg.GatewayExposeEnabled = cfg.MCPGateway.Enabled && cfg.MCPServer.GatewayExpose.Enabled
		// Delegation conclusion protocol options (default-off).
		orchCfg.Delegation = mesnadaOrch.DelegationConfig{
			Enabled:            cfg.Mesnada.Delegation.Enabled,
			SynthesizeFallback: cfg.Mesnada.Delegation.SynthesizeFallback,
			ReuseWarmInstances: cfg.Mesnada.Delegation.ReuseWarmInstances,
		}
	}

	return orchCfg
}

// convertPandoMCPServers converts the pando MCPServers map into a slice of
// PandoMCPServerEntry values understood by the mesnada agent package.
func convertPandoMCPServers(servers map[string]config.MCPServer) []mesnadaAgent.PandoMCPServerEntry {
	if len(servers) == 0 {
		return nil
	}
	entries := make([]mesnadaAgent.PandoMCPServerEntry, 0, len(servers))
	for name, srv := range servers {
		resolved, err := config.ResolveMCPServerSecrets(srv)
		if err != nil {
			logging.Warn("failed to resolve MCP server secrets for subagent forwarding", "server", name, "error", err)
			resolved = srv
		}
		entries = append(entries, mesnadaAgent.PandoMCPServerEntry{
			Name:    name,
			Command: resolved.Command,
			Args:    resolved.Args,
			Env:     resolved.Env,
			Type:    string(resolved.Type),
			URL:     resolved.URL,
			Headers: resolved.Headers,
		})
	}
	return entries
}

// makeProjectResolver returns a conclusion.ProjectResolver backed by the project
// registry. Given a canonical project path it looks up the registered project and
// returns its id + display name. On any miss/error it returns empty strings so the
// enricher degrades gracefully to filepath.Base. Returns nil when no project
// service is available.
func makeProjectResolver(ctx context.Context, svc project.Service) conclusion.ProjectResolver {
	if svc == nil {
		return nil
	}
	return func(canonicalPath string) (string, string) {
		if canonicalPath == "" {
			return "", ""
		}
		p, err := svc.GetByPath(ctx, canonicalPath)
		if err != nil || p == nil {
			return "", ""
		}
		return p.ID, p.Name
	}
}

// projectRefResolverAdapter implements orchestrator.ProjectRefResolver over the
// project registry. It resolves a free-form reference (item B1) to a registered
// project by, in order: exact registry id, canonical directory path, then
// case-insensitive exact display name. Registry calls use the caller's ctx.
type projectRefResolverAdapter struct {
	svc project.Service
}

func (a projectRefResolverAdapter) ResolveProjectRef(ctx context.Context, ref string) (mesnadaOrch.ProjectRef, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || a.svc == nil {
		return mesnadaOrch.ProjectRef{}, false
	}
	// 1) Exact registry id.
	if p, err := a.svc.Get(ctx, ref); err == nil && p != nil {
		return mesnadaOrch.ProjectRef{ID: p.ID, Name: p.Name, Path: p.Path}, true
	}
	// 2) Canonical directory path.
	if canonical := config.CanonicalProjectPath(ref); canonical != "" {
		if p, err := a.svc.GetByPath(ctx, canonical); err == nil && p != nil {
			return mesnadaOrch.ProjectRef{ID: p.ID, Name: p.Name, Path: p.Path}, true
		}
	}
	// 3) Case-insensitive exact display name (newest first on ambiguity).
	if list, err := a.svc.List(ctx); err == nil {
		for i := range list {
			if strings.EqualFold(strings.TrimSpace(list[i].Name), ref) {
				return mesnadaOrch.ProjectRef{ID: list[i].ID, Name: list[i].Name, Path: list[i].Path}, true
			}
		}
	}
	return mesnadaOrch.ProjectRef{}, false
}

func (a projectRefResolverAdapter) ListProjectRefs(ctx context.Context) []mesnadaOrch.ProjectRef {
	if a.svc == nil {
		return nil
	}
	list, err := a.svc.List(ctx)
	if err != nil {
		return nil
	}
	refs := make([]mesnadaOrch.ProjectRef, 0, len(list))
	for i := range list {
		refs = append(refs, mesnadaOrch.ProjectRef{ID: list[i].ID, Name: list[i].Name, Path: list[i].Path})
	}
	return refs
}

// makeProjectRefResolver returns an orchestrator.ProjectRefResolver backed by the
// project registry (item B1), or nil when no project service is available so the
// spawn tool's "project" argument is reported as unsupported.
func makeProjectRefResolver(svc project.Service) mesnadaOrch.ProjectRefResolver {
	if svc == nil {
		return nil
	}
	return projectRefResolverAdapter{svc: svc}
}

// warmTargetResolverFunc adapts a plain function to the orchestrator's
// WarmTargetResolver interface.
type warmTargetResolverFunc func(ctx context.Context, projectID, projectPath, promptText, correlationID string) (*mesnadaOrch.WarmRunResult, error)

func (f warmTargetResolverFunc) RunWarm(ctx context.Context, projectID, projectPath, promptText, correlationID string) (*mesnadaOrch.WarmRunResult, error) {
	return f(ctx, projectID, projectPath, promptText, correlationID)
}

// makeWarmTargetResolver bridges the orchestrator's WarmTargetResolver to the
// project Manager's warm-delegation path (Phase 7.3). It returns nil when warm
// reuse is disabled or no project manager is available, so the orchestrator keeps
// cold-spawning every delegated task. Project-level sentinels that mean "this
// project cannot be served warm" are mapped to orchestrator.ErrNoWarmTarget so
// the orchestrator falls back to the cold path; any other error is a genuine
// warm-run failure surfaced to the (terminal) task.
func makeWarmTargetResolver(mgr *project.Manager, cfg *config.Config) mesnadaOrch.WarmTargetResolver {
	if mgr == nil || cfg == nil || !cfg.Mesnada.Delegation.ReuseWarmInstances {
		return nil
	}
	autoStart := cfg.Mesnada.Delegation.AutoStartWarmInstance
	maxConcurrent := cfg.Mesnada.Delegation.MaxConcurrent
	queueDepth := cfg.Mesnada.Delegation.WarmQueueDepth
	// AllowExternalWarmTargets opts the caller into routing a delegated task whose
	// project is served by an external (editor-launched) instance to that peer over
	// the IPC bus (B3) instead of cold-spawning a CLI. Default OFF.
	allowExternal := cfg.Mesnada.Delegation.AllowExternalWarmTargets

	return warmTargetResolverFunc(func(ctx context.Context, projectID, projectPath, promptText, correlationID string) (*mesnadaOrch.WarmRunResult, error) {
		res, err := mgr.WarmDelegate(ctx, projectID, projectPath, promptText, autoStart, maxConcurrent, queueDepth, allowExternal, correlationID)
		if err != nil {
			// Map the cap-reached sentinel to the orchestrator's cap-specific
			// fallback error so it is counted separately in delegation metrics (E1);
			// every other cold-fallback reason maps to the generic ErrNoWarmTarget.
			if errors.Is(err, project.ErrWarmCapReached) {
				return nil, mesnadaOrch.ErrWarmCapReached
			}
			if isWarmColdFallback(err) {
				return nil, mesnadaOrch.ErrNoWarmTarget
			}
			return nil, err
		}
		return &mesnadaOrch.WarmRunResult{
			ChildSessionID: res.ChildSessionID,
			Output:         res.Output,
			StopReason:     res.StopReason,
			External:       res.External,
		}, nil
	})
}

// externalDelegationRecovererFunc adapts a plain function to the orchestrator's
// ExternalDelegationRecoverer interface.
type externalDelegationRecovererFunc func(ctx context.Context, projectID, projectPath, correlationID string) (*mesnadaOrch.WarmRunResult, mesnadaOrch.ExternalRecoveryState, error)

func (f externalDelegationRecovererFunc) RecoverExternalDelegation(ctx context.Context, projectID, projectPath, correlationID string) (*mesnadaOrch.WarmRunResult, mesnadaOrch.ExternalRecoveryState, error) {
	return f(ctx, projectID, projectPath, correlationID)
}

// makeExternalDelegationRecoverer bridges the orchestrator's recovery interface to
// the project Manager's external re-attach query (A2). It maps the project layer's
// protocol-string state onto the orchestrator's ExternalRecoveryState enum and a
// project.DelegateResult onto a WarmRunResult, so the orchestrator stays free of an
// internal/project import (same pattern as makeWarmTargetResolver). A non-nil error
// (unreachable / declined peer) maps to RecoveryUnknown so the task is marked failed
// — the pre-A2 outcome.
func makeExternalDelegationRecoverer(mgr *project.Manager) mesnadaOrch.ExternalDelegationRecoverer {
	if mgr == nil {
		return nil
	}
	return externalDelegationRecovererFunc(func(ctx context.Context, projectID, projectPath, correlationID string) (*mesnadaOrch.WarmRunResult, mesnadaOrch.ExternalRecoveryState, error) {
		res, state, err := mgr.RecoverExternalDelegation(ctx, projectID, projectPath, correlationID)
		if err != nil {
			return nil, mesnadaOrch.RecoveryUnknown, err
		}
		switch state {
		case protocol.DelegationStateCompleted:
			var wr *mesnadaOrch.WarmRunResult
			if res != nil {
				wr = &mesnadaOrch.WarmRunResult{
					ChildSessionID: res.ChildSessionID,
					Output:         res.Output,
					StopReason:     res.StopReason,
					External:       true,
				}
			}
			return wr, mesnadaOrch.RecoveryCompleted, nil
		case protocol.DelegationStateRunning:
			return nil, mesnadaOrch.RecoveryRunning, nil
		default:
			return nil, mesnadaOrch.RecoveryUnknown, nil
		}
	})
}

// isWarmColdFallback reports whether a WarmDelegate error means "no warm target;
// take the cold path" rather than a genuine run failure.
func isWarmColdFallback(err error) bool {
	return errors.Is(err, project.ErrInstanceNotRunning) ||
		errors.Is(err, project.ErrExternalInstance) ||
		errors.Is(err, project.ErrProjectNeedsInit) ||
		errors.Is(err, project.ErrProjectNotRegistered) ||
		errors.Is(err, project.ErrWarmCapReached) ||
		// External (editor-launched) peer that is unreachable or has not opted in
		// to accept delegations: take the cold path rather than fail the task (B3).
		errors.Is(err, project.ErrExternalUnreachable) ||
		errors.Is(err, project.ErrExternalDelegationRefused)
}

// makePandoModelResolver returns a function that converts a model ID (possibly
// empty or shorthand) into the full "provider.model" string expected by pando's
// -m flag when spawning CLI subagents.
//
// Resolution rules:
//  1. If modelID is non-empty and already contains "." or "/", return as-is.
//  2. If modelID is non-empty and exists in SupportedModels, return the canonical ID
//     (which already encodes the provider as a prefix, e.g. "anthropic.claude-sonnet-4-6").
//  3. If modelID is non-empty but unknown, prepend the provider of the currently
//     active AgentCoder model so that the pando CLI can look it up correctly.
//  4. If modelID is empty, return the currently active AgentCoder model ID.
func makePandoModelResolver(cfg *config.Config) func(string) string {
	return func(modelID string) string {
		// Determine the currently active model for the coder agent.
		activeModel := ""
		if cfg != nil {
			if coderAgent, ok := cfg.Agents[config.AgentCoder]; ok && coderAgent.Model != "" {
				activeModel = string(coderAgent.Model)
			}
		}

		if modelID == "" {
			// No model requested – use whatever pando itself is configured to use.
			return activeModel
		}

		// If the ID already encodes a provider (contains "." or "/"), use it directly.
		if strings.ContainsAny(modelID, "./") {
			return modelID
		}

		// Try to normalize via the models registry (handles shorthands like "sonnet").
		normalized := models.NormalizeModelID(modelID)
		if _, ok := models.SupportedModels[normalized]; ok {
			return string(normalized)
		}

		// Unknown model ID – prepend the provider from the active model so the
		// pando CLI can resolve it against the configured provider.
		if activeModel != "" {
			sepIdx := strings.IndexAny(activeModel, "./")
			if sepIdx > 0 {
				provider := activeModel[:sepIdx]
				return provider + "." + modelID
			}
		}

		return modelID
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
	if enginesDir := expandMesnadaPath(cfg.Mesnada.Orchestrator.EnginesDir); enginesDir != "" {
		mesnadaCfg.Orchestrator.EnginesDir = enginesDir
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
	discoveryPaths := skills.ConfiguredDiscoveryPaths(config.WorkingDirectory(), cfg.Skills.Paths)

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

func (a *evaluatorPromptAdapter) ClassifyTask(text string) string {
	return a.svc.ClassifyTask(text)
}

// agentContextTrimmerAdapter adapts *evaluator.ContextTrimmer to the agent.ContextTrimmer
// interface. It converts tool descriptions and delegates the profile confidence check so
// the agent package never needs to import the evaluator package directly.
type agentContextTrimmerAdapter struct {
	trimmer *evaluator.ContextTrimmer
}

func (a *agentContextTrimmerAdapter) ProfileTask(ctx context.Context, firstMessage string, availableTools []agent.ToolDescription) ([]string, error) {
	infos := make([]evaluator.ToolInfo, len(availableTools))
	for i, t := range availableTools {
		infos[i] = evaluator.ToolInfo{Name: t.Name, Description: t.Description}
	}
	profile, err := a.trimmer.ProfileTask(ctx, firstMessage, infos)
	if err != nil || profile == nil || profile.Confidence < 0.5 {
		return nil, err //nolint:nilerr // intentional fallback: low confidence → use all tools
	}
	return profile.RelevantToolNames, nil
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

// initIcons selects the active TUI icon set. When Nerd Fonts are disabled (via
// the TUI.NerdFonts config field or the PANDO_NERD_FONTS env override) the TUI
// falls back to plain BMP/ASCII glyphs that render on any terminal.
func (app *App) initIcons() {
	enabled := config.Get().NerdFontsEnabled()
	styles.SetNerdFonts(enabled)
	logging.Debug("Set TUI icon set", "nerdFonts", enabled)
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

// NonInteractiveGoalResult is the serialized outcome of a CLI goal-mode run.
type NonInteractiveGoalResult struct {
	SessionID          string `json:"session_id"`
	Objective          string `json:"objective"`
	Status             string `json:"status"`
	Iteration          int64  `json:"iteration"`
	MaxIterations      int64  `json:"max_iterations"`
	MaxDurationSeconds int64  `json:"max_duration_seconds"`
	Response           string `json:"response"`
	Progress           string `json:"progress,omitempty"`
	NextStep           string `json:"next_step,omitempty"`
	BlockedReason      string `json:"blocked_reason,omitempty"`
}

// RunNonInteractive handles the execution flow when a prompt is provided via CLI flag.
func (a *App) RunNonInteractive(ctx context.Context, prompt string, outputFormat string, quiet bool, yoloMode bool) error {
	logging.Info("Running in non-interactive mode")
	logging.Debug("Non-interactive mode started", "promptLength", len(prompt), "outputFormat", outputFormat, "yoloMode", yoloMode)

	// Tell all agents to act autonomously — no user is present to answer questions.
	agent.SetNonInteractiveMode(true)

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

func (a *App) RunNonInteractiveGoal(ctx context.Context, objective string, outputFormat string, quiet bool, yoloMode bool) (NonInteractiveGoalResult, error) {
	logging.Info("Running in non-interactive goal mode")
	logging.Debug("Non-interactive goal mode started", "objectiveLength", len(objective), "outputFormat", outputFormat, "yoloMode", yoloMode)

	objective = strings.TrimSpace(objective)
	if objective == "" {
		return NonInteractiveGoalResult{}, errors.New("goal objective cannot be empty")
	}

	agent.SetNonInteractiveMode(true)

	if yoloMode {
		a.Permissions.SetGlobalAutoApprove(true)
	}

	const maxObjectiveLengthForTitle = 100
	titleSuffix := objective
	if len(titleSuffix) > maxObjectiveLengthForTitle {
		titleSuffix = titleSuffix[:maxObjectiveLengthForTitle] + "..."
	}

	sess, err := a.Sessions.Create(ctx, "Goal: "+titleSuffix)
	if err != nil {
		return NonInteractiveGoalResult{}, fmt.Errorf("failed to create session for goal mode: %w", err)
	}
	logging.Info("Created session for non-interactive goal run", "session_id", sess.ID)

	if !yoloMode {
		a.Permissions.AutoApproveSession(sess.ID)
	}

	writeGoalProgress(os.Stderr, "Goal started: %s\n", objective)

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
					logging.Warn("Failed to stream non-interactive goal response", "session_id", sess.ID, "error", err)
					return
				}
			}
		}()
	}

	goalRunner := agent.NewGoalRunner(a.CoderAgent, a.DBQuerier)
	done, err := goalRunner.Run(ctx, sess.ID, objective, agent.GoalOptions{})
	if err != nil {
		if streamCancel != nil {
			streamCancel()
			streamWG.Wait()
		}
		return NonInteractiveGoalResult{}, fmt.Errorf("failed to start goal processing stream: %w", err)
	}

	var (
		lastEvent    agent.AgentEvent
		lastResponse string
		iterations   int
	)
	for event := range done {
		lastEvent = event
		if event.Type == agent.AgentEventTypeResponse {
			response := strings.TrimSpace(event.Message.Content().String())
			if response != "" {
				lastResponse = response
			}
			iterations++
			if !quiet {
				writeGoalProgress(os.Stderr, "Goal iteration %d completed.\n", iterations)
			}
		}
	}

	if streamCancel != nil {
		streamCancel()
		streamWG.Wait()
	}

	goal, goalErr := a.DBQuerier.GetGoalBySession(context.Background(), sess.ID)
	if goalErr != nil {
		if lastEvent.Error != nil {
			return NonInteractiveGoalResult{}, fmt.Errorf("goal processing failed: %w", lastEvent.Error)
		}
		return NonInteractiveGoalResult{}, fmt.Errorf("failed to load final goal status: %w", goalErr)
	}

	result := nonInteractiveGoalResultFromDB(goal, lastResponse)
	logGoalCompletion(os.Stderr, result)

	if outputFormat == format.Text.String() {
		content := result.Response
		if content == "" {
			content = result.Progress
		}
		if content == "" {
			content = result.BlockedReason
		}
		if content == "" {
			content = "No content available"
		}
		if streamer != nil {
			if err := streamer.PrintFinalContent(content); err != nil {
				return result, fmt.Errorf("failed to render final goal response: %w", err)
			}
			if streamer.wrote {
				if err := streamer.CloseLine(); err != nil {
					return result, fmt.Errorf("failed to finalize streamed goal response: %w", err)
				}
			} else {
				fmt.Println(content)
			}
		} else {
			fmt.Println(content)
		}
	} else {
		serialized, err := formatNonInteractiveGoalResult(result)
		if err != nil {
			return result, fmt.Errorf("failed to format goal result: %w", err)
		}
		fmt.Println(serialized)
	}

	if lastEvent.Error != nil && result.Status == agent.GoalStatusRunning {
		return result, fmt.Errorf("goal processing failed: %w", lastEvent.Error)
	}

	logging.Info("Non-interactive goal run completed", "session_id", sess.ID, "status", result.Status)
	return result, nil
}

func nonInteractiveGoalResultFromDB(goal db.SessionGoal, response string) NonInteractiveGoalResult {
	result := NonInteractiveGoalResult{
		SessionID:          goal.SessionID,
		Objective:          goal.Objective,
		Status:             goal.Status,
		Iteration:          goal.Iteration,
		MaxIterations:      goal.MaxIterations,
		MaxDurationSeconds: goal.MaxDurationSeconds,
		Response:           strings.TrimSpace(response),
	}
	if goal.LastProgress.Valid {
		result.Progress = strings.TrimSpace(goal.LastProgress.String)
	}
	if goal.NextStep.Valid {
		result.NextStep = strings.TrimSpace(goal.NextStep.String)
	}
	if goal.BlockedReason.Valid {
		result.BlockedReason = strings.TrimSpace(goal.BlockedReason.String)
	}
	return result
}

func formatNonInteractiveGoalResult(result NonInteractiveGoalResult) (string, error) {
	return format.FormatOutput(string(mustGoalResultJSON(result)), format.JSON.String()), nil
}

func mustGoalResultJSON(result NonInteractiveGoalResult) []byte {
	data, err := json.Marshal(result)
	if err != nil {
		return []byte("{}")
	}
	return data
}

func logGoalCompletion(w io.Writer, result NonInteractiveGoalResult) {
	statusLine := "Goal finished with status: " + result.Status
	if result.Status == agent.GoalStatusCompleted {
		statusLine = "Goal completed."
	}
	writeGoalProgress(w, "%s\n", statusLine)
	if result.Progress != "" {
		writeGoalProgress(w, "Progress: %s\n", result.Progress)
	}
	if result.BlockedReason != "" && result.BlockedReason != result.Progress {
		writeGoalProgress(w, "Reason: %s\n", result.BlockedReason)
	}
	if result.NextStep != "" {
		writeGoalProgress(w, "Next step: %s\n", result.NextStep)
	}
}

func writeGoalProgress(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, format, args...)
}

// refreshDynamicModels fetches model lists from configured provider accounts asynchronously.
// modelRefreshInterval is how often dynamic models are re-fetched from providers while running.
const modelRefreshInterval = 24 * time.Hour

func (app *App) refreshDynamicModels(ctx context.Context) {
	RefreshDynamicModels(ctx)

	ticker := time.NewTicker(modelRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			RefreshDynamicModels(ctx)
		}
	}
}

// RefreshDynamicModels fetches and caches models from all configured provider accounts.
// It is safe to call concurrently and is exported so other startup modes (e.g. LLM Proxy)
// can trigger the same refresh without a full App instance.
func RefreshDynamicModels(ctx context.Context) {
	cfg := config.Get()
	if cfg == nil {
		return
	}

	accounts := config.GetProviderAccounts()
	logging.Debug("Refreshing dynamic models", "accountCount", len(accounts))

	// Count non-disabled accounts per provider type (needed for model ID prefix logic)
	typeCount := make(map[models.ModelProvider]int)
	for _, acc := range accounts {
		if !acc.Disabled {
			typeCount[acc.Type]++
		}
	}

	for _, acc := range accounts {
		if acc.Disabled {
			continue
		}

		apiKey := acc.APIKey
		var bearerToken string

		switch acc.Type {
		case models.ProviderCopilot:
			token, err := auth.LoadGitHubOAuthToken()
			if err != nil || token == "" {
				continue
			}
			bearerToken = token
			apiKey = ""
		case models.ProviderOllama:
			// Ollama does not require an API key
		case models.ProviderAnthropic:
			// Anthropic accounts may authenticate via Claude.ai OAuth instead of an
			// API key. Without this, an OAuth-only account is skipped here, never
			// registers per-account models, and selection silently falls back to
			// another (API-key) account of the same type.
			if apiKey == "" {
				token, err := auth.LoadClaudeBearerToken()
				if err != nil || token == "" {
					continue
				}
				bearerToken = token
			}
		default:
			// Providers with a static catalog but no listing API (e.g. Vertex AI,
			// which authenticates via OAuth and has no API key) still need their
			// per-account static models registered, so don't skip them for a
			// missing key. Listing-capable providers do need a key to fetch.
			if apiKey == "" && models.ProviderSupportsModelListing(acc.Type) {
				continue
			}
		}

		params := models.AccountModelRefreshParams{
			AccountID:         acc.ID,
			ProviderType:      acc.Type,
			APIKey:            apiKey,
			BearerToken:       bearerToken,
			BaseURL:           acc.BaseURL,
			ExtraHeaders:      acc.ExtraHeaders,
			AllAccountsOfType: typeCount[acc.Type],
		}
		if err := models.RefreshProviderModelsForAccount(ctx, params); err != nil {
			logging.Debug("Failed to refresh models from account", "account", acc.ID, "error", err)
		} else {
			logging.Debug("Refreshed models from account", "account", acc.ID)
			if err := models.SaveModelCache(); err != nil {
				logging.Debug("Failed to save model cache", "error", err)
			}
		}
	}
}

// StartModelRefreshLoop refreshes dynamic models immediately and then every 24 h until ctx is cancelled.
// Use this in startup modes that do not create a full App instance (e.g. LLM Proxy).
func StartModelRefreshLoop(ctx context.Context) {
	RefreshDynamicModels(ctx)

	ticker := time.NewTicker(modelRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			RefreshDynamicModels(ctx)
		}
	}
}

// Shutdown performs a clean shutdown of the application
// SetupIPC configures the primary IPC bus for this instance.
// Call this after New() on the primary instance to enable ZMQ event broadcasting
// and to register the db.write handler so secondary instances can proxy writes.
// bus must already be started (bus.Start called) before calling SetupIPC.
func (app *App) SetupIPC(bus *ipc.Bus) {
	app.IPCBus = bus
	app.IPCIsPrimary = true
	// Wire the bus as the ZMQ publisher for the session service so session
	// create/update/delete events are broadcast over PUB to other instances.
	session.SetIPCPublisher(bus)

	// Register the Remembrances write dispatcher so that KB, Events and Code
	// indexing writes forwarded from secondary instances via IPC are correctly
	// applied to the primary's read-write SQLite database.
	if app.Remembrances != nil {
		dispatcher := ragproxy.NewRemembrancesWriteDispatcher(app.Remembrances)
		dbproxy.RegisterRemembrancesDispatcher(dispatcher)
		logging.Info("Remembrances IPC write dispatcher registered on primary")
	}

	logging.Info("IPC bus wired to session service", "pubAddr", bus.PubAddr, "rpcAddr", bus.RPCAddr)
}

// SetIPCSecondaryContext stores the secondary IPC state and registers the active-probe
// function on the watcher so it can perform per-prompt and per-minute liveness checks.
//
// busSetupFunc is called during promotion with the new Bus and RW connection.
// It must wire the writecoordinator, changepub publisher, and bridge handlers.
func (app *App) SetIPCSecondaryContext(
	client *ipc.Client,
	roConn *sql.DB,
	workdir, instanceID string,
	pubPort, rpcPort int,
	watcher *failover.Watcher,
	busSetupFunc func(ctx context.Context, bus *ipc.Bus, rwConn *sql.DB) error,
) {
	app.ipcClient = client
	app.ipcROConn = roConn
	app.ipcWorkdir = workdir
	app.ipcInstanceID = instanceID
	app.ipcPubPort = pubPort
	app.ipcRPCPort = rpcPort
	app.ipcWatcher = watcher
	app.ipcBusSetupFunc = busSetupFunc

	// Wire the per-prompt probe function on the watcher so it can do active pings.
	if proxy, ok := app.DBQuerier.(*dbproxy.DBProxy); ok {
		watcher.SetProbePrimary(proxy.ProbePrimary)
	}
}

// EnsurePrimary performs an active liveness probe of the primary and, if the
// primary is unreachable and auto-failover is enabled, triggers the failover
// sequence.  Must be called before processing each user prompt on a secondary
// instance.  Safe to call on the primary instance (no-op).
func (app *App) EnsurePrimary(ctx context.Context) {
	if app.IPCIsPrimary || app.ipcWatcher == nil {
		return
	}
	if err := app.ipcWatcher.CheckAndMaybeFailover(ctx); err != nil {
		logging.Warn("EnsurePrimary: primary unavailable", "error", err)
	}
}

// dbCompactCallTimeout bounds how long a secondary waits for the primary to finish
// a forwarded db.compact RPC. A full VACUUM on a large database can take minutes,
// so this is far longer than the default IPC call timeout.
const dbCompactCallTimeout = 30 * time.Minute

// CompactDatabase reclaims unused space in the SQLite database (VACUUM). Because
// only the primary owns DB writes, this is a single funnel used by every UI
// (/db-compact in TUI/WebUI/ACP) and the IPC db.compact handler:
//
//   - On the primary (or when IPC is not active) it runs db.Compact directly on
//     the read-write connection.
//   - On a secondary it forwards the request to the primary over IPC and returns
//     the primary's result, so two writers never contend for the database.
//
// incremental runs only PRAGMA incremental_vacuum; enableAutoVacuum switches the
// database to auto_vacuum=INCREMENTAL before the full VACUUM.
func (app *App) CompactDatabase(ctx context.Context, incremental, enableAutoVacuum bool) (protocol.DBCompactResult, error) {
	// Secondary: forward to the primary's writer connection over IPC.
	if !app.IPCIsPrimary && app.ipcClient != nil {
		rpcAddr := fmt.Sprintf("tcp://127.0.0.1:%d", app.ipcRPCPort)
		params := protocol.DBCompactParams{Incremental: incremental, EnableAutoVacuum: enableAutoVacuum}
		raw, err := app.ipcClient.CallWithTimeout(ctx, rpcAddr, protocol.MethodDBCompact, params, dbCompactCallTimeout)
		if err != nil {
			return protocol.DBCompactResult{}, fmt.Errorf("compact via primary: %w", err)
		}
		var res protocol.DBCompactResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return protocol.DBCompactResult{}, fmt.Errorf("compact: decode primary result: %w", err)
		}
		return res, nil
	}

	// Primary (or standalone): run the VACUUM locally on the writer connection.
	if app.rwConn == nil {
		return protocol.DBCompactResult{}, fmt.Errorf("compact: no writable database connection")
	}
	r, err := db.Compact(ctx, app.rwConn, db.CompactOptions{
		Incremental:      incremental,
		EnableAutoVacuum: enableAutoVacuum,
	})
	if err != nil {
		return protocol.DBCompactResult{}, err
	}
	return protocol.DBCompactResult{
		Mode:       r.Mode,
		SizeBefore: r.SizeBefore,
		SizeAfter:  r.SizeAfter,
		Freed:      r.Freed,
	}, nil
}

// acpDBCompactorAdapter adapts App.CompactDatabase to the ACP package's
// DBCompactor interface (translating the protocol result to the acp result type),
// so the /db-compact slash command works over ACP without an import cycle.
type acpDBCompactorAdapter struct{ app *App }

func (a acpDBCompactorAdapter) CompactDatabase(ctx context.Context, incremental, enableAutoVacuum bool) (mesnadaACP.DBCompactResult, error) {
	r, err := a.app.CompactDatabase(ctx, incremental, enableAutoVacuum)
	if err != nil {
		return mesnadaACP.DBCompactResult{}, err
	}
	return mesnadaACP.DBCompactResult{
		Mode:       r.Mode,
		SizeBefore: r.SizeBefore,
		SizeAfter:  r.SizeAfter,
		Freed:      r.Freed,
	}, nil
}

// ACPDBCompactor returns an ACP DBCompactor backed by this app's CompactDatabase.
func (app *App) ACPDBCompactor() mesnadaACP.DBCompactor { return acpDBCompactorAdapter{app: app} }

// PromoteToPrimary is the failover.PromoteFunc implementation.
// It is called by the Watcher when this secondary wins the lock race.
// lockFile is the open flock file that must be kept open for the duration of
// this instance's primary role.
//
// The method:
//  1. Closes the old read-only SQLite connection.
//  2. Opens a new read-write connection (with migrations).
//  3. Creates a new IPC Bus and calls ipcBusSetupFunc to wire all handlers.
//  4. Updates the app's Querier, IPCBus, and IPCIsPrimary fields.
//  5. Publishes instance.promoted on the new Bus so other secondaries reconnect.
func (app *App) PromoteToPrimary(ctx context.Context, lockFile *os.File) error {
	logging.Info("failover: PromoteToPrimary starting",
		"instance_id", app.ipcInstanceID,
		"workdir", app.ipcWorkdir,
	)

	// 1. Close old read-only connection.
	if app.ipcROConn != nil {
		_ = app.ipcROConn.Close()
		app.ipcROConn = nil
	}
	if app.ipcClient != nil {
		_ = app.ipcClient.Close()
		app.ipcClient = nil
	}

	// 2. Open read-write SQLite connection.
	rwConn, err := db.Connect()
	if err != nil {
		return fmt.Errorf("failover: open RW DB: %w", err)
	}

	// 3. Create a new IPC Bus and wire all handlers via the injected setup function.
	bus := ipc.NewBus(app.ipcInstanceID)
	if app.ipcBusSetupFunc != nil {
		if setupErr := app.ipcBusSetupFunc(ctx, bus, rwConn); setupErr != nil {
			_ = rwConn.Close()
			return fmt.Errorf("failover: bus setup: %w", setupErr)
		}
	}
	if startErr := bus.Start(ctx, app.ipcPubPort, app.ipcRPCPort); startErr != nil {
		_ = rwConn.Close()
		return fmt.Errorf("failover: start bus: %w", startErr)
	}

	// 4. Update app state.
	app.DBQuerier = db.New(rwConn)
	app.rwConn = rwConn
	app.SetupIPC(bus)

	// 5. Publish instance.promoted so other secondaries reset their heartbeat timers
	//    and reconnect to the new primary.
	_ = bus.Publish(protocol.TopicInstancePromoted, protocol.PromotedPayload{
		InstanceID: app.ipcInstanceID,
		PubAddr:    bus.PubAddr,
		RPCAddr:    bus.RPCAddr,
	})

	logging.Info("failover: PromoteToPrimary complete",
		"instance_id", app.ipcInstanceID,
		"pub_addr", bus.PubAddr,
		"rpc_addr", bus.RPCAddr,
	)

	// The lockFile is intentionally NOT closed here — it must remain open for the
	// duration of this instance's primary role to maintain the flock.
	_ = lockFile

	return nil
}

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
	if app.delegationSupervisor != nil {
		app.delegationSupervisor.Stop()
	}
	if app.MesnadaOrchestrator != nil {
		if err := app.MesnadaOrchestrator.Shutdown(); err != nil {
			logging.Error("Failed to shutdown Mesnada orchestrator", "error", err)
		}
	}

	// Cleanup old agent-vcs data
	if app.AgentVCS != nil {
		cfg := config.Get()
		if cfg != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := app.AgentVCS.Cleanup(cleanupCtx, cfg.Snapshots.AutoCleanupDays, cfg.Snapshots.MaxSnapshots); err != nil {
				logging.Error("Failed to cleanup agent-vcs", "error", err)
			}
			cancel()
		}
	}

	// Flush the token-savings ledger background writer.
	savings.Close()

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
	// Shutdown IPC bus (primary only).
	if app.IPCBus != nil {
		if err := app.IPCBus.Shutdown(); err != nil {
			logging.Error("Failed to shutdown IPC bus", "error", err)
		}
	}
	logging.Debug("App shutdown completed")
}

// ---------------------------------------------------------------------------
// ACP adapter types — bridge app services to the narrow interfaces that
// PandoACPAgent requires, avoiding import cycles between mesnada/acp and
// internal/llm/agent, internal/session, and internal/permission.
// ---------------------------------------------------------------------------

type appACPAgentAdapter struct {
	svc        agent.Service
	goalRunner *agent.GoalRunner
}

func (a *appACPAgentAdapter) Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan mesnadaACP.AgentEvent, error) {
	realCh, err := a.svc.Run(ctx, sessionID, content, attachments...)
	if err != nil {
		return nil, err
	}
	return a.forwardEvents(ctx, realCh), nil
}

func (a *appACPAgentAdapter) RunGoal(ctx context.Context, sessionID string, objective string) (<-chan mesnadaACP.AgentEvent, error) {
	realCh, err := a.goalRunner.Run(ctx, sessionID, objective, agent.GoalOptions{})
	if err != nil {
		return nil, err
	}
	return a.forwardEvents(ctx, realCh), nil
}

func (a *appACPAgentAdapter) forwardEvents(ctx context.Context, realCh <-chan agent.AgentEvent) <-chan mesnadaACP.AgentEvent {
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
				acpEv.Progress = ev.Progress
			case agent.AgentEventTypeSystemMessage:
				acpEv.Type = mesnadaACP.AgentEventTypeSystemMessage
				acpEv.SystemMessage = ev.SystemMessage
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
	return acpCh
}

func (a *appACPAgentAdapter) Cancel(sessionID string) { a.svc.Cancel(sessionID) }

func (a *appACPAgentAdapter) Steer(sessionID string, content string, attachments ...message.Attachment) error {
	return a.svc.Steer(sessionID, content, attachments...)
}

func (a *appACPAgentAdapter) PendingSteering(sessionID string) int {
	return a.svc.PendingSteering(sessionID)
}

func (a *appACPAgentAdapter) IsSessionBusy(sessionID string) bool {
	return a.svc.IsSessionBusy(sessionID)
}

func (a *appACPAgentAdapter) LastRunSystemMessages(sessionID string) []string {
	return a.svc.LastRunSystemMessages(sessionID)
}

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

func (a *appACPAgentAdapter) SetSessionLLMOverrides(sessionID string, overrides mesnadaACP.SessionLLMOverrides) {
	agent.SetSessionLLMOverrides(sessionID, agent.SessionLLMOverrides{
		Model:           models.ModelID(overrides.Model),
		ReasoningEffort: overrides.ReasoningEffort,
		ThinkingMode:    config.ThinkingMode(overrides.ThinkingMode),
		Persona:         overrides.Persona,
		PersonaScoped:   overrides.PersonaScoped,
	})
}

func (a *appACPAgentAdapter) SetPonytailMode(sessionID string, mode string) (string, bool) {
	m, ok := ponytail.ParseMode(mode)
	if !ok {
		return "", false
	}
	agent.SetPonytailMode(sessionID, m)
	return m.String(), true
}

func (a *appACPAgentAdapter) SetCavemanMode(sessionID string, mode string) (string, bool) {
	m, ok := caveman.ParseMode(mode)
	if !ok {
		return "", false
	}
	agent.SetCavemanMode(sessionID, m)
	return m.String(), true
}

func (a *appACPAgentAdapter) SetSuperpowersMode(sessionID string, enabled bool) {
	agent.SetSuperpowersMode(sessionID, enabled)
}

func (a *appACPAgentAdapter) SuperpowersMode(sessionID string) bool {
	return agent.SuperpowersMode(sessionID)
}

func (a *appACPAgentAdapter) SuperpowersFinish(ctx context.Context, sessionID string) (<-chan mesnadaACP.AgentEvent, error) {
	realCh, err := agent.RunSuperpowersFinish(ctx, a.svc, sessionID)
	if err != nil {
		return nil, err
	}
	return a.forwardEvents(ctx, realCh), nil
}

func (a *appACPAgentAdapter) ListPersonas() []string             { return agent.ListAvailablePersonas() }
func (a *appACPAgentAdapter) GetActivePersona() string           { return agent.GetActivePersona() }
func (a *appACPAgentAdapter) SetActivePersona(name string) error { return agent.SetActivePersona(name) }
func (a *appACPAgentAdapter) Summarize(ctx context.Context, sessionID string) error {
	return a.svc.Summarize(ctx, sessionID)
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
	q      db.Querier
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

func (a *appACPSessionAdapter) SaveSessionTitle(ctx context.Context, id string, title string) (mesnadaACP.ACPSessionInfo, error) {
	sess, err := a.svc.Get(ctx, id)
	if err != nil {
		return mesnadaACP.ACPSessionInfo{}, err
	}
	sess.Title = title
	saved, err := a.svc.Save(ctx, sess)
	if err != nil {
		return mesnadaACP.ACPSessionInfo{}, err
	}
	return mesnadaACP.ACPSessionInfo{ID: saved.ID, Title: saved.Title, UpdatedAt: saved.UpdatedAt}, nil
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

func (a *appACPSessionAdapter) GetACPSessionState(ctx context.Context, sessionID string) (string, error) {
	return a.svc.GetACPSessionState(ctx, sessionID)
}

func (a *appACPSessionAdapter) GetActiveGoal(ctx context.Context, sessionID string) (db.SessionGoal, error) {
	return a.q.GetActiveGoal(ctx, sessionID)
}

func (a *appACPSessionAdapter) GetGoalBySession(ctx context.Context, sessionID string) (db.SessionGoal, error) {
	return a.q.GetGoalBySession(ctx, sessionID)
}

func (a *appACPSessionAdapter) CancelGoal(ctx context.Context, sessionID string) (db.SessionGoal, error) {
	goal, err := a.q.GetActiveGoal(ctx, sessionID)
	if err != nil {
		return db.SessionGoal{}, err
	}
	return a.q.UpdateGoalStatus(ctx, db.UpdateGoalStatusParams{
		Status:        agent.GoalStatusCancelled,
		Iteration:     goal.Iteration,
		LastProgress:  goal.LastProgress,
		NextStep:      sql.NullString{},
		BlockedReason: sql.NullString{},
		CompletedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
		ID:            goal.ID,
	})
}

func (a *appACPSessionAdapter) GetMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	return a.msgSvc.List(ctx, sessionID)
}

func (a *appACPSessionAdapter) SaveACPSessionState(ctx context.Context, sessionID string, state string) error {
	return a.svc.SaveACPSessionState(ctx, sessionID, state)
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
