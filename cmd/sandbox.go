package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/sandbox"
)

// sandboxStatusJSON is shared by "pando sandbox" and "pando sandbox status":
// registered as a persistent flag on the parent command so both spellings
// accept --json, the same convention as "pando telemetry".
var sandboxStatusJSON bool

var sandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "Inspect and exercise the host command sandbox",
	Long: `The host command sandbox confines commands Pando spawns on the agent's
behalf (the bash tool's persistent shell, and — depending on config — ACP
terminals, skill CLI tools, MCP servers and subagents) to a filesystem,
network and environment policy.

With no subcommand this prints the current status (same as "status").`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runSandboxStatus(cmd)
	},
}

var sandboxStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the resolved sandbox policy and backend capability",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runSandboxStatus(cmd)
	},
}

var sandboxExecCmd = &cobra.Command{
	Use:                   "exec -- <command> [args...]",
	Short:                 "Run a command through the sandbox (manual testing)",
	DisableFlagsInUseLine: true,
	Long: `Runs <command> wrapped exactly the way the bash tool's persistent shell
would wrap it (sandbox.WrapCmd with the bash purpose): the currently
resolved policy, enforced by whichever backend this OS supports. Stdio is
streamed straight through and the child's exit code becomes pando's own.

Useful for checking what the sandbox actually allows without going through
the agent, e.g. from inside a project workspace:

  pando sandbox exec -- sh -c 'touch ./probe && rm ./probe'   # inside the workspace: OK
  pando sandbox exec -- sh -c 'touch "$HOME/.probe"'          # outside it: denied

Put "--" before the command so its own flags are not parsed by "pando".`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSandboxExec(cmd, args)
	},
}

func init() {
	sandboxCmd.PersistentFlags().BoolVar(&sandboxStatusJSON, "json", false, "print status as JSON")

	sandboxCmd.AddCommand(sandboxStatusCmd, sandboxExecCmd)
	// Cobra reads SilenceUsage off the command that actually failed, not off
	// its parent — see the note next to silenceUsage in design.go.
	silenceUsage(sandboxCmd)
	rootCmd.AddCommand(sandboxCmd)
}

