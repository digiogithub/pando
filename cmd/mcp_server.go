package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
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
- remembrances tools
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
			Version:      version.Version,
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
		Version:      version.Version,
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
// server tool set (fetch, search, browser, mesnada, remembrances).
func enableMCPServerFeatures() {
	cfg := config.Get()
	if cfg == nil {
		return
	}

	cfg.Mesnada.Enabled = true
	cfg.Remembrances.Enabled = true
	cfg.InternalTools.FetchEnabled = true
	cfg.InternalTools.GoogleSearchEnabled = true
	cfg.InternalTools.BraveSearchEnabled = true
	cfg.InternalTools.PerplexitySearchEnabled = true
	cfg.InternalTools.ExaSearchEnabled = true
	cfg.InternalTools.BrowserEnabled = true

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
		llmtools.NewFetchTool(appSvc.Permissions),
		llmtools.NewGoogleSearchTool(appSvc.Permissions),
		llmtools.NewBraveSearchTool(appSvc.Permissions),
		llmtools.NewPerplexitySearchTool(appSvc.Permissions),
		llmtools.NewExaSearchTool(appSvc.Permissions),
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
		llmtools.NewCacheReadTool(),
		llmtools.NewCacheStatsTool(),
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
		)
	}

	// --- Conditional tool groups ---

	if cfg != nil && cfg.MCPServer.FileTools.Enabled {
		tools = append(tools,
			llmtools.NewViewTool(appSvc.LSPClients),
			llmtools.NewGlobTool(),
			llmtools.NewGrepTool(),
			llmtools.NewLsTool(),
		)
		if cfg.MCPServer.FileTools.AllowWrite {
			tools = append(tools,
				llmtools.NewWriteTool(appSvc.LSPClients, appSvc.Permissions, appSvc.History),
				llmtools.NewEditTool(appSvc.LSPClients, appSvc.Permissions, appSvc.History),
				llmtools.NewPatchTool(appSvc.LSPClients, appSvc.Permissions, appSvc.History),
			)
		}
		logging.Info("MCP server: file tools enabled", "allow_write", cfg.MCPServer.FileTools.AllowWrite)
	}

	if cfg != nil && cfg.MCPServer.SystemExecution.Enabled {
		tools = append(tools, llmtools.NewBashTool(appSvc.Permissions))
		logging.Info("MCP server: system execution tools enabled")
	}

	if cfg != nil && cfg.MCPServer.GatewayExpose.Enabled && appSvc.MCPGateway != nil {
		gatewayTools := agent.GetMcpToolsWithGateway(ctx, appSvc.Permissions, appSvc.MCPGateway)
		tools = append(tools, gatewayTools...)
		logging.Info("MCP server: gateway tools exposed", "count", len(gatewayTools))
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
