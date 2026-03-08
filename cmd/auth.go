package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication helpers",
}

var authCopilotCmd = &cobra.Command{
	Use:   "copilot",
	Short: "Manage GitHub Copilot login",
}

var authCopilotLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with GitHub Copilot using device login",
	RunE: func(cmd *cobra.Command, args []string) error {
		enterpriseURL, _ := cmd.Flags().GetString("enterprise-url")
		noBrowser, _ := cmd.Flags().GetBool("no-browser")

		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
		defer cancel()

		deviceCode, err := auth.StartCopilotDeviceFlow(ctx, enterpriseURL)
		if err != nil {
			return err
		}

		fmt.Fprintln(os.Stderr, auth.CopilotDeviceFlowInstructions(*deviceCode))
		if !noBrowser {
			if err := auth.OpenBrowser(deviceCode.VerificationURI); err != nil {
				fmt.Fprintf(os.Stderr, "Could not open browser automatically: %v\n", err)
			}
		}
		fmt.Fprintln(os.Stderr, "Waiting for GitHub Copilot authorization...")

		if _, err := auth.CompleteCopilotDeviceFlow(ctx, enterpriseURL, deviceCode); err != nil {
			return err
		}

		fmt.Fprintln(os.Stdout, "GitHub Copilot login saved.")
		return nil
	},
}

var authCopilotLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove the saved GitHub Copilot session",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := auth.DeleteCopilotSession(); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "GitHub Copilot session removed.")
		return nil
	},
}

var authCopilotStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show GitHub Copilot authentication status",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stdout, auth.GetCopilotAuthStatus().Message)
		return nil
	},
}

func init() {
	authCopilotLoginCmd.Flags().String("enterprise-url", "", "GitHub Enterprise URL or domain")
	authCopilotLoginCmd.Flags().Bool("no-browser", false, "Print the URL and code without opening a browser")

	authCopilotCmd.AddCommand(authCopilotLoginCmd)
	authCopilotCmd.AddCommand(authCopilotLogoutCmd)
	authCopilotCmd.AddCommand(authCopilotStatusCmd)
	authCmd.AddCommand(authCopilotCmd)
	rootCmd.AddCommand(authCmd)
}
