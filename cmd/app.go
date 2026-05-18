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

	"github.com/digiogithub/pando/internal/api"
	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/instanceregistry"
	"github.com/digiogithub/pando/internal/ipc/bridge"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	"github.com/digiogithub/pando/internal/ipc/writecoordinator"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/tlsutil"
	"github.com/digiogithub/pando/internal/version"
	"github.com/google/uuid"
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

	staticFS, err := api.EmbeddedWebUI()
	if err != nil {
		return fmt.Errorf("failed to load embedded web ui: %w", err)
	}

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
		StaticFS:    staticFS,
		OpenUI:      true,
		UIBaseURL:   baseURL,
		TLSCertFile: tlsCert,
		TLSKeyFile:  tlsKey,
		InstanceID:  instanceID,
		Role:        string(rt.Role),
		PubPort:     rt.PubPort,
		RPCPort:     rt.RPCPort,
	})
	if err != nil {
		return fmt.Errorf("failed to create app server: %w", err)
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
		appBus := rt.Bus
		appCoord := writecoordinator.New(ctx, rt.Querier, 256)
		defer appCoord.Shutdown()
		dbproxy.RegisterHandlersWithCoordinator(appBus, appCoord)
		bridge.RegisterHandlers(appBus, instanceID, pandoApp.Sessions, pandoApp.Messages, time.Now())
		if busErr := appBus.Start(ctx, rt.PubPort, rt.RPCPort); busErr != nil {
			logging.Warn("IPC: app mode failed to start bus", "error", busErr)
		} else {
			appBridge := bridge.New(appBus, pandoApp.Sessions, pandoApp.CoderAgent)
			appBridge.Start(ctx)
			logging.Debug("IPC: app mode announced", "instanceID", instanceID, "pubPort", rt.PubPort, "rpcPort", rt.RPCPort)
		}
	}

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	// Watchdog: unconditionally force-exit if the process has not terminated
	// within 6 seconds of receiving the shutdown signal, in case something in
	// the graceful-shutdown path hangs (watcher WaitGroup, SSE connections, etc.).
	go func() {
		<-sigCtx.Done()
		time.Sleep(6 * time.Second)
		logging.Error("App shutdown watchdog: forced exit after 6s")
		os.Exit(1)
	}()

	go func() {
		<-sigCtx.Done()
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logging.Error("App server shutdown error: %v", err)
		}
	}()

	versionPrefix := ""
	if !strings.HasPrefix(version.Version, "v") {
		versionPrefix = "v"
	}
	fmt.Printf("Pando app %s%s listening on %s\n", versionPrefix, version.Version, baseURL)
	if server.IsTLS() {
		fmt.Println("TLS enabled (self-signed certificate — accept the browser security warning for local use)")
	}
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
