package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc/protocol"
)

var (
	dbCompactIncremental bool
	dbCompactNoAutoVac   bool
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database maintenance commands",
}

var dbCompactCmd = &cobra.Command{
	Use:   "compact",
	Short: "Compact the database (VACUUM) and reclaim free space",
	Long: `Reclaim unused space in the Pando SQLite database with a full VACUUM.

VACUUM rewrites the whole file, so every Pando instance using the database must
be closed first; the command refuses to run while any of them is open.

Under the multi-writer engine (the default on Linux and macOS) the database must
keep auto_vacuum=NONE, so --incremental and auto_vacuum are not available there.
On Windows (stock SQLite engine) --incremental runs PRAGMA incremental_vacuum,
and a full compaction enables auto_vacuum=INCREMENTAL unless --no-auto-vacuum.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err := config.Load(cwd, false, ""); err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		path, err := db.DBPath()
		if err != nil {
			return err
		}
		opts := db.CompactOptions{Incremental: dbCompactIncremental}
		if db.SelectedEngine() != db.EngineMultiwriter {
			opts.EnableAutoVacuum = !dbCompactNoAutoVac
		}
		r, err := db.CompactPath(cmd.Context(), path, opts)
		if err != nil {
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

func printCompactResult(res protocol.DBCompactResult) {
	fmt.Printf("Database compacted (%s).\n  before: %s\n  after:  %s\n  freed:  %s\n",
		res.Mode, humanBytes(res.SizeBefore), humanBytes(res.SizeAfter), humanBytes(res.Freed))
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
	dbCompactCmd.Flags().BoolVar(&dbCompactIncremental, "incremental", false, "only reclaim already-freed pages (stock SQLite engine only)")
	dbCompactCmd.Flags().BoolVar(&dbCompactNoAutoVac, "no-auto-vacuum", false, "do not enable auto_vacuum=INCREMENTAL (stock SQLite engine only)")

	dbCompactAliasCmd.Flags().AddFlagSet(dbCompactCmd.Flags())

	dbCmd.AddCommand(dbCompactCmd)
	rootCmd.AddCommand(dbCmd)
	rootCmd.AddCommand(dbCompactAliasCmd)
}
