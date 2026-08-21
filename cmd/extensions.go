package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/extensions"
	"github.com/digiogithub/pando/internal/version"
	"github.com/digiogithub/pando/pkg/extension"
)

var extensionsJSON bool

var extensionsCmd = &cobra.Command{
	Use:   "extensions",
	Short: "Inspect the extensions compiled into this binary",
	Long: `Extensions are Go packages linked into the Pando binary at build time.

Which extensions a binary contains is decided when it is built, not at runtime:
configuration under [Extensions] only chooses which of the compiled-in ones are
loaded and how they are configured. A binary that was not built with an
extension cannot enable it.`,
}

var extensionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List compiled-in extensions and whether they loaded",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err := config.Load(cwd, false, ""); err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		mgr := extensions.NewManager(extensions.Options{})
		// Errors are per-extension and already recorded in the statuses below.
		_ = mgr.Load(context.Background())
		defer mgr.Cleanup()

		statuses := mgr.Statuses()

		if extensionsJSON {
			type row struct {
				ID          string `json:"id"`
				Name        string `json:"name,omitempty"`
				Description string `json:"description,omitempty"`
				Version     string `json:"version,omitempty"`
				Author      string `json:"author,omitempty"`
				License     string `json:"license,omitempty"`
				State       string `json:"state"`
				Error       string `json:"error,omitempty"`
			}
			rows := make([]row, 0, len(statuses))
			for _, st := range statuses {
				r := row{
					ID:          string(st.Info.ID),
					Name:        st.Info.Name,
					Description: st.Info.Description,
					Version:     st.Info.Version,
					Author:      st.Info.Author,
					License:     string(st.Info.License),
					State:       stateOf(st),
				}
				if st.Err != nil {
					r.Error = st.Err.Error()
				}
				rows = append(rows, r)
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		}

		variant := version.Variant
		if variant == "" {
			variant = "standard"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Pando %s (%s build)\n\n", version.Version, variant)

		if len(statuses) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No extensions are compiled into this binary.")
			return nil
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tVERSION\tLICENSE\tSTATE\tDESCRIPTION")
		for _, st := range statuses {
			desc := st.Info.Description
			if st.Err != nil {
				desc = st.Err.Error()
			}
			lic := st.Info.License
			if lic == "" {
				lic = extension.LicenseMIT
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", st.Info.ID, st.Info.Version, lic, stateOf(st), desc)
		}
		return w.Flush()
	},
}

func stateOf(st extension.Status) string {
	switch {
	case st.Err != nil:
		return "error"
	case st.Loaded:
		return "loaded"
	case st.Disabled:
		return "disabled"
	default:
		return "registered"
	}
}

// extCmd is where extension-contributed subcommands are mounted. Keeping them
// under one namespace means an extension can never shadow a core command, and
// `pando ext --help` is an exact list of what this build added.
var extCmd = &cobra.Command{
	Use:   "ext",
	Short: "Run commands contributed by compiled-in extensions",
	Long: `Subcommands here come from the extensions compiled into this binary.

A standard build has none. Run "pando extensions list" to see which extensions
are present and whether configuration enabled them.`,
}

func init() {
	extensionsListCmd.Flags().BoolVar(&extensionsJSON, "json", false, "Output as JSON")
	extensionsCmd.AddCommand(extensionsListCmd)
	rootCmd.AddCommand(extensionsCmd)

	// Building the command tree only instantiates extensions, it does not
	// provision them or read configuration: cobra must be able to print help
	// without starting Pando. Whether an extension is enabled is resolved when
	// a command actually runs.
	if subs := extensions.Commands(); len(subs) > 0 {
		extCmd.AddCommand(subs...)
		rootCmd.AddCommand(extCmd)
	}
}
