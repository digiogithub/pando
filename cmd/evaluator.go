package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/evaluator"
)

var evaluatorDoctorJSON bool

var evaluatorCmd = &cobra.Command{
	Use:   "evaluator",
	Short: "Inspect the self-improvement evaluator",
	Long: `Tools to check the self-improvement loop. To score sessions use "pando evaluate";
to review learned skills use "pando skills".`,
}

var evaluatorDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Explain whether the self-improvement loop is working, and why not",
	Long: `Reports whether the evaluator is enabled (and why not), how many sessions were
never evaluated, the last evaluation and its error, the LLM judge budget for
today, the prompt variant directories and sections, the learned skills by
review state, and lints the correction/task patterns (patterns that do not
compile, or contain a literal double backslash that never matches what was
meant). It only reads; it does not evaluate anything.

Examples:
  pando evaluator doctor
  pando evaluator doctor --json`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.Load(cwd, false, "")
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		conn, err := db.Connect()
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer conn.Close()

		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		rep, err := evaluator.Diagnose(ctx, evaluator.DiagnoseOptions{
			Config:  cfg.Evaluator,
			DB:      db.New(conn),
			WorkDir: cfg.WorkingDir,
		})
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if evaluatorDoctorJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(rep)
		}
		fmt.Fprint(out, rep.Text())
		return nil
	},
}

func init() {
	evaluatorDoctorCmd.Flags().BoolVar(&evaluatorDoctorJSON, "json", false, "Print the report as JSON")
	evaluatorCmd.AddCommand(evaluatorDoctorCmd)
	rootCmd.AddCommand(evaluatorCmd)
}