// loadSandboxConfig loads the configuration the same lightweight way other
// headless subcommands do (e.g. "pando telemetry status", "pando gain"): no
// TUI, no app/DB startup. Sandbox status/exec only ever reads the resolved
// policy, so a project-local .pando.toml/.pando.json's sandbox tightening is
// honored the same as it would be for a real bash command.
func loadSandboxConfig() (*config.Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	cfg, err := config.Load(cwd, false, "")
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

// sandboxStatusView is the --json shape for "pando sandbox status": the
// fields a debugging session or a bug report needs — which backend is in
// play, whether it is really enforced, and the effective policy.
type sandboxStatusView struct {
	// Backend/Version/Enforced/Reason describe the capability probe
	// (sandbox.Capability): which mechanism this OS offers and whether it
	// actually confines a child here.
	Backend  string `json:"backend"`
	Version  string `json:"version,omitempty"`
	Enforced bool   `json:"enforced"`
	Reason   string `json:"reason,omitempty"`
	// Active: the policy is enabled AND the backend enforces it.
	Active bool `json:"active"`
	// Full: Active and no gaps (sandbox.Guarantees); only then does bash
	// skip its permission prompt. Gaps lists what is missing otherwise.
	Full bool     `json:"full"`
	Gaps []string `json:"gaps,omitempty"`
	// Label is the badge text, e.g. "workspace-write (bwrap+landlock v6)".
	Label string `json:"label"`

	Mode    string `json:"mode"`
	Network string `json:"network"`
	// Source is where the mode came from: default, config, env or lock.
	Source    string `json:"source"`
	Workspace string `json:"workspace"`

	WritableRoots  []string `json:"writableRoots,omitempty"`
	ReadableRoots  []string `json:"readableRoots,omitempty"`
	ProtectedPaths []string `json:"protectedPaths,omitempty"`
	DenyPaths      []string `json:"denyPaths,omitempty"`
	ExtendTo       []string `json:"extendTo,omitempty"`
	AutoAllowBash  bool     `json:"autoAllowBash"`
	UseBwrap       string   `json:"useBwrap"`
	// GuardedPorts are Pando's own TCP ports (this process's and other live
	// instances') that confined commands cannot connect to.
	GuardedPorts []int `json:"guardedPorts,omitempty"`
	// AutoAllowEffective: bash really skips its prompt (auto-allow on and
	// full guarantees).
	AutoAllowEffective bool `json:"autoAllowEffective"`

	// Hash is Policy.Hash(): two policies with the same hash confine a
	// child identically.
	Hash string `json:"policyHash"`
}

func buildSandboxStatusView() sandboxStatusView {
	status := sandbox.CurrentStatus()
	p := status.Policy
	extend := make([]string, 0, len(p.ExtendTo))
	for _, purpose := range p.ExtendTo {
		extend = append(extend, string(purpose))
	}
	return sandboxStatusView{
		Backend:            status.Capability.Backend,
		Version:            status.Capability.Version,
		Enforced:           status.Capability.Enforced,
		Reason:             status.Capability.Reason,
		Active:             status.Active,
		Full:               status.Full,
		Gaps:               status.Gaps,
		Label:              status.Label(),
		GuardedPorts:       p.DenyConnectPorts,
		AutoAllowEffective: status.Full && p.AutoAllowBash,
		Mode:               string(p.Mode),
		Network:            string(p.Network),
		Source:             string(p.Source),
		Workspace:          p.Workspace,
		WritableRoots:      p.WritableRoots,
		ReadableRoots:      p.ReadableRoots,
		ProtectedPaths:     p.ProtectedPaths,
		DenyPaths:          p.DenyPaths,
		ExtendTo:           extend,
		AutoAllowBash:      p.AutoAllowBash,
		UseBwrap:           string(p.UseBwrap),
		Hash:               status.Hash,
	}
}

func runSandboxStatus(cmd *cobra.Command) error {
	if _, err := loadSandboxConfig(); err != nil {
		return err
	}
	view := buildSandboxStatusView()

	out := cmd.OutOrStdout()
	if sandboxStatusJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}

	fmt.Fprintln(out, "Sandbox:")
	fmt.Fprintf(out, "  backend:        %s\n", nonEmpty(view.Backend, "none"))
	fmt.Fprintf(out, "  version:        %s\n", nonEmpty(view.Version, "(unknown)"))
	fmt.Fprintf(out, "  enforced:       %s\n", yesNo(view.Enforced))
	if view.Reason != "" {
		fmt.Fprintf(out, "  reason:         %s\n", view.Reason)
	}
	fmt.Fprintf(out, "  active:         %s\n", yesNo(view.Active))
	if view.Active {
		if view.Full {
			fmt.Fprintf(out, "  protection:     full\n")
		} else {
			fmt.Fprintf(out, "  protection:     partial: %s\n", strings.Join(view.Gaps, "; "))
		}
	}
	fmt.Fprintf(out, "  mode:           %s\n", view.Mode)
	fmt.Fprintf(out, "  network:        %s\n", view.Network)
	fmt.Fprintf(out, "  source:         %s\n", view.Source)
	fmt.Fprintf(out, "  workspace:      %s\n", view.Workspace)
	fmt.Fprintf(out, "  writable roots: %s\n", formatPathList(view.WritableRoots))
	fmt.Fprintf(out, "  readable roots: %s\n", formatPathList(view.ReadableRoots))
	fmt.Fprintf(out, "  protected:      %s\n", formatPathList(view.ProtectedPaths))
	fmt.Fprintf(out, "  deny paths:     %s\n", formatPathList(view.DenyPaths))
	fmt.Fprintf(out, "  extend to:      %s\n", formatPathList(view.ExtendTo))
	fmt.Fprintf(out, "  guarded ports:  %s\n", formatPorts(view.GuardedPorts))
	autoAllow := yesNo(view.AutoAllowEffective)
	if view.AutoAllowBash && !view.AutoAllowEffective && view.Active {
		autoAllow += " (configured, but the sandbox is partial: bash keeps prompting)"
	}
	fmt.Fprintf(out, "  auto-allow:     %s\n", autoAllow)
	fmt.Fprintf(out, "  use bwrap:      %s\n", view.UseBwrap)
	fmt.Fprintf(out, "  policy hash:    %s\n", view.Hash)
	return nil
}

func formatPorts(ports []int) string {
	if len(ports) == 0 {
		return "(none)"
	}
	parts := make([]string, len(ports))
	for i, port := range ports {
		parts[i] = strconv.Itoa(port)
	}
	return strings.Join(parts, ", ")
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func formatPathList(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ", "
		}
		out += item
	}
	return out
}

// runSandboxExec runs args[0] (with args[1:]) through sandbox.WrapCmd under
// PurposeBash — the same wrap a bash-tool command gets — with stdio streamed
// straight through, and propagates the child's exit code as pando's own
// (via cliExitCode; see cmd/root.go's Execute).
func runSandboxExec(cmd *cobra.Command, args []string) error {
	if _, err := loadSandboxConfig(); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	child := exec.Command(args[0], args[1:]...)
	child.Dir = cwd
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	policy, capability, err := sandbox.WrapCmd(child, sandbox.PurposeBash)
	if err != nil {
		return fmt.Errorf("sandbox: cannot wrap command: %w", err)
	}
	errOut := cmd.ErrOrStderr()
	switch {
	case !policy.Enabled():
		fmt.Fprintln(errOut, "sandbox: policy is off (mode=off); running unconfined")
	case !capability.Enforced:
		fmt.Fprintf(errOut, "sandbox: backend %q not enforced on this OS (%s); running unconfined\n",
			capability.Backend, nonEmpty(capability.Reason, "unavailable"))
	default:
		msg := fmt.Sprintf("sandbox: running confined (mode=%s, backend=%s)", policy.Mode, capability.String())
		if full, gaps := sandbox.Guarantees(policy, capability); !full {
			msg += "; partial: " + strings.Join(gaps, "; ")
		}
		fmt.Fprintln(errOut, msg)
	}

	if err := child.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}
	waitErr := child.Wait()
	if waitErr == nil {
		cliExitCode = 0
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		cliExitCode = exitErr.ExitCode()
		return nil
	}
	return fmt.Errorf("run command: %w", waitErr)
}
