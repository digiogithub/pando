package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/api"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/instanceregistry"
	"github.com/digiogithub/pando/internal/ipc/bridge"
	"github.com/digiogithub/pando/internal/ipc/changepub"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	"github.com/digiogithub/pando/internal/ipc/writecoordinator"
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

		selectedPort, err := chooseAvailablePort(host, preferredPort)
		if err != nil {
			return err
		}
		if selectedPort != preferredPort {
			logging.Warn("Preferred port unavailable, using fallback", "preferred", preferredPort, "selected", selectedPort)
			fmt.Printf("Port %d in use, switching to %d\n", preferredPort, selectedPort)
		}
		port = selectedPort

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current working directory: %v", err)
		}

		_, err = config.Load(cwd, debug, "")
		if err != nil {
			return err
		}
		logging.Debug("Config loaded", "workingDir", cwd)

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

		// Resolve TLS certificate: use provided files or auto-generate.
		if tlsCert == "" || tlsKey == "" {
			dataDir := config.Get().Data.Directory
			if dataDir == "" {
				dataDir = ".pando"
			}
			certPaths, err := tlsutil.EnsureCert(dataDir)
			if err != nil {
				return fmt.Errorf("failed to ensure TLS certificate: %w", err)
			}
			tlsCert = certPaths.CertFile
			tlsKey = certPaths.KeyFile
			logging.Debug("Using auto-generated TLS certificate", "cert", tlsCert)
		}

		scheme := "https"
		baseURL := fmt.Sprintf("%s://%s:%d", scheme, host, port)
		server, err := api.NewServer(ctx, api.ServerConfig{
			Host:        host,
			Port:        port,
			Version:     version.Version,
			DB:          conn,
			Querier:     rt.Querier,
			CWD:         cwd,
			UIBaseURL:   baseURL,
			TLSCertFile: tlsCert,
			TLSKeyFile:  tlsKey,
			InstanceID:  instanceID,
			Role:        string(rt.Role),
			PubPort:     rt.PubPort,
			RPCPort:     rt.RPCPort,
		})
		if err != nil {
			return fmt.Errorf("failed to create API server: %w", err)
		}

		_ = instanceregistry.Announce(&instanceregistry.Entry{
			InstanceID: instanceID,
			Path:       cwd,
			PID:        os.Getpid(),
			PubPort:    rt.PubPort,
			RPCPort:    rt.RPCPort,
			StartedAt:  time.Now(),
			Mode:       instanceregistry.ModeWebUI,
			IsPrimary:  rt.Role == ipcruntime.RolePrimary,
		})
		defer func() { _ = instanceregistry.Revoke(instanceID) }()

		// Start IPC bus and register handlers only on the primary instance.
		if rt.Role == ipcruntime.RolePrimary {
			pandoApp := server.PandoApp()
			serveBus := rt.Bus
			serveCoord := writecoordinator.New(ctx, rt.Querier, 256)
			defer serveCoord.Shutdown()
			servePub := changepub.NewBusPublisher(serveBus.Publish, instanceID, cwd)
			serveCoord.SetPublisher(servePub)
			dbproxy.RegisterHandlersWithCoordinator(serveBus, serveCoord)
			bridge.RegisterHandlers(serveBus, instanceID, pandoApp.Sessions, pandoApp.Messages, time.Now())
			if busErr := serveBus.Start(ctx, rt.PubPort, rt.RPCPort); busErr != nil {
				logging.Warn("IPC: serve mode failed to start bus", "error", busErr)
			} else {
				serveBridge := bridge.New(serveBus, pandoApp.Sessions, pandoApp.CoderAgent)
				serveBridge.Start(ctx)
				logging.Debug("IPC: serve mode announced", "instanceID", instanceID, "pubPort", rt.PubPort, "rpcPort", rt.RPCPort)
			}
		}

		sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stopSignals()

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
		if !strings.HasPrefix(version.Version, "v") {
			versionPrefix = "v"
		}
		fmt.Printf("Pando API server %s%s listening on %s\n", versionPrefix, version.Version, baseURL)
		if server.IsTLS() {
			fmt.Println("TLS enabled (self-signed certificate — accept the browser security warning for local use)")
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
}
