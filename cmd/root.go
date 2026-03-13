package cmd

import (
	"context"
	"fmt"
	"io"
	// "log" // Commented out - used in runACPServer which is TODO
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
	// "github.com/digiogithub/pando/internal/mesnada/acp" // Commented out - used in runACPServer which is TODO
	"github.com/digiogithub/pando/internal/pubsub"
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
		_, err := config.Load(cwd, debug, logFile)
		if err != nil {
			return err
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
		// Defer shutdown here so it runs for both interactive and non-interactive modes
		defer app.Shutdown()

		// Initialize MCP tools early for both modes
		initMCPTools(ctx, app)

		// Non-interactive mode
		if prompt != "" {
			quiet = true
			// Run non-interactive flow using the App method
			return app.RunNonInteractive(ctx, prompt, outputFormat, quiet, yoloMode)
		}

		// Interactive mode
		if yoloMode {
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

// runACPServer starts Pando in ACP server mode.
// TODO: Re-enable once Fase 3 (PandoACPAgent with stdio transport) is complete
func runACPServer() error {
	return fmt.Errorf("ACP stdio transport not yet implemented - use HTTP/SSE transport via 'pando server' instead")

	/* TODO: Uncomment once Fase 3 is complete
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %v", err)
	}

	// Create logger for ACP agent
	logger := log.New(os.Stderr, "[ACP] ", log.LstdFlags)
	logger.Printf("Starting Pando ACP Agent v%s", version.Version)

	// Create ACP agent
	agent := acp.NewPandoACPAgent(version.Version, cwd, logger)

	// Create stdio transport
	transport := acp.NewStdioTransport(agent, logger)

	// Create context with cancellation
	ctx := context.Background()

	// Run the transport loop
	logger.Printf("ACP agent listening on stdio")
	if err := transport.Run(ctx); err != nil {
		logger.Printf("ACP agent error: %v", err)
		return fmt.Errorf("ACP agent error: %w", err)
	}

	logger.Printf("ACP agent shutdown complete")
	return nil
	*/
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
