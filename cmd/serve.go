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
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/tlsutil"
	"github.com/digiogithub/pando/internal/version"
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

		conn, err := db.Connect()
		if err != nil {
			return err
		}
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

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		scheme := "https"
		baseURL := fmt.Sprintf("%s://%s:%d", scheme, host, port)
		cfg := api.ServerConfig{
			Host:        host,
			Port:        port,
			Version:     version.Version,
			DB:          conn,
			CWD:         cwd,
			UIBaseURL:   baseURL,
			TLSCertFile: tlsCert,
			TLSKeyFile:  tlsKey,
		}

		server, err := api.NewServer(ctx, cfg)
		if err != nil {
			return fmt.Errorf("failed to create API server: %w", err)
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
