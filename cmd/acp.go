package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var acpCmd = &cobra.Command{
	Use:   "acp",
	Short: "Manage ACP server",
	Long: `Manage the Pando ACP (Agent Client Protocol) server.

The ACP server allows other clients to connect to Pando and execute conversations.
Use these commands to start, stop, and monitor the ACP server.

When called without a subcommand, starts the ACP server (equivalent to "pando acp start").
This is the mode used by editors like VS Code, Zed, and JetBrains.`,
	Example: `
  # Start ACP server (stdio mode, for editors)
  pando acp

  # Start with explicit subcommand
  pando acp start

  # Start with debug logging
  pando acp --debug`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := cmd.Flags().GetString("cwd")
		if cwd == "" {
			var err error
			cwd, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %w", err)
			}
		}
		debug, _ := cmd.Flags().GetBool("debug")
		autoPerm, _ := cmd.Flags().GetBool("auto-permission")
		return runACPServerWithOptions(cwd, debug, autoPerm)
	},
}

var acpStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start ACP server",
	Long: `Start the Pando ACP server.

The server can run in different transport modes:
- stdio: Standard input/output (for process-based communication)
- http: HTTP with Server-Sent Events for real-time updates

Configuration is read from .pando.toml or can be overridden with flags.`,
	Example: `
  # Start with default configuration
  pando acp start

  # Start on specific port
  pando acp start --port 9000

  # Start with HTTP transport
  pando acp start --transport http

  # Start with debug logging
  pando acp start --debug`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := cmd.Flags().GetString("cwd")
		if cwd == "" {
			var err error
			cwd, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %w", err)
			}
		}
		debug, _ := cmd.Flags().GetBool("debug")
		autoPerm, _ := cmd.Flags().GetBool("auto-permission")
		return runACPServerWithOptions(cwd, debug, autoPerm)
	},
}

var acpStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show ACP server status",
	Long:  `Query and display the current status of the ACP server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")

		url := fmt.Sprintf("http://%s:%d/mesnada/acp/health", host, port)

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			return fmt.Errorf("failed to connect to ACP server: %w\nIs the server running?", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned status %d", resp.StatusCode)
		}

		var health map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		// Display status
		fmt.Println("ACP Server Status")
		fmt.Println("=================")
		fmt.Printf("Status:           %s\n", health["status"])
		fmt.Printf("Transport:        %s\n", health["transport"])
		fmt.Printf("Active Sessions:  %v\n", health["active_sessions"])
		if uptime, ok := health["uptime_seconds"].(float64); ok {
			duration := time.Duration(uptime) * time.Second
			fmt.Printf("Uptime:           %s\n", formatDuration(duration))
		}
		if version, ok := health["version"].(string); ok {
			fmt.Printf("Version:          %s\n", version)
		}

		return nil
	},
}

var acpSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List active sessions",
	Long:  `List all active ACP sessions with their details.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")

		url := fmt.Sprintf("http://%s:%d/api/acp/sessions", host, port)

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			return fmt.Errorf("failed to connect to ACP server: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned status %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		sessions, ok := result["sessions"].([]interface{})
		if !ok || len(sessions) == 0 {
			fmt.Println("No active sessions")
			return nil
		}

		// Display sessions in table format
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SESSION ID\tCREATED\tLAST ACTIVITY\tWORKSPACE")
		fmt.Fprintln(w, "----------\t-------\t-------------\t---------")

		for _, s := range sessions {
			session, ok := s.(map[string]interface{})
			if !ok {
				continue
			}

			sessionID := truncateString(fmt.Sprintf("%v", session["session_id"]), 12)
			created := formatTime(session["created_at"])
			lastActivity := formatTime(session["last_activity"])
			workspace := truncateString(fmt.Sprintf("%v", session["workspace"]), 40)

			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", sessionID, created, lastActivity, workspace)
		}
		w.Flush()

		fmt.Printf("\nTotal sessions: %d\n", len(sessions))

		return nil
	},
}

var acpStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show server statistics",
	Long:  `Display detailed statistics about the ACP server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")

		url := fmt.Sprintf("http://%s:%d/api/acp/stats", host, port)

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			return fmt.Errorf("failed to connect to ACP server: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned status %d", resp.StatusCode)
		}

		var stats map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		// Display statistics
		fmt.Println("ACP Server Statistics")
		fmt.Println("=====================")
		fmt.Printf("Active Sessions:  %v\n", stats["active_sessions"])
		fmt.Printf("Total Requests:   %v\n", stats["total_requests"])
		if uptime, ok := stats["uptime_seconds"].(float64); ok {
			duration := time.Duration(uptime) * time.Second
			fmt.Printf("Uptime:           %s\n", formatDuration(duration))
		}
		if version, ok := stats["version"].(string); ok {
			fmt.Printf("Version:          %s\n", version)
		}

		return nil
	},
}

var acpStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop ACP server",
	Long:  `Stop the running ACP server gracefully.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")

		url := fmt.Sprintf("http://%s:%d/api/acp/shutdown", host, port)

		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to connect to ACP server: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned status %d", resp.StatusCode)
		}

		fmt.Println("✓ ACP server is shutting down...")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(acpCmd)

	// Add subcommands
	acpCmd.AddCommand(acpStartCmd)
	acpCmd.AddCommand(acpStatusCmd)
	acpCmd.AddCommand(acpSessionsCmd)
	acpCmd.AddCommand(acpStatsCmd)
	acpCmd.AddCommand(acpStopCmd)

	// Persistent flags available to all acp subcommands
	acpCmd.PersistentFlags().String("cwd", "", "Working directory (default: current directory)")
	acpCmd.PersistentFlags().Bool("debug", false, "Enable debug logging")
	acpCmd.PersistentFlags().Bool("auto-permission", false, "Automatically approve tool permission requests")

	// Flags for start command (start-specific options)
	acpStartCmd.Flags().String("host", "localhost", "Host to bind to (HTTP transport)")
	acpStartCmd.Flags().Int("port", 8765, "Port to listen on (HTTP transport)")
	acpStartCmd.Flags().String("transport", "stdio", "Transport mode (stdio, http)")
	acpStartCmd.Flags().Int("max-sessions", 10, "Maximum concurrent sessions")
	acpStartCmd.Flags().String("idle-timeout", "30m", "Session idle timeout")

	// Flags for status/sessions/stats/stop commands (connection info)
	for _, cmd := range []*cobra.Command{acpStatusCmd, acpSessionsCmd, acpStatsCmd, acpStopCmd} {
		cmd.Flags().String("host", "localhost", "ACP server host")
		cmd.Flags().Int("port", 8765, "ACP server port")
	}
}

// Helper functions

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
}

func formatTime(t interface{}) string {
	if t == nil {
		return "N/A"
	}

	timeStr, ok := t.(string)
	if !ok {
		return "N/A"
	}

	parsed, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return timeStr
	}

	now := time.Now()
	duration := now.Sub(parsed)

	if duration < time.Minute {
		return "just now"
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm ago", int(duration.Minutes()))
	}
	if duration < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(duration.Hours()))
	}
	return parsed.Format("Jan 2 15:04")
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
