package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/api"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/design"
	"github.com/digiogithub/pando/internal/desktop"
	"github.com/digiogithub/pando/internal/instanceregistry"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/nativeui"
	"github.com/digiogithub/pando/internal/version"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var desktopCmd = &cobra.Command{
	Use:   "desktop",
	Short: "Launch Pando as a native desktop app",
	Long: `Start Pando in desktop mode: starts the API server on a local port and
opens the native desktop window (GPU-rendered Go UI, no web view) on it.

Linux requirements: GTK 3 runtime libraries. WebKitGTK (libwebkit2gtk-4.1) is only
needed for the design preview canvas and extension panels, which show in an
embedded web view (without it they offer "Open in browser").
  Ubuntu 22.04:         sudo apt install libgtk-3-0
  Ubuntu 24.04, 26.04:  sudo apt install libgtk-3-0t64

Without a display (SSH, WSL without WSLg) use the browser UI instead: pando app

The previous WebView window (Wails) is still available behind the hidden
--webview flag until the native desktop reaches parity.`,
	Example: `
  # Launch desktop app on default port
  pando desktop

  # Launch on a specific port
  pando desktop --port 9000

  # Start in simple mode
  pando desktop --simple

  # Use a specific working directory (default: current directory, or the home
  # directory when opened from the app icon)
  pando desktop --cwd /path/to/project
  pando desktop -c /path/to/project`,
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
	desktopCmd.Flags().StringP("cwd", "c", "", "Working directory (root path) for Pando to use")
	// Transitional: the Wails WebView window, removed in P5 of the native
	// desktop plan (.kb/pando/plans/native_desktop_mygo.md).
	desktopCmd.Flags().Bool("webview", false, "Use the legacy WebView (Wails) window instead of the native UI")
	_ = desktopCmd.Flags().MarkHidden("webview")
}

