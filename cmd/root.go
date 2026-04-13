package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/format"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/logging"
	acpPkg "github.com/digiogithub/pando/internal/mesnada/acp"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/tui"
	"github.com/digiogithub/pando/internal/version"
	zone "github.com/lrstanley/bubblezone"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pando",
	Short: "Terminal-based AI assistant for software development",
	Long: `Pando is a powerful terminal-based AI assistant that helps with software development tasks.
It provides an interactive chat interface with AI capabilities, code analysis, and LSP integration
to assist developers in writing, debugging, and understanding code directly from the terminal.
It also supports non-interactive prompts via the -p flag or piped stdin input.
The prompt can also be provided via the PANDO_PROMPT environment variable.`,
	Example: `
  # Run in interactive mode
  pando

  # Run with debug logging
  pando -d

  # Run with debug logging in a specific directory
  pando -d -c /path/to/project

  # Run with debug logging to a specific file
  pando -d -l /tmp/pando-debug.log

  # Print version
  pando -v

  # Run a single non-interactive prompt
  pando -p "Explain the use of context in Go"

  # Run a single non-interactive prompt with a specific model for this run
  pando -p "Explain the use of context in Go" -m copilot.gpt-5.4

  # Run a single non-interactive prompt with JSON output format
  pando -p "Explain the use of context in Go" -f json

  # Run with all tools auto-approved (no permission prompts)
  pando -p "Fix all lint errors" --yolo

  # Same but with full flag name
  pando -p "Refactor main.go" --allow-all-tools

  # Pipe prompt from stdin
  echo "Explain Go context" | pando

  # Read prompt from file
  cat prompt.txt | pando

  # Use environment variable for prompt
  PANDO_PROMPT="Explain Go context" pando

  # Combine with other flags
  echo "Fix lint errors" | pando --yolo -f json

  # Priority: -p flag > stdin > PANDO_PROMPT > interactive mode
  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		// If the help flag is set, show the help message
		if cmd.Flag("help").Changed {
			cmd.Help()
			return nil
		}
		if cmd.Flag("version").Changed {
			fmt.Println(version.Version)
			return nil
		}

		// Check if ACP server mode is requested
		acpServer, _ := cmd.Flags().GetBool("acp-server")
		if acpServer {
			return runACPServer()
		}

		// Load the config
		debug, _ := cmd.Flags().GetBool("debug")
		logFile, _ := cmd.Flags().GetString("log-file")
		cwd, _ := cmd.Flags().GetString("cwd")
		prompt, _ := cmd.Flags().GetString("prompt")
		modelOverride, _ := cmd.Flags().GetString("model")
		outputFormat, _ := cmd.Flags().GetString("output-format")
		quiet, _ := cmd.Flags().GetBool("quiet")
		yolo, _ := cmd.Flags().GetBool("yolo")
		allowAll, _ := cmd.Flags().GetBool("allow-all-tools")
		yoloMode := yolo || allowAll

		// Read prompt from stdin if piped (not a terminal)
		if prompt == "" {
			fi, err := os.Stdin.Stat()
			if err == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
				data, err := io.ReadAll(os.Stdin)
				if err == nil {
					stdinPrompt := strings.TrimSpace(string(data))
					if stdinPrompt != "" {
						prompt = stdinPrompt
					}
				}
			}
		}

		// Check PANDO_PROMPT environment variable as last resort
		if prompt == "" {
			if envPrompt := os.Getenv("PANDO_PROMPT"); envPrompt != "" {
				prompt = strings.TrimSpace(envPrompt)
			}
		}

		// Validate format option
		if !format.IsValid(outputFormat) {
			return fmt.Errorf("invalid format option: %s\n%s", outputFormat, format.GetHelpText())
		}

		if cwd != "" {
			err := os.Chdir(cwd)
			if err != nil {
				return fmt.Errorf("failed to change directory: %v", err)
			}
		}
		if cwd == "" {
			c, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %v", err)
			}
			cwd = c
		}
		cfg, err := config.Load(cwd, debug, logFile)
		if err != nil {
			return err
		}

		// Start the config file watcher so hot-reload works across TUI and Web-UI.
		if cfgPath, pathErr := config.ResolveConfigFilePath(); pathErr == nil && cfgPath != "" {
			// ctx is created below; use a background context here and let the
			// watcher stop naturally when the process exits.
			watchCtx, watchCancel := context.WithCancel(context.Background())
			defer watchCancel()
			config.WatchConfigFile(watchCtx, cfgPath)
		}
		if strings.TrimSpace(modelOverride) != "" {
			if err := config.OverrideAgentModel(config.AgentCoder, models.ModelID(strings.TrimSpace(modelOverride))); err != nil {
				return fmt.Errorf("failed to override model %q: %w", modelOverride, err)
			}
			logging.Debug("Runtime model override applied", "agent", config.AgentCoder, "model", modelOverride)
		}
		logging.Debug("Config loaded", "workingDir", cwd, "debug", debug, "logFile", logFile)

		// Connect DB, this will also run migrations
		conn, err := db.Connect()
		if err != nil {
			return err
		}
		logging.Debug("Database connected")

		// Create main context for the application
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		app, err := app.New(ctx, conn)
		if err != nil {
			logging.Error("Failed to create app: %v", err)
			return err
		}
		logging.Debug("App initialized")

		// Initialize MCP tools early for both modes
		initMCPTools(ctx, app)

		// Non-interactive mode
		if prompt != "" {
			quiet = true
			defer app.Shutdown()
			// Run non-interactive flow using the App method
			return app.RunNonInteractive(ctx, prompt, outputFormat, quiet, yoloMode)
		}

		// Interactive mode
		if yoloMode || (cfg != nil && cfg.Permissions.AutoApproveTools) {
			app.Permissions.SetGlobalAutoApprove(true)
		}

		// Set up the TUI
		zone.NewGlobal()
		program := tea.NewProgram(
			tui.New(app),
			tea.WithAltScreen(),
		)

		// Setup the subscriptions, this will send services events to the TUI
		ch, cancelSubs := setupSubscriptions(app, ctx)

		// Create a context for the TUI message handler
		tuiCtx, tuiCancel := context.WithCancel(ctx)
		var tuiWg sync.WaitGroup
		tuiWg.Add(1)

		// Set up message handling for the TUI
		go func() {
			defer tuiWg.Done()
			defer logging.RecoverPanic("TUI-message-handler", func() {
				attemptTUIRecovery(program)
			})

			for {
				select {
				case <-tuiCtx.Done():
					logging.Info("TUI message handler shutting down")
					return
				case msg, ok := <-ch:
					if !ok {
						logging.Info("TUI message channel closed")
						return
					}
					program.Send(msg)
				}
			}
		}()

		// Cleanup function for when the program exits
		cleanup := func() {
			// Cancel root context first so background tasks (e.g. KB import/index)
			// stop promptly before waiting in app shutdown.
			cancel()

			// Shutdown the app
			app.Shutdown()

			// Cancel subscriptions first
			cancelSubs()

			// Then cancel TUI message handler
			tuiCancel()

			// Wait for TUI message handler to finish
			tuiWg.Wait()

			logging.Info("All goroutines cleaned up")
		}

		// Run the TUI
		result, err := program.Run()
		cleanup()

		if err != nil {
			logging.Error("TUI error: %v", err)
			return fmt.Errorf("TUI error: %v", err)
		}

		logging.Info("TUI exited with result: %v", result)
		return nil
	},
}

// attemptTUIRecovery tries to recover the TUI after a panic
func attemptTUIRecovery(program *tea.Program) {
	logging.Info("Attempting to recover TUI after panic")

	// We could try to restart the TUI or gracefully exit
	// For now, we'll just quit the program to avoid further issues
	program.Quit()
}

func initMCPTools(ctx context.Context, app *app.App) {
	go func() {
		defer logging.RecoverPanic("MCP-goroutine", nil)
		logging.Debug("initMCPTools started")

		// Create a context with timeout for the initial MCP tools fetch
		ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// Set this up once with proper error handling
		agent.GetMcpTools(ctxWithTimeout, app.Permissions)
		logging.Debug("initMCPTools completed")
		logging.Info("MCP message handling goroutine exiting")
	}()
}

func setupSubscriber[T any](
	ctx context.Context,
	wg *sync.WaitGroup,
	name string,
	subscriber func(context.Context) <-chan pubsub.Event[T],
	outputCh chan<- tea.Msg,
) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer logging.RecoverPanic(fmt.Sprintf("subscription-%s", name), nil)

		subCh := subscriber(ctx)

		for {
			select {
			case event, ok := <-subCh:
				if !ok {
					logging.Info("subscription channel closed", "name", name)
					return
				}

				var msg tea.Msg = event

				select {
				case outputCh <- msg:
				case <-time.After(2 * time.Second):
					logging.Warn("message dropped due to slow consumer", "name", name)
				case <-ctx.Done():
					logging.Info("subscription cancelled", "name", name)
					return
				}
			case <-ctx.Done():
				logging.Info("subscription cancelled", "name", name)
				return
			}
		}
	}()
}

func setupSubscriptions(app *app.App, parentCtx context.Context) (chan tea.Msg, func()) {
	ch := make(chan tea.Msg, 100)

	wg := sync.WaitGroup{}
	ctx, cancel := context.WithCancel(parentCtx) // Inherit from parent context

	setupSubscriber(ctx, &wg, "logging", logging.Subscribe, ch)
	setupSubscriber(ctx, &wg, "sessions", app.Sessions.Subscribe, ch)
	setupSubscriber(ctx, &wg, "messages", app.Messages.Subscribe, ch)
	setupSubscriber(ctx, &wg, "permissions", app.Permissions.Subscribe, ch)
	setupSubscriber(ctx, &wg, "coderAgent", app.CoderAgent.Subscribe, ch)

	cleanupFunc := func() {
		logging.Info("Cancelling all subscriptions")
		cancel() // Signal all goroutines to stop

		waitCh := make(chan struct{})
		go func() {
			defer logging.RecoverPanic("subscription-cleanup", nil)
			wg.Wait()
			close(waitCh)
		}()

		select {
		case <-waitCh:
			logging.Info("All subscription goroutines completed successfully")
			close(ch) // Only close after all writers are confirmed done
		case <-time.After(5 * time.Second):
			logging.Warn("Timed out waiting for some subscription goroutines to complete")
			close(ch)
		}
	}
	return ch, cleanupFunc
}

// runACPServer starts Pando in ACP server mode (stdio transport) using default settings.
func runACPServer() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	return runACPServerWithOptions(cwd, false, false)
}

// runACPServerWithOptions starts Pando in ACP server mode (stdio transport).
// Editors like VS Code, Zed, and JetBrains spawn this as a subprocess and
// communicate via JSON-RPC over stdin/stdout.
func runACPServerWithOptions(cwd string, debug bool, autoPerm bool) error {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current working directory: %w", err)
		}
	}

	logFlags := log.LstdFlags
	if debug {
		logFlags |= log.Lshortfile
	}
	logger := log.New(os.Stderr, "[ACP] ", logFlags)
	logger.Printf("Starting Pando ACP Agent v%s (cwd=%s, debug=%v, autoPerm=%v)", version.Version, cwd, debug, autoPerm)

	// Load config (required to connect DB and initialize agent)
	cfg, err := config.Load(cwd, debug, "")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Command-line flags override config file settings
	if autoPerm {
		cfg.ACP.AutoPermission = true
	}

	// Connect to database
	conn, err := db.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create app with all services (sessions, messages, agent, etc.).
	// LSP is skipped: in ACP stdio mode the editor manages its own language servers.
	ctx := context.Background()
	pandoApp, err := app.New(ctx, conn, app.AppOptions{SkipLSP: true})
	if err != nil {
		return fmt.Errorf("failed to initialize app: %w", err)
	}
	defer pandoApp.Shutdown()

	// ACP stdio mode is non-interactive from Pando's perspective.
	// Always auto-approve permissions so tool calls never block waiting for
	// terminal UI confirmation that does not exist in this mode.
	if !cfg.ACP.AutoPermission && !autoPerm {
		logger.Printf("ACP auto-permission forced on for stdio mode")
	}
	pandoApp.Permissions.SetGlobalAutoApprove(true)

	// Build adapters (defined below) that bridge internal services to ACP interfaces,
	// avoiding import cycles between internal/mesnada/acp and internal/llm/agent.
	agentAdapter := &acpAgentAdapter{svc: pandoApp.CoderAgent}
	sessionAdapter := &acpSessionAdapter{svc: pandoApp.Sessions, msgSvc: pandoApp.Messages}
	permAdapter := &acpPermissionAdapter{svc: pandoApp.Permissions}

	pandoAgent := acpPkg.NewPandoACPAgent(
		version.Version,
		cwd,
		logger,
		agentAdapter,
		sessionAdapter,
		permAdapter,
	)

	transport := acpPkg.NewStdioTransport(pandoAgent, logger)
	logger.Printf("ACP agent listening on stdio")
	return transport.Run(ctx)
}

// acpAgentAdapter adapts agent.Service to acpPkg.AgentService.
// Defined here to avoid import cycles between internal/mesnada/acp and internal/llm/agent.
type acpAgentAdapter struct {
	svc agent.Service
}

func (a *acpAgentAdapter) Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan acpPkg.AgentEvent, error) {
	realCh, err := a.svc.Run(ctx, sessionID, content, attachments...)
	if err != nil {
		return nil, err
	}

	acpCh := make(chan acpPkg.AgentEvent)
	go func() {
		defer close(acpCh)
		for ev := range realCh {
			var acpEv acpPkg.AgentEvent
			switch ev.Type {
			case agent.AgentEventTypeError:
				acpEv.Type = acpPkg.AgentEventTypeError
				acpEv.Error = ev.Error
			case agent.AgentEventTypeResponse:
				acpEv.Type = acpPkg.AgentEventTypeResponse
				acpEv.Message = ev.Message
			case agent.AgentEventTypeSummarize:
				acpEv.Type = acpPkg.AgentEventTypeSummarize
			case agent.AgentEventTypeContentDelta:
				acpEv.Type = acpPkg.AgentEventTypeContentDelta
				acpEv.Delta = ev.Delta
			case agent.AgentEventTypeThinkingDelta:
				acpEv.Type = acpPkg.AgentEventTypeThinkingDelta
				acpEv.Delta = ev.Delta
			case agent.AgentEventTypeToolCall:
				acpEv.Type = acpPkg.AgentEventTypeToolCall
				acpEv.ToolCall = ev.ToolCall
			case agent.AgentEventTypeToolResult:
				acpEv.Type = acpPkg.AgentEventTypeToolResult
				acpEv.ToolResult = ev.ToolResult
			default:
				continue
			}
			select {
			case acpCh <- acpEv:
			case <-ctx.Done():
				return
			}
		}
	}()

	return acpCh, nil
}

func (a *acpAgentAdapter) Cancel(sessionID string) {
	a.svc.Cancel(sessionID)
}

// CurrentModelID returns the ID of the currently active model for the coder agent.
func (a *acpAgentAdapter) CurrentModelID() string {
	return string(a.svc.Model().ID)
}

// AvailableModels returns all registered models with name metadata.
func (a *acpAgentAdapter) AvailableModels() []acpPkg.ACPModelInfo {
	allModels := models.GetAllModels()
	result := make([]acpPkg.ACPModelInfo, 0, len(allModels))
	for _, m := range allModels {
		provider := string(m.Provider)
		displayName := m.Name
		if provider != "" && provider != "__mock" {
			providerLabel := strings.ToUpper(provider[:1]) + provider[1:]
			displayName = providerLabel + ": " + m.Name
		}
		result = append(result, acpPkg.ACPModelInfo{
			ID:   string(m.ID),
			Name: displayName,
		})
	}
	return result
}

// SetModelOverride temporarily overrides the active model for the coder agent.
// The change is in-memory only and is not persisted to the config file.
func (a *acpAgentAdapter) SetModelOverride(modelID string) error {
	if modelID == "" {
		return nil
	}
	return config.OverrideAgentModel(config.AgentCoder, models.ModelID(modelID))
}

// ListPersonas returns all available persona names.
func (a *acpAgentAdapter) ListPersonas() []string {
	return agent.ListAvailablePersonas()
}

// GetActivePersona returns the currently active persona name.
func (a *acpAgentAdapter) GetActivePersona() string {
	return agent.GetActivePersona()
}

// SetActivePersona sets the active persona by name (empty = clear).
func (a *acpAgentAdapter) SetActivePersona(name string) error {
	return agent.SetActivePersona(name)
}

// ListAvailableTools returns the name and description of all tools available to the agent.
func (a *acpAgentAdapter) ListAvailableTools() []acpPkg.ACPToolInfo {
	baseTools := a.svc.GetTools()
	result := make([]acpPkg.ACPToolInfo, 0, len(baseTools))
	for _, t := range baseTools {
		info := t.Info()
		result = append(result, acpPkg.ACPToolInfo{
			Name:        info.Name,
			Description: info.Description,
		})
	}
	return result
}

// acpSessionAdapter adapts session.Service to acpPkg.SessionService.
type acpSessionAdapter struct {
	svc    session.Service
	msgSvc message.Service
}

func (a *acpSessionAdapter) CreateSession(ctx context.Context, title string) (string, error) {
	sess, err := a.svc.Create(ctx, title)
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func (a *acpSessionAdapter) GetSession(ctx context.Context, id string) (acpPkg.ACPSessionInfo, error) {
	sess, err := a.svc.Get(ctx, id)
	if err != nil {
		return acpPkg.ACPSessionInfo{}, err
	}
	return acpPkg.ACPSessionInfo{
		ID:        sess.ID,
		Title:     sess.Title,
		UpdatedAt: sess.UpdatedAt,
	}, nil
}

func (a *acpSessionAdapter) ListSessions(ctx context.Context) ([]acpPkg.ACPSessionInfo, error) {
	sessions, err := a.svc.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]acpPkg.ACPSessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = acpPkg.ACPSessionInfo{
			ID:        s.ID,
			Title:     s.Title,
			UpdatedAt: s.UpdatedAt,
		}
	}
	return result, nil
}

func (a *acpSessionAdapter) GetMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	return a.msgSvc.List(ctx, sessionID)
}

// acpPermissionAdapter adapts permission.Service to acpPkg.PermissionService.
// It converts between permission.CreatePermissionRequest and acpPkg.PermissionRequestData
// so the ACP layer does not need to import the permission package directly.
type acpPermissionAdapter struct {
	svc permission.Service
}

func (a *acpPermissionAdapter) AutoApproveSession(sessionID string) {
	a.svc.AutoApproveSession(sessionID)
}

func (a *acpPermissionAdapter) RemoveAutoApproveSession(sessionID string) {
	a.svc.RemoveAutoApproveSession(sessionID)
}

func (a *acpPermissionAdapter) RegisterSessionHandler(sessionID string, handler func(req acpPkg.PermissionRequestData) bool) {
	a.svc.RegisterSessionHandler(sessionID, func(req permission.CreatePermissionRequest) bool {
		return handler(acpPkg.PermissionRequestData{
			SessionID:   req.SessionID,
			ToolName:    req.ToolName,
			Description: req.Description,
			Action:      req.Action,
			Path:        req.Path,
			Params:      req.Params,
		})
	})
}

func (a *acpPermissionAdapter) UnregisterSessionHandler(sessionID string) {
	a.svc.UnregisterSessionHandler(sessionID)
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().BoolP("help", "h", false, "Help")
	rootCmd.Flags().BoolP("version", "v", false, "Version")
	rootCmd.Flags().BoolP("debug", "d", false, "Debug")
	rootCmd.Flags().StringP("log-file", "l", "", "Path to log file (enables debug logging to file)")
	rootCmd.Flags().StringP("cwd", "c", "", "Current working directory")
	rootCmd.Flags().StringP("prompt", "p", "", "Prompt to run in non-interactive mode")
	rootCmd.Flags().StringP("model", "m", "", "Override the model for this run without changing the saved config")

	// Add format flag with validation logic
	rootCmd.Flags().StringP("output-format", "f", format.Text.String(),
		"Output format for non-interactive mode (text, json)")

	// Add quiet flag to hide spinner in non-interactive mode
	rootCmd.Flags().BoolP("quiet", "q", false, "Hide spinner in non-interactive mode")
	rootCmd.Flags().Bool("yolo", false, "Auto-approve all tool permissions including MCP tools")
	rootCmd.Flags().Bool("allow-all-tools", false, "Auto-approve all tool permissions (alias for --yolo)")

	// Add ACP server flag
	rootCmd.Flags().Bool("acp-server", false, "Run as ACP server (allows other agents to connect)")
	rootCmd.PersistentFlags().String("host", "localhost", "Host to bind the app/api server to")
	rootCmd.PersistentFlags().Int("port", 8765, "Port to bind the app/api server to")

	// Register custom validation for the format flag
	rootCmd.RegisterFlagCompletionFunc("output-format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return format.SupportedFormats, cobra.ShellCompDirectiveNoFileComp
	})

	rootCmd.RegisterFlagCompletionFunc("model", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		matches := make([]string, 0, len(models.SupportedModels))
		needle := strings.ToLower(strings.TrimSpace(toComplete))
		for modelID := range models.SupportedModels {
			candidate := string(modelID)
			if needle == "" || strings.Contains(strings.ToLower(candidate), needle) {
				matches = append(matches, candidate)
			}
		}
		sort.Strings(matches)
		return matches, cobra.ShellCompDirectiveNoFileComp
	})
}
