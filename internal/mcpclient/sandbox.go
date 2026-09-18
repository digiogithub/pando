package mcpclient

import (
	"context"
	"fmt"
	"os"
	"os/exec"

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
// instead and lets it decide.
//
// Wrap errors: when the server explicitly asked for the sandbox
// (Sandbox=true), fail closed and never start the process unconfined. When
// coverage came only from the global Sandbox.ExtendTo policy, log and fall
// back to the plain, unwrapped command — the operator did not single this
// server out, so a setup problem here should degrade the same way a
// disabled/unavailable sandbox does elsewhere (fail open with a warning).
func newSandboxedCommandFunc(serverName string, explicit bool) transport.CommandFunc {
	return func(ctx context.Context, command string, env []string, args []string) (*exec.Cmd, error) {
		plain := func() *exec.Cmd {
			c := exec.CommandContext(ctx, command, args...)
			c.Env = append(os.Environ(), env...)
			return c
		}

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
