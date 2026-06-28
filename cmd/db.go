package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
)

var (
	dbCompactIncremental bool
	dbCompactNoAutoVac   bool
)

// dbCompactForwardTimeout bounds how long the CLI waits for a running primary to
// finish a forwarded VACUUM. Matches the secondary-side timeout in the app.
const dbCompactForwardTimeout = 30 * time.Minute

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database maintenance commands",
}

var dbCompactCmd = &cobra.Command{
	Use:   "compact",
	Short: "Compact the database (VACUUM) and reclaim free space",
	Long: `Reclaim unused space in the Pando SQLite database.

By default it runs a full VACUUM and enables auto_vacuum=INCREMENTAL so future
space can be reclaimed cheaply. Use --incremental to only reclaim already-freed
pages without a full rewrite (requires auto_vacuum to already be incremental).

VACUUM needs exclusive access. If another Pando instance is already running for
this directory, the command automatically forwards the request to it over IPC so
the running instance performs the VACUUM on its own writer connection. If no
instance is running, the VACUUM runs in-process.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err := config.Load(cwd, false, ""); err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		params := protocol.DBCompactParams{
			Incremental:      dbCompactIncremental,
			EnableAutoVacuum: !dbCompactNoAutoVac,
		}

		// Prefer forwarding to a running primary: it owns the writer connection, so
		// running our own VACUUM here would only collide with it ("database locked").
		if res, forwarded, err := compactViaRunningInstance(cmd.Context(), cwd, params); forwarded {
			if err != nil {
				return err
			}
			printCompactResult(res)
			return nil
		}

		// No instance running for this directory: VACUUM in-process.
		conn, err := db.Connect()
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer conn.Close()

		r, err := db.Compact(cmd.Context(), conn, db.CompactOptions{
			Incremental:      params.Incremental,
			EnableAutoVacuum: params.EnableAutoVacuum,
		})
		if err != nil {
			if isDBLockedErr(err) {
				return fmt.Errorf("%w\nanother Pando instance may be using the database; stop it or run /db-compact inside the running app", err)
			}
			return err
		}

		printCompactResult(protocol.DBCompactResult{
			Mode: r.Mode, SizeBefore: r.SizeBefore, SizeAfter: r.SizeAfter, Freed: r.Freed,
		})
		return nil
	},
}

// dbCompactAliasCmd exposes the command as `pando db-compact` in addition to
// the grouped `pando db compact`.
var dbCompactAliasCmd = &cobra.Command{
	Use:    "db-compact",
	Short:  "Alias for `pando db compact`",
	Hidden: true,
	RunE:   dbCompactCmd.RunE,
}

// compactViaRunningInstance forwards a db.compact RPC to the primary instance
// holding the IPC lock for workdir, if one is alive. The returned forwarded flag
// reports whether a live instance handled (or attempted) the request: when false
// the caller should compact in-process. A stale lock file (primary dead) is
// treated as "not running" so the caller falls back to a local VACUUM.
func compactViaRunningInstance(ctx context.Context, workdir string, params protocol.DBCompactParams) (protocol.DBCompactResult, bool, error) {
	info, err := ipc.ReadLockForPath(workdir)
	if err != nil || info == nil || info.RPCPort == 0 {
		return protocol.DBCompactResult{}, false, nil
	}

	client, err := ipc.NewClient(ctx)
	if err != nil {
		return protocol.DBCompactResult{}, false, nil
	}
	defer client.Close()

	rpcAddr := fmt.Sprintf("tcp://127.0.0.1:%d", info.RPCPort)

	// Quick liveness probe: a stale lock (no live primary) means we should run
	// locally instead of blocking on a dead endpoint.
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, perr := client.Call(probeCtx, rpcAddr, protocol.MethodInstancePing, nil); perr != nil {
		return protocol.DBCompactResult{}, false, nil
	}

	fmt.Printf("Forwarding compaction to the running Pando instance (pid %d)...\n", info.PID)

	raw, err := client.CallWithTimeout(ctx, rpcAddr, protocol.MethodDBCompact, params, dbCompactForwardTimeout)
	if err != nil {
		if errors.Is(err, ipc.ErrMethodNotFound) {
			return protocol.DBCompactResult{}, true, fmt.Errorf("the running Pando instance is too old to handle db compaction over IPC; please stop it and retry, or upgrade it")
		}
		return protocol.DBCompactResult{}, true, fmt.Errorf("compact via running instance: %w", err)
	}
	var res protocol.DBCompactResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return protocol.DBCompactResult{}, true, fmt.Errorf("decode running instance result: %w", err)
	}
	return res, true, nil
}

func printCompactResult(res protocol.DBCompactResult) {
	fmt.Printf("Database compacted (%s).\n  before: %s\n  after:  %s\n  freed:  %s\n",
		res.Mode, humanBytes(res.SizeBefore), humanBytes(res.SizeAfter), humanBytes(res.Freed))
}

func isDBLockedErr(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "locked") || strings.Contains(s, "busy")
}

func humanBytes(n int64) string {
	neg := ""
	if n < 0 {
		neg = "-"
		n = -n
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%s%d B", neg, n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%s%.1f %cB", neg, float64(n)/float64(div), "KMGTPE"[exp])
}

func init() {
	dbCompactCmd.Flags().BoolVar(&dbCompactIncremental, "incremental", false, "only reclaim already-freed pages (no full rewrite)")
	dbCompactCmd.Flags().BoolVar(&dbCompactNoAutoVac, "no-auto-vacuum", false, "do not enable auto_vacuum=INCREMENTAL")

	dbCompactAliasCmd.Flags().AddFlagSet(dbCompactCmd.Flags())

	dbCmd.AddCommand(dbCompactCmd)
	rootCmd.AddCommand(dbCmd)
	rootCmd.AddCommand(dbCompactAliasCmd)
}
