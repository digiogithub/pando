package cmd

import (
	"context"
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
	"github.com/digiogithub/pando/internal/convert"
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
  pando llm-proxy --debug

  # Also serve POST /v1/convert (documents to Markdown)
  pando llm-proxy --api-key mysecretkey --convert`,
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")
		debug, _ := cmd.Flags().GetBool("debug")
		apiKey, _ := cmd.Flags().GetString("api-key")
		convertHandler, err := llmProxyConvertHandler(cmd, host, apiKey)
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
			Host:    host,
			Port:    port,
			Debug:   debug,
			APIKey:  apiKey,
			Convert: convertHandler,
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

// llmProxyConvertHandler builds the /v1/convert handler from the --convert*
// flags, or returns nil when --convert is off. The endpoint converts
// untrusted uploads, so it refuses to start without an API key unless the
// proxy only listens on loopback.
func llmProxyConvertHandler(cmd *cobra.Command, host, apiKey string) (http.Handler, error) {
	enabled, _ := cmd.Flags().GetBool("convert")
	if !enabled {
		return nil, nil
	}
	if apiKey == "" && !isLoopbackHost(host) {
		return nil, fmt.Errorf("--convert on a non-loopback host (%s) requires --api-key", host)
	}
	maxBytes, _ := cmd.Flags().GetInt64("convert-max-bytes")
	maxOutput, _ := cmd.Flags().GetInt64("convert-max-output-bytes")
	timeout, _ := cmd.Flags().GetDuration("convert-timeout")
	concurrent, _ := cmd.Flags().GetInt("convert-max-concurrent")
	memLimit, _ := cmd.Flags().GetString("convert-memory-limit")
	inProcess, _ := cmd.Flags().GetBool("convert-in-process")

	var runner convert.Runner = convert.ProcessRunner{MemoryLimit: memLimit}
	if inProcess {
		runner = convert.InProcessRunner{Converter: convert.New()}
	}
	return convert.NewHTTPHandler(convert.HTTPOptions{
		MaxInputBytes:  maxBytes,
		MaxOutputBytes: maxOutput,
		Timeout:        timeout,
		MaxConcurrent:  concurrent,
		Runner:         runner,
	}), nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func init() {
	rootCmd.AddCommand(llmProxyCmd)
	llmProxyCmd.Flags().Bool("convert", false, "Serve POST /v1/convert: convert an uploaded document (PDF, Office, HTML, …) to Markdown")
	llmProxyCmd.Flags().Int64("convert-max-bytes", convert.DefaultMaxInputBytes, "Largest document /v1/convert accepts, in bytes")
	llmProxyCmd.Flags().Int64("convert-max-output-bytes", convert.DefaultMaxOutputBytes, "Largest Markdown /v1/convert returns, in bytes")
	llmProxyCmd.Flags().Duration("convert-timeout", convert.DefaultTimeout, "Time limit of one /v1/convert conversion, including the wait for a slot")
	llmProxyCmd.Flags().Int("convert-max-concurrent", convert.DefaultMaxConcurrent, "Conversions /v1/convert runs at once")
	llmProxyCmd.Flags().String("convert-memory-limit", "1GiB", "GOMEMLIMIT of each conversion child process (soft limit; empty for none)")
	llmProxyCmd.Flags().Bool("convert-in-process", false, "Convert inside the proxy process instead of a killable child process (trusted input only)")
	llmProxyCmd.Flags().String("host", "localhost", "Host to bind to")
	llmProxyCmd.Flags().Int("port", 11434, "Port to listen on")
	llmProxyCmd.Flags().Bool("debug", false, "Enable debug logging")
	llmProxyCmd.Flags().String("api-key", "", "Optional API key for proxy authentication")
}