func runDesktopMode(cmd *cobra.Command) error {
	// Flags are parsed by now: later failures are runtime errors, for which
	// the usage text is only noise.
	cmd.SilenceUsage = true

	host, _ := cmd.Flags().GetString("host")
	port, _ := cmd.Flags().GetInt("port")
	debug, _ := cmd.Flags().GetBool("debug")
	simpleMode, _ := cmd.Flags().GetBool("simple")
	cwdFlag, _ := cmd.Flags().GetString("cwd")
	useWebView, _ := cmd.Flags().GetBool("webview")
	ageKeys, _ := cmd.Flags().GetString("age-keys")
	config.SetAgeKeysOverride(ageKeys)

	// Native desktop, one per working directory: a second `pando desktop`
	// for the same directory focuses the running window and exits before it
	// starts a server or opens the database. Other directories (a project's
	// own window, spawned with --cwd) are separate instances. Resolved
	// early: the directory below also depends on the app-icon launch.
	if !useWebView {
		key := cwdFlag
		if key == "" {
			if dir, derr := desktop.DefaultWorkingDir(); derr == nil {
				key = dir
			}
		}
		if abs, aerr := filepath.Abs(key); aerr == nil {
			key = abs
		}
		if !nativeui.AcquireSingleInstance(key) {
			fmt.Println("Pando desktop is already running for this directory; focused its window.")
			return nil
		}
	}

	selectedPort, err := chooseAvailablePort(host, port)
	if err != nil {
		return err
	}
	if selectedPort != port {
		logging.Warn("Preferred port unavailable, using fallback", "preferred", port, "selected", selectedPort)
	}
	port = selectedPort

	var cwd string
	if cwdFlag != "" {
		cwd, err = filepath.Abs(cwdFlag)
		if err != nil {
			return fmt.Errorf("invalid --cwd path: %w", err)
		}
	} else {
		// Opened from the macOS app icon (or otherwise without a usable
		// directory): work from the home directory as a general workspace.
		cwd, err = desktop.DefaultWorkingDir()
		if err != nil {
			return err
		}
		_ = os.Chdir(cwd)
	}
	desktop.ImportLoginShellPath()
	startup := resolveStartupContext(cwd, "desktop")

	_, err = config.Load(cwd, debug, "")
	if err != nil {
		return err
	}

	// Capture SIGINT/SIGTERM before any startup work, so a stop during
	// startup winds down and runs the deferred cleanups (see notifyShutdown).
	sigCtx, _, releaseShutdown := notifyShutdown("Desktop")
	defer releaseShutdown()

	ctx, cancel := context.WithCancel(sigCtx)
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
		return fmt.Errorf("failed to load embedded web UI: %w", err)
	}

	dataDir := config.Get().Data.Directory
	if dataDir == "" {
		dataDir = ".pando"
	}

	baseURL := fmt.Sprintf("http://%s:%d", host, port)
	server, err := api.NewServer(ctx, api.ServerConfig{
		Host:               host,
		Port:               port,
		Version:            version.Normalize(),
		DB:                 conn,
		Querier:            rt.Querier,
		CWD:                cwd,
		StaticFS:           staticFS,
		OpenUI:             false,
		UIBaseURL:          baseURL,
		ParentInstanceID:   startup.ParentInstanceID,
		ProjectID:          startup.ProjectID,
		ProjectName:        startup.ProjectName,
		PublicBasePath:     startup.PublicBasePath,
		WebChildTLSDataDir: config.TLSCertDir(dataDir),
		InstanceID:         instanceID,
		Role:               string(rt.Role),
		PubPort:            rt.PubPort,
		RPCPort:            rt.RPCPort,
		StartupMode:        startup.Mode,
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
		Mode:             instanceregistry.ModeDesktop,
		ParentInstanceID: startup.ParentInstanceID,
		IsPrimary:        rt.Role == ipcruntime.RolePrimary,
	})
	defer func() { _ = instanceregistry.Revoke(instanceID) }()

	wireIPCRole(ctx, rt, server.PandoApp(), instanceID, cwd, "desktop")

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
	if !strings.HasPrefix(version.Normalize(), "v") {
		versionPrefix = "v"
	}
	fmt.Printf("Pando desktop %s%s — API on %s\n", versionPrefix, version.Normalize(), baseURL)

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

	if !useWebView {
		// The native UI runs on this (main) goroutine: MyGo's event loop must
		// own the thread that started the process.
		if ctx.Err() == nil {
			err := nativeui.Run(nativeui.Options{
				Ctx:     ctx,
				BaseURL: baseURL,
				Token:   server.GetToken(),
				CWD:     cwd,
				Simple:  simpleMode,
			})
			if errors.Is(err, nativeui.ErrNoDisplay) {
				err = fmt.Errorf("%w\nUse the browser UI instead: pando app", err)
			}
			if err != nil {
				cancel()
				shutdownServer(server)
				return err
			}
		}
		cancel()
		shutdownServer(server)
		return nil
	}

	createdEvents := design.Events().Subscribe(ctx)
	openedDesignWindows := map[string]struct{}{}
	var openedDesignWindowsMu sync.Mutex
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logging.Warn("Design desktop auto-open stopped", "error", r)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-createdEvents:
				if !ok {
					return
				}
				if event.Payload.Kind != design.EventCreated || event.Payload.ArtifactID == "" {
					continue
				}
				artifactID := event.Payload.ArtifactID
				openedDesignWindowsMu.Lock()
				if _, seen := openedDesignWindows[artifactID]; seen {
					openedDesignWindowsMu.Unlock()
					continue
				}
				openedDesignWindows[artifactID] = struct{}{}
				openedDesignWindowsMu.Unlock()

				targetURL := fmt.Sprintf("%s/design/%s", baseURL, url.PathEscape(artifactID))
				if err := desktop.LaunchWindow(desktop.EmbeddedDesktopBinary(), targetURL); err != nil {
					openedDesignWindowsMu.Lock()
					delete(openedDesignWindows, artifactID)
					openedDesignWindowsMu.Unlock()
					logging.Warn("Design desktop auto-open failed", "artifactID", artifactID, "error", err)
				}
			}
		}
	}()

	// A stop that arrived during startup has already shut the API server
	// down: do not open a window onto it.
	if ctx.Err() == nil {
		if err := desktop.Launch(desktop.EmbeddedDesktopBinary(), baseURL, simpleMode); err != nil {
			return fmt.Errorf("desktop window exited with error: %w", err)
		}
	}

	// Desktop window closed — shut down the API server gracefully.
	cancel()
	shutdownServer(server)
	return nil
}

// shutdownServer stops the API server, waiting up to 5 seconds.
func shutdownServer(server *api.Server) {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logging.Error("API server shutdown error: %v", err)
	}
}
