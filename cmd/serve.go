package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/api"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/logging"
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
  # Start with default configuration (port 8765)
  pando serve

  # Start on specific port
  pando serve --port 9000

  # Start bound to all interfaces (for remote access)
  pando serve --host 0.0.0.0

  # Start with debug logging
  pando serve --debug`,
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")
		debug, _ := cmd.Flags().GetBool("debug")

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

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cfg := api.ServerConfig{
			Host:    host,
			Port:    port,
			Version: version.Version,
			DB:      conn,
			CWD:     cwd,
		}

		server, err := api.NewServer(ctx, cfg)
		if err != nil {
			return fmt.Errorf("failed to create API server: %w", err)
		}

		go func() {
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
			<-sigChan
			logging.Info("Shutdown signal received")

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutdownCancel()

			if err := server.Shutdown(shutdownCtx); err != nil {
				logging.Error("Server shutdown error: %v", err)
			}
			cancel()
		}()

		addr := fmt.Sprintf("%s:%d", host, port)
		logging.Info("Pando API server starting on %s", addr)
		fmt.Printf("Pando API server v%s listening on http://%s\n", version.Version, addr)
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
}
