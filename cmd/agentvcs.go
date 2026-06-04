package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/digiogithub/pando/internal/agentvcs"
	"github.com/digiogithub/pando/internal/config"
	"github.com/spf13/cobra"
)

// agentVCSCmd is the top-level "pando agent-vcs" command.
var agentVCSCmd = &cobra.Command{
	Use:     "agent-vcs",
	Aliases: []string{"avcs"},
	Short:   "Inspect and manage the agent-vcs commit history",
	Long: `Agent-VCS is a lightweight, jj-inspired version control system that tracks
file changes across agent sessions. Each session produces a linear chain of
immutable commits, enabling per-session changelogs and the ability to review
or revert incremental work.`,
	Example: `
  # List all sessions with commit history
  pando agent-vcs sessions

  # Show the commit log for a session
  pando agent-vcs log <session-id>

  # Show details and diff for a specific commit
  pando agent-vcs show <commit-id>

  # Revert working directory to a commit's state
  pando agent-vcs revert <commit-id>

  # Compact: keep only the last N sessions (default 50)
  pando agent-vcs compact --keep 20`,
}

// --- sessions ---

var agentVCSSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List all sessions with commit history",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithAgentVCS(func(ctx context.Context, svc agentvcs.Service) error {
			sessions, err := svc.ListSessions(ctx)
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				fmt.Println("No sessions with commits found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SESSION ID\tCOMMITS\tLATEST COMMIT\tDESCRIPTION")
			for _, sid := range sessions {
				log, err := svc.Log(ctx, sid)
				if err != nil || len(log) == 0 {
					continue
				}
				latest := log[len(log)-1]
				ts := time.Unix(latest.CreatedAt, 0).Format("2006-01-02 15:04")
				desc := truncate(latest.Description, 40)
				fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", sid, len(log), ts, desc)
			}
			w.Flush()
			return nil
		})
	},
}

// --- log ---

var agentVCSLogCmd = &cobra.Command{
	Use:   "log <session-id>",
	Short: "Show the commit log for a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := args[0]
		return runWithAgentVCS(func(ctx context.Context, svc agentvcs.Service) error {
			log, err := svc.Log(ctx, sessionID)
			if err != nil {
				return err
			}
			if len(log) == 0 {
				fmt.Printf("No commits found for session %s\n", sessionID)
				return nil
			}

			// Print newest first.
			for i := len(log) - 1; i >= 0; i-- {
				cs := log[i]
				ts := time.Unix(cs.CreatedAt, 0).Format("2006-01-02 15:04:05")

				marker := "●"
				if cs.ParentID == "" {
					marker = "○"
				}

				fmt.Printf("%s %s %s\n", marker, colorYellow(cs.ShortID), ts)
				if cs.Description != "" {
					fmt.Printf("  %s\n", cs.Description)
				}
				if cs.ChangedFiles > 0 {
					fmt.Printf("  %s changed, %s\n",
						colorCyan(fmt.Sprintf("%d files", cs.ChangedFiles)),
						formatSize(cs.ChangedTotalSize),
					)
				} else if cs.ParentID == "" {
					fmt.Printf("  %s changed (initial), %s\n",
						colorCyan(fmt.Sprintf("%d files", cs.ChangedFiles)),
						formatSize(cs.ChangedTotalSize),
					)
				}
				if i > 0 {
					fmt.Println("  │")
				}
			}
			return nil
		})
	},
}

// --- show ---

