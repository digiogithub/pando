package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/api"
	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/version"
	"github.com/spf13/cobra"
)

func chooseAvailablePort(host string, preferred int) (int, error) {
	if preferred <= 0 {
		preferred = 8765
	}

	candidates := []int{preferred}
	for offset := 1; offset <= 10; offset++ {
		candidates = append(candidates, preferred+offset)
	}

	for _, port := range candidates {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
		if err == nil {
			_ = ln.Close()
			return port, nil
		}
	}

	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, fmt.Errorf("failed to find available port near %d: %w", preferred, err)
	}
	defer ln.Close()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("failed to determine random available port")
	}

	return addr.Port, nil
}

func runAppMode(cmd *cobra.Command) error {
	host, _ := cmd.Flags().GetString("host")
	port, _ := cmd.Flags().GetInt("port")
	debug, _ := cmd.Flags().GetBool("debug")
	preferredPort := port

	selectedPort, err := chooseAvailablePort(host, preferredPort)
	if err != nil {
		return err
	}
	if selectedPort != preferredPort {
		logging.Warn("Preferred port %d unavailable, using %d", preferredPort, selectedPort)
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
		return fmt.Errorf("failed to load embedded web ui: %w", err)
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
		OpenUI:    true,
		UIBaseURL: baseURL,
	})
	if err != nil {
		return fmt.Errorf("failed to create app server: %w", err)
	}

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	go func() {
		<-sigCtx.Done()
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		shutdownDone := make(chan struct{})
		go func() {
			defer close(shutdownDone)
			if err := server.Shutdown(shutdownCtx); err != nil {
				logging.Error("App server shutdown error: %v", err)
			}
		}()

		select {
		case <-shutdownDone:
		case <-shutdownCtx.Done():
			logging.Error("App server shutdown timed out; forcing process exit")
			os.Exit(1)
		}
	}()

	fmt.Printf("Pando app v%s listening on %s\n", version.Version, baseURL)
	fmt.Println("Press Ctrl+C to stop")

	go func() {
		time.Sleep(350 * time.Millisecond)
		if err := auth.OpenBrowser(baseURL); err != nil {
			logging.Warn("Could not open browser automatically: %v", err)
		}
	}()

	if err := server.Start(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}
