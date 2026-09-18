# Host command sandbox

Pando runs the shell commands the agent writes directly on your machine. The host command
sandbox confines those commands with the operating system's own mechanisms, so a command
cannot write outside your project, cannot change Pando's own configuration or your git hooks,
and does not see your API keys. There are no containers and nothing to install. On Linux and
macOS the sandbox is **on by default**.

Pando confines each command it starts, not its own process. The TUI, WebUI, database and
knowledge base are never confined, and a settings change applies to the next command without a
restart.

- [What gets confined](#what-gets-confined)
- [Modes](#modes)
- [Protected paths](#protected-paths)
- [Network](#network)
- [Environment scrubbing](#environment-scrubbing)
- [When a command is blocked: hints and escalation](#when-a-command-is-blocked-hints-and-escalation)
- [Platform support](#platform-support)
- [Turning it off or changing the mode](#turning-it-off-or-changing-the-mode)
- [Configuration reference](#configuration-reference)
- [CLI: `pando sandbox`](#cli-pando-sandbox)
- [Observability](#observability)
- [Limitations](#limitations)
- [Comparison with Grok Build](#comparison-with-grok-build)

## What gets confined

| Process | Default | How to change it |
|---|---|---|
| Bash tool (the persistent shell) | Confined whenever a mode is enabled | `Mode = "off"` is the only way out |
| Embedded runtime shell | Confined whenever a mode is enabled | Same as the bash tool |
| ACP terminals that Pando serves to sub-agents | Confined | Always included in `ExtendTo` |
| Skill CLI tools | Confined | Always included in `ExtendTo` |
| MCP stdio servers | **Not confined** | Opt in with `ExtendTo = ["mcp"]`, or `Sandbox = true` on one server |
| Mesnada sub-agent CLIs (claude, copilot, gemini, ...) | **Not confined** | Opt in with `ExtendTo = ["subagents"]` |
| Your own terminals (TUI and WebUI) and `pando ?` (cliassist) | Never confined | They run commands you typed, not commands the agent wrote |

[docs/sandbox-coverage.md](sandbox-coverage.md) lists every place Pando starts a process, with
file references and the reason for each decision.

The sandbox applies only to the host runtime. When `Container.Runtime` resolves to docker or
podman, the container is the isolation and the host sandbox does not apply.

## Modes

| Mode | Writes allowed | Reads allowed | Child network | Use it for |
|---|---|---|---|---|
| `workspace-write` (default) | Workspace, temp dirs, dependency caches, `WritableRoots` | Everything | Allowed (set `Network = "restricted"` to block it) | Normal development |
| `read-only` | Temp dirs only | Everything | Blocked | Exploring or reviewing code the agent must not change |
| `strict` | Workspace, temp dirs, `WritableRoots` (no caches) | Workspace, writable roots, system directories, toolchains, `ReadOnlyRoots` | Blocked | Untrusted repositories: the agent cannot read your home directory |
| `off` | Everything | Everything | Allowed | Turning the sandbox off |

The temp dirs are `/tmp`, `/var/tmp` and `$TMPDIR`; on macOS they also include
`/private/tmp`, `/private/var/tmp` and `/private/var/folders`. The dependency caches are the Go
build and module caches, npm, pnpm, yarn, bun, pip, cargo, gradle, maven and `~/.cache`
(`$XDG_CACHE_HOME`). To take them out of the writable roots, set `CacheDirsDisabled = true`.

While the sandbox gives its **full protection**, the bash tool **auto-approves** commands,
because a confined command can do little harm. Full protection means that the backend is
enforced, that the protected paths inside the workspace are enforced (see
[Protected paths](#protected-paths)), and that Pando's own network ports are blocked (see
[Network](#network)). When one of these is missing, the status shows `partial: <what is
missing>` and bash keeps asking for approval. Dangerous commands, such as `sudo` or `rm -rf` of
a system path, still ask for your approval. Banned commands such as `curl` are still refused. To
keep the prompt for every command, set `AutoAllowBashDisabled = true`. On a platform where the
sandbox is not enforced, Pando never auto-approves.

## Protected paths

The following paths are read-only for a confined command, even inside the workspace. This stops
the agent from turning the sandbox off or loosening it by editing files:

- `<workspace>/.pando.toml`, `<workspace>/.pando.json`, and the project config file Pando loaded
- `<workspace>/.pando/`, and the data directory when it is set to another location
- `<workspace>/.git/hooks` and `<workspace>/.git/config`
- `~/.pando.toml`, `~/.pando.json`, `~/.pando.yaml`, `~/.pando.yml` and the global config directory (for example `~/.config/pando`)
- Everything in `DenyPaths`. These paths are also **unreadable** (globs are allowed, for example `~/.ssh` or `*.env`).

A protected path cannot be swapped out from under its protection either. On macOS the profile
denies renaming or replacing any parent directory of a protected path. On Linux with bubblewrap,
every existing directory between a writable root and a protected path (for example `.git`, or
the workspace itself when it lies under `/tmp`) is turned into a mount point, which cannot be
renamed or removed. A command therefore cannot `mv .git` aside, rebuild it without the
read-only binds and plant a hook. Git keeps working normally inside `.git`.

On Linux, how completely these paths are enforced depends on bubblewrap. See
[Platform support](#platform-support).

## Network

In `workspace-write` mode a command can use the network. Set `Network = "restricted"`, or
choose `read-only` or `strict`, to block it:

- **Linux**: a seccomp filter refuses creating any socket other than a Unix socket (and
  `io_uring`). Without bubblewrap, connecting to Unix sockets is refused as well. With
  bubblewrap, local Unix sockets keep working, but the Docker, Podman and containerd API
  sockets are masked. With bubblewrap, the D-Bus and systemd sockets are masked in every mode,
  so a command cannot ask systemd to start something outside the sandbox.
- **macOS**: the Seatbelt profile denies network access.

The network setting applies to the commands Pando starts. Pando's own connections, such as
LLM providers and MCP servers over HTTP, are not affected.

**Pando's own ports are always blocked.** Even with the network allowed, a confined command
cannot connect to a TCP port that a Pando process listens on: the HTTP API and WebUI, the
AG-UI listener, the MCP HTTP server, the LLM proxy, the IPC bus, the design preview server,
OAuth callback servers and the DevTools port of the browser Pando drives. These listeners can
hand out the API token to local callers, change the configuration (including turning the
sandbox off), add MCP servers or run tools outside the sandbox, so reaching one would be an
escape. Every Pando process records its ports in `~/.config/pando/run/ports/<pid>.json`
(a protected path), so the ports of other Pando instances on the machine (another project's
TUI, the desktop app, a `pando serve` started by the Projects feature, the IPC primary) are
blocked too. `pando sandbox status` lists them as `guarded ports`.

- **Linux** blocks them with Landlock network rules, which need **Landlock ABI 4 (Linux 6.7 or
  later)**. On an older kernel the ports cannot be blocked: the status shows
  `partial: Pando's own ports reachable ...` and bash keeps asking for approval.
- **macOS**: the Seatbelt profile denies outbound TCP to those ports.

The block is by port number, so a connection to the same port number on another host is
refused as well. When a new Pando listener starts, the persistent shell is restarted on the
next command so that the new port is blocked.

## Environment scrubbing

Before a confined command starts, Pando removes variables whose names look like credentials:
names containing `API_KEY`, `APIKEY`, `ACCESS_KEY`, `PRIVATE_KEY`, `SECRET`, `TOKEN`,
`PASSWORD`, `PASSWD` or `CREDENTIAL`, and names ending in `_KEY`, `_PAT`, `_PASS` or `_DSN`.
Your provider keys therefore never reach the agent's shell. You can tune this under
`[Sandbox.Env]`:

- `Keep`: name globs to pass through even when they look like secrets
- `Exclude`: extra name globs to remove
- `Inherit`: `all` (default), `core` (a minimal set: PATH, HOME, locale, terminal, ...) or `none`
- `KeepSecrets = true`: turns scrubbing off

## When a command is blocked: hints and escalation

The sandbox cannot tell the agent directly that it blocked something. The kernel only returns
"Permission denied", "Operation not permitted" or "Read-only file system". Pando therefore
classifies the failed command's output. It compares the path in the error with the policy and
checks whether the path is writable by you. When a failure looks like a sandbox denial, Pando
appends a note to the tool output:

```
[sandbox] This command likely failed because Pando's sandbox (mode workspace-write,
backend bwrap+landlock) blocked a write to /home/me/.npmrc. Evidence: ...
If possible, use a path inside the workspace or a temp dir instead. If access outside
the sandbox is truly needed, call bash again with sandbox_permissions: "require_escalated"
and a one-line justification; the user will be asked to approve running the command
once outside the sandbox.
```

The classifier recognises English and, for common tools, Spanish, French, German, Italian and
Portuguese error messages.

**Escalation.** The bash tool accepts `sandbox_permissions: "require_escalated"` together with
a `justification`. Pando then asks for an `execute_unsandboxed` permission:

- The request always needs your **explicit approval**, in the TUI, the WebUI and ACP clients.
  Auto-approve, yolo, goal/autopilot and headless modes never grant it.
- If you approve, the command runs **once** outside the sandbox. It runs in the shell's current
  directory, but not in the persistent shell, so exported variables and functions from earlier
  commands are not available.
- "Allow for session" applies only to the same command prefix (for example `npm install`), or to
  that exact command when it uses shell syntax, redirections, variables or globs.
- `AllowAutoEscalation = true` lets escalations run without a prompt. Dangerous commands still
  ask. This is off by default, and a project config cannot turn it on.

## Platform support

| Platform | Backend | Enforced | Notes |
|---|---|---|---|
| Linux, kernel ≥ 6.7, with bubblewrap | `bwrap+landlock` | Yes, full | The complete implementation: Landlock limits writes and blocks Pando's own ports, bubblewrap makes protected paths read-only and hides deny paths, and seccomp blocks namespaces, mounts and (optionally) the network |
| Linux, kernel 5.13 to 6.6, with bubblewrap | `bwrap+landlock` | Yes, partial | Pando's own ports cannot be blocked (Landlock ABI < 4), so bash keeps its permission prompts |
| Linux, kernel ≥ 5.13, without bubblewrap | `landlock` | Yes, partial | See the weaker points below; bash keeps its permission prompts |
| Linux, kernel < 5.13 (no Landlock), or an architecture other than amd64/arm64 | `none` | No | Shows "not enforced"; bash keeps its permission prompts |
| WSL 2 | Same as Linux | Same as Linux | WSL runs the Linux backend |
| macOS | `seatbelt` (`/usr/bin/sandbox-exec` with a generated profile) | Yes | Protected paths, deny paths and network restriction are all enforced |
| Windows | `jobobject` | **No** | Commands are not confined. They run in a Job Object, so they are killed when Pando exits. Bash keeps its permission prompts |

**Linux without bubblewrap.** Landlock can only grant access to a directory and everything
under it. It cannot make one path read-only inside a writable directory. Without bubblewrap,
Pando works around this by granting each top-level entry of the workspace on its own and
skipping the protected entries. As a result:

- A command **cannot create new files or directories directly in the workspace root**, and
  cannot replace a top-level file by renaming another file over it. Creating files in
  subdirectories works. The `[sandbox]` hint explains this when it happens.
- Protected paths below the top level, **`.git/hooks` and `.git/config`, are not protected**.
  Pando cannot split `.git` without breaking git, which writes `.git/index.lock`.
- Deny paths are write-protected but **still readable**.
- Under a restricted network, Unix sockets are blocked as well.

Because of these gaps the sandbox reports `partial: protected paths inside the workspace not
enforced (install bubblewrap)` and the bash tool keeps asking for approval. Install bubblewrap
(`apt install bubblewrap`, `dnf install bubblewrap`, ...) to close them.
Ubuntu 24.04 and later restrict unprivileged user namespaces through AppArmor, which prevents
bubblewrap from starting. Pando detects this, falls back to Landlock-only and reports the reason
in `pando sandbox status`. `UseBwrap = "never"` skips bubblewrap entirely. `UseBwrap = "always"`
makes a command fail instead of falling back when bubblewrap cannot run.

**macOS.** Apple has deprecated `sandbox-exec`, but it is still shipped and it is the same
mechanism Codex, Claude Code and Gemini CLI use. When Pando itself already runs inside a
sandbox, `sandbox-exec` cannot nest and the status reports "not enforced".

## Turning it off or changing the mode

Any of these takes effect on the next command. You do not need to restart Pando:

- **TUI**: Settings > Sandbox. The footer and the chat sidebar show a badge with the current
  state, for example `workspace-write (bwrap+landlock v6)`, `off`,
  `workspace-write (landlock+seccomp v3; partial: ...)` or
  `workspace-write (not enforced: ...)`.
- **WebUI**: Settings > Sandbox. The same badge appears in the chat info sidebar.
- **Config file** (global `~/.pando.toml`):

  ```toml
  [Sandbox]
  Disabled = true          # or: Mode = "read-only" | "strict" | "off"
  ```

- **Environment variable**, for one process: `PANDO_SANDBOX=off` (also `false`, `0`, `no`,
  `disabled`, `none`), `PANDO_SANDBOX=read-only`, `PANDO_SANDBOX=strict`, or
  `PANDO_SANDBOX=workspace-write`. `PANDO_SANDBOX=on` turns on the mode from your config.

The TUI, the WebUI, and `PUT /api/v1/config/sandbox` all write to the **global** config file.
The agent cannot change the sandbox: `pando_setup` refuses `sandbox.*` keys, a project config
can only make it stricter, and the config files are protected paths.

## Configuration reference

```toml
[Sandbox]
Disabled = false                 # true turns the sandbox off (the UI toggle)
Mode = "workspace-write"         # workspace-write | read-only | strict | off
Network = "allowed"              # allowed | restricted (read-only and strict always restrict)
AutoAllowBashDisabled = false    # true keeps the bash permission prompt while confined
WritableRoots = ["~/work/shared"]   # extra writable dirs (absolute, ~, or relative to the workspace)
ReadOnlyRoots = ["~/datasets"]      # extra readable dirs for strict mode
DenyPaths = ["~/.ssh", "~/.aws", "*.pem"]  # neither readable nor writable
CacheDirsDisabled = false        # true removes the dependency caches from the writable roots
UseBwrap = "auto"                # auto | always | never (Linux)
ExtendTo = ["mcp", "subagents"]  # extra spawn sites; acp-terminals and skills are always on
AllowAutoEscalation = false      # true lets escalations run without a prompt (dangerous commands still ask)

[Sandbox.Env]
Inherit = "all"                  # all | core | none
KeepSecrets = false
Keep = ["NPM_TOKEN"]
Exclude = ["MY_PRIVATE_*"]
```

In JSON config the section is `"sandbox"` and the keys are camelCase (`"mode"`,
`"autoAllowBashDisabled"`, `"env": {"keepSecrets": ...}`).

**Precedence**, from strongest to weakest: an enterprise locked key, then `PANDO_SANDBOX`, then
the project config (which can only tighten), then the global config, then the defaults.

- **A project config can only tighten.** A `.pando.toml` or `.pando.json` in a repository
  cannot set `Disabled`, choose a looser mode, open the network, re-enable auto-allow, allow
  auto-escalation, keep secrets, or add writable or readable roots outside the workspace. Pando
  drops those values when it loads the config. A cloned repository therefore cannot disable
  your sandbox.
- **Enterprise locks**: a configuration overlay can lock `sandbox.mode`, `sandbox.disabled`, any
  other `sandbox.*` field, or the whole section. `PANDO_SANDBOX` is ignored for a locked mode.
  The settings screens show locked fields as read-only, and the API answers `409
  config_key_locked` when a request tries to change one.

## CLI: `pando sandbox`

```sh
pando sandbox status          # backend, enforced?, full or partial protection, mode, network, roots, protected paths, guarded ports, policy hash
pando sandbox status --json   # the same as JSON (for bug reports and scripts)
pando sandbox exec -- sh -c 'touch src/probe && rm src/probe'   # run one command confined, as bash would
pando sandbox exec -- sh -c 'touch "$HOME/.probe"'              # expected: Permission denied
```

`exec` uses the policy resolved for the current directory, prints on stderr whether the command
ran confined, and exits with the command's exit code.

## Observability

Every sandbox event is logged through slog and appears on the TUI and WebUI log pages. If you
have enabled remote diagnostics, these log lines are shipped as well. The events are:

- `sandbox.applied`: a command started confined. For the persistent shell this is logged once
  per shell, not once per command.
- `sandbox.unavailable`: sandboxing is enabled but cannot be enforced here. Logged once per
  process, with the reason.
- `sandbox.denied`: a command failed because of the sandbox, with the kind, operation and path.
- `sandbox.escalation.requested`, `sandbox.escalation.granted`, `sandbox.escalation.denied`.

Command lines in events are redacted (known secret patterns and your home directory are
removed) and truncated. The events are also appended to
`<data dir>/sandbox-events.jsonl` (by default `.pando/data/sandbox-events.jsonl`), which rotates
to a single `.1` backup at 5 MiB. Event counters appear in the `pando_stats` tool.

## Limitations

- **Bash run by an ACP client is not confined.** When Pando runs as an ACP agent in Zed, VS Code
  or JetBrains and the client runs the command in its own terminal, the client executes it on
  its own host. Pando reports "sandbox: delegated to client".
- **Lua** hooks and tools (`os.execute`, `io.popen`, the `cmd` and `sh` modules) run inside
  Pando's process and are not confined. Lua scripts are written by the user, not generated by
  the agent. Confining them is a planned follow-up.
- **LSP servers** and the LSP installer are not confined. Pando starts them itself, and they
  write caches in places the policy does not know about.
- **MCP stdio servers and sub-agent CLIs are opt-in** (`ExtendTo`), because they usually need
  their own state outside the workspace.
- **Windows** is not enforced (see [Platform support](#platform-support)). Linux kernels older
  than 5.13 are not enforced either.
- **Linux without bubblewrap** is weaker: `.git/hooks` and `.git/config` are not protected, the
  workspace root is closed to new entries, and deny paths stay readable. The sandbox reports
  itself as partial and bash keeps prompting.
- **Linux before 6.7** (Landlock ABI < 4) cannot block Pando's own ports. The sandbox reports
  itself as partial and bash keeps prompting.
- A protected path that **does not exist** when the command starts can be created where its
  parent directory is writable. The exception is the workspace root on Landlock-only Linux,
  which is closed to new entries anyway. A new `.pando.toml` created this way can only tighten
  the project configuration. However, in a directory with **no `.git` yet** (or a `.git`
  without a `hooks` directory), a command can run `git init` and plant a hook, which runs
  outside the sandbox on your next git command. Review new repositories the agent creates
  before running git in them yourself.
- **Background processes keep the policy they started with.** When the policy changes (a
  settings change, or a new Pando listener), the persistent shell is replaced on the next
  command, but a process the old shell left running in the background keeps the old policy
  until it exits.
- A confined command can still **send signals** to Pando's own processes (for example kill
  them). That is a denial of service, not a way out of the sandbox.
- **Temp dirs are always writable**, even in `read-only` mode. A workspace located under `/tmp`
  is therefore writable in every mode.
- **Denial detection is heuristic.** It depends on the command's error text. A tool that hides
  the underlying error, or prints it in a language the classifier does not know, gets no
  `[sandbox]` hint.
- **The network setting is all or nothing.** There is no per-host allow list yet.

## Comparison with Grok Build

The design follows Grok Build's `xai-grok-sandbox` (profiles, protected paths, the seccomp
filter set and the event log), with these differences:

| | Grok Build | Pando |
|---|---|---|
| Scope | Confines its own process once, at startup, irreversibly | Confines each spawned command; Pando itself is not confined, and changes apply live |
| Default | Off | On (`workspace-write`) |
| macOS network restriction | Not enforced | Enforced by the Seatbelt profile |
| Blocked command | No hint to the model | `[sandbox]` hint, plus an escalation with explicit approval |
| Windows | Nothing | Honest "not enforced" status, plus Job Object containment |
| Configuration | `sandbox.toml` profiles | `[Sandbox]` in the normal config, TUI and WebUI settings, `PANDO_SANDBOX` |
