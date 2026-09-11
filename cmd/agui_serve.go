package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/agui"
	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/instanceregistry"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/tlsutil"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// aguiServeCmd is the strongest of the three AG-UI deployment shapes: a process
// that serves the AG-UI protocol and nothing else. No Web-UI, no REST API, no
// static assets, no IPC bus — a browser origin that reaches this process can
// only drive the agents listed in --agent.
var aguiServeCmd = &cobra.Command{
	Use:   "agui-serve",
	Short: "Serve the AG-UI protocol (CopilotKit and other Generative-UI frontends)",
	Long: `Start a dedicated AG-UI protocol server.

AG-UI (https://docs.ag-ui.com) is the wire contract CopilotKit and other
Generative-UI frontends speak. This command exposes Pando agents over it in a
process of their own, which is the recommended shape for anything a browser
reaches: the Web-UI API, the session endpoints and the static UI are simply not
served here.

The bearer token is printed on startup unless --token or --no-token is given.`,
	Example: `
  # Serve the coder agent for a Next.js app running on localhost:3000
  pando agui-serve --port 8090 --allow-origin http://localhost:3000

  # Serve the coder agent with a persona injected into every run's prompt
  pando agui-serve --port 8090 --allow-origin http://localhost:3000 --persona perfumer

  # Serve a different project directory with a fixed token
  pando agui-serve --cwd /path/to/project --token "$PANDO_AGUI_TOKEN"

  # Plain HTTP (only sane behind a reverse proxy that terminates TLS)
  pando agui-serve --no-tls`,
	RunE: runAGUIServe,
}

