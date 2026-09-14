package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
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
- design / Design Studio tools (design_*, --design-tools)
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
	mcpServerCmd.Flags().Bool("print-token", false, "Print the stored MCP HTTP listener token (if any) to stdout and exit, without starting the server")

	// Tool group flags – when provided they override the config file.
	mcpServerCmd.Flags().Bool("file-tools", false, "Enable file read tools (view, glob, grep, ls)")
	mcpServerCmd.Flags().Bool("file-tools-write", false, "Also enable file write tools (write, edit, patch); implies --file-tools")
	mcpServerCmd.Flags().Bool("system-exec", false, "Enable bash/shell execution tool")
	mcpServerCmd.Flags().Bool("gateway-expose", false, "Re-export MCPGateway tools through this MCP server")
	mcpServerCmd.Flags().Bool("self-improvement", false, "Expose self-improvement evaluator tools")
	mcpServerCmd.Flags().Bool("design-tools", false, "Expose Design Studio tools (design_*)")
}

func runMCPServerMode(cmd *cobra.Command) error {
	if printToken, _ := cmd.Flags().GetBool("print-token"); printToken {
		// --print-token is consumed by scripts: its only output on success is
		// the token on stdout, so a failure must be one error line and not the
		// command's whole usage text dumped after it.
		cmd.SilenceUsage = true
		return printStoredListenerToken(mcpTokenKind)
	}

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

		configuredToken := ""
		if cfg != nil {
			configuredToken = cfg.MCPServer.HttpToken
		}
		httpToken, tokenGenerated, err := ensureMCPHTTPToken(host, configuredToken)
		if err != nil {
			return err
		}
		if configuredToken == "" {
			// The token came from the stored file (PANDO-US-0030), whether
			// just generated or read back from an earlier start: tell the
			// operator where to find it again. An explicitly configured
			// MCPServer.HttpToken never touches this file and never prints
			// here -- the operator already knows it, since they set it.
			if tokenPath, perr := listenerTokenFilePath(mcpTokenKind); perr == nil {
				fmt.Fprintf(os.Stderr, "MCP HTTP token file: %s\n", tokenPath)
			}
			if tokenGenerated {
				// Printed once to stderr, exactly like cmd/agui_serve.go's
				// token line, and never logged afterwards (see
				// MCPServer.HttpToken's doc in internal/config/config.go for
				// why the field name alone keeps it out of any config dump
				// too). A later start reads the same token back from the
				// file above instead of generating (and printing) a new one.
				fmt.Fprintf(os.Stderr, "Generated a new token and stored it there:\n%s\n", httpToken)
			}
		}

		var allowedOrigins []string
		if cfg != nil {
			allowedOrigins = cfg.MCPServer.HttpAllowedOrigins
		}
		if len(allowedOrigins) == 0 {
			logging.Warn("MCP HTTP transport has no MCPServer.HttpAllowedOrigins configured: all browser requests will be refused (non-browser clients are unaffected)")
		}

		addr := fmt.Sprintf("%s:%d", host, port)
		httpSrv = mesnadaServer.New(mesnadaServer.Config{
			Addr:           addr,
			Orchestrator:   pandoApp.MesnadaOrchestrator,
			Version:        version.Normalize(),
			UseStdio:       false,
			Remembrances:   pandoApp.Remembrances,
			PandoTools:     toolList,
			Token:          httpToken,
			AllowedOrigins: allowedOrigins,
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

// enableMCPServerFeatures derives subsystem activation from the explicit
// [MCPServer] exposure toggles. It deliberately does NOT force-enable any tool
// group: the MCP server mirrors the user's global configuration, so a subsystem
// left disabled (Mesnada/subagents, Remembrances KB+code, memory, browser,
// fetch, search engines, …) stays unexposed. app.New builds each subsystem only
// when its config flag is on, and buildMCPServerTools additionally gates
// API-key tools on a present key — nothing here overrides those decisions.
//
// The only subsystems turned on here are the ones a dedicated [MCPServer]
// feature proxies: gateway re-export and self-improvement. Those toggles are
// themselves opt-in, and without their backing subsystem the requested
// exposure would be empty.
func enableMCPServerFeatures() {
	cfg := config.Get()
	if cfg == nil {
		return
	}

	// Enable MCPGateway when gateway re-export is explicitly requested.
	if cfg.MCPServer.GatewayExpose.Enabled {
		cfg.MCPGateway.Enabled = true
	}

	// Enable the Evaluator when self-improvement exposure is explicitly requested.
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
	if cmd.Flags().Changed("design-tools") {
		v, _ := cmd.Flags().GetBool("design-tools")
		cfg.MCPServer.Design.Enabled = v
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
		// Desktop Controller (accessibility-tree based UI automation, internal/uiauto).
		// This is the ONLY place the desktop_* tools cross an MCP boundary: the
		// subsystem itself stays an internal tool provider (internal/uiauto +
		// internal/llm/tools/desktop_*.go, wired into the agent directly in
		// internal/llm/agent/tools.go), never routed through MCP internally.
		// External MCP clients get the same 12 tools an in-process Pando agent
		// would, gated on the exact same config flag, so a client sees this
		// group present/absent consistently with the agent's own tool list.
		if it.DesktopEnabled {
			tools = append(tools,
				llmtools.NewDesktopAppsTool(),
				llmtools.NewDesktopObserveTool(),
				llmtools.NewDesktopFindTool(),
				llmtools.NewDesktopReadTool(),
				llmtools.NewDesktopClickTool(appSvc.Permissions),
				llmtools.NewDesktopTypeTool(appSvc.Permissions),
				llmtools.NewDesktopKeyTool(appSvc.Permissions),
				llmtools.NewDesktopScrollTool(appSvc.Permissions),
				llmtools.NewDesktopFocusTool(appSvc.Permissions),
				llmtools.NewDesktopWaitTool(),
				llmtools.NewDesktopScreenshotTool(appSvc.Permissions),
				llmtools.NewDesktopClickAtTool(appSvc.Permissions),
			)
			logging.Info("MCP server: Desktop Controller tools enabled", "count", 12)
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
			llmtools.NewCodeImpactAnalysisTool(appSvc.Remembrances.Code),
			llmtools.NewCodeRelatedFilesTool(appSvc.Remembrances.Code),
		)

		// The wiki graph navigator only exists when the graph does: with
		// KBWikiLinks off it would answer empty on every call.
		if appSvc.Remembrances.KB != nil && appSvc.Remembrances.KB.WikiLinksEnabled() {
			tools = append(tools, llmtools.NewKBRelatedDocumentsTool(appSvc.Remembrances.KB))
		}

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

	if cfg != nil && cfg.MCPServer.Design.Enabled {
		tools = append(tools, agent.DesignTools(appSvc.Permissions)...)
		logging.Info("MCP server: design tools enabled")
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

// ensureMCPHTTPToken decides the bearer token the MCP HTTP transport will
// require, mirroring the token handling in cmd/agui_serve.go.
//
// A configured token is returned as-is. An empty token is only accepted when
// host resolves to loopback: the port is then reachable only from this
// machine, but that still includes any other local process or the user's own
// browser (see PANDO-US-0026 for the CORS half of that). Binding to a
// non-loopback interface with no configured token is refused outright —
// loopback is not a security boundary against the network, and there is
// nothing else standing between a remote caller and the tool surface (see
// cmd/mcp_server.go:124's SetGlobalAutoApprove(true)). This refusal is
// checked before ever looking at a stored token file (PANDO-US-0030): a
// token generated for a loopback bind must never silently become the
// credential for a network-facing one just because the file happens to
// exist.
//
// On a loopback bind with no configured token, a previously stored token is
// read back (see resolveStoredOrGeneratedListenerToken) so a client
// configured once keeps working across restarts; only on the very first
// start is a new token generated and persisted.
func ensureMCPHTTPToken(host, configuredToken string) (token string, generated bool, err error) {
	if configuredToken != "" {
		return configuredToken, false, nil
	}
	if !isLoopbackMCPHost(host) {
		return "", false, fmt.Errorf(
			"MCP HTTP transport is bound to non-loopback host %q with no MCPServer.HttpToken configured; "+
				"set MCPServer.HttpToken in .pando.toml (or bind --host to loopback) before starting", host)
	}
	return resolveStoredOrGeneratedListenerToken(mcpTokenKind)
}

// isLoopbackMCPHost reports whether host only accepts local connections. An
// empty host or a wildcard bind (0.0.0.0, ::) is reachable from the network
// and therefore not loopback.
func isLoopbackMCPHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func shutdownHTTPMCPServer(server *mesnadaServer.Server) {
	if server == nil {
		return
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}
