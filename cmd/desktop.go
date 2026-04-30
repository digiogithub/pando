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
	"github.com/digiogithub/pando/internal/desktop"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/version"
	"github.com/spf13/cobra"
)

var desktopCmd = &cobra.Command{
	Use:   "desktop",
	Short: "Launch Pando as a desktop app (WebView window)",
	Long: `Start Pando in desktop mode: starts the API server on a local port and
opens the web UI inside a native WebView window instead of the system browser.

The desktop window includes a system menu for:
  - Showing or hiding the window
  - Toggling between Simple and Advanced UI modes

Requires the pando-desktop binary to be embedded (built with 'make desktop-embed').`,
	Example: `
  # Launch desktop app on default port
  pando desktop

  # Launch on a specific port
  pando desktop --port 9000

  # Start in simple mode
  pando desktop --simple`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDesktopMode(cmd)
	},
}

func init() {
	rootCmd.AddCommand(desktopCmd)
	desktopCmd.Flags().String("host", "localhost", "Host to bind the API server to")
	desktopCmd.Flags().Int("port", 8765, "Preferred port for the API server")
	desktopCmd.Flags().Bool("simple", false, "Start the desktop app in simple mode")
	desktopCmd.Flags().Bool("debug", false, "Enable debug logging")
}

func runDesktopMode(cmd *cobra.Command) error {
	host, _ := cmd.Flags().GetString("host")
	port, _ := cmd.Flags().GetInt("port")
	debug, _ := cmd.Flags().GetBool("debug")
	simpleMode, _ := cmd.Flags().GetBool("simple")

	selectedPort, err := chooseAvailablePort(host, port)
	if err != nil {
		return err
	}
	if selectedPort != port {
		logging.Warn("Preferred port %d unavailable, using %d", port, selectedPort)
	}
	port = selectedPort

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	_, err = config.Load(cwd, debug, "")
	if err != nil {
		return err
	}

	conn, err := db.Connect()
	if err != nil {
		return err
	}

	staticFS, err := api.EmbeddedWebUI()
	if err != nil {
		return fmt.Errorf("failed to load embedded web UI: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	baseURL := fmt.Sprintf("http://%s:%d", host, port)
	server, err := api.NewServer(ctx, api.ServerConfig{
		Host:      host,
		Port:      port,
		Version:   version.Version,
		DB:        conn,
		CWD:       cwd,
		StaticFS:  staticFS,
		OpenUI:    false,
		UIBaseURL: baseURL,
	})
	if err != nil {
		return fmt.Errorf("failed to create API server: %w", err)
	}

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	go func() {
		<-sigCtx.Done()
		time.Sleep(6 * time.Second)
		logging.Error("Desktop shutdown watchdog: forced exit after 6s")
		os.Exit(1)
	}()

	go func() {
		<-sigCtx.Done()
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logging.Error("API server shutdown error: %v", err)
		}
	}()

	versionPrefix := ""
	if !strings.HasPrefix(version.Version, "v") {
		versionPrefix = "v"
	}
	fmt.Printf("Pando desktop %s%s — API on %s\n", versionPrefix, version.Version, baseURL)

	serverReady := make(chan struct{})
	go func() {
		time.Sleep(300 * time.Millisecond)
		close(serverReady)
	}()

	go func() {
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			logging.Error("API server error: %v", err)
		}
	}()

	<-serverReady

	if err := desktop.Launch(desktop.DesktopBinary, baseURL, simpleMode); err != nil {
		return fmt.Errorf("desktop window exited with error: %w", err)
	}

	// Desktop window closed — shut down the API server gracefully.
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logging.Error("API server shutdown error: %v", err)
	}

	return nil
}