func runAGUIServe(cmd *cobra.Command, _ []string) error {
	host, _ := cmd.Flags().GetString("host")
	port, _ := cmd.Flags().GetInt("port")
	cwdFlag, _ := cmd.Flags().GetString("cwd")
	debug, _ := cmd.Flags().GetBool("debug")
	origins, _ := cmd.Flags().GetStringArray("allow-origin")
	agents, _ := cmd.Flags().GetStringArray("agent")
	token, _ := cmd.Flags().GetString("token")
	noToken, _ := cmd.Flags().GetBool("no-token")
	noTLS, _ := cmd.Flags().GetBool("no-tls")
	tlsCert, _ := cmd.Flags().GetString("tls-cert")
	tlsKey, _ := cmd.Flags().GetString("tls-key")
	autoApprove, _ := cmd.Flags().GetBool("auto-approve")
	persona, _ := cmd.Flags().GetString("persona")

	cwd, err := resolveWorkingDir(cwdFlag)
	if err != nil {
		return err
	}
	if _, err := config.Load(cwd, debug, ""); err != nil {
		return err
	}

	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration was not loaded")
	}
	// This command IS the AG-UI surface, so the config gate is implied. Flags
	// win over the file, which only supplies the values the user did not pass.
	cfg.AGUI.Enabled = true
	cfg.AGUI.Port = port
	cfg.AGUI.Host = host
	if len(origins) > 0 {
		cfg.AGUI.AllowedOrigins = origins
	}
	if len(agents) > 0 {
		cfg.AGUI.Agents = agents
	}
	if persona != "" {
		cfg.AGUI.Persona = persona
	}
	if cmd.Flags().Changed("auto-approve") {
		cfg.AGUI.AutoApprove = autoApprove
	}
	cfg.AGUI.RequireToken = !noToken

	if !noToken && token == "" {
		token, err = randomToken()
		if err != nil {
			return err
		}
	}
	if noToken {
		logging.Warn("AG-UI server started without a token: any local process can drive the agent")
	}
	if len(cfg.AGUI.AllowedOrigins) == 0 {
		logging.Warn("AG-UI server has no allowed origins: browsers will be refused, only server-side clients can connect")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- IPC bootstrap (P4 of pando/plans/mcp_server_ipc_bootstrap.md): a
	// long-running peer of `pando serve`, so the same default policy, the same
	// role-aware App and the same shared wiring. ---
	instanceID := uuid.New().String()
	rt, err := ipcruntime.Bootstrap(ctx, cwd, instanceID)
	if err != nil {
		return fmt.Errorf("IPC bootstrap failed: %w", err)
	}
	conn := rt.SQLDB
	if conn == nil {
		// A secondary whose DB failed to open (Bootstrap logs why): app.New
		// would panic on the nil pool, so fail with the reason instead.
		rt.Cleanup()
		return fmt.Errorf("IPC bootstrap: no database connection (see the log for the cause)")
	}

	pandoApp, err := app.New(ctx, conn, app.AppOptions{
		StartupMode: "agui",
		DBQuerier:   rt.Querier,
		IPCRole:     rt.Role,
	})
	if err != nil {
		rt.Cleanup()
		return fmt.Errorf("failed to initialize app: %w", err)
	}
	unwireIPC := wireIPC(ctx, rt, pandoApp, instanceID, cwd, instanceregistry.ModeAGUI, wireOptions{})
	// Registered before the AG-UI runtime's own defer, so (LIFO) the listener
	// and runtime stop first, then the ordered IPC handover runs.
	defer shutdownEntrypointOrdered(pandoApp.Shutdown, unwireIPC, rt.Cleanup)

	if !noTLS && (tlsCert == "" || tlsKey == "") {
		dataDir := cfg.Data.Directory
		if dataDir == "" {
			dataDir = ".pando"
		}
		certPaths, certErr := tlsutil.EnsureCert(dataDir)
		if certErr != nil {
			return fmt.Errorf("failed to ensure TLS certificate: %w", certErr)
		}
		tlsCert, tlsKey = certPaths.CertFile, certPaths.KeyFile
	}
	if noTLS {
		tlsCert, tlsKey = "", ""
	}

	runtime, err := agui.New(agui.Deps{
		Sessions:     pandoApp.Sessions,
		Messages:     pandoApp.Messages,
		History:      pandoApp.History,
		Skills:       pandoApp.SkillManager,
		Gateway:      pandoApp.MCPGateway,
		Orchestrator: pandoApp.MesnadaOrchestrator,
		Remembrances: pandoApp.Remembrances,
		LSP:          pandoApp,
		DB:           conn,
		Token:        token,
	}, agui.ConfigFromApp(cfg.AGUI))
	if err != nil {
		return fmt.Errorf("failed to build the AG-UI adapter: %w", err)
	}
	defer runtime.Close()

	listener, err := runtime.StartListener(agui.ListenerOptions{
		Host:     host,
		CertFile: tlsCert,
		KeyFile:  tlsKey,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Pando AG-UI server listening on %s%s\n", listener.URL(), cfg.AGUI.Path)
	fmt.Printf("Project: %s\n", cwd)
	fmt.Printf("Agents:  %v\n", cfg.AGUI.Agents)
	if cfg.AGUI.Persona != "" {
		fmt.Printf("Persona: %s\n", cfg.AGUI.Persona)
	}
	if token != "" {
		fmt.Printf("Token:   %s\n", token)
	}
	if len(cfg.AGUI.AllowedOrigins) > 0 {
		fmt.Printf("Origins: %v\n", cfg.AGUI.AllowedOrigins)
	}
	fmt.Println("Press Ctrl+C to stop")

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	// Watchdog, as in `pando serve`: force-exit if the process has not
	// terminated within 6 seconds of the shutdown signal. The IPC handover
	// (drain, lock release, instance.shutdown) is the first thing
	// App.Shutdown does, so it has run by then; only slower teardown is cut.
	go func() {
		<-sigCtx.Done()
		time.Sleep(6 * time.Second)
		logging.Error("AG-UI server shutdown watchdog: forced exit after 6s")
		os.Exit(1)
	}()

	select {
	case <-sigCtx.Done():
		logging.Info("Shutdown signal received")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := listener.Shutdown(shutdownCtx); err != nil {
			logging.Debug("AG-UI listener shutdown", "error", err)
		}
		return nil
	case err := <-serveResult(listener):
		return err
	}
}

// serveResult adapts the listener's blocking Wait to a channel so the signal
// handler and a listener failure can be selected on together.
func serveResult(l *agui.Listener) <-chan error {
	out := make(chan error, 1)
	go func() { out <- l.Wait() }()
	return out
}

// resolveWorkingDir applies --cwd, mirroring what the MCP server command does:
// chdir first, then always re-derive the directory with os.Getwd so it is
// absolute (the IPC lock, ports and registry entry must not see a relative
// spelling).
func resolveWorkingDir(cwdFlag string) (string, error) {
	if cwdFlag != "" {
		if err := os.Chdir(cwdFlag); err != nil {
			return "", fmt.Errorf("failed to change directory to %q: %w", cwdFlag, err)
		}
	}
	return os.Getwd()
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate an AG-UI token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func init() {
	rootCmd.AddCommand(aguiServeCmd)

	aguiServeCmd.Flags().String("host", "localhost", "Host to bind to")
	aguiServeCmd.Flags().Int("port", 8090, "Port to listen on")
	aguiServeCmd.Flags().String("cwd", "", "Project directory to serve (defaults to the current one)")
	aguiServeCmd.Flags().Bool("debug", false, "Enable debug logging")
	aguiServeCmd.Flags().StringArray("allow-origin", nil, "Browser origin allowed to connect (repeatable)")
	aguiServeCmd.Flags().StringArray("agent", nil, "Agent exposed over AG-UI (repeatable, defaults to the configured list)")
	aguiServeCmd.Flags().String("persona", "", "Persona injected into every run's system prompt (per-session override; must be a loaded persona)")
	aguiServeCmd.Flags().String("token", "", "Bearer token clients must present (generated when omitted)")
	aguiServeCmd.Flags().Bool("no-token", false, "Disable bearer-token authentication")
	aguiServeCmd.Flags().Bool("no-tls", false, "Serve plain HTTP instead of TLS")
	aguiServeCmd.Flags().String("tls-cert", "", "Path to a TLS certificate file (auto-generated if omitted)")
	aguiServeCmd.Flags().String("tls-key", "", "Path to a TLS private key file (auto-generated if omitted)")
	aguiServeCmd.Flags().Bool("auto-approve", false, "Approve tool permissions without asking the client")
}
