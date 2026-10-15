package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/app"
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

Compatible with any OpenAI client (LiteLLM, Continue.dev, etc.).

API key authentication is optional. The key is taken from --api-key, else from
the file given by --api-key-file, else from the PANDO_LLM_PROXY_API_KEY
environment variable (precedence: flag > file > env). --api-key and
--api-key-file together are an error. Prefer the file or the environment
variable: a key passed with --api-key is visible in the process arguments
(ps, /proc/<pid>/cmdline).`,
	Example: `
  # Start with defaults (port 11434)
  pando llm-proxy

  # Start on specific port
  pando llm-proxy --port 8080

  # Require API key authentication
  pando llm-proxy --api-key mysecretkey

  # Keep the key out of the process arguments
  pando llm-proxy --api-key-file /run/secrets/proxy_key
  PANDO_LLM_PROXY_API_KEY=mysecretkey pando llm-proxy

  # Debug mode
  pando llm-proxy --debug`,
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")
		debug, _ := cmd.Flags().GetBool("debug")
		apiKeyFlag, _ := cmd.Flags().GetString("api-key")
		apiKeyFile, _ := cmd.Flags().GetString("api-key-file")
		apiKey, err := llmproxy.ResolveAPIKey(apiKeyFlag, apiKeyFile, nil)
		if err != nil {
			return err
		}

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

		// Refresh dynamic models from configured providers and keep them fresh every 24 h.
		go app.StartModelRefreshLoop(ctx)

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
	llmProxyCmd.Flags().String("api-key", "", "Optional API key for proxy authentication (visible in the process arguments; prefer --api-key-file)")
	llmProxyCmd.Flags().String("api-key-file", "", "Read the API key from this file (trailing newline trimmed); precedence: --api-key > --api-key-file > "+llmproxy.APIKeyEnv)
}
