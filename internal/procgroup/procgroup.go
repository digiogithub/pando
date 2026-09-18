// Package procgroup gives the spawn sites that wrap a command with the host
// sandbox (internal/sandbox) a small, OS-specific way to stop the whole
// process tree they start, not just the direct child pid.
//
// A sandboxed long-running child (an ACP terminal, an MCP stdio server, a
// mesnada sub-agent CLI) may actually be a launcher in front of the real
// program: bubblewrap on Linux, or a shell script a template/ACP agent
// config points at. Signalling only the direct pid can leave the real work
// running, or leave grandchildren behind once the launcher exits. Putting
// the child in its own process group (Ensure, called before Start) and
// signalling that group (Kill) reaches the launcher and everything it
// spawned in one call, the same pattern internal/llm/tools/shell already
// uses for the persistent shell.
//
// Ensure is safe to call unconditionally, whether or not the command ends up
// wrapped by the sandbox: grouping is a general cleanup improvement, not a
// sandbox-only concern (a plain shell script can spawn children too).
package procgroup
