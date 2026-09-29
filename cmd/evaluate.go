package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/message"
)

var (
	evaluateAll   bool
	evaluateLimit int
	evaluateJudge bool
)

var evaluateCmd = &cobra.Command{
	Use:   "evaluate [session-id]",
	Short: "Score sessions with the self-improvement evaluator",
	Long: `Compute and persist the self-improvement reward for one session, or for
every session that has no score yet.

The reward decomposition (success, efficiency, corrections, tokens) is printed
for each evaluated session. The LLM judge is off unless --judge is given.
Sessions that already have a score are left untouched.

Examples:
  pando evaluate 3f2a9c1e-...            # evaluate one session
  pando evaluate --all --limit 100       # backfill up to 100 unscored sessions`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if evaluateAll == (len(args) == 1) {
			return fmt.Errorf("provide either a session id or --all")
		}
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err := config.Load(cwd, false, ""); err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		conn, err := db.Connect()
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer conn.Close()
		q := db.New(conn)

		cfg := config.EvaluatorWithDefaults(config.Get().Evaluator)
		cfg.Enabled = true
		cfg.Async = false
		if !evaluateJudge {
			cfg.Model = ""
		}
		svc, err := evaluator.New(cfg, q, message.NewService(q))
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		opts := evaluator.EvaluateOptions{SkipJudge: !evaluateJudge}

		if !evaluateAll {
			opts.Force = true
			res, err := svc.EvaluateNow(ctx, args[0], opts)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, res.Summary())
			return nil
		}

		n, err := svc.Sweep(ctx, evaluator.SweepOptions{
			Limit:     evaluateLimit,
			SkipJudge: !evaluateJudge,
			OnResult:  func(r *evaluator.Result) { fmt.Fprintln(out, r.Summary()) },
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%d session(s) evaluated\n", n)
		return nil
	},
}

func init() {
	evaluateCmd.Flags().BoolVar(&evaluateAll, "all", false, "Evaluate every session without a score (oldest first)")
	evaluateCmd.Flags().IntVar(&evaluateLimit, "limit", 50, "Maximum sessions to evaluate with --all")
	evaluateCmd.Flags().BoolVar(&evaluateJudge, "judge", false, "Run the LLM judge (needs evaluator.model configured)")
	rootCmd.AddCommand(evaluateCmd)
}
