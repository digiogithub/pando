package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/evaluatortools"
	llmtools "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	mesnadaServer "github.com/digiogithub/pando/internal/mesnada/server"
	"github.com/digiogithub/pando/internal/version"
	"github.com/spf13/cobra"
)

var mcpServerCmd = &cobra.Command{
	Use:   "mcp-server",
	Short: "Start Pando as an MCP server",
	Long: `Start Pando as an MCP server that exposes Pando's internal tools to external agents.

By default this mode enables both transports at the same time:
- stdio for process-based MCP clients
- streamable HTTP on /mcp for remote MCP clients

Tool groups exposed (configurable via .pando.toml [MCPServer] section or CLI flags):
- fetch and web search tools
- browser / Chrome DevTools-style tools
- remembrances tools (KB, events, code-intelligence, and KB-backed memory: remember/recall/forget)
- Mesnada orchestration tools
- cache and pagination tools
- file tools: view, glob, grep, ls (and optionally write, edit, patch)
- system execution: bash shell
- mcp gateway: re-export all connected MCP server tools
- self-improvement: evaluator stats, skills, and session evaluation`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMCPServerMode(cmd)
	},
}

func init() {
	rootCmd.AddCommand(mcpServerCmd)

	mcpServerCmd.Flags().Bool("debug", false, "Enable debug logging")
	mcpServerCmd.Flags().Bool("no-stdio", false, "Disable the stdio MCP transport")
	mcpServerCmd.Flags().Bool("no-http", false, "Disable the HTTP MCP transport")
	mcpServerCmd.Flags().StringP("cwd", "c", "", "Working directory for the MCP server (defaults to current directory)")

	// Tool group flags – when provided they override the config file.
	mcpServerCmd.Flags().Bool("file-tools", false, "Enable file read tools (view, glob, grep, ls)")
	mcpServerCmd.Flags().Bool("file-tools-write", false, "Also enable file write tools (write, edit, patch); implies --file-tools")
	mcpServerCmd.Flags().Bool("system-exec", false, "Enable bash/shell execution tool")
	mcpServerCmd.Flags().Bool("gateway-expose", false, "Re-export MCPGateway tools through this MCP server")
	mcpServerCmd.Flags().Bool("self-improvement", false, "Expose self-improvement evaluator tools")
}

func runMCPServerMode(cmd *cobra.Command) error {
	host, _ := cmd.Flags().GetString("host")
	port, _ := cmd.Flags().GetInt("port")
	debug, _ := cmd.Flags().GetBool("debug")
	noStdio, _ := cmd.Flags().GetBool("no-stdio")
	noHTTP, _ := cmd.Flags().GetBool("no-http")
	cwdFlag, _ := cmd.Flags().GetString("cwd")
	ageKeys, _ := cmd.Flags().GetString("age-keys")
	config.SetAgeKeysOverride(ageKeys)
	if !cmd.Flags().Changed("port") {
		port = 9777
	}

	if noStdio && noHTTP {
		return fmt.Errorf("at least one MCP transport must be enabled")
	}

	var cwd string
	if cwdFlag != "" {
		if err := os.Chdir(cwdFlag); err != nil {
			return fmt.Errorf("failed to change directory to %q: %w", cwdFlag, err)
		}
		cwd = cwdFlag
	} else {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current working directory: %w", err)
		}
	}

	if _, err := config.Load(cwd, debug, ""); err != nil {
		return err
	}
	enableMCPServerFeatures()

	// Apply CLI flag overrides for tool groups on top of the config defaults.
	applyMCPServerFlagOverrides(cmd)

	conn, err := db.Connect()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pandoApp, err := app.New(ctx, conn, app.AppOptions{
		SkipLSP:           true,
		SkipMesnadaServer: true,
		StartupMode:       "mcp",
	})
	if err != nil {
		return fmt.Errorf("failed to initialize app: %w", err)
	}
	defer pandoApp.Shutdown()
	pandoApp.Permissions.SetGlobalAutoApprove(true)

	toolList := buildMCPServerTools(ctx, pandoApp)
	if len(toolList) == 0 {
		return fmt.Errorf("no MCP tools available")
	}

	// Config may override host/port when not supplied via CLI.
	cfg := config.Get()
	if cfg != nil {
		if !cmd.Flags().Changed("host") && cfg.MCPServer.HttpHost != "" {
			host = cfg.MCPServer.HttpHost
		}
		if !cmd.Flags().Changed("port") && cfg.MCPServer.HttpPort > 0 {
			port = cfg.MCPServer.HttpPort
		}
		// Config-level transport toggles apply when CLI flags are not set.
		if !cmd.Flags().Changed("no-stdio") && !cfg.MCPServer.StdioEnabled && cfg.MCPServer.HttpEnabled {
			noStdio = true
		}
		if !cmd.Flags().Changed("no-http") && !cfg.MCPServer.HttpEnabled && cfg.MCPServer.StdioEnabled {
			noHTTP = true
		}
	}

	errCh := make(chan error, 2)
	var httpSrv *mesnadaServer.Server

	if !noHTTP {
		selectedPort, err := chooseAvailablePort(host, port)
		if err != nil {
			return err
		}
		if selectedPort != port {
			logging.Warn("Preferred MCP port unavailable, using alternative", "preferred", port, "actual", selectedPort)
			port = selectedPort
		}

		addr := fmt.Sprintf("%s:%d", host, port)
		httpSrv = mesnadaServer.New(mesnadaServer.Config{
			Addr:         addr,
			Orchestrator: pandoApp.MesnadaOrchestrator,
			Version:      version.Normalize(),
			UseStdio:     false,
			Remembrances: pandoApp.Remembrances,
			PandoTools:   toolList,
		})
		go func() {
			if err := httpSrv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}()
		fmt.Fprintf(os.Stderr, "Pando MCP HTTP transport listening on http://%s/mcp\n", addr)
	}

	if noStdio {
		sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stopSignals()

		select {
		case <-sigCtx.Done():
			cancel()
			shutdownHTTPMCPServer(httpSrv)
			return nil
		case err := <-errCh:
			cancel()
			shutdownHTTPMCPServer(httpSrv)
			return err
		}
	}

	stdioSrv := mesnadaServer.New(mesnadaServer.Config{
		Orchestrator: pandoApp.MesnadaOrchestrator,
		Version:      version.Normalize(),
		UseStdio:     true,
		Remembrances: pandoApp.Remembrances,
		PandoTools:   toolList,
	})

	if noHTTP {
		return stdioSrv.Start()
	}

	go func() {
		if err := stdioSrv.Start(); err != nil {
			errCh <- err
		}
	}()

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	select {
	case <-sigCtx.Done():
		cancel()
		shutdownHTTPMCPServer(httpSrv)
		return nil
	case err := <-errCh:
		cancel()
		shutdownHTTPMCPServer(httpSrv)
		return err
	}
}

