package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/llm/tools/outputfilter"
)

var filterCmd = &cobra.Command{
	Use:   "filter",
	Short: "Inspect and test output-compression filters",
	Long: `Work with the RTK-style output-compression filters that shrink verbose
command output before it reaches the model.

See 'pando filter test --help' to validate a .pando/filters.toml authoring file.`,
}

var filterTestCmd = &cobra.Command{
	Use:   "test [file]",
	Short: "Run the inline [[tests]] declared in a filter TOML file",
	Long: `Run the inline self-tests embedded in an output-filter TOML file.

Each [filters.<name>] block can declare one or more [[filters.<name>.tests]]
cases with an 'input' and the 'expected' compressed output. This command feeds
every input through its filter's pipeline and reports pass/fail, so you can
author a project-local .pando/filters.toml with confidence.

With no argument it runs the tests embedded in Pando's built-in default filters.

Examples:
  pando filter test .pando/filters.toml
  pando filter test            # validate the built-in defaults`,
	Args: cobra.MaximumNArgs(1),
	// A failed test case is a legitimate result, not command misuse — don't
	// dump usage on it.
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			eng    *outputfilter.Engine
			err    error
			source string
		)
		if len(args) == 1 {
			source = args[0]
			data, readErr := os.ReadFile(source)
			if readErr != nil {
				return fmt.Errorf("filter test: cannot read %q: %w", source, readErr)
			}
			eng, err = outputfilter.LoadFileFilters(source, data)
		} else {
			source = "built-in defaults"
			eng, err = outputfilter.LoadDefaults()
		}
		if err != nil {
			// Non-fatal: some filters may have failed to compile; report and
			// still run the tests for the ones that did.
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
		}

		results := eng.RunInlineTests()
		out := cmd.OutOrStdout()
		if len(results) == 0 {
			fmt.Fprintf(out, "No inline [[tests]] found in %s.\n", source)
			return nil
		}

		passed, failed := 0, 0
		for _, r := range results {
			name := r.Test
			if name == "" {
				name = "(unnamed)"
			}
			if r.Passed {
				passed++
				fmt.Fprintf(out, "  PASS  %s · %s\n", r.Filter, name)
				continue
			}
			failed++
			fmt.Fprintf(out, "  FAIL  %s · %s\n", r.Filter, name)
			if r.Err != "" {
				fmt.Fprintf(out, "        %s\n", r.Err)
			}
			fmt.Fprintf(out, "        --- got ---\n%s\n        --- want ---\n%s\n",
				indent(r.Got), indent(r.Expected))
		}

		fmt.Fprintf(out, "\n%d passed, %d failed (%d cases in %s)\n",
			passed, failed, len(results), source)
		if failed > 0 {
			return fmt.Errorf("%d filter test(s) failed", failed)
		}
		return nil
	},
}

// indent prefixes every line with 8 spaces so multi-line got/want blocks stay
// visually nested under their failing case.
func indent(s string) string {
	if s == "" {
		return "        (empty)"
	}
	var b []byte
	b = append(b, []byte("        ")...)
	for i := 0; i < len(s); i++ {
		b = append(b, s[i])
		if s[i] == '\n' && i != len(s)-1 {
			b = append(b, []byte("        ")...)
		}
	}
	return string(b)
}

func init() {
	filterCmd.AddCommand(filterTestCmd)
	rootCmd.AddCommand(filterCmd)
}
