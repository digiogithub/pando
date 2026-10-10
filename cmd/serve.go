package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/api"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/instanceregistry"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/tlsutil"
	"github.com/digiogithub/pando/internal/version"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start Pando HTTP API server",
	Long: `Start the Pando HTTP API server for WebUI integration.

The server provides REST endpoints and SSE streaming for:
- Project context and file management
- Session/chat history
- LLM agent interaction with streaming responses
- MCP tools discovery

This is the backend for the Pando Desktop/Web UI.`,
	Example: `
  # Start with default configuration (port 8765, auto-generated TLS certificate)
  pando serve

  # Start on specific port
  pando serve --port 9000

  # Start bound to all interfaces (for remote access)
  pando serve --host 0.0.0.0

  # Use a custom TLS certificate and key
  pando serve --tls-cert /path/to/server.crt --tls-key /path/to/server.key

  # Start with debug logging
  pando serve --debug`,
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")
		debug, _ := cmd.Flags().GetBool("debug")
		tlsCert, _ := cmd.Flags().GetString("tls-cert")
		tlsKey, _ := cmd.Flags().GetString("tls-key")
		ageKeys, _ := cmd.Flags().GetString("age-keys")
		config.SetAgeKeysOverride(ageKeys)
		preferredPort := port

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current working directory: %v", err)
		}
		startup := resolveStartupContext(cwd, "serve")

		if startup.Mode == startupModeProjectChild {
			// The parent probes and proxies exactly this port: a silent fallback
			// would leave it talking to whatever else owns the requested one.
			if len(startup.APIToken) < minChildAPITokenLen {
				return fmt.Errorf("project child requires a parent-minted API token (%s)", childAPITokenEnv)
			}
			ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(preferredPort)))
			if err != nil {
				return fmt.Errorf("project child cannot bind requested port %d: %w", preferredPort, err)
			}
			_ = ln.Close()
		} else {
			selectedPort, err := chooseAvailablePort(host, preferredPort)
			if err != nil {
				return err
			}
			if selectedPort != preferredPort {
				logging.Warn("Preferred port unavailable, using fallback", "preferred", preferredPort, "selected", selectedPort)
				fmt.Printf("Port %d in use, switching to %d\n", preferredPort, selectedPort)
			}
			port = selectedPort
		}

		_, err = config.Load(cwd, debug, "")
		if err != nil {
			return err
		}
		logging.Debug("Config loaded", "workingDir", cwd)

		// --agui-port turns the AG-UI adapter on and moves it to its own listener,
		// so a browser origin allowed to reach it cannot reach this API server.
		if aguiPort, _ := cmd.Flags().GetInt("agui-port"); aguiPort > 0 {
			if cfg := config.Get(); cfg != nil {
				cfg.AGUI.Enabled = true
				cfg.AGUI.Port = aguiPort
				if aguiHost, _ := cmd.Flags().GetString("agui-host"); aguiHost != "" {
					cfg.AGUI.Host = aguiHost
				}
				logging.Info("AG-UI adapter enabled by flag", "port", aguiPort)
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// --- IPC bootstrap: determine primary/secondary role, open DB, wire services ---
		instanceID := uuid.New().String()
		rt, err := ipcruntime.Bootstrap(ctx, cwd, instanceID)
		if err != nil {
			return fmt.Errorf("IPC bootstrap failed: %w", err)
		}
		defer rt.Cleanup()

		conn := rt.SQLDB
		logging.Debug("Database connected")

		dataDir := config.Get().Data.Directory
		if dataDir == "" {
			dataDir = ".pando"
		}

		// Resolve TLS certificate: use provided files or auto-generate.
		tlsCAFile := ""
		if tlsCert == "" || tlsKey == "" {
			certPaths, err := tlsutil.EnsureCert(config.TLSCertDir(dataDir))
			if err != nil {
				return fmt.Errorf("failed to ensure TLS certificate: %w", err)
			}
			tlsCert = certPaths.CertFile
			tlsKey = certPaths.KeyFile
			tlsCAFile = certPaths.CAFile
			logging.Debug("Using auto-generated TLS certificate", "cert", tlsCert)
		}

		// A project child is shown inside the parent's WebUI through the parent's
		// reverse proxy, so unlike a plain API server it serves the embedded UI.
		var staticFS fs.FS
		if startup.Mode == startupModeProjectChild {
			staticFS, err = api.EmbeddedWebUI()
			if err != nil {
				return fmt.Errorf("failed to load embedded web ui: %w", err)
			}
		}

		scheme := "https"
		baseURL := fmt.Sprintf("%s://%s:%d", scheme, host, port)
		server, err := api.NewServer(ctx, api.ServerConfig{
			StaticFS:            staticFS,
			Host:                host,
			Port:                port,
			Version:             version.Normalize(),
			DB:                  conn,
			Querier:             rt.Querier,
			CWD:                 cwd,
			UIBaseURL:           baseURL,
			TLSCertFile:         tlsCert,
			TLSKeyFile:          tlsKey,
			ParentInstanceID:    startup.ParentInstanceID,
			ProjectID:           startup.ProjectID,
			ProjectName:         startup.ProjectName,
			PublicBasePath:      startup.PublicBasePath,
			APIToken:            startup.APIToken,
			WebChildTLSCertFile: tlsCert,
			WebChildTLSKeyFile:  tlsKey,
			WebChildTLSDataDir:  config.TLSCertDir(dataDir),
			InstanceID:          instanceID,
			Role:                string(rt.Role),
			PubPort:             rt.PubPort,
			RPCPort:             rt.RPCPort,
			StartupMode:         startup.Mode,
		})
		if err != nil {
			return fmt.Errorf("failed to create API server: %w", err)
		}

		_ = instanceregistry.Announce(&instanceregistry.Entry{
			InstanceID:       instanceID,
			Path:             cwd,
			PID:              os.Getpid(),
			PubPort:          rt.PubPort,
			RPCPort:          rt.RPCPort,
			WebPort:          port,
			StartedAt:        time.Now(),
			Mode:             instanceregistry.ModeWebUI,
			ParentInstanceID: startup.ParentInstanceID,
			IsPrimary:        rt.Role == ipcruntime.RolePrimary,
		})
		defer func() { _ = instanceregistry.Revoke(instanceID) }()

		wireIPCRole(ctx, rt, server.PandoApp(), instanceID, cwd, "serve")

		shutdownBase, requestShutdown := context.WithCancel(context.Background())
		defer requestShutdown()
		sigCtx, stopSignals := signal.NotifyContext(shutdownBase, syscall.SIGINT, syscall.SIGTERM)
		defer stopSignals()

		// A project child never outlives its parent: the parent keeps the only
		// copy of the child's API token, so an orphan would be unreachable.
		if startup.Mode == startupModeProjectChild && startup.ParentPID > 0 && runtime.GOOS != "windows" {
			go watchParentProcess(sigCtx, startup.ParentPID, parentWatchInterval, func() {
				logging.Warn("Parent process is gone, shutting down project child", "parentPid", startup.ParentPID)
				requestShutdown()
			})
		}

		// Watchdog: unconditionally force-exit if the process has not terminated
		// within 6 seconds of receiving the shutdown signal.
		go func() {
			<-sigCtx.Done()
			time.Sleep(6 * time.Second)
			logging.Error("Server shutdown watchdog: forced exit after 6s")
			os.Exit(1)
		}()

		go func() {
			<-sigCtx.Done()
			logging.Info("Shutdown signal received")
			cancel()

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()

			if err := server.Shutdown(shutdownCtx); err != nil {
				logging.Error("Server shutdown error: %v", err)
			}
		}()

		addr := fmt.Sprintf("%s:%d", host, port)
		logging.Info("Pando API server starting on %s", addr)

		versionPrefix := ""
		if !strings.HasPrefix(version.Normalize(), "v") {
			versionPrefix = "v"
		}
		fmt.Printf("Pando API server %s%s listening on %s\n", versionPrefix, version.Normalize(), baseURL)
		if startup.Mode == startupModeProjectChild {
			projectName := startup.ProjectName
			if projectName == "" {
				projectName = cwd
			}
			fmt.Printf("Running as project child: %s (public base %s)\n", projectName, startup.PublicBasePath)
		}
		if server.IsTLS() {
			if tlsCAFile != "" {
				fmt.Printf("TLS enabled — import %s as a trusted certificate authority to avoid the browser security warning\n", tlsCAFile)
			} else {
				fmt.Println("TLS enabled")
			}
		}
		fmt.Println("Press Ctrl+C to stop")

		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}

		logging.Info("Server stopped")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().String("host", "localhost", "Host to bind to")
	serveCmd.Flags().Int("port", 8765, "Port to listen on")
	serveCmd.Flags().Bool("debug", false, "Enable debug logging")
	serveCmd.Flags().String("tls-cert", "", "Path to TLS certificate file (auto-generated if omitted)")
	serveCmd.Flags().String("tls-key", "", "Path to TLS private key file (auto-generated if omitted)")
	serveCmd.Flags().Int("agui-port", 0, "Serve the AG-UI protocol (CopilotKit) on its own port")
	serveCmd.Flags().String("agui-host", "", "Host for the AG-UI listener (defaults to localhost)")
}

// parentWatchInterval is how often a project child checks that its parent is
// still running.
const parentWatchInterval = 3 * time.Second

// watchParentProcess calls onGone once the process pid no longer exists, or
// returns when ctx ends.
func watchParentProcess(ctx context.Context, pid int, interval time.Duration, onGone func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !processExists(pid) {
				onGone()
				return
			}
		}
	}
}

func processExists(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
