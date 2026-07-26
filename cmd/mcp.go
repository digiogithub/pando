package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/mcpauth"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage authentication for configured MCP servers",
	Long: `Inspect and manage authentication for the MCP servers configured under [MCPServers]
in .pando.toml, including the interactive OAuth 2.1 authorization flow for servers whose
Auth.Type is "oauth".`,
}

func init() {
	rootCmd.AddCommand(mcpCmd)

	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpLoginCmd)
	mcpCmd.AddCommand(mcpLogoutCmd)
	mcpCmd.AddCommand(mcpStatusCmd)

	mcpLoginCmd.Flags().Bool("no-browser", false, "Do not open a browser automatically; print the authorization URL instead")
	mcpLoginCmd.Flags().Bool("manual", false, "Paste the redirect URL (or the code) by hand instead of using the local callback server")
	mcpLoginCmd.Flags().Duration("timeout", mcpauth.DefaultCallbackTimeout, "How long to wait for the authorization to complete")
	mcpLoginCmd.Flags().Bool("force", false, "With --manual: skip the confirmation prompt when pasting a bare code, which cannot be validated against Pando's CSRF state")

	mcpLogoutCmd.Flags().Bool("yes", false, "Skip the confirmation prompt")
}

// loadMCPServer loads the config the same lightweight way other cmd/
// commands do (config.Load against the cwd, no app/database bootstrap) and
// returns the named server entry.
func loadMCPServer(cmd *cobra.Command, name string) (config.MCPServer, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return config.MCPServer{}, err
	}
	ageKeys, _ := cmd.Flags().GetString("age-keys")
	config.SetAgeKeysOverride(ageKeys)
	if _, err := config.Load(cwd, false, ""); err != nil {
		return config.MCPServer{}, fmt.Errorf("load config: %w", err)
	}
	cfg := config.Get()
	if cfg == nil {
		return config.MCPServer{}, fmt.Errorf("no configuration loaded")
	}
	srv, ok := cfg.MCPServers[name]
	if !ok {
		return config.MCPServer{}, fmt.Errorf("no MCP server named %q is configured", name)
	}
	return srv, nil
}

var mcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured MCP servers and their authentication status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err := config.Load(cwd, false, ""); err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		cfg := config.Get()
		if cfg == nil || len(cfg.MCPServers) == 0 {
			fmt.Println("No MCP servers configured.")
			return nil
		}

		names := make([]string, 0, len(cfg.MCPServers))
		for name := range cfg.MCPServers {
			names = append(names, name)
		}
		sort.Strings(names)

		mgr := mcpauth.Default()
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tTYPE\tAUTH\tSTATUS")
		for _, name := range names {
			srv := cfg.MCPServers[name]
			status := authStatusLabel(mgr, name, srv)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, srv.Type, srv.Auth.ResolvedType(), status)
		}
		return tw.Flush()
	},
}

// authStatusLabel summarizes one server's status for `pando mcp list`: ok /
// expired / needs login / n/a (non-OAuth servers do not need a login flow).
func authStatusLabel(mgr *mcpauth.Manager, name string, srv config.MCPServer) string {
	if !srv.Auth.IsOAuth() {
		return "n/a"
	}
	info := mgr.Status(name, srv)
	switch {
	case !info.HasTokens:
		return "needs login"
	case info.Expired:
		return "expired (auto-refreshes on use)"
	default:
		return "ok"
	}
}

var mcpStatusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "Show detailed OAuth status for one or all MCP servers",
	Long: `Show detailed OAuth status. With a server name, exits non-zero if that server
still needs authorization, so it can be used as a precondition check in scripts.`,
	Args: cobra.MaximumNArgs(1),
	// The "needs authorization" exit is a normal script-facing outcome, not a
	// misuse of the command: printing the usage block on it is just noise.
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err := config.Load(cwd, false, ""); err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		cfg := config.Get()
		if cfg == nil {
			return fmt.Errorf("no configuration loaded")
		}
		mgr := mcpauth.Default()

		if len(args) == 1 {
			name := args[0]
			srv, ok := cfg.MCPServers[name]
			if !ok {
				return fmt.Errorf("no MCP server named %q is configured", name)
			}
			printStatus(mgr.Status(name, srv))
			if srv.Auth.IsOAuth() && !mgr.HasTokens(name, srv.URL) {
				return fmt.Errorf("server %q needs authorization; run: pando mcp login %s", name, name)
			}
			return nil
		}

		if len(cfg.MCPServers) == 0 {
			fmt.Println("No MCP servers configured.")
			return nil
		}
		names := make([]string, 0, len(cfg.MCPServers))
		for name := range cfg.MCPServers {
			names = append(names, name)
		}
		sort.Strings(names)
		for i, name := range names {
			if i > 0 {
				fmt.Println()
			}
			printStatus(mgr.Status(name, cfg.MCPServers[name]))
		}
		return nil
	},
}

func printStatus(info mcpauth.StatusInfo) {
	fmt.Printf("%s\n", info.ServerName)
	fmt.Printf("  url:  %s\n", info.ServerURL)
	fmt.Printf("  auth: %s\n", info.AuthType)
	if info.AuthType != string(config.MCPAuthOAuth) && info.AuthType != string(config.MCPAuthOAuthClientCredentials) {
		return
	}
	fmt.Printf("  tokens: %v\n", info.HasTokens)
	if info.HasTokens && !info.ExpiresAt.IsZero() {
		fmt.Printf("  expires: %s", info.ExpiresAt.Format(time.RFC3339))
		if info.Expired {
			fmt.Printf(" (expired; will attempt refresh on next use)")
		}
		fmt.Println()
	}
	if info.ClientID != "" {
		registered := "static config"
		if info.DynamicallyRegistered {
			registered = "dynamic client registration"
		}
		fmt.Printf("  client_id: %s (%s)\n", info.ClientID, registered)
	}
}

var mcpLoginCmd = &cobra.Command{
	Use:          "login <name>",
	Short:        "Interactively authorize Pando against an OAuth-protected MCP server",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		srv, err := loadMCPServer(cmd, name)
		if err != nil {
			return err
		}

		if srv.Auth.ResolvedType() == config.MCPAuthOAuthClientCredentials {
			if err := mcpauth.Default().LoginClientCredentials(cmd.Context(), name, srv); err != nil {
				return err
			}
			fmt.Printf("Successfully obtained a client_credentials access token for MCP server %q.\n", name)
			return nil
		}

		noBrowser, _ := cmd.Flags().GetBool("no-browser")
		manual, _ := cmd.Flags().GetBool("manual")
		timeout, _ := cmd.Flags().GetDuration("timeout")
		force, _ := cmd.Flags().GetBool("force")

		prompt := mcpauth.LoginPrompt{
			OnAuthorizationURL: func(u string) {
				fmt.Fprintln(os.Stdout, "Open the following URL to authorize Pando:")
				fmt.Fprintln(os.Stdout, "  "+u)
			},
			OpenBrowser: !noBrowser && !manual,
			OnBrowserError: func(err error) {
				fmt.Fprintf(os.Stderr, "could not open a browser automatically (%v); use the URL above\n", err)
			},
			Timeout: timeout,
		}

		if manual {
			prompt.ManualCode = func(ctx context.Context) (string, string, string, error) {
				fmt.Fprintln(os.Stdout, "After authorizing, paste the full redirect URL (or just the code) here:")
				reader := bufio.NewReader(os.Stdin)
				line, err := reader.ReadString('\n')
				if err != nil {
					return "", "", "", fmt.Errorf("read input: %w", err)
				}
				code, state, iss, wasBareCode, err := parseManualCodeInput(strings.TrimSpace(line))
				if err != nil {
					return "", "", "", err
				}
				if wasBareCode && !force {
					fmt.Fprintln(os.Stderr, "")
					fmt.Fprintln(os.Stderr, "WARNING: a bare authorization code cannot be checked against Pando's CSRF")
					fmt.Fprintln(os.Stderr, "state value the way a full redirect URL can. Proceeding is safe only")
					fmt.Fprintln(os.Stderr, "because you are the one who navigated the authorization URL Pando just")
					fmt.Fprintln(os.Stderr, "printed and are now typing its result back into this same terminal.")
					fmt.Fprint(os.Stderr, "Continue without CSRF state validation? [y/N] ")
					confirmReader := bufio.NewReader(os.Stdin)
					confirmLine, _ := confirmReader.ReadString('\n')
					confirmLine = strings.ToLower(strings.TrimSpace(confirmLine))
					if confirmLine != "y" && confirmLine != "yes" {
						return "", "", "", fmt.Errorf("aborted: paste the full redirect URL instead (it carries state), or re-run with --force to skip this confirmation")
					}
				}
				return code, state, iss, nil
			}
		}

		if err := mcpauth.Default().Login(cmd.Context(), name, srv, prompt); err != nil {
			return err
		}
		fmt.Printf("Successfully authorized MCP server %q.\n", name)
		return nil
	},
}

