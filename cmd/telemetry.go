package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/telemetry"
)

// telemetryJSON is shared by "pando telemetry" and "pando telemetry status":
// it is registered as a persistent flag on the parent command so both spellings
// accept --json.
var telemetryJSON bool

var telemetryCmd = &cobra.Command{
	Use:   "telemetry",
	Short: "Manage opt-in remote diagnostics (logs shipped to Better Stack)",
	Long: `Remote telemetry ships Pando's logs as JSON to a Better Stack source so a
user who hits a problem can share diagnostics with the maintainers. It is OFF
by default and nothing is ever sent unless you enable it here (or from
TUI Settings > General, or WebUI Settings > General).

Only redacted, anonymous data is sent: no secrets, no session request/response
bodies, and any $HOME path is rewritten to "~". Every shipped record carries a
random 16-digit debug ID — not tied to your identity or machine — that you can
quote in a bug report so a maintainer can find your records.

With no subcommand this prints the current status (same as "status").`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runTelemetryStatus(cmd)
	},
}

var telemetryStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether remote telemetry is enabled and available",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runTelemetryStatus(cmd)
	},
}

var telemetryEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Turn on remote telemetry (generates a debug ID on first enable)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runTelemetryEnable(cmd)
	},
}

var telemetryDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Turn off remote telemetry (keeps the debug ID for next time)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runTelemetryDisable(cmd)
	},
}

var telemetryIDCmd = &cobra.Command{
	Use:   "id",
	Short: "Print only the debug ID (script-friendly; fails if none exists yet)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runTelemetryID(cmd)
	},
}

var telemetryRegenerateCmd = &cobra.Command{
	Use:   "regenerate",
	Short: "Replace the debug ID with a freshly generated one",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runTelemetryRegenerate(cmd)
	},
}

var telemetryLevelCmd = &cobra.Command{
	Use:       "level <debug|info|warn|error>",
	Short:     "Set the minimum level of log record that is shipped",
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{config.TelemetryLevelDebug, config.TelemetryLevelInfo, config.TelemetryLevelWarn, config.TelemetryLevelError},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTelemetryLevel(cmd, args[0])
	},
}

func init() {
	telemetryCmd.PersistentFlags().BoolVar(&telemetryJSON, "json", false, "print status as JSON")

	telemetryCmd.AddCommand(
		telemetryStatusCmd,
		telemetryEnableCmd,
		telemetryDisableCmd,
		telemetryIDCmd,
		telemetryRegenerateCmd,
		telemetryLevelCmd,
	)
	// Cobra reads SilenceUsage off the command that actually failed, not off
	// its parent — see the note next to silenceUsage in design.go.
	silenceUsage(telemetryCmd)
	rootCmd.AddCommand(telemetryCmd)
}