var agentVCSShowCmd = &cobra.Command{
	Use:   "show <commit-id>",
	Short: "Show details and file diff for a commit",
	Long: `Show the metadata and file-level changes for a specific commit.
You can use a full commit ID or an unambiguous prefix.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		commitID := args[0]
		return runWithAgentVCS(func(ctx context.Context, svc agentvcs.Service) error {
			commit, err := svc.GetCommit(ctx, commitID)
			if err != nil {
				return fmt.Errorf("commit not found: %w", err)
			}

			ts := time.Unix(commit.CreatedAt, 0).Format("2006-01-02 15:04:05")

			fmt.Printf("Commit:    %s\n", colorYellow(commit.ID))
			if commit.ParentID != "" {
				fmt.Printf("Parent:    %s\n", shortStr(commit.ParentID))
			}
			fmt.Printf("Session:   %s\n", commit.SessionID)
			fmt.Printf("Author:    %s\n", commit.Author)
			fmt.Printf("Date:      %s\n", ts)
			fmt.Printf("Files:     %d (%s)\n", commit.FileCount, formatSize(commit.TotalSize))
			if commit.Description != "" {
				fmt.Printf("\n    %s\n", commit.Description)
			}

			diffs, err := svc.DiffFromParent(ctx, commitID)
			if err != nil {
				return nil // no diff available is OK
			}

			if len(diffs) == 0 {
				fmt.Println("\nNo file changes.")
				return nil
			}

			fmt.Printf("\n%s\n", colorBold("Changed files:"))

			var added, modified, deleted int
			for _, d := range diffs {
				var prefix string
				switch d.Type {
				case agentvcs.DiffAdded:
					prefix = colorGreen("A")
					added++
				case agentvcs.DiffModified:
					prefix = colorYellow("M")
					modified++
				case agentvcs.DiffDeleted:
					prefix = colorRed("D")
					deleted++
				}
				sizeInfo := ""
				if d.NewSize > 0 {
					sizeInfo = fmt.Sprintf(" (%s)", formatSize(d.NewSize))
				}
				fmt.Printf("  %s %s%s\n", prefix, d.Path, sizeInfo)
			}

			fmt.Printf("\n%d added, %d modified, %d deleted\n", added, modified, deleted)
			return nil
		})
	},
}

// --- revert ---

var agentVCSRevertCmd = &cobra.Command{
	Use:   "revert <commit-id>",
	Short: "Revert working directory to a commit's state",
	Long: `Restore the working directory to the exact state captured in a commit.
A safety commit is created automatically before reverting so the operation
can be undone.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		commitID := args[0]
		force, _ := cmd.Flags().GetBool("force")

		return runWithAgentVCS(func(ctx context.Context, svc agentvcs.Service) error {
			commit, err := svc.GetCommit(ctx, commitID)
			if err != nil {
				return fmt.Errorf("commit not found: %w", err)
			}

			ts := time.Unix(commit.CreatedAt, 0).Format("2006-01-02 15:04:05")

			if !force {
				fmt.Printf("About to revert working directory to commit:\n")
				fmt.Printf("  ID:      %s\n", colorYellow(shortStr(commit.ID)))
				fmt.Printf("  Date:    %s\n", ts)
				fmt.Printf("  Desc:    %s\n", commit.Description)
				fmt.Printf("  Files:   %d\n", commit.FileCount)
				fmt.Printf("\nA safety commit will be created before reverting.\n")
				fmt.Printf("Continue? [y/N] ")

				var answer string
				fmt.Scanln(&answer)
				if answer != "y" && answer != "Y" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			if err := svc.Revert(ctx, commitID); err != nil {
				return fmt.Errorf("revert failed: %w", err)
			}

			fmt.Printf("Reverted to commit %s\n", colorYellow(shortStr(commitID)))
			return nil
		})
	},
}

// --- compact ---

var agentVCSCompactCmd = &cobra.Command{
	Use:   "compact",
	Short: "Remove old sessions, keeping only the most recent ones",
	Long: `Compact removes old session data and orphan blobs, keeping only the
most recent N sessions (default 50). Use --keep to specify how many sessions
to retain. Use --days to also remove sessions older than N days.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		keep, _ := cmd.Flags().GetInt("keep")
		days, _ := cmd.Flags().GetInt("days")

		return runWithAgentVCS(func(ctx context.Context, svc agentvcs.Service) error {
			// Count before cleanup.
			sessionsBefore, _ := svc.ListSessions(ctx)
			totalBefore := 0
			for _, sid := range sessionsBefore {
				n, _ := svc.SessionCommitCount(ctx, sid)
				totalBefore += n
			}

			if err := svc.Cleanup(ctx, days, keep); err != nil {
				return fmt.Errorf("compact failed: %w", err)
			}

			sessionsAfter, _ := svc.ListSessions(ctx)
			totalAfter := 0
			for _, sid := range sessionsAfter {
				n, _ := svc.SessionCommitCount(ctx, sid)
				totalAfter += n
			}

			removedSessions := len(sessionsBefore) - len(sessionsAfter)
			removedCommits := totalBefore - totalAfter

			fmt.Printf("Compacted: %d sessions removed (%d commits pruned)\n",
				removedSessions, removedCommits)
			fmt.Printf("Remaining: %d sessions, %d commits\n",
				len(sessionsAfter), totalAfter)
			return nil
		})
	},
}

func init() {
	rootCmd.AddCommand(agentVCSCmd)

	agentVCSCmd.AddCommand(agentVCSSessionsCmd)
	agentVCSCmd.AddCommand(agentVCSLogCmd)
	agentVCSCmd.AddCommand(agentVCSShowCmd)
	agentVCSCmd.AddCommand(agentVCSRevertCmd)
	agentVCSCmd.AddCommand(agentVCSCompactCmd)

	agentVCSRevertCmd.Flags().Bool("force", false, "Skip confirmation prompt")

	agentVCSCompactCmd.Flags().Int("keep", 50, "Number of most recent sessions to keep")
	agentVCSCompactCmd.Flags().Int("days", 0, "Remove sessions older than N days (0 = no age limit)")
}

// runWithAgentVCS loads config and creates an agent-vcs Service for the CLI.
func runWithAgentVCS(fn func(ctx context.Context, svc agentvcs.Service) error) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	if _, err := config.Load(cwd, false, ""); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	svc, err := agentvcs.NewService()
	if err != nil {
		return fmt.Errorf("failed to initialize agent-vcs: %w", err)
	}

	return fn(context.Background(), svc)
}

// --- formatting helpers ---

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func shortStr(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// ANSI color helpers (no-op when piped, but simple enough for now).
func colorYellow(s string) string { return "\033[33m" + s + "\033[0m" }
func colorGreen(s string) string  { return "\033[32m" + s + "\033[0m" }
func colorRed(s string) string    { return "\033[31m" + s + "\033[0m" }
func colorCyan(s string) string   { return "\033[36m" + s + "\033[0m" }
func colorBold(s string) string   { return "\033[1m" + s + "\033[0m" }

// isTerminal returns true if stdout is a terminal (for future color gating).
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
