package cliassist

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

// Run is the entrypoint for --cli-assist mode.
// It detects the OS/shell, builds a prompt, fetches a command from the LLM,
// shows an interactive menu, and either executes or exits.
func Run(ctx context.Context, cfg *config.Config, args []string) {
	requestText := strings.Join(args, " ")
	if strings.TrimSpace(requestText) == "" {
		fmt.Fprintln(os.Stderr, "Usage: pando --cli-assist <what you want to do>")
		os.Exit(1)
	}

	info := DetectSysInfo()

	var command string

	for {
		if command == "" {
			systemPrompt := BuildSystemPrompt(info)
			userPrompt := BuildUserPrompt(args)

			var err error
			command, err = FetchCommand(ctx, cfg, systemPrompt, userPrompt)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching command: %v\n", err)
				os.Exit(1)
			}
		}

		action, newCmd, newPrompt := ShowMenu(command)
		switch action {
		case ActionExecute:
			exitCode := RunCommand(info, command)
			os.Exit(exitCode)
		case ActionEditPrompt:
			if strings.TrimSpace(newPrompt) != "" {
				args = []string{newPrompt}
			}
			command = "" // force re-fetch
		case ActionEditCommand:
			command = newCmd // re-show menu with edited command, no re-fetch
		case ActionQuit:
			os.Exit(0)
		}
	}
}
