package mcpclient

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/procgroup"
	"github.com/digiogithub/pando/internal/sandbox"
	"github.com/mark3labs/mcp-go/client/transport"
)

// wrapStdioCommand applies the host sandbox to cmd for an MCP stdio server.
// Unlike sandbox.WrapCmd, which only checks the global policy (ExtendTo
// contains "mcp"), an operator can also opt a single server in with its own
// Sandbox=true, so the two conditions are ORed here: WrapCmd itself has no
// per-call override for that, per the story's design note.
//
// Coverage decided, cmd.Env is scrubbed and the wrapper (real backend, or a
// fake one under SetDefaultForTests) rewrites cmd in place, exactly like
// WrapCmd. An error means enforcement was possible but setup failed; cmd
// must not be started (see sandbox.Wrapper's contract).
func wrapStdioCommand(cmd *exec.Cmd, explicit bool) (sandbox.Policy, sandbox.Capability, error) {
	p := sandbox.Current()
	w := sandbox.Default()
	c := w.Capability()
	if !p.Covers(sandbox.PurposeMCP) && !explicit {
		return p, c, nil
	}
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = sandbox.ScrubEnv(cmd.Env, p)
	if err := w.Wrap(cmd, p); err != nil {
		return p, c, err
	}
	procgroup.Ensure(cmd)
	sandbox.EmitSpawn(context.Background(), sandbox.PurposeMCP, true, p, c)
	return p, c, nil
}

// newSandboxedCommandFunc returns the transport.CommandFunc that builds the
// exec.Cmd for a stdio MCP server (see client/transport.WithCommandFunc):
// mcp-go otherwise builds the command itself (client.NewStdioMCPClient),
// giving us no chance to sandbox it, so New() always installs this factory
// instead and lets it decide. resolved is the server's fully-resolved config
// (post ResolveMCPServerSecrets), so IsXcodeMCPBridge/SandboxExempt see the
// real command/args.
//
// Exemption: a server for which resolved.SandboxExempt() is true (NoSandbox,
// or Xcode's mcpbridge detected automatically) is NEVER wrapped, regardless
// of the global Sandbox.ExtendTo policy or the server's own Sandbox flag —
// plain command, unscrubbed env, same as when the sandbox is off entirely.
// This exists because the SBPL rule hiding the parent process from a sandboxed
// child (internal/sandbox/sbpl.go, `process-info* (target same-sandbox)`)
// breaks Xcode's "Allow 'pando-gateway' to access Xcode?" identity check: it
// can only remember an agent whose path and code signature it can resolve.
//
// Wrap errors (non-exempt path): when the server explicitly asked for the
// sandbox (Sandbox=true), fail closed and never start the process unconfined.
// When coverage came only from the global Sandbox.ExtendTo policy, log and
// fall back to the plain, unwrapped command — the operator did not single
// this server out, so a setup problem here should degrade the same way a
// disabled/unavailable sandbox does elsewhere (fail open with a warning).
func newSandboxedCommandFunc(serverName string, resolved config.MCPServer) transport.CommandFunc {
	return func(ctx context.Context, command string, env []string, args []string) (*exec.Cmd, error) {
		plain := func() *exec.Cmd {
			c := exec.CommandContext(ctx, command, args...)
			c.Env = append(os.Environ(), env...)
			return c
		}

		if resolved.Sandbox && resolved.NoSandbox {
			logging.Warn("sandbox: MCP stdio server sets both Sandbox and NoSandbox; NoSandbox wins, running unconfined",
				"server", serverName)
		}
		if resolved.SandboxExempt() {
			if !resolved.NoSandbox && resolved.IsXcodeMCPBridge() {
				logging.Info("sandbox: running Xcode mcpbridge outside the sandbox so Xcode can identify the agent",
					"server", serverName)
			}
			return plain(), nil
		}

		explicit := resolved.Sandbox
		cmd := plain()
		policy, capability, err := wrapStdioCommand(cmd, explicit)
		if err != nil {
			if explicit {
				return nil, fmt.Errorf("sandbox: cannot start MCP server %q confined: %w", serverName, err)
			}
			logging.Warn("sandbox: failed to wrap MCP stdio server, running unsandboxed",
				"server", serverName, "error", err)
			return plain(), nil
		}
		if (policy.Covers(sandbox.PurposeMCP) || explicit) && !capability.Enforced {
			logging.Debug("sandbox: MCP stdio server requested confinement but the backend is not enforced on this OS",
				"server", serverName, "backend", capability.Backend, "reason", capability.Reason)
		}
		return cmd, nil
	}
}