// enableMCPServerFeatures turns on subsystems required for the standard MCP
// server tool set (mesnada, remembrances, and internal tools that are properly configured).
// Search tools are only enabled when their API keys are present.
func enableMCPServerFeatures() {
	cfg := config.Get()
	if cfg == nil {
		return
	}

	cfg.Mesnada.Enabled = true
	cfg.Remembrances.Enabled = true
	// Enable the KB-backed memory system so the remember/recall/forget tools are
	// exposed and the lightweight TTL garbage collector runs in server mode.
	cfg.Remembrances.MemoryEnabled = true
	cfg.InternalTools.FetchEnabled = true
	cfg.InternalTools.BrowserEnabled = true

	// Only enable search tools if their API keys are configured.
	it := cfg.InternalTools
	if strings.TrimSpace(it.GoogleAPIKey) != "" {
		cfg.InternalTools.GoogleSearchEnabled = true
	}
	if strings.TrimSpace(it.BraveAPIKey) != "" {
		cfg.InternalTools.BraveSearchEnabled = true
	}
	if strings.TrimSpace(it.PerplexityAPIKey) != "" {
		cfg.InternalTools.PerplexitySearchEnabled = true
	}
	if strings.TrimSpace(it.ExaAPIKey) != "" {
		cfg.InternalTools.ExaSearchEnabled = true
	}

	// Enable MCPGateway if gateway-expose is configured.
	if cfg.MCPServer.GatewayExpose.Enabled {
		cfg.MCPGateway.Enabled = true
	}

	// Enable Evaluator if self-improvement exposure is configured.
	if cfg.MCPServer.SelfImprovement.Enabled {
		cfg.Evaluator.Enabled = true
	}
}

// applyMCPServerFlagOverrides applies CLI flag overrides for tool groups on
// top of whatever was already set in the config file.
func applyMCPServerFlagOverrides(cmd *cobra.Command) {
	cfg := config.Get()
	if cfg == nil {
		return
	}

	if cmd.Flags().Changed("file-tools") || cmd.Flags().Changed("file-tools-write") {
		cfg.MCPServer.FileTools.Enabled = true
	}
	if cmd.Flags().Changed("file-tools-write") {
		cfg.MCPServer.FileTools.AllowWrite = true
	}
	if cmd.Flags().Changed("system-exec") {
		v, _ := cmd.Flags().GetBool("system-exec")
		cfg.MCPServer.SystemExecution.Enabled = v
	}
	if cmd.Flags().Changed("gateway-expose") {
		v, _ := cmd.Flags().GetBool("gateway-expose")
		cfg.MCPServer.GatewayExpose.Enabled = v
		if v {
			cfg.MCPGateway.Enabled = true
		}
	}
	if cmd.Flags().Changed("self-improvement") {
		v, _ := cmd.Flags().GetBool("self-improvement")
		cfg.MCPServer.SelfImprovement.Enabled = v
		if v {
			cfg.Evaluator.Enabled = true
		}
	}
}

