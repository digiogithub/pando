package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/digiogithub/pando/internal/cliassist"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/spf13/cobra"
)

var cliAssistCmd = &cobra.Command{
	Use:   "cli-assist [request...]",
	Short: "Generate and run a shell command using AI",
	Long: `cli-assist generates a shell command for the current OS and shell based on your
natural language request, then presents an interactive menu to execute, edit, or cancel it.

The request is formed by joining all positional arguments. You can also pass a
single quoted string.`,
	Example: `
  # Find text files containing "hola"
  pando cli-assist find all text files containing "hola" in all subdirectories

  # List large files
  pando cli-assist list files larger than 100MB in current directory

  # Use a specific model for this run
  pando cli-assist -m copilot.gpt-4o find duplicate files
  `,
	// Accept any number of positional args (the natural language request)
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: pando cli-assist <what you want to do>")
			fmt.Fprintln(os.Stderr, "Example: pando cli-assist find all text files containing \"hola\"")
			return fmt.Errorf("no request provided")
		}

		debug, _ := cmd.Flags().GetBool("debug")
		logFile, _ := cmd.Flags().GetString("log-file")
		cwd, _ := cmd.Flags().GetString("cwd")
		modelOverride, _ := cmd.Flags().GetString("model")

		if cwd != "" {
			if err := os.Chdir(cwd); err != nil {
				return fmt.Errorf("failed to change directory: %v", err)
			}
		} else {
			var err error
			cwd, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %v", err)
			}
		}

		cfg, err := config.Load(cwd, debug, logFile)
		if err != nil {
			return err
		}
		logging.Debug("Config loaded for cli-assist", "workingDir", cwd)

		if strings.TrimSpace(modelOverride) != "" {
			if err := config.OverrideAgentModel(config.AgentCLIAssist, models.ModelID(strings.TrimSpace(modelOverride))); err != nil {
				return fmt.Errorf("failed to override model %q: %w", modelOverride, err)
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cliassist.Run(ctx, cfg, args)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(cliAssistCmd)

	cliAssistCmd.Flags().BoolP("debug", "d", false, "Enable debug logging")
	cliAssistCmd.Flags().StringP("log-file", "l", "", "Path to log file")
	cliAssistCmd.Flags().StringP("cwd", "c", "", "Working directory")
	cliAssistCmd.Flags().StringP("model", "m", "", "Override the model for this run")
}