// parseManualCodeInput accepts either a bare authorization code or a full
// redirect URL (e.g. copy-pasted from the browser address bar when the
// local callback page could not be reached) and extracts (code, state, iss)
// from it.
//
// A full redirect URL is required to carry its state parameter: Pando always
// embeds one in the authorization URL it generates, so any genuine redirect
// coming back from that same request has it too; a URL missing it is either
// a truncated paste or an authorization server dropping state entirely, and
// is rejected rather than silently downgraded to the weaker bare-code path.
// wasBareCode is true only for the deliberately weaker path — the whole
// input didn't parse as a URL with a query string, so it is treated as a
// bare authorization code with no state (and no iss) to validate; callers
// must gate that path on an explicit user confirmation (see mcpLoginCmd's
// --force flag) since it cannot be checked against Pando's CSRF state value.
func parseManualCodeInput(input string) (code string, state string, iss string, wasBareCode bool, err error) {
	if input == "" {
		return "", "", "", false, fmt.Errorf("no input provided")
	}
	if u, perr := url.Parse(input); perr == nil && u.Scheme != "" && u.RawQuery != "" {
		q := u.Query()
		if errCode := q.Get("error"); errCode != "" {
			desc := q.Get("error_description")
			if desc != "" {
				return "", "", "", false, fmt.Errorf("authorization server returned an error: %s: %s", errCode, desc)
			}
			return "", "", "", false, fmt.Errorf("authorization server returned an error: %s", errCode)
		}
		code = q.Get("code")
		state = q.Get("state")
		iss = q.Get("iss")
		if code == "" {
			return "", "", "", false, fmt.Errorf("redirect URL is missing the code parameter")
		}
		if state == "" {
			return "", "", "", false, fmt.Errorf("redirect URL is missing the state parameter; this is required to validate the response against CSRF (see RFC 6749 §10.12) — if the authorization server truly never returns it, paste the bare code instead and confirm the weaker check")
		}
		return code, state, iss, false, nil
	}
	// Not a URL: treat the whole input as the bare code. No state or iss is
	// available on this path.
	return input, "", "", true, nil
}

var mcpLogoutCmd = &cobra.Command{
	Use:          "logout <name>",
	Short:        "Remove stored OAuth tokens and client registration for an MCP server",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if _, err := loadMCPServer(cmd, name); err != nil {
			return err
		}

		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Printf("Remove stored OAuth credentials for %q? [y/N] ", name)
			reader := bufio.NewReader(os.Stdin)
			line, _ := reader.ReadString('\n')
			line = strings.ToLower(strings.TrimSpace(line))
			if line != "y" && line != "yes" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		if err := mcpauth.Default().Logout(name); err != nil {
			return err
		}
		fmt.Printf("Logged out of MCP server %q.\n", name)
		return nil
	},
}