// buildMCPServerTools assembles the list of tools to expose based on the
// current config, including conditionally-enabled tool groups.
func buildMCPServerTools(ctx context.Context, appSvc *app.App) []llmtools.BaseTool {
	cfg := config.Get()

	tools := []llmtools.BaseTool{
		llmtools.NewCacheReadTool(),
		llmtools.NewCacheStatsTool(),
		llmtools.NewSavingsStatsTool(),
	}

	// Only expose internal tools that are enabled and properly configured (API keys present).
	if cfg != nil {
		it := cfg.InternalTools
		if it.FetchEnabled {
			tools = append(tools, llmtools.NewFetchTool(appSvc.Permissions))
		}
		if it.GoogleSearchEnabled && strings.TrimSpace(it.GoogleAPIKey) != "" {
			tools = append(tools, llmtools.NewGoogleSearchTool(appSvc.Permissions))
		}
		if it.BraveSearchEnabled && strings.TrimSpace(it.BraveAPIKey) != "" {
			tools = append(tools, llmtools.NewBraveSearchTool(appSvc.Permissions))
		}
		if it.PerplexitySearchEnabled && strings.TrimSpace(it.PerplexityAPIKey) != "" {
			tools = append(tools, llmtools.NewPerplexitySearchTool(appSvc.Permissions))
		}
		if it.ExaSearchEnabled && strings.TrimSpace(it.ExaAPIKey) != "" {
			tools = append(tools, llmtools.NewExaSearchTool(appSvc.Permissions))
		}
		if it.BrowserEnabled {
			tools = append(tools,
				llmtools.NewBrowserNavigateTool(),
				llmtools.NewBrowserScreenshotTool(),
				llmtools.NewBrowserGetContentTool(),
				llmtools.NewBrowserEvaluateTool(),
				llmtools.NewBrowserClickTool(),
				llmtools.NewBrowserFillTool(),
				llmtools.NewBrowserScrollTool(),
				llmtools.NewBrowserConsoleLogsTool(),
				llmtools.NewBrowserNetworkTool(),
				llmtools.NewBrowserPDFTool(),
			)
		}
		if it.Context7Enabled {
			tools = append(tools, llmtools.NewContext7Tools()...)
			logging.Info("MCP server: Context7 tools enabled")
		}
		if it.SourcegraphEnabled {
			tools = append(tools, llmtools.NewSourcegraphTool())
			logging.Info("MCP server: Sourcegraph tool enabled")
		}
	}

	if appSvc.MesnadaOrchestrator != nil {
		tools = append(tools,
			llmtools.NewMesnadaSpawnTool(appSvc.MesnadaOrchestrator),
			llmtools.NewMesnadaGetTaskTool(appSvc.MesnadaOrchestrator),
			llmtools.NewMesnadaListTasksTool(appSvc.MesnadaOrchestrator),
			llmtools.NewMesnadaWaitTaskTool(appSvc.MesnadaOrchestrator),
			llmtools.NewMesnadaCancelTaskTool(appSvc.MesnadaOrchestrator),
			llmtools.NewMesnadaGetOutputTool(appSvc.MesnadaOrchestrator),
		)
	}

	if appSvc.Remembrances != nil {
		tools = append(tools,
			llmtools.NewKBAddDocumentTool(appSvc.Remembrances.KB),
			llmtools.NewKBImportPathTool(appSvc.Remembrances.KB),
			llmtools.NewKBSearchDocumentsTool(appSvc.Remembrances.KB),
			llmtools.NewKBGetDocumentTool(appSvc.Remembrances.KB),
			llmtools.NewKBDeleteDocumentTool(appSvc.Remembrances.KB),
			llmtools.NewKBRelatedDocumentsTool(appSvc.Remembrances.KB),
			llmtools.NewSaveEventTool(appSvc.Remembrances.Events),
			llmtools.NewSearchEventsTool(appSvc.Remembrances.Events),
			llmtools.NewHybridSearchRemembrancesTool(appSvc.Remembrances),
			llmtools.NewCodeIndexProjectTool(appSvc.Remembrances.Code),
			llmtools.NewCodeIndexStatusTool(appSvc.Remembrances.Code),
			llmtools.NewCodeHybridSearchTool(appSvc.Remembrances.Code),
			llmtools.NewCodeFindSymbolTool(appSvc.Remembrances.Code),
			llmtools.NewCodeGetSymbolsOverviewTool(appSvc.Remembrances.Code),
			llmtools.NewCodeGetProjectStatsTool(appSvc.Remembrances.Code),
			llmtools.NewCodeDeleteProjectTool(appSvc.Remembrances.Code),
			llmtools.NewCodeReindexFileTool(appSvc.Remembrances.Code),
			llmtools.NewCodeListProjectsTool(appSvc.Remembrances.Code),
			llmtools.NewCodeSearchPatternTool(appSvc.Remembrances.Code),
			llmtools.NewCodeImpactAnalysisTool(appSvc.Remembrances.Code),
			llmtools.NewCodeRelatedFilesTool(appSvc.Remembrances.Code),
		)

		// KB-backed persistent memory tools (remember/recall/forget). Gated by
		// MemoryEnabled, which enableMCPServerFeatures turns on in server mode.
		if cfg != nil && cfg.Remembrances.MemoryEnabled {
			tools = append(tools,
				llmtools.NewRememberTool(appSvc.Remembrances.KB, cfg.Remembrances.MemoryDefaultTTLDays),
				llmtools.NewRecallTool(appSvc.Remembrances.KB, cfg.Remembrances.MemoryDefaultTTLDays),
				llmtools.NewForgetTool(appSvc.Remembrances.KB),
			)
			logging.Info("MCP server: memory tools enabled")
		}
	}

	// --- Conditional tool groups ---
	//
	// File (view/glob/grep/ls + optional write/edit/patch) and system-execution
	// (bash) tools are intentionally NOT exposed by default. Most MCP clients are
	// coding agents that already ship their own editing, listing and terminal
	// tools, so re-exposing Pando's would duplicate capabilities and cause
	// ambiguity. They are opt-in only, via CLI flags (--file-tools,
	// --file-tools-write, --system-exec) or the equivalent .pando.toml
	// [MCPServer.FileTools] / [MCPServer.SystemExecution] config. Note that
	// enableMCPServerFeatures() never turns these on, keeping the default safe.

	if cfg != nil && cfg.MCPServer.FileTools.Enabled {
		tools = append(tools,
			llmtools.NewViewTool(appSvc),
			llmtools.NewGlobTool(),
			llmtools.NewGrepTool(),
			llmtools.NewLsTool(),
		)
		if cfg.MCPServer.FileTools.AllowWrite {
			tools = append(tools,
				llmtools.NewWriteTool(appSvc, appSvc.Permissions, appSvc.History),
				llmtools.NewEditTool(appSvc, appSvc.Permissions, appSvc.History),
				llmtools.NewPatchTool(appSvc, appSvc.Permissions, appSvc.History),
			)
		}
		logging.Info("MCP server: file tools enabled", "allow_write", cfg.MCPServer.FileTools.AllowWrite)
	}

	if cfg != nil && cfg.MCPServer.SystemExecution.Enabled {
		tools = append(tools, llmtools.NewBashTool(appSvc.Permissions))
		logging.Info("MCP server: system execution tools enabled")
	}

	// Expose configured MCP server tools as proxy tools.
	// When the gateway is active, tools are routed through it (catalog + call proxy + favorites);
	// otherwise they are exposed as direct MCP tool wrappers.
	if cfg != nil && len(cfg.MCPServers) > 0 {
		if cfg.MCPServer.GatewayExpose.Enabled && appSvc.MCPGateway != nil {
			gatewayTools := agent.GetMcpToolsWithGateway(ctx, appSvc.Permissions, appSvc.MCPGateway)
			tools = append(tools, gatewayTools...)
			logging.Info("MCP server: gateway tools exposed", "count", len(gatewayTools))
		} else {
			mcpProxyTools := agent.GetMcpTools(ctx, appSvc.Permissions)
			tools = append(tools, mcpProxyTools...)
			logging.Info("MCP server: MCP proxy tools exposed", "count", len(mcpProxyTools))
		}
	}

	if cfg != nil && cfg.MCPServer.SelfImprovement.Enabled && appSvc.Evaluator != nil {
		tools = append(tools,
			evaluatortools.NewEvaluatorStatsTool(appSvc.Evaluator),
			evaluatortools.NewEvaluatorSkillsTool(appSvc.Evaluator),
			evaluatortools.NewEvaluatorEvaluateTool(appSvc.Evaluator),
		)
		logging.Info("MCP server: self-improvement tools enabled")
	}

	return tools
}

func shutdownHTTPMCPServer(server *mesnadaServer.Server) {
	if server == nil {
		return
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}
