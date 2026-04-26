package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
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

The server exposes the requested Pando tool families together:
- fetch and web search tools
- browser / Chrome DevTools-style tools
- remembrances tools
- Mesnada orchestration tools
- cache and pagination tools`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMCPServerMode(cmd)
	},
}

func init() {
	rootCmd.AddCommand(mcpServerCmd)

	mcpServerCmd.Flags().Bool("debug", false, "Enable debug logging")
	mcpServerCmd.Flags().Bool("no-stdio", false, "Disable the stdio MCP transport")
	mcpServerCmd.Flags().Bool("no-http", false, "Disable the HTTP MCP transport")
}

func runMCPServerMode(cmd *cobra.Command) error {
	host, _ := cmd.Flags().GetString("host")
	port, _ := cmd.Flags().GetInt("port")
	debug, _ := cmd.Flags().GetBool("debug")
	noStdio, _ := cmd.Flags().GetBool("no-stdio")
	noHTTP, _ := cmd.Flags().GetBool("no-http")
	if !cmd.Flags().Changed("port") {
		port = 9777
	}

	if noStdio && noHTTP {
		return fmt.Errorf("at least one MCP transport must be enabled")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	if _, err := config.Load(cwd, debug, ""); err != nil {
		return err
	}
	enableMCPServerFeatures()

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

	toolList := buildMCPServerTools(pandoApp)
	if len(toolList) == 0 {
		return fmt.Errorf("no MCP tools available")
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
			if httpSrv != nil {
				shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
				defer shutdownCancel()
				_ = httpSrv.Shutdown(shutdownCtx)
			}
			return nil
		case err := <-errCh:
			cancel()
			if httpSrv != nil {
				shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
				defer shutdownCancel()
				_ = httpSrv.Shutdown(shutdownCtx)
			}
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
		if httpSrv != nil {
			shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
			defer shutdownCancel()
			_ = httpSrv.Shutdown(shutdownCtx)
		}
		return nil
	case err := <-errCh:
		cancel()
		if httpSrv != nil {
			shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
			defer shutdownCancel()
			_ = httpSrv.Shutdown(shutdownCtx)
		}
		return err
	}
}

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
}

func buildMCPServerTools(appSvc *app.App) []llmtools.BaseTool {
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

	return tools
}
