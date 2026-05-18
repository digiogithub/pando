// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/digiogithub/pando/internal/instanceregistry"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/spf13/cobra"
)

var ipcCmd = &cobra.Command{
	Use:   "ipc",
	Short: "IPC topology commands",
	Long: `Commands for inspecting the IPC topology of running Pando instances.

Pando uses a single-writer SQLite model: the first instance to start for a
working directory acquires the write lock (primary). All subsequent instances
for the same directory connect as secondaries and proxy writes to the primary.`,
}

// ipcStatusCmd shows the IPC status of the current working directory.
var ipcStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show IPC status of the current working directory",
	Example: `
  # Show IPC status for the current directory
  pando ipc status

  # Show IPC status for a specific path
  pando ipc status --path /path/to/project`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("path")
		if path == "" {
			var err error
			path, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get working directory: %w", err)
			}
		}

		pubPort, rpcPort := ipc.PortsForPath(path)

		// Determine whether a primary is active by reading the lock file.
		lockInfo, lockErr := ipc.ReadLockForPath(path)

		reg := instanceregistry.New()
		entries, _ := reg.ListByPath(path)

		// Find primary entry from registry if available.
		var primaryEntry *instanceregistry.Entry
		for _, e := range entries {
			if e.IsPrimary {
				primaryEntry = e
				break
			}
		}

		fmt.Println("IPC Status")
		fmt.Println("----------")
		fmt.Printf("Workdir:   %s\n", path)
		fmt.Printf("PUB port:  %d\n", pubPort)
		fmt.Printf("RPC port:  %d\n", rpcPort)
		fmt.Println()

		if lockErr != nil || lockInfo == nil {
			fmt.Println("Lock:      no active primary (lock file absent or unreadable)")
		} else {
			fmt.Printf("Lock:      held by instance %s (PID %d)\n", lockInfo.InstanceID, lockInfo.PID)
			fmt.Printf("  PUB port: %d\n", lockInfo.PubPort)
			fmt.Printf("  RPC port: %d\n", lockInfo.RPCPort)
			fmt.Printf("  Started:  %s\n", lockInfo.StartedAt.Format(time.RFC3339))
		}

		if primaryEntry != nil {
			fmt.Printf("\nPrimary instance (registry):\n")
			fmt.Printf("  Instance ID: %s\n", primaryEntry.InstanceID)
			fmt.Printf("  PID:         %d\n", primaryEntry.PID)
			fmt.Printf("  Mode:        %s\n", primaryEntry.Mode)
		}

		if len(entries) > 0 {
			fmt.Printf("\nKnown instances for this path: %d\n", len(entries))
		}

		return nil
	},
}

// ipcInstancesCmd lists all live instances for a given path.
var ipcInstancesCmd = &cobra.Command{
	Use:   "instances",
	Short: "List all running instances for a path",
	Example: `
  # List instances for the current directory
  pando ipc instances

  # List instances for a specific path
  pando ipc instances --path /path/to/project`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("path")
		if path == "" {
			var err error
			path, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get working directory: %w", err)
			}
		}

		reg := instanceregistry.New()
		entries, err := reg.ListByPath(path)
		if err != nil {
			return fmt.Errorf("failed to list instances: %w", err)
		}

		if len(entries) == 0 {
			fmt.Printf("No running Pando instances found for %s\n", path)
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "INSTANCE ID\tROLE\tPID\tMODE\tSTARTED")
		for _, e := range entries {
			role := "secondary"
			if e.IsPrimary {
				role = "primary"
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n",
				e.InstanceID,
				role,
				e.PID,
				e.Mode,
				e.StartedAt.Format(time.RFC3339),
			)
		}
		_ = w.Flush()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(ipcCmd)

	ipcCmd.AddCommand(ipcStatusCmd)
	ipcCmd.AddCommand(ipcInstancesCmd)

	// Both subcommands accept an optional --path flag.
	ipcStatusCmd.Flags().String("path", "", "Working directory to inspect (defaults to current directory)")
	ipcInstancesCmd.Flags().String("path", "", "Working directory to inspect (defaults to current directory)")
}
