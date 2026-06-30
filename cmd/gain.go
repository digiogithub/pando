package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/savings"
)

var (
	gainJSON  bool
	gainDays  int
	gainPrice float64
)

var gainCmd = &cobra.Command{
	Use:     "gain",
	Aliases: []string{"stats", "savings"},
	Short:   "Report cumulative token savings from context optimization",
	Long: `Aggregate Pando's append-only token-savings ledger and report how many tokens
were saved by the context-optimization features:

  - compressed file reads (view signatures/map/auto modes)
  - unchanged-re-read F-references (dedup)
  - RTK-style shell-output filtering (bash)

The ledger lives at <data-dir>/savings/ledger.jsonl and is written automatically
unless disabled via [TokenOptimization] SavingsLedgerDisabled.

Examples:
  pando gain                 # all-time summary
  pando gain --days 7        # last week only
  pando gain --price 3       # estimate $ saved at $3 / 1M tokens
  pando gain --json          # machine-readable output`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err := config.Load(cwd, false, ""); err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		dataDir := config.Get().Data.Directory
		if dataDir == "" {
			return fmt.Errorf("no data directory configured")
		}

		opts := savings.SummaryOptions{}
		if gainDays > 0 {
			opts.Since = time.Now().AddDate(0, 0, -gainDays)
		}
		rep, err := savings.Summarize(dataDir, opts)
		if err != nil {
			return fmt.Errorf("read savings ledger: %w", err)
		}

		out := cmd.OutOrStdout()
		if gainJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(rep)
		}

		printGainReport(out, rep)
		return nil
	},
}

func printGainReport(out interface{ Write([]byte) (int, error) }, rep savings.Report) {
	w := func(format string, a ...any) { fmt.Fprintf(out, format, a...) }

	w("Token Savings\n")
	w("=============\n\n")
	if rep.Events == 0 {
		w("No savings recorded yet.\n")
		w("\nReads and shell commands populate the ledger as you work; run a few\n")
		w("'view'/'bash' operations through Pando and check again.\n")
		return
	}

	scope := "all time"
	if rep.Since != nil {
		scope = fmt.Sprintf("since %s", rep.Since.Format("2006-01-02"))
	}
	w("Scope:     %s\n", scope)
	w("Events:    %d\n", rep.Events)
	w("Saved:     %d tokens (%.1f%% reduction)\n", rep.SavedTokens, rep.ReductionPct)
	w("Baseline:  %d tokens  →  actual %d tokens\n", rep.BaselineTokens, rep.ActualTokens)
	if gainPrice > 0 {
		w("Estimated: $%.4f saved (at $%.2f / 1M tokens)\n", rep.EstimatedUSD(gainPrice), gainPrice)
	}

	w("\nBy source\n---------\n")
	for _, st := range rep.BySource {
		w("  %-8s %10d saved  (%d events)\n", st.Source, st.Saved, st.Events)
	}

	if len(rep.ByDay) > 0 {
		w("\nBy day\n------\n")
		days := rep.ByDay
		// Show at most the last 14 days for a compact CLI view.
		if len(days) > 14 {
			days = days[len(days)-14:]
		}
		for _, dt := range days {
			w("  %s  %10d saved  (%d events)\n", dt.Day, dt.Saved, dt.Events)
		}
	}
}

func init() {
	gainCmd.Flags().BoolVar(&gainJSON, "json", false, "Output the report as JSON")
	gainCmd.Flags().IntVar(&gainDays, "days", 0, "Only count savings from the last N days (0 = all time)")
	gainCmd.Flags().Float64Var(&gainPrice, "price", 0, "Estimate $ saved at this price per 1M tokens")
	rootCmd.AddCommand(gainCmd)
}
