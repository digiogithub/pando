# Host sandbox coverage

Pando confines commands it spawns to a host-native sandbox (`internal/sandbox`, Landlock +
seccomp on Linux, `sandbox-exec`/Seatbelt on macOS, Job Object containment on Windows) so a
command the agent runs cannot read or write outside the workspace, temp dirs and allow-listed
caches, and cannot touch Pando's own config, data or git hooks. The design is per-spawn
wrapping, not process-wide: Pando itself stays unconfined, and the toggle applies live to the
next command. See `.kb/pando/analysis/grok-build-sandbox-research.md` for the full design and
`[Sandbox]` in `.pando.toml` for configuration (`Sandbox.Mode`, `Sandbox.ExtendTo`, ...).

Every call site that spawns a host process is listed below, with what actually confines it and
why. "Covered" means `sandbox.WrapCmd`/`sandbox.Default().Wrap` is called for that purpose;
"by default" means the purpose is always covered once any sandbox mode is enabled
(`sandbox.DefaultExtendTo`); "opt-in" means it is covered only when the operator adds the
purpose to `Sandbox.ExtendTo` (or, for a single MCP server, sets that server's own `Sandbox`
flag).

| Spawn site | File | Purpose | Coverage | Notes |
|---|---|---|---|---|
| Bash tool, persistent shell | `internal/llm/tools/shell/shell.go` (`newPersistentShell`) | `PurposeBash` | Always, when any sandbox mode is enabled | The one purpose that is never opt-out per spawn; `Sandbox.Mode = "off"` is the only way out. Re-spawns when the policy hash changes so a settings change takes effect on the next command. |
| Embedded runtime | `internal/runtime/embedded_runtime.go` | `PurposeBash` | Always, when enabled | Runs the host shell with the embedded rootfs on `PATH`; wrapped the same way as the bash tool (`sandbox.WrapCmd(cmd, sandbox.PurposeBash)`). |
| Host runtime adapter | `internal/runtime/host.go` | `PurposeBash` | Always, when enabled | Delegates to `shell.GetPersistentShell`, so it inherits that wrap; no separate call needed. |
| ACP client-terminal bash | `internal/llm/tools/bash.go` (`runWithACP`) | n/a | **Never** — delegated to the client | When Pando runs as an ACP agent inside Zed/VS Code, the client executes the command in its own terminal on its own host. Pando reports "sandbox: delegated to client"; the banned-command and permission checks that normally gate bash are also skipped there, because the host running the command is not this process. |
| ACP terminals Pando serves | `internal/mesnada/acp/client.go` (`CreateTerminal`) | `PurposeACPTerminals` | By default | A sub-agent asked *this* Pando instance (acting as an ACP server) to run a command; `sandbox.WrapCmd` is called before `cmd.Start`, and a wrap error fails closed (`CreateTerminal` returns an error instead of an unconfined terminal). `KillTerminal` signals the whole process group (`internal/procgroup`), not just the direct pid, so a sandbox launcher in front of the real command (bwrap, the Linux helper) is still reaped when the terminal is killed. |
| Skill CLI tools | `internal/skills/tool_bridge.go` (`CLIToolSkill.Run`) | `PurposeSkills` | By default | Same fail-closed treatment: a wrap error returns a Go error from `Run` and the skill executable never starts. Skills run synchronously (a single `cmd.Run()`), so no separate kill-code path was needed. |
| MCP stdio servers | `internal/mcpclient/client.go` / `sandbox.go` | `PurposeMCP` | **Opt-in**, two ways | (1) Global: add `"mcp"` to `Sandbox.ExtendTo` — every stdio server is then wrapped. (2) Per-server: set `Sandbox = true` on that one `[MCPServers.<name>]` entry regardless of the global list (`config.MCPServer.Sandbox`, additive field). The two conditions are ORed in `mcpclient.wrapStdioCommand`, since `sandbox.WrapCmd`'s own `Covers` check only sees the global policy. mcp-go builds the child process itself (`client.NewStdioMCPClient`), so the only hook available is `transport.WithCommandFunc`: `mcpclient.newSandboxedCommandFunc` builds the `exec.Cmd`, wraps it, and hands it back. **Failure mode differs by how coverage was decided**: a wrap error on a server that explicitly asked for it (`Sandbox = true`) fails closed (the server never starts); a wrap error on a server only covered by the global `ExtendTo` list logs a warning and falls back to running it unwrapped, matching the general "sandbox unavailable, fail open with a warning" posture elsewhere. Non-stdio servers (`sse`, `streamable-http`) are network clients, not local processes, and are unaffected. |
| Mesnada external-agent spawners | `internal/mesnada/agent/spawner*.go` (claude, copilot, gemini, opencode, ollama-claude, ollama-opencode, vibe/mistral, pando_cli, template, acp) | `PurposeSubagents` | **Opt-in** (`Sandbox.ExtendTo` includes `"subagents"`) | Off by default: these CLIs already sandbox themselves and need broad access to their own state (`~/.claude`, `~/.copilot`, `~/.gemini`, `~/.config/opencode`, `~/.vibe`, `~/.codex`, ...). `agent.wrapSubagentCmd` resolves the policy for the **task's own `WorkDir`** (not Pando's own working directory, which can differ), then widens a per-call copy of that `Policy` with the spawned CLI's own config directory (`agent.sandboxExtraRoots`, keyed by the executable's base name) plus the spawner's own `logDir` (where per-task MCP config/settings files are written) and, where a spawner deliberately injects a credential-shaped env var of its own (the Ollama routing spawners' `ANTHROPIC_AUTH_TOKEN`/`ANTHROPIC_API_KEY`), an extra `Env.Keep` pattern so `ScrubEnv`'s secret heuristic does not strip it. `Policy.WritableRoots`/`ProtectedPaths`/`Env.Keep` are plain exported fields, which is what makes this per-call widening possible without any change to `internal/sandbox`. A wrap error fails closed: the operator opted in, so a setup problem stops the spawn. Kill paths (`Cancel`/`Pause`/`Shutdown`) signal the process group via `internal/procgroup`, falling back to the direct pid when grouping is unsupported (Windows) or the command was never grouped. |
| `pando_cli` spawner specifically | `internal/mesnada/agent/spawner_pando_cli.go` | `PurposeSubagents` | Opt-in, with `AllowOwnControlPlane` | The spawned process is Pando itself, running `task.WorkDir` as its own project — protecting that project's `.pando`/`.pando.toml`/`.pando.json` would just lock the nested instance out of its own database and config. `wrapSubagentCmd`'s `AllowOwnControlPlane` option strips only those three workspace-relative entries from `Policy.ProtectedPaths`; the outer instance's global config dir, `$HOME/.pando.toml` and `.git/hooks`/`.git/config` stay protected. The generic ACP spawner (`spawner_acp.go`) applies the same exemption when the resolved agent binary's base name is `pando` (a nested Pando reachable over ACP). |
| LSP servers and the LSP installer | `internal/lsp/client.go`, `internal/lsp/runtime/install.go` | n/a | **Unsandboxed** (out of scope for this epic) | LSP servers are started automatically (not directly agent-invoked) and need to write their own caches/index state in locations the sandbox does not know about ahead of time (e.g. a language server's global cache dir, or a package manager the installer shells out to). Confining them well would need per-server writable-root knowledge Pando does not have yet; tracked as a follow-up. |
| Lua `cmd`/`sh` modules | `internal/luaengine/lua.go` (`luacmd.Preload`, `gluash` `sh` module) | n/a | **Unsandboxed** (documented follow-up, no code change in this story) | Lua tools/hooks run inside Pando's own process with a full Lua state (`os.execute`, `io.popen` plus the `cmd` and `sh` modules), so there is no `exec.Cmd` at the call site to intercept — wrapping it would need a `cmd`/`sh` module shim that shells out through `sandbox.WrapCmd` itself. Left as a P2 follow-up per the story; user-authored Lua is agent-invoked but not agent-generated. |
| User terminals (WebUI "Terminal" tab) | `internal/api/terminal_pty.go` (`newPTYSession`) | n/a | **Never** | The user's own interactive shell, not an agent-driven command; the code carries a comment stating this explicitly. |
| User terminals (TUI) | `internal/tui/components/terminal/terminal.go` | n/a | **Never** | Same reasoning as the WebUI terminal; commented at the call site. |
| `cliassist` | `internal/cliassist/runner.go` (`RunCommand`) | n/a | **Never** | Runs only a command the user typed and explicitly confirmed (`pando ?`-style assist); never one the agent generated on its own. Commented at the call site. |

## Pando's own listeners (guarded ports)

A confined command with the network allowed could otherwise reach Pando's own servers, most of
which trust loopback callers (the API hands its token to any loopback client) and can change
the configuration or run code outside the sandbox. Every listener registers its TCP port with
`internal/sandbox/portguard` (`portguard.Guard` wraps the `net.Listener` and unregisters on
`Close`; `portguard.Register` returns an unregister function). `sandbox.Resolve` copies the
ports into `Policy.DenyConnectPorts` (hashed, so the persistent shell re-spawns when a
listener appears). Linux blocks them with Landlock `LANDLOCK_ACCESS_NET_CONNECT_TCP` rules (ABI
4, Linux 6.7+); macOS with `(deny network-outbound (remote tcp "*:PORT"))`. Each process also
mirrors its ports into `<global config dir>/run/ports/<pid>.json` (a protected path), and
`portguard.Ports` merges the files of every live Pando process, so another instance's
listeners are blocked as well.

| Listener | File | Owner label |
|---|---|---|
| HTTP API / WebUI (every bind, including the external-access rebind to 0.0.0.0) | `internal/api/server.go` (`newListener`) | `api` |
| AG-UI dedicated listener | `internal/agui/listener.go` (`StartListener`) | `agui` |
| MCP/ACP HTTP server (`pando mcp-server`, embedded mesnada server) | `internal/mesnada/server/server.go` (`Start`) | `mcp-http` |
| LLM proxy | `internal/llmproxy/server.go` (`Start`) | `llm-proxy` |
| IPC bus PUB and ROUTER (primary) | `internal/ipc/bus.go` (`Start`, released in `Shutdown`) | `ipc-pub`, `ipc-rpc` |
| IPC primary's ports, seen from a secondary (read from `ipc.lock`) | `internal/ipc/runtime/runtime.go` (`Bootstrap`) | `ipc-primary-pub`, `ipc-primary-rpc` |
| Design preview server | `internal/design/preview/preview.go` (`StartLoopback`) | `design-preview` |
| MCP OAuth callback | `internal/mcpauth/callback.go` | `mcp-oauth-callback` |
| Claude OAuth callback | `internal/auth/claude.go` | `claude-oauth-callback` |
| Antigravity OAuth callback | `internal/tui/page/antigravity_commands.go` | `antigravity-oauth-callback` |
| Browser DevTools (CDP) port of the browser Pando drives (Pando now picks the port) | `internal/llm/tools/browser_session.go` | `browser-cdp` |
| `pando serve` children of the Projects feature, the desktop app | Their own process registers its API/IPC ports; this process sees them through the shared registry | — |

Port probes that bind and immediately close (`internal/ipc/ports.go`, `internal/ipc/bind.go`,
`cmd/app.go`, `internal/app/app.go`) are not listeners and are not registered.

Consequence for opted-in sub-agents (`ExtendTo = ["subagents"]`): a nested Pando running
confined cannot reach the parent's IPC bus or HTTP servers. `internal/ipc/runtime`
(`primaryResponds`) treats a connect refused with `EACCES`/`EPERM` as "sandboxed, primary
presumed alive", so a confined secondary never kills the primary as unresponsive.

## Testing notes

Each covered call site has unit tests built on `sandbox.SetDefaultForTests` (a fake `Wrapper`
that records calls and can inject an error), so they run everywhere without depending on a real
Landlock/Seatbelt backend:

- `internal/mesnada/acp/client_sandbox_test.go` — `CreateTerminal` is wrapped and `cmd.Env` is
  scrubbed by default; a wrap error fails closed (no terminal is created); and, with a fake
  wrapper that enforces the *real*, `Resolve()`-computed default policy's `WritableRoots`, a
  terminal writing outside the workspace is denied while one writing inside it succeeds — the
  acceptance-criterion scenario for this story.
- `internal/skills/tool_bridge_sandbox_test.go` — `Run` is wrapped and env-scrubbed by default;
  a wrap error fails closed and the skill executable never runs; the sandbox being fully
  disabled leaves the command untouched.
- `internal/mcpclient/client_sandbox_test.go` — not wrapped by default; wrapped when
  `Sandbox.ExtendTo` includes `"mcp"`; wrapped when a single server sets `Sandbox = true` with
  no global `ExtendTo` at all; a wrap error fails closed only for the explicit-flag case and
  falls back to running unwrapped for the global-only case. `TestStdioHandshakeWithFakeWrapper`
  goes one step further: it re-executes the test binary itself as a trivial MCP server
  (`mcpserver.ServeStdio`, the same self-exec pattern as the Go standard library's
  `os/exec` `TestHelperProcess` tests) through a fake wrapper that prefixes the real command
  with `env` (a real, harmless launcher), and asserts the `initialize` handshake still
  completes end to end while the command was in fact wrapped.
- `internal/mesnada/agent/sandbox_wrap_test.go` — `wrapSubagentCmd` is a no-op unless
  `Sandbox.ExtendTo` includes `"subagents"`; once it does, the CLI's own config directory and
  the spawner's `logDir` are added to `WritableRoots`, env is scrubbed (with `KeepEnv`
  overrides honored), `AllowOwnControlPlane` un-protects only the nested project's own
  `.pando*` entries (never `.git/hooks`), and a wrap error is reported rather than swallowed.
- `internal/sandbox/portguard/portguard_test.go` and `internal/sandbox/guarantees_test.go` —
  the registry (in-process and shared), the hash change when a port registers, and
  `Guarantees`/`AutoAllowBash` refusing auto-approval while the sandbox is partial.
  `internal/sandbox/guard_linux_test.go` runs the real helper: a guarded listener is refused
  while another port connects (Landlock ABI ≥ 4), and the `mv .git` hook-plant attack fails
  under bubblewrap while `git add`/`git commit` keep working.
- `internal/procgroup/procgroup_unix_test.go` — `Ensure` groups a command (and respects an
  existing `Setsid`), and `Kill` reaches every process in the group, not just the one whose pid
  was passed in, proving that stopping a wrapped long-running command (an ACP terminal, an MCP
  server, a sub-agent CLI) also stops whatever a sandbox launcher spawned in front of it.

Real OS-backend enforcement (actual Landlock/Seatbelt denial of a write outside the workspace)
is exercised by `internal/sandbox`'s own end-to-end tests (`e2e_linux_test.go`,
`e2e_darwin_test.go`); the tests above verify that each new spawn site threads the resolved
policy through to the wrapper correctly, not the OS mechanism itself.
