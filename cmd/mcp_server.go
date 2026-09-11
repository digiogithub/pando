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
	"github.com/digiogithub/pando/internal/instanceregistry"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/evaluatortools"
	llmtools "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	mesnadaServer "github.com/digiogithub/pando/internal/mesnada/server"
	"github.com/digiogithub/pando/internal/version"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// mcpServerProbeTimeout bounds how long a freshly started `pando mcp-server`
// secondary waits for an existing primary to answer ipc.ping before treating
// it as unresponsive (see ipcruntime.Options.ProbeTimeout). Shorter than
// ipcruntime.DefaultOptions' 10s because mcp-server is spawned interactively
// by an editor/agent, which should not stall long on a slow probe.
const mcpServerProbeTimeout = 3 * time.Second

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

	// Tool group flags – when provided they override the config file.
	mcpServerCmd.Flags().Bool("file-tools", false, "Enable file read tools (view, glob, grep, ls)")
	mcpServerCmd.Flags().Bool("file-tools-write", false, "Also enable file write tools (write, edit, patch); implies --file-tools")
	mcpServerCmd.Flags().Bool("system-exec", false, "Enable bash/shell execution tool")
	mcpServerCmd.Flags().Bool("gateway-expose", false, "Re-export MCPGateway tools through this MCP server")
	mcpServerCmd.Flags().Bool("self-improvement", false, "Expose self-improvement evaluator tools")
	mcpServerCmd.Flags().Bool("design-tools", false, "Expose Design Studio tools (design_*)")
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

	// Chdir first (if requested), then always resolve cwd via os.Getwd() so it
	// is absolute regardless of whether --cwd was relative. ipcruntime.Bootstrap
	// canonicalises it again internally (Abs + EvalSymlinks), but config.Load
	// below and the instance registry entry should already see the real path.
	if cwdFlag != "" {
		if err := os.Chdir(cwdFlag); err != nil {
			return fmt.Errorf("failed to change directory to %q: %w", cwdFlag, err)
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	if _, err := config.Load(cwd, debug, ""); err != nil {
		return err
	}
	enableMCPServerFeatures()

	// Apply CLI flag overrides for tool groups on top of the config defaults.
	applyMCPServerFlagOverrides(cmd)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, pandoApp, unwireIPC, err := bootstrapMCPServer(ctx, cwd)
	if err != nil {
		return err
	}

	// httpSrv/stdioSrv are assigned further down, only for the transports that
	// end up enabled; the deferred shutdown below reads them by reference (via
	// the closure) at return time, so it always sees whichever of the two (or
	// neither, on an early error before either is created) actually started.
	var httpSrv, stdioSrv *mesnadaServer.Server
	defer func() {
		// Ordered shutdown (P2 of pando/plans/mcp_server_ipc_bootstrap.md §5.5):
		// stop accepting new MCP requests, then hand over the IPC primary role
		// (pandoApp.Shutdown's releasePrimaryRole: drain, release the lock,
		// announce instance.shutdown, close the bus), then drop this instance
		// from the registry, then release the runtime's own resources (DB,
		// watcher; ReleaseLock again is a no-op by then). All four steps are
		// individually idempotent, and this defer is the only place that calls
		// them, so each runs exactly once no matter which return path got here.
		shutdownMCPServerOrdered(
			func() { stopMCPTransports(httpSrv, stdioSrv) },
			pandoApp.Shutdown,
			unwireIPC,
			rt.Cleanup,
		)
	}()
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

	// httpErrCh/stdioDoneCh: each transport that is enabled runs in its own
	// goroutine and reports back on its own channel. A disabled transport's
	// channel is simply never written to, so the select below only ever fires
	// on the transports actually running plus the signal context.
	httpErrCh := make(chan error, 1)
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
				httpErrCh <- err
			}
		}()
		fmt.Fprintf(os.Stderr, "Pando MCP HTTP transport listening on http://%s/mcp\n", addr)
	}

	// stdioDoneCh receives exactly once when the stdio transport stops, either
	// with nil (the client closed stdin — EOF, a graceful signal to shut down)
	// or a real read/encode error. Running it in a goroutine even when it is
	// the only enabled transport (--no-http) is what lets the select below
	// react to SIGINT/SIGTERM: os.Stdin's blocking Scan() cannot itself observe
	// ctx cancellation, so previously (no goroutine, a bare `return
	// stdioSrv.Start()`) a signal had no handler installed and killed the
	// process outright, skipping every deferred cleanup above.
	stdioDoneCh := make(chan error, 1)
	if !noStdio {
		stdioSrv = mesnadaServer.New(mesnadaServer.Config{
			Orchestrator: pandoApp.MesnadaOrchestrator,
			Version:      version.Normalize(),
			UseStdio:     true,
			Remembrances: pandoApp.Remembrances,
			PandoTools:   toolList,
		})
		go func() {
			stdioDoneCh <- stdioSrv.Start()
		}()
	}

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	return waitForMCPServerShutdown(sigCtx, stdioDoneCh, httpErrCh)
}

