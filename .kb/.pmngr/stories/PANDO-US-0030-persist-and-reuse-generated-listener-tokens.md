---
id: PANDO-US-0030
type: story
title: Persist and reuse the generated AG-UI and MCP listener tokens
status: in_progress
priority: high
parent: PANDO-EP-0004
author: claude
labels: [agui, mcp, ops, security]
estimate: 5
created: 2026-09-14T00:00:00Z
updated: 2026-09-14T00:00:00Z
---

## Description

As an operator starting a listener with no token configured, I want the generated token written
somewhere I can read it later, so that I do not have to catch one line of startup output before it
scrolls away.

Today both generated tokens are write-once to a stream and then gone.
`ensureMCPHTTPToken` (`cmd/mcp_server.go:532`) generates a token on a loopback bind and
`runMCPServerMode` prints it to stderr once; `resolveAGUIToken` (`cmd/agui_serve.go:275`) does the
same to stdout. Under a process supervisor, a wrapper script, or any terminal that has since
scrolled, that line is unrecoverable and the only way back in is to stop the process and configure
a token by hand.

Decision of 2026-09-14: the generated token becomes **stable**, not per-start. On the first start
with no configured token, generate one, write it to a file, and on every later start read that file
back instead of generating a new one. A client configured once keeps working across restarts. The
cost is accepted deliberately: the token lives on disk until the file is removed.

Implementation:

- Store under `config.GlobalConfigDir()` (`internal/config/global_projects.go:39`, XDG-aware:
  `$XDG_CONFIG_HOME/pando` or `~/.config/pando`), one file per listener kind, e.g.
  `agui-token` and `mcp-token`. Create the directory `0700` and the file `0600`.
- Refuse to read a token file whose mode is group- or world-readable, and say which file and what
  mode; a token anyone on the box can read is not a token. Do not silently repair the mode.
- Slot the file into the existing precedence *below* every explicit source and *above* generating:
  for AG-UI `--token`, then `--token-file`, then `PANDO_AGUI_TOKEN`, then the stored file, then
  generate-and-store; for MCP `MCPServer.HttpToken`, then the stored file, then
  generate-and-store. An explicitly configured token must never be overwritten by the stored one,
  and must never be written to the file.
- Print the file's path on startup, always, whether the token was generated or read back. Print
  the token itself only when it was just generated. The existing rule that no log line at any
  level carries the token still holds and its regression test must still pass.
- Add a way to read it without starting the server: a `--print-token` flag on the serve commands,
  or a small subcommand, whichever fits this CLI's existing shape. It must print only the token on
  stdout, exit non-zero if no stored token exists, and not start a listener.
- The MCP refusal to start on a non-loopback bind with no configured token
  (`cmd/mcp_server.go:536`) stays exactly as it is. A stored token does NOT satisfy it: a token
  generated for loopback must not silently become the credential for a network-facing bind.

## Acceptance Criteria

- [ ] First start with no configured token generates one, writes it `0600` under
      `GlobalConfigDir()`, prints the path and the token.
- [ ] A second start reads the same token back, prints the path, does not print the token, and a
      client configured from the first start still authenticates.
- [ ] An explicitly configured token (`--token`, `--token-file`, `PANDO_AGUI_TOKEN`,
      `MCPServer.HttpToken`) wins over the stored file and is never written to it.
- [ ] A token file with mode `0644` is refused with an error naming the file and its mode.
- [ ] The token can be read back without starting a listener, on stdout, exiting non-zero when
      there is none.
- [ ] A non-loopback MCP bind with no configured token still refuses to start, stored file present
      or not.
- [ ] No log line at any level carries the token; the existing regression test still passes.

## Notes

Follow-up of PANDO-US-0023 and PANDO-US-0025, raised on 2026-09-14: stderr is too fleeting to be
the only channel for a credential the operator needs again. Applies to both listeners with one
mechanism and one file format, because the MCP one prints to stderr and is the more fleeting of the
two.