// loadTelemetryConfig loads the configuration the same lightweight way other
// headless subcommands do (e.g. "pando cronjob list"): no TUI, no app/DB
// startup. Telemetry itself is a GLOBAL-only setting (internal/config's
// UpdateTelemetry/RegenerateTelemetryID/UpdateTelemetryMinLevel always persist
// to the profile-level config file, never a project-local one), but reading
// still goes through the normal config.Load so a project-local .pando.toml's
// other settings are respected for anything that shares this process.
func loadTelemetryConfig() (*config.Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	cfg, err := config.Load(cwd, false)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

// telemetryStatusView is the --json shape for "pando telemetry status".
type telemetryStatusView struct {
	Available    bool   `json:"available"`
	Enabled      bool   `json:"enabled"`
	DebugID      string `json:"debugId,omitempty"`
	MinLevel     string `json:"minLevel"`
	EndpointHost string `json:"endpointHost"`
}

func runTelemetryStatus(cmd *cobra.Command) error {
	cfg, err := loadTelemetryConfig()
	if err != nil {
		return err
	}

	view := telemetryStatusView{
		Available:    telemetry.Available(),
		Enabled:      cfg.Telemetry.Enabled,
		DebugID:      config.TelemetryDebugIDDisplay(),
		MinLevel:     cfg.Telemetry.MinLevel,
		EndpointHost: telemetryEndpointHost(),
	}

	if telemetryJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Remote telemetry:")
	fmt.Fprintf(out, "  available:  %s\n", yesNo(view.Available))
	fmt.Fprintf(out, "  enabled:    %s\n", yesNo(view.Enabled))
	debugID := view.DebugID
	if debugID == "" {
		debugID = `(none yet — run "pando telemetry enable")`
	}
	fmt.Fprintf(out, "  debug id:   %s\n", debugID)
	fmt.Fprintf(out, "  min level:  %s\n", view.MinLevel)
	fmt.Fprintf(out, "  endpoint:   %s\n", view.EndpointHost)
	if !view.Available {
		fmt.Fprintln(out, "\nUnavailable in this build: no Better Stack ingest token was linked in.")
		fmt.Fprintln(out, "Maintainers: build with PANDO_BETTERSTACK_TOKEN, or set PANDO_TELEMETRY_TOKEN")
		fmt.Fprintln(out, "(and optionally PANDO_TELEMETRY_ENDPOINT) to point at a self-hosted sink.")
	}
	return nil
}

// telemetryEndpointHost returns only the host of the ingest endpoint (never
// the token, which is not part of the URL anyway — it travels in the
// Authorization header).
func telemetryEndpointHost() string {
	endpoint := telemetry.Endpoint()
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint
	}
	return u.Host
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func runTelemetryEnable(cmd *cobra.Command) error {
	if _, err := loadTelemetryConfig(); err != nil {
		return err
	}
	if !telemetry.Available() {
		return fmt.Errorf(`remote telemetry is not available in this build: no Better Stack ingest token is linked in.
Maintainers: build with PANDO_BETTERSTACK_TOKEN=$(kvage get pando_betterstack_token) make build,
or set PANDO_TELEMETRY_TOKEN (and optionally PANDO_TELEMETRY_ENDPOINT) to point at a self-hosted sink`)
	}

	id, err := config.UpdateTelemetry(true)
	if err != nil {
		return fmt.Errorf("enable telemetry: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Remote telemetry enabled.")
	fmt.Fprintf(cmd.OutOrStdout(), "Debug ID: %s\n", telemetry.FormatDebugID(id))
	return nil
}

func runTelemetryDisable(cmd *cobra.Command) error {
	if _, err := loadTelemetryConfig(); err != nil {
		return err
	}
	if _, err := config.UpdateTelemetry(false); err != nil {
		return fmt.Errorf("disable telemetry: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Remote telemetry disabled.")
	return nil
}

func runTelemetryID(cmd *cobra.Command) error {
	cfg, err := loadTelemetryConfig()
	if err != nil {
		return err
	}
	if cfg.Telemetry.DebugID == "" {
		return fmt.Errorf(`no telemetry debug ID yet: run "pando telemetry enable" first`)
	}
	fmt.Fprintln(cmd.OutOrStdout(), telemetry.FormatDebugID(cfg.Telemetry.DebugID))
	return nil
}

func runTelemetryRegenerate(cmd *cobra.Command) error {
	if _, err := loadTelemetryConfig(); err != nil {
		return err
	}
	if !telemetry.Available() {
		return fmt.Errorf("remote telemetry is not available in this build: no Better Stack ingest token is linked in")
	}
	id, err := config.RegenerateTelemetryID()
	if err != nil {
		return fmt.Errorf("regenerate telemetry debug id: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "New debug ID: %s\n", telemetry.FormatDebugID(id))
	return nil
}

func runTelemetryLevel(cmd *cobra.Command, level string) error {
	if _, err := loadTelemetryConfig(); err != nil {
		return err
	}
	if err := config.UpdateTelemetryMinLevel(level); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Telemetry minimum level set to %s.\n", strings.ToLower(strings.TrimSpace(level)))
	return nil
}