// waitForMCPServerShutdown blocks until one of three shutdown triggers fires
// and returns the error runMCPServerMode should itself return (nil for a
// graceful stop):
//
//   - sigCtx.Done(): SIGINT/SIGTERM.
//   - stdioDoneCh: the stdio transport stopped on its own. nil means the
//     client closed stdin (EOF) — a graceful signal to shut down, same as a
//     signal. A non-nil error is a real read/encode failure and is returned.
//   - httpErrCh: the HTTP transport failed to serve and is returned.
//
// A disabled transport's channel is never written to; passing one of those
// (buffered, empty, nothing left to send) is fine since an empty channel a
// select is not ready to read from simply never fires that case.
//
// Extracted from runMCPServerMode so the shutdown-trigger logic — in
// particular that a graceful stdin EOF and a graceful signal both return nil,
// while a genuine stdio error or HTTP failure is propagated — is unit
// testable without cobra flags, a real IPC bootstrap, or a real MCP
// transport. See TestWaitForMCPServerShutdown_* in mcp_server_ipc_test.go.
func waitForMCPServerShutdown(sigCtx context.Context, stdioDoneCh, httpErrCh <-chan error) error {
	select {
	case <-sigCtx.Done():
		logging.Info("mcp-server: shutdown signal received")
		return nil
	case err := <-stdioDoneCh:
		if err != nil {
			logging.Warn("mcp-server: stdio transport ended with an error", "error", err)
			return err
		}
		logging.Info("mcp-server: stdio transport closed (stdin EOF)")
		return nil
	case err := <-httpErrCh:
		logging.Error("mcp-server: HTTP transport failed", "error", err)
		return err
	}
}

// bootstrapMCPServer performs the IPC-aware startup for `pando mcp-server`
// (P2 of pando/plans/mcp_server_ipc_bootstrap.md, §5.3): it joins the shared
// primary/secondary bootstrap instead of calling db.Connect() directly, builds
// the App against the resulting querier, and wires the shared IPC handlers
// (registry, write coordinator, changepub, bridge, failover watcher).
//
// mcp-server never kills an unresponsive primary (AllowKillStalePrimary:
// false): it is an ephemeral, low-trust process spawned by an editor/agent,
// and must never SIGKILL a user's long-running TUI/desktop/serve instance
// just because that instance is slow, under a debugger, or SIGSTOPped to
// answer one liveness probe (see ipcruntime.Options's doc comment). It also
// never accepts peer delegations (AcceptDelegations: false): it dies with the
// client that spawned it, so it is not a stable target for another instance
// to route work to.
//
// Extracted from runMCPServerMode so a regression test can drive exactly this
// bootstrap+wiring sequence with os.Stdout redirected to a pipe and assert
// nothing was written to it — see cmd/mcp_server_ipc_test.go. (That test
// exercises ipcruntime.BootstrapWithOptions and wireIPC directly against a
// minimal test App rather than calling this function, mirroring
// cmd/ipc_wiring_test.go's bareAppForIPCTest: a real app.New pulls in LLM
// providers, LSP and the MCP gateway, which is deliberately never exercised
// by this package's tests. The two code paths share the exact same
// BootstrapWithOptions/wireIPC arguments, so the source-shape test in
// cmd/mcp_server_ipc_test.go also asserts this function calls them with
// mcp-server's specific options.)
func bootstrapMCPServer(ctx context.Context, cwd string) (rt *ipcruntime.BootstrapResult, pandoApp *app.App, unwireIPC func(), err error) {
	instanceID := uuid.New().String()

	rt, err = ipcruntime.BootstrapWithOptions(ctx, cwd, instanceID, ipcruntime.Options{
		ProbeTimeout:          mcpServerProbeTimeout,
		AllowKillStalePrimary: false,
	})
	if err != nil {
		logging.Error("mcp-server: IPC bootstrap failed", "error", err)
		return nil, nil, nil, fmt.Errorf("IPC bootstrap failed: %w", err)
	}

	pandoApp, err = app.New(ctx, rt.SQLDB, app.AppOptions{
		SkipLSP:           true,
		SkipMesnadaServer: true,
		StartupMode:       "mcp",
		DBQuerier:         rt.Querier,
		IPCRole:           rt.Role,
	})
	if err != nil {
		rt.Cleanup()
		return nil, nil, nil, fmt.Errorf("failed to initialize app: %w", err)
	}

	acceptDelegations := false
	unwireIPC = wireIPC(ctx, rt, pandoApp, instanceID, cwd, instanceregistry.ModeMCP, wireOptions{
		AcceptDelegations: &acceptDelegations,
	})

	return rt, pandoApp, unwireIPC, nil
}

// shutdownMCPServerOrdered runs the four P2 shutdown steps in the order the
// plan requires: stop accepting new MCP requests, then hand over the IPC
// primary role, then drop this instance from the registry, then release the
// bootstrap runtime's own resources. Factored into a plain function of
// closures (rather than relying on defer's LIFO order across several
// statements) so the order itself — not just each individual step — is
// covered directly by TestShutdownMCPServerOrdered without needing real
// IPC/DB resources.
func shutdownMCPServerOrdered(stopTransports, shutdownApp, unwireIPC, cleanupRuntime func()) {
	stopTransports()
	shutdownApp()
	unwireIPC()
	cleanupRuntime()
}

// stopMCPTransports gracefully stops whichever MCP transports are non-nil.
// The HTTP transport's Shutdown drains in-flight requests and closes its
// listener; the stdio transport's Shutdown only releases per-session
// resources (llmtools.RegisterSessionCache et al. via cleanupSessions) — it
// has no way to interrupt a blocked read from os.Stdin, so on the
// signal-triggered shutdown path that goroutine is simply abandoned when the
// process exits after this function's caller returns.
func stopMCPTransports(httpSrv, stdioSrv *mesnadaServer.Server) {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if httpSrv != nil {
		_ = httpSrv.Shutdown(shutdownCtx)
	}
	if stdioSrv != nil {
		_ = stdioSrv.Shutdown(shutdownCtx)
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
