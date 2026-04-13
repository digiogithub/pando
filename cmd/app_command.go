package cmd

import "github.com/spf13/cobra"

var appCmd = &cobra.Command{
	Use:   "app",
	Short: "Start API server and embedded web UI",
	Long:  "Start Pando in app mode, serving the API and embedded web UI from the same port.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAppMode(cmd)
	},
}

func init() {
	rootCmd.AddCommand(appCmd)
	appCmd.Flags().String("host", "localhost", "Host to bind the app server to")
	appCmd.Flags().Int("port", 8765, "Preferred port to bind the app server to")
}
