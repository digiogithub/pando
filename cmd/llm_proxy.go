package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/instanceregistry"
	"github.com/digiogithub/pando/internal/llmproxy"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var llmProxyCmd = &cobra.Command{
	Use:   "llm-proxy",
	Short: "Start Pando OpenAI-compatible LLM proxy server",
	Long: `Start an OpenAI-compatible HTTP proxy server that exposes all configured
Pando LLM providers through a single unified API endpoint.

Compatible with any OpenAI client (LiteLLM, Continue.dev, etc.).`,
	Example: `
  # Start with defaults (port 11434)
  pando llm-proxy

  # Start on specific port
  pando llm-proxy --port 8080

  # Require API key authentication
  pando llm-proxy --api-key mysecretkey

  # Debug mode
  pando llm-proxy --debug`,
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")
		debug, _ := cmd.Flags().GetBool("debug")
		apiKey, _ := cmd.Flags().GetString("api-key")

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get cwd: %w", err)
		}

		_, err = config.Load(cwd, debug, "")
		if err != nil {
			return err
		}

		instanceID := uuid.New().String()
		_ = instanceregistry.Announce(&instanceregistry.Entry{
			InstanceID: instanceID,
			Path:       cwd,
			PID:        os.Getpid(),
			StartedAt:  time.Now(),
			Mode:       instanceregistry.ModeProxy,
		})
		defer func() { _ = instanceregistry.Revoke(instanceID) }()

		cfg := llmproxy.ProxyConfig{
			Host:   host,
			Port:   port,
			Debug:  debug,
			APIKey: apiKey,
		}

		server := llmproxy.NewServer(cfg)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stopSignals()

		go func() {
			<-sigCtx.Done()
			logging.Info("Shutdown signal received")
			cancel()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				logging.Error("Proxy server shutdown error: %v", err)
			}
		}()

		// Watchdog: force exit if shutdown takes too long
		go func() {
			<-sigCtx.Done()
			time.Sleep(6 * time.Second)
			os.Exit(1)
		}()

		_ = ctx // used for future phases

		addr := fmt.Sprintf("%s:%d", host, port)
		fmt.Printf("Pando LLM Proxy listening on http://%s\n", addr)
		fmt.Println("Press Ctrl+C to stop")

		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("proxy server error: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(llmProxyCmd)
	llmProxyCmd.Flags().String("host", "localhost", "Host to bind to")
	llmProxyCmd.Flags().Int("port", 11434, "Port to listen on")
	llmProxyCmd.Flags().Bool("debug", false, "Enable debug logging")
	llmProxyCmd.Flags().String("api-key", "", "Optional API key for proxy authentication")
}
