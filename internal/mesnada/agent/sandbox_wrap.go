package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/procgroup"
	"github.com/digiogithub/pando/internal/sandbox"
)

// sandboxExtraRoots returns the extra directories bin's own CLI needs
// writable to keep working under the sandbox: its session/auth/config state,
// which normally lives outside the delegated task's workspace (~/.claude,
// ~/.copilot, ...). internal/sandbox has no dedicated "extra roots" hook, so
// wrapSubagentCmd extends a per-call copy of the resolved Policy directly —
// its WritableRoots field is exported for exactly this. bin is the spawned
// executable's base name (filepath.Base), not the engine name.
func sandboxExtraRoots(bin string) []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	switch bin {
	case "claude":
		return []string{filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json")}
	case "copilot":
		return []string{filepath.Join(home, ".copilot"), filepath.Join(home, ".config", "github-copilot")}
	case "gemini":
		return []string{filepath.Join(home, ".gemini")}
	case "opencode":
		return []string{filepath.Join(home, ".config", "opencode"), filepath.Join(home, ".local", "share", "opencode")}
	case "vibe":
		return []string{filepath.Join(home, ".vibe"), filepath.Join(home, ".config", "vibe")}
	case "codex":
		return []string{filepath.Join(home, ".codex")}
	default:
		return nil
	}
}

// subagentSandboxOpts widens the resolved Policy for one spawner call, on
// top of the sandboxExtraRoots(bin) defaults.
type subagentSandboxOpts struct {
	// ExtraRoots are additional writable roots, e.g. the spawner's own
	// logDir, where per-task MCP config and settings files are written.
	ExtraRoots []string
	// KeepEnv are extra glob patterns (Policy.Env.Keep) of variable names to
	// pass through even though ScrubEnv's secret heuristic would otherwise
	// drop them — for a spawner that deliberately sets a credential-shaped
	// variable itself (e.g. routing a CLI at a local Ollama endpoint with a
	// dummy bearer token), not for anything read from the outer environment.
	KeepEnv []string
	// AllowOwnControlPlane un-protects workspace's own .pando/.pando.toml/
	// .pando.json (but not .git/hooks, .git/config, the global config dir or
	// $HOME/.pando.toml, which stay protected). Only for the pando_cli
	// spawner: workspace there is a *nested* Pando instance's own project,
	// and that project's control plane is data the nested instance must
	// read and write for itself, not the outer Pando's own control plane
	// the sandbox exists to protect.
	AllowOwnControlPlane bool
}

// pandoControlPlaneBasenames are the workspace-relative entries
// AllowOwnControlPlane un-protects for a nested Pando's own project. Keep in
// sync with internal/sandbox's own project-level protected paths (workspace
// data/config only; global and git entries are untouched).
var pandoControlPlaneBasenames = map[string]bool{
	".pando":      true,
	".pando.toml": true,
	".pando.json": true,
}

// wrapSubagentCmd applies the sandbox to a mesnada external-agent spawn,
// opt-in via Sandbox.ExtendTo containing PurposeSubagents (sandbox.Covers
// does the check): most deployments run these CLIs unsandboxed, since they
// already sandbox themselves and need broad access to their own state.
//
// When opted in, the policy is resolved for workspace (the task's own
// WorkDir, which may differ from Pando's own working directory) rather than
// sandbox.Current()'s global one, and widened with bin's own config
// directories plus opts so the CLI can still read/write the state it needs.
// A wrap error fails closed: the caller opted in, so a setup problem must
// stop the spawn rather than silently run the sub-agent unconfined.
func wrapSubagentCmd(cmd *exec.Cmd, bin, workspace string, opts subagentSandboxOpts) (sandbox.Policy, sandbox.Capability, error) {
	p := sandbox.Resolve(config.Get(), workspace)
	w := sandbox.Default()
	capability := w.Capability()
	if !p.Covers(sandbox.PurposeSubagents) {
		return p, capability, nil
	}

	roots := append(append([]string(nil), sandboxExtraRoots(bin)...), opts.ExtraRoots...)
	if len(roots) > 0 {
		merged := make([]string, 0, len(p.WritableRoots)+len(roots))
		merged = append(merged, p.WritableRoots...)
		for _, r := range roots {
			if r != "" {
				merged = append(merged, filepath.Clean(r))
			}
		}
		p.WritableRoots = merged
	}
	if len(opts.KeepEnv) > 0 {
		p.Env.Keep = append(append([]string(nil), p.Env.Keep...), opts.KeepEnv...)
	}
	if opts.AllowOwnControlPlane && len(p.ProtectedPaths) > 0 {
		// p.Workspace, not the raw workspace argument: Resolve made it
		// absolute and cleaned it, exactly like every entry in
		// p.ProtectedPaths, so filepath.Dir(pp) == ws compares like with
		// like even when the caller passed a relative WorkDir.
		ws := p.Workspace
		kept := make([]string, 0, len(p.ProtectedPaths))
		for _, pp := range p.ProtectedPaths {
			if filepath.Dir(pp) == ws && pandoControlPlaneBasenames[filepath.Base(pp)] {
				continue
			}
			kept = append(kept, pp)
		}
		p.ProtectedPaths = kept
	}

	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = sandbox.ScrubEnv(cmd.Env, p)
	if err := w.Wrap(cmd, p); err != nil {
		return p, capability, err
	}
	procgroup.Ensure(cmd)
	sandbox.EmitSpawn(context.Background(), sandbox.PurposeSubagents, true, p, capability)
	return p, capability, nil
}

// signalProcessTree signals the process group cmd was started in (see
// procgroup.Ensure, called by wrapSubagentCmd) so a sandbox launcher in
// front of the real CLI, or any children the CLI forked itself, are reached
// too. It falls back to signalling the process alone when group signalling
// is unsupported (Windows) or cmd was never grouped.
func signalProcessTree(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if procgroup.Kill(cmd.Process.Pid, sig) {
		return nil
	}
	return cmd.Process.Signal(sig)
}

// killProcessTree is signalProcessTree with SIGKILL, matching the existing
// call sites' `cmd.Process.Kill()` signature and error handling.
func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if procgroup.Kill(cmd.Process.Pid, syscall.SIGKILL) {
		return nil
	}
	return cmd.Process.Kill()
}
