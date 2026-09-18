---
created_at: 2026-09-18T08:36:46Z
updated_at: 2026-09-18T08:36:46Z
tags:
    - analysis
    - sandbox
    - security
    - grok-build
---

> Backlog: epic PANDO-EP-0009 (stories PANDO-US-0040..0049). Related: [[pando/analysis/grok-build-memory-survey.md]]

# Host command sandboxing for Pando (no containers): research and epic proposal

Date: 2026-09-18. Read-only research. No files changed in either repo.
Sources: `/www/MCP/Pando/grok-build` (Rust, reference) and `/www/MCP/Pando/pando` (Go).

---

## A) How Grok Build does it

### A.1 Crate and backend
- Crate: `crates/codegen/xai-grok-sandbox`. Its description reads "OS-level sandboxing ... (Landlock/Seatbelt) via nono" (`Cargo.toml:5`).
- The backend is the third-party crate **`nono` =0.53.0**, pinned exactly (`Cargo.toml:26-37`). It pulls in the `landlock` crate on Linux and emits Seatbelt profiles on macOS (`Cargo.lock:6682-6708`). Grok itself only builds a `nono::CapabilitySet` (a list of path + AccessMode grants) and calls `Sandbox::apply(&caps)` (`src/lib.rs:208-240`).
- The `enforce` feature is on by default. Without it (and on Windows) `apply()` is a stub that only logs (`src/lib.rs:241-249`, `Cargo.toml:47-52`). **There is no Windows sandbox.** Platform labels are only "linux/landlock" and "macos/seatbelt" (`src/types.rs:67-73`).

### A.2 Architecture: process-wide and in-process, not per command
- The sandbox is applied **once, to the whole grok process, at startup, and cannot be undone**. There is no helper binary for Landlock or Seatbelt: `SandboxManager::new` -> `apply` -> `install` (`src/lib.rs:160-280`).
- Wiring: `xai-grok-shell/src/config/mod.rs:1431-1588` (`apply_sandbox`), called from `xai-grok-pager-bin/src/main.rs:2158` before the agent boots.
- Every child process (bash, rg, hooks, MCP stdio, LSP) inherits the Landlock/Seatbelt domain automatically. The in-process fs tools (`read_file`, `search_replace`) are confined too (docs `xai-grok-pager/docs/user-guide/18-sandbox.md` "How It Works").
- The consequence is that **no per-command escalation is possible**. The docs say: "The sandbox is irreversible once applied. The agent cannot relax restrictions at runtime." To get more access, the user starts a new session with another profile. Sessions persist their profile, and resuming with a different `--sandbox` is refused (`xai-grok-pager/src/app/cli.rs:929-934`; test `xai-grok-shell/src/session/persistence_resumed_sandbox_profile_tests.rs`).
- Side effects of confinement:
  - The shared "leader" process is disabled under a sandbox (`xai-grok-shell/src/leader/mod.rs:822,1382`).
  - `grok workspace start` is refused (`main.rs:548`).

### A.3 Linux mechanisms
1. **Landlock** for the filesystem, through nono, needing kernel 5.13 or later. Device files and directories are granted explicitly: `/dev/null`, `/dev/tty`, `/dev/ptmx`, `/dev/pts`, `/dev/fd` (`src/paths.rs:21-38`). A `/dev/tty` that returns ENXIO is skipped so the whole ruleset does not abort (`src/profiles.rs:180-194`).
2. **Bubblewrap re-exec** for denies inside a granted tree. Landlock cannot carve a subpath out of an allowed tree, so read-deny and write-deny paths are enforced like this:
   - Grok re-execs itself under `bwrap --cap-drop ALL --bind / / --ro-bind <deny_write> ...`.
   - Read-deny paths get a bind-over with a chmod-000 placeholder.
   - A sentinel dir and the env marker `__GROK_INSIDE_BWRAP` are set (`src/lib.rs:281-368`).
   - A "Verify" step catches spoofing of that env marker (`config/mod.rs:1503-1540`).
   - bwrap is **optional**: if exec fails, Grok falls back to Landlock only (`config/mod.rs:1494-1502`). It becomes mandatory (refuse to start) when the profile has a non-empty `deny` or hook write-deny.
3. **seccomp**, two filters (`src/child_net.rs`):
   - **Per-child network filter.** It is installed in `pre_exec` (between fork and exec). It returns EPERM for `connect, bind, sendto, sendmsg, sendmmsg, listen, accept, accept4, io_uring_setup/enter/register`. It also gates on arch and x32 (`child_net.rs:178-216`). The BPF program is built in the parent because allocating after fork can deadlock (`child_net.rs:218-231`). Call sites use `restrict_child_network(&mut cmd)` (`child_net.rs:269-281`).
   - The network stays open for the grok process itself, which needs the LLM API. **Only children lose network**, and only on Linux. On macOS the network restriction is a no-op.
   - **Namespace lockdown filter** inside bwrap. It blocks `mount`/`umount2`/`pivot_root`/`open_tree`/`move_mount`/`fsopen...`, `unshare`, `setns` and `clone(CLONE_NEW*)`. `clone3` gets ENOSYS so libc falls back to `clone`. This stops a child from remounting its way out of the bwrap binds (`child_net.rs:97-122`).
4. **Launch-time unix-socket masks.** Container runtime sockets (docker, podman, containerd) are denied when network is restricted. D-Bus and systemd private sockets are always denied, because a D-Bus message can make systemd spawn a unit outside the Landlock domain (`src/runtime_sockets.rs:1-60`, `profiles.rs:366-400`).

### A.4 macOS mechanism
- Seatbelt, through nono's `CapabilitySet` plus `add_platform_rule` for denies.
- Deny precedence depends on the order in which nono emits rules, which is why nono is pinned exactly (`src/deny/mod.rs:75-80`, `Cargo.toml:26-30`).
- Deny aliases cover the `/private` firmlink (`deny/mod.rs:37-71`).
- Globs become anchored Seatbelt regexes, which is airtight at runtime. On Linux, globs are expanded once at launch.

### A.5 Policy model
- `ProfileName` = `workspace` | `devbox` | `read-only` | `strict` | `off` | `Custom(name)` (`src/profiles.rs:69-112`).
- The resolved `SandboxProfile` has these fields: `read_only`, `read_write`, `deny`, `write_deny` (hook sources), `default_read`, `restrict_network` (`profiles.rs:26-43`).

| profile | read | write | child net |
|---|---|---|---|
| workspace | all (`default_read`) | cwd + `~/.grok` + `/tmp`,`/var/tmp`,`$TMPDIR` (macOS `/private/var/folders`) | open |
| devbox | all | every top-level dir except `/data` | open |
| read-only | all | `~/.grok` + temp | blocked |
| strict | cwd + system dirs (`/usr`,`/lib`,`/etc`,`/run`,`/var`,macOS `/System`,`/Library`,`~/Library`) + `~/.grok` | cwd + `~/.grok/sessions` + temp | blocked |

Table source: `profiles.rs:402-499`, `paths.rs:40-94`.

- **Protected paths.** Grok's own config and trust files are write-denied under every profile except devbox: `~/.grok/hooks/`, `hooks-paths`, `config.toml`, `trusted_folders.toml`, `managed_config.toml`, `requirements.toml`, `sandbox.toml`. This stops the agent from rewriting its own policy or planting hooks (`src/hook_write_deny.rs`, docs "Direct global write protection").
  - `.git` is **not** protected by default. Users add `deny` globs such as `**/.env` and `**/*.pem` in custom profiles.
- **Custom profiles** live in `~/.grok/sandbox.toml` and `.grok/sandbox.toml`. The project file is **additive only**: it cannot redefine a global profile name. This stops a malicious repo from hollowing out a trusted profile (`profiles.rs:114-164`).
- **Configuration and resolution:**
  - `[sandbox] profile` and `auto_allow_bash` in config.toml (`xai-grok-shell/src/agent/config.rs:1093-1127`).
  - Resolution order: managed requirement > CLI `--sandbox` > env `GROK_SANDBOX` > config > **default "off"**. Grok ships it **off by default**.
  - The settings API writes `sandbox.profile` and `sandbox.auto_allow_bash` (`config/mod.rs:1304-1322`). A change applies from the next session, because the sandbox is irreversible.
- **Env scrubbing** is separate: `[shell_environment_policy]` with `inherit=all|core|none`, default excludes `*KEY*`/`*SECRET*`/`*TOKEN*`, and `exclude`/`include_only`/`set` (`xai-grok-tools/src/util/shell_env_policy.rs`). The persistent shell snapshot drops `SSH_AUTH_SOCK`, `DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR` and similar (`xai-grok-tools/src/computer/local/shell_state.rs:104,160`).

### A.6 Approval integration
- Sandbox plus approval: `auto_allow_bash` skips the bash permission prompt **while the sandbox is actually applied** (`should_auto_allow_bash() = flag && is_active()`, `lib.rs:101-107`).
  - It is still subject to the "floor": explicit file-write evaluations, policy, auto and hook forced prompts, and protected edits (`xai-grok-workspace/src/permission/manager/mod.rs:390-391,1109-1125`).
  - It is tested at `:5231-5246`.
- This is the main UX benefit: **the sandbox replaces prompts**, instead of prompts leading to escalation.

### A.7 Failure detection and reporting
- Violations are only inferred. The in-process fs tools check `is_permission_error` and call `xai_grok_sandbox::log_violation(path, op)` (`xai-grok-tools/src/computer/local/file_system.rs:140-187`).
- Events are written to JSONL at `~/.grok/sessions/sandbox-events.jsonl` (`paths.rs:15-17`, `logging.rs`).
  - Event types: `ProfileApplied`, `ApplyFailed`, `FsViolation`, `NetViolation`, `BypassGranted`, `BypassDenied`.
  - Atomic counters live in `SandboxMetrics` (`src/types.rs`). `BypassGranted` and `BypassDenied` are modeled but never emitted.
- There is **no model-facing hint**. A bash command blocked by Landlock simply fails with EACCES or EPERM in stderr.
- If the sandbox cannot be applied: built-in profiles warn and continue. Custom profiles and profiles that need hook protection **refuse to start** (fail closed) (`config/mod.rs:1550-1586`).

### A.8 What is covered
- Everything in the process tree: bash persistent and static shells, rg, subagents, hooks, MCP stdio servers, LSP.
- The per-child **network** filter is applied explicitly at every known spawn site:
  - terminal (`xai-grok-tools/src/computer/local/terminal.rs:769,885,3149,3310`)
  - static shell (`static_shell.rs:80`) and shell state (`shell_state.rs:298`)
  - hooks (`xai-grok-hooks/src/runner/command.rs:199`)
  - MCP stdio (`xai-grok-mcp/src/servers.rs:4746`)
  - LSP (`xai-grok-tools/src/implementations/lsp/client.rs:431`)

### A.9 Tests
- `xai-grok-sandbox/tests/deny_paths_e2e.rs` (1392 lines; the macOS part self-skips in CI).
- `tests/child_net_e2e.rs`, `tests/read_write_trailing_glob_e2e.rs`, `tests/integration_test.rs`.
- Unit: an in-crate classic-BPF interpreter evaluates the seccomp programs against synthetic syscalls, covering wrong arch and x32 (`child_net.rs` tests, lines around 310-470).
- Unit: `read_deny_verify_tests.rs`, `hook_write_deny_tests.rs`, `runtime_sockets_tests.rs`.
- Integration: `xai-grok-shell/tests/test_leader_sandbox_confinement.rs`.
- Smoke example: `examples/sandbox_smoke_test.rs`.

### A.10 What transfers to Pando, and what does not
- **Transfers:**
  - the profile table and the writable-roots model (workspace + temp + app home)
  - the device-file allowlist and the `/dev/tty` ENXIO lesson
  - protected config/hook paths (anti self-disable)
  - project config being additive only
  - the seccomp syscall list (it covers io_uring and sendmmsg)
  - D-Bus/systemd and docker socket masks
  - the event JSONL and metrics design
  - `auto_allow_bash` while the sandbox is active
  - the fail-closed rules
- **Does not transfer:**
  - The process-wide, irreversible model. Pando's own process must keep writing `.pando/data/pando.db`, KB, snapshots and caches, runs WebUI/ACP/MCP servers and a desktop app, and the user wants an on/off toggle in settings.
  - Go has no `pre_exec` hook, so seccomp cannot be installed between fork and exec from a normal `exec.Cmd`.
  - Pando therefore needs **per-spawn wrapping through a helper** (see C).

---

## B) Pando today

### B.1 Where Pando runs host processes

| Site | File | Agent-driven? | Sandbox priority |
|---|---|---|---|
| bash tool, host runtime | `internal/llm/tools/bash.go:307-450` (Run), `:453-507` (executeCommand) -> `shell.GetPersistentShell(config.WorkingDirectory())` | yes | **P0** |
| Persistent shell | `internal/llm/tools/shell/shell.go:47-130`. A single process-global `$SHELL -l` started with `exec.Command` and `cmd.Env = os.Environ()+GIT_EDITOR=true`. Commands go through stdin; cwd is tracked with `pwd > tmpfile`; `killChildren` uses `pgrep -P` (`:248`) | yes | **P0**: wrap the spawn |
| Host runtime adapter | `internal/runtime/host.go:51-68` (same persistent shell) | yes | P0 (same path) |
| ACP terminals Pando serves for sub-agents | `internal/mesnada/acp/client.go:401` `exec.CommandContext(params.Command, params.Args...)`, with cwd checked by `validatePathInWorkspace` | yes (sub-agent) | **P1** |
| Skill CLI tools | `internal/skills/tool_bridge.go:111` | yes (skill executable) | P1 |
| Lua tools/hooks | `internal/luaengine/lua.go:36-70` opens all std libs (`os.execute`, `io.popen`) plus `luacmd` and `gluash` `sh`. Lua tools are exposed to the agent through `internal/llm/tools/lua_tools.go` | user-authored, agent-invoked | P2 (in-process, cannot be wrapped; would need a `cmd`/`sh` shim) |
| MCP stdio servers | `internal/mcpclient/client.go:65` `client.NewStdioMCPClient(cmd, env, args...)` | user config | P2 opt-in (wrap the command) |
| LSP servers and installer | `internal/lsp/client.go:71`, `internal/lsp/runtime/install.go` | automatic | out of scope v1 (needs cache writes) |
| Mesnada external-agent spawners | `internal/mesnada/agent/spawner*.go` (claude, copilot, gemini, opencode, vibe, template, acp, pando_cli) | agent-delegated | P2 opt-in (these CLIs have their own sandboxes and write `~/.claude` and similar) |
| Container runtimes (docker/podman/embedded) | `internal/runtime/{docker,podman,embedded_runtime}.go` | yes | n/a. Already isolated, except that **embedded runs the host shell with the rootfs on PATH and no isolation** (`embedded_runtime.go:21-26,258`), so it should also go through the host sandbox |
| User terminals and editor | `internal/api/terminal_pty.go`, `handlers_terminal.go`, `internal/tui/components/terminal/terminal.go`, `tui/components/chat/editor.go` | **user**, not agent | never sandbox |
| cliassist (`pando ?` style, runs the user-confirmed command) | `internal/cliassist/runner.go:27` | user-confirmed | never (user intent) |
| Misc (cron, browser, auth, desktop, project child instances) | `cmd/cronjob.go`, `internal/llm/tools/browser_session.go`, `internal/auth/browser.go`, `internal/desktop/launcher.go`, `internal/project/manager.go` | infrastructure | never |

Note on ACP mode: when Pando runs as an ACP agent inside Zed or VS Code, bash goes through `runWithACP` (`bash.go:325-328,548-660`). The **client** then runs the command in its own terminal, and the banned-command and permission checks are skipped. That path is out of scope, because the host belongs to the client. Pando should report "sandbox: delegated to client".

### B.2 Existing safety layers
- **Banned commands.** `effectiveBannedCommands()` covers curl, wget, nc and similar, configured by `Bash.BannedCommands` and `AllowedCommands` (`bash.go:73-107,331-336`). The check only looks at the first word of the command.
- **Safe read-only allowlist.** Commands on it skip the permission prompt (`bash.go:109,338-346`). The check is a prefix match, and it is trivially bypassed with `ls; rm -rf`.
- **Dangerous-pattern detector.** `internal/safety/commands.go` `IsDangerousShellCommand(cmd, cfg.Goal.DangerousPatterns)`. A match sets `RequireExplicitApproval` (`bash.go:352-358`).
- **Permission service.** `internal/permission/permission.go`:
  - `CreatePermissionRequest{SessionID, ToolName, Description, Action, Params, Path, RequireExplicitApproval}`
  - `Request`/`RequestWithContext`, `Grant`/`GrantPersistant`/`Deny`
  - session auto-approve, global auto-approve (`Permissions.AutoApproveTools`), per-session handlers (used by ACP and AG-UI), and `PendingRequests` for WebUI replay
- **Permission dialogs:**
  - TUI: `internal/tui/components/dialog/permission.go` (Allow / Allow for session / Deny)
  - WebUI: `web-ui/src/components/chat/PermissionDialog.tsx`
- **Existing container option.** A full `internal/runtime` package with `ExecutionRuntime`, a resolver for `host|docker|podman|embedded|auto` (`internal/runtime/runtime.go`, `resolver.go`) and `SecurityPolicy` (`internal/runtime/security.go`: network none, read-only, no-new-privs, pids). It is configured by `config.ContainerConfig` (`internal/config/config.go:968-989`, key `Container`), with API `/api/v1/container/*` (`internal/api/handlers_container.go`, `routes.go:56-57`) and TUI section `buildContainerRuntimeSection` (`internal/tui/page/settings.go:2099`). WebUI has `ContainerRuntimeSettings.tsx`.
  - Plan doc: KB `plans/container-runtime-support-implementation-plan.md`.
  - The host sandbox should apply **only when the runtime resolves to host (or embedded)**.
- No prior KB docs on host sandboxing, Landlock or Seatbelt were found. The only related KB docs are about containers and devcontainers (`research/devcontainer.md` and the plan above).

### B.3 Config and settings surfaces
- **Config struct:** `internal/config/config.go:1015-1102`. Neighbouring sections are `Shell ShellConfig` (`:414`), `Bash BashConfig` (`:420-434`), `Permissions PermissionsConfig` (`:198`) and `Container ContainerConfig`.
- **Setter pattern:** the `UpdateX(...)` functions (e.g. `UpdateAutoCompact` `:4739`, `UpdateContainer` `:5530`) mutate `cfg` and persist with `updateCfgFile(func(*Config))`, rolling back on error.
- **Enterprise locks:** `internal/config/overlay.go:228` `LockedKeys()`, exposed at `/api/v1/config/locked-keys`. Use this for an admin-enforced "sandbox cannot be disabled".
- **Zero-value trap:** bools are `omitempty`, so a default-true feature must be modelled as a `Disabled` flag or a string mode where empty means on. Precedent: `Bash.OutputFilterDisabled`, shown as a `bash.outputFilter` toggle with `boolString(!bash.OutputFilterDisabled)` (`settings.go:3225-3230`).
- **TUI settings:**
  - `internal/tui/page/settings.go` `buildSections()` (`:973-1012`) groups sections. The "Tools" group holds `buildContainerRuntimeSection`, `buildInternalToolsSection`, `buildBashSection` (`:3216-3252`) and `buildTokenOptimizationSection`.
  - Save handling is a key switch (e.g. `case "bash.outputFilter":` `:5925`).
  - Field types: `settings.FieldToggle` and `FieldText`, plus select-like options (see `container.runtime`).
  - `applyFieldPolicy` hides locked or policy fields (`:1027`).
- **WebUI settings:**
  - `web-ui/src/components/settings/SettingsView.tsx` (category list around `:38-67`, render around `:349`)
  - `BashSettings.tsx` (uses `useBashStore` from `web-ui/packages/pando-client/src/stores/settingsStore.ts`)
  - i18n: `web-ui/src/i18n/locales/{en,es,fr,de,pt,ja,zh}.json`
- **API:** `internal/api/handlers_config.go:1006-1030` `handleConfigBash` (GET/PUT), route `routes.go:97` `/api/v1/config/bash`.
- **`pando_setup` tool.** `internal/llm/tools/pando_setup.go` lets the model change config. **Sandbox keys must not be writable there**, or the agent could turn off its own sandbox.

### B.4 Go and platform constraints
- `go.mod`: go 1.26 and `golang.org/x/sys v0.47.0`. **No Landlock or seccomp library yet.** `github.com/docker/docker` is present (container runtime).
- Candidate libraries:
  - `github.com/landlock-lsm/go-landlock` (pure Go, uses x/sys; BestEffort ABI downgrade)
  - `github.com/elastic/go-seccomp-bpf` (pure Go), or a hand-built BPF with `golang.org/x/net/bpf` plus `unix.Prctl(PR_SET_NO_NEW_PRIVS)` / `unix.Syscall(SYS_SECCOMP)`
  - Neither needs cgo.
- Builds: `Makefile:51` has `CGO_ENABLED ?= 1` (zig cc cross), `.goreleaser.yml:7` has `CGO_ENABLED=0`. Targets are linux, darwin and windows (`.goreleaser.yml:8-35`).
  - With `CGO_ENABLED=0`, go-landlock's all-threads restrict uses `syscall.AllThreadsSyscall`.
  - The helper design below sidesteps threading entirely: apply on a locked thread, then `syscall.Exec` straight away.
- **Go cannot run code between fork and exec**, so Grok's `pre_exec` seccomp trick is impossible. Use a **re-exec helper**: the hidden subcommand `pando __sandbox-exec --policy <fd|b64> -- <argv>`. It applies Landlock and seccomp to itself and then `execve`s the target. Landlock domains and seccomp filters are inherited across exec (with `no_new_privs`).

---

## C) Proposed design for Pando

### C.1 Principles
1. **Per-spawn wrapping, not process-wide.** Pando's own process stays unconfined; only agent-driven children are wrapped. This keeps the DB, KB, WebUI and desktop working and makes a live on/off toggle possible.
2. **On by default** for the host bash tool (and the embedded runtime). The default mode is `workspace-write`.
3. **Fail open with a visible warning** when the OS cannot sandbox (old kernel, Windows). Every such run is flagged `sandbox: unavailable`, and `auto-allow` is disabled. Fail closed only when an enterprise lock requires the sandbox (then refuse to run and require an explicit approval per command).
4. **The sandbox reduces prompts and escalation restores capability.** Inside the sandbox, bash can be auto-approved (Grok's `auto_allow_bash`). When a command fails because of the sandbox, the model can re-request it unsandboxed with a justification, and the user approves through the normal permission dialog. This is Codex-style escalation, which Grok lacks.
5. **Protect Pando's own control plane** so the agent cannot disable the sandbox by editing config.

### C.2 Package layout

`internal/sandbox/`:
- `policy.go`: `Policy{Mode, WritableRoots, ReadOnlyRoots, DenyPaths, Network, EnvPolicy}` and `Resolve(cfg, workspace)`
- `sandbox.go`: `type Wrapper interface { Wrap(cmd *exec.Cmd, p Policy) error; Available() Capability }` and `Default()`
- `linux.go` (`//go:build linux`): Landlock and seccomp plus optional bwrap
- `darwin.go`: `sandbox-exec -p <SBPL>`
- `windows.go`: degrade plus Job Object
- `other.go`
- `helper.go`: the `__sandbox-exec` entry point, called from `main.go`/`cmd/` before cobra init, with no config or DB load
- `detect.go`: classify violations
- `events.go`: slog, event ring and telemetry
- `sbpl.go`: macOS profile template

### C.3 Policy modes (config key `Sandbox.Mode`)

| mode | FS write | FS read | network (children) | default |
|---|---|---|---|---|
| `workspace-write` | workspace + extra `WritableRoots` + temp (`/tmp`,`/var/tmp`,`$TMPDIR`, macOS `/private/var/folders`) + tool caches (`~/.cache`, `~/go/pkg/mod`, `~/.npm`, `~/.cargo/registry`, configurable) | all | **allowed** (configurable) | **yes** |
| `read-only` | temp only | all | blocked | |
| `strict` | workspace + temp | workspace + system dirs + toolchains | blocked | |
| `off` | unrestricted | unrestricted | unrestricted | |

- **Network default = allowed** in v1. `npm install`, `go mod download` and `git fetch` are core dev tasks. Blocking them by default would train users to click "off". Pando already bans curl and wget at the command level, so `Sandbox.Network = "restricted"` is the opt-in. A later story can add an allowlisting proxy (the sandbox-runtime and Codex approach: `HTTP(S)_PROXY` to an in-process proxy, plus seccomp/SBPL that only allows loopback to it).
- **Protected paths** stay read-only even inside writable roots:
  - `<ws>/.pando.toml`, `<ws>/.pando/` (config, `data/pando.db`, KB), `$HOME/.pando.toml`, and Pando's global config dir. This is the anti self-disable measure.
  - `<ws>/.git/hooks`, `<ws>/.git/config` (code-exec vectors). The rest of `.git` stays writable so `git commit` works.
  - Any user `DenyPaths` (e.g. `**/.env`, `~/.ssh`, `~/.aws`) are denied for both read and write.
- **Env scrubbing** (`Sandbox.Env`): by default drop provider keys that Pando itself holds (`*_API_KEY`, `*_TOKEN`, `*SECRET*`), `SSH_AUTH_SOCK` when network is restricted, and `DBUS_SESSION_BUS_ADDRESS`. This follows Grok's `shell_environment_policy` and is applied in `shell.go` where it sets `cmd.Env = os.Environ()`.

### C.4 Per-OS mechanism

**Linux** (helper re-exec; no cgo; kernel 5.13 or later for Landlock):
1. Pando spawns `/proc/self/exe __sandbox-exec --policy-fd 3 -- $SHELL -l` and passes the policy JSON on an extra fd (`cmd.ExtraFiles`).
2. The helper runs `runtime.LockOSThread()` and `unix.Prctl(PR_SET_NO_NEW_PRIVS)`.
3. It applies go-landlock `landlock.V5.BestEffort().RestrictPaths(RODirs("/"), RWDirs(roots...), RWFiles(devices...))`.
   - Include the `/dev/tty` ENXIO skip (the Grok lesson).
   - ABI ≥3 adds truncate and ABI ≥4 adds TCP bind/connect. On kernel 6.7+, Landlock itself can block TCP connect, which is an extra layer for `restricted`.
4. When `Network=restricted`, it installs a seccomp filter with the Grok syscall set: connect, bind, sendto, sendmsg, sendmmsg, listen, accept, accept4, io_uring_*. It gates on arch and x32. AF_UNIX to allow-listed sockets can be permitted by filtering `socket()` domain instead (a choice to settle in the story).
5. It installs the namespace-lockdown seccomp filter (`unshare`, `setns`, `mount*`, `clone` with NEW flags; `clone3` returns ENOSYS). A sandboxed process could otherwise create a user namespace and escape bind mounts. Landlock is not bypassable that way, but bwrap binds are.
6. It calls `syscall.Exec(target, argv, env)`.
7. **Protected subpaths:** Landlock cannot deny `<ws>/.pando` inside an RW `<ws>`. Options, in order:
   - (a) If `bwrap` is on PATH and unprivileged user namespaces work, prepend `bwrap --bind / / --ro-bind <protected> <protected> --dev-bind /dev /dev --proc /proc --` (Grok's `bwrap_reexec_command_ex`) and set `--cap-drop ALL`.
   - (b) If not, a Landlock-only fallback grants RW to each top-level workspace entry except the protected ones. This is Grok's devbox trick (`profiles.rs:421-441`). Its drawback is that new top-level files at the workspace root cannot be created. Mitigate by granting `LANDLOCK_ACCESS_FS_MAKE_REG` on the root dir only. **Status must say "protected paths: best effort".**
   - Plus in every case, a defence-in-depth check: the sandbox on/off state is read from **in-memory config changed only through the UI or API**, and on-disk edits to `Sandbox.*` are ignored until restart. Pando warns if the file hash changed after a sandboxed command.
8. **Detection:** `landlock.Get ABI` (the `landlock_create_ruleset` version syscall) and a `bwrap --version` probe run once at startup and are cached in `sandbox.Capability{Backend:"landlock+seccomp"|"bwrap+landlock"|"none", ABI, Reason}`.

**macOS** (`/usr/bin/sandbox-exec`, deprecated but still shipped and used by Codex, Claude Code and Gemini CLI):
- Generate an SBPL profile in `sbpl.go`:
  - `(version 1) (deny default) (allow process-exec process-fork signal sysctl-read mach-lookup ipc-posix-shm ...)`
  - `(allow file-read*)`
  - `(allow file-write* (subpath ws) (subpath tmp...) (literal "/dev/null") ...)`
  - `(deny file-write* (subpath ws/.pando) (literal ws/.pando.toml) (subpath ws/.git/hooks))`, emitted **after** the allows, since last match wins
  - `(allow network*)` or `(deny network-outbound (remote ip))` with `(allow network-outbound (remote unix-socket))` as needed
- Pass it with `-p` and `-D WS=...` parameters, which avoids escaping bugs; validate the path with the Grok control-char check (`deny/mod.rs:26-35`).
- Cover `/private` aliases (`/tmp` <-> `/private/tmp`, `/var`), following Grok's `macos_deny_aliases`.
- Unlike Grok, **network restriction works on macOS**, because SBPL has network rules.
- Hardened-runtime note: `sandbox-exec` is an external binary, so Pando's entitlements (`scripts/pando.entitlements`) are unaffected. Test it inside the notarized desktop app bundle.

**Windows** (be realistic):
- No unprivileged FS sandbox is comparable. The options are AppContainer (needs ACL grants on the workspace, is complex, and breaks many dev tools), restricted token / low integrity (low IL cannot write the medium-IL workspace unless you relabel it), and Codex's experimental restricted-token plus ACL approach.
- v1: report `Backend:"none", Reason:"windows: not supported"`. Keep the **permission prompts** (no auto-allow). Put children in a **Job Object** with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` and no breakaway, for cleanup and resource limits. This is containment, not a sandbox.
- A later spike can explore AppContainer. **WSL counts as Linux** and gets the full sandbox.
- The UI shows the toggle as "On (not enforced on this OS)".

### C.5 Integration points

1. **Persistent shell** (`internal/llm/tools/shell/shell.go`):
   - `newPersistentShell` builds `exec.Command(shellPath, args...)`, then calls `sandbox.Default().Wrap(cmd, policy)`, which rewrites `cmd.Path` and `cmd.Args` and sets `ExtraFiles`.
   - Store the applied policy hash on `PersistentShell`.
   - `GetPersistentShell` re-spawns when the hash differs, so a settings toggle takes effect on the next command (remember that `shellInstanceOnce` is global).
   - Pando writes the `pwd` tmpfile and the stdout/stderr temp files that the shell produces. They must live under a writable temp root, and today they do.
   - `killChildren` uses `pgrep -P <pid>`. With the helper exec'ing in place, the PID is unchanged. With bwrap there is an extra bwrap parent, so use a process group (`Setpgid`) and kill `-pgid`.
2. **Escalation path** (`bash.go`):
   - Add an optional param `sandbox_permissions: "require_escalated"` plus `justification` to `BashParams`.
   - When set, or when the user chose "Run unsandboxed" in the retry prompt, call `permissions.RequestWithContext(ctx, CreatePermissionRequest{ToolName: BashToolName, Action: "execute_unsandboxed", RequireExplicitApproval: true, Description: ..., Params: BashPermissionsParams{Command, Justification, SandboxDenied}})`.
   - On approval, run a **one-shot** unsandboxed `exec.CommandContext($SHELL, "-c", cmd)` in the persistent shell's current cwd, not in the persistent shell itself.
   - "Allow for session" (`GrantPersistant`) caches the grant by `(tool, action, command prefix)`.
3. **Auto-approve** (`bash.go:359`): when `sandbox.Active()` and `Sandbox.AutoAllowBash` (default true on Linux and macOS), skip the prompt unless `isDangerous`, following Grok's floor (`permission/manager/mod.rs:390`). Result metadata records `approved_by: sandbox`.
4. **Violation detection** (`internal/sandbox/detect.go`):
   - After a non-zero exit, scan stderr for `Permission denied`, `Operation not permitted`, `EACCES`, `EPERM`, `Read-only file system`, and for network failures such as `Could not resolve host`, `Network is unreachable` and `connect: operation not permitted` when network is restricted.
   - Cross-check any referenced paths against the policy. On macOS, optionally query `log show --predicate 'sender=="Sandbox"'` for the PID, behind a debug flag because it is slow.
   - Set `BashResponseMetadata.SandboxDenied=true` and `SandboxBackend`.
   - Append to the tool text: `"[sandbox] This command likely failed because Pando's sandbox (mode workspace-write) blocked <write outside workspace|network>. If the operation is necessary, call bash again with sandbox_permissions=\"require_escalated\" and a one-line justification; the user will be asked to approve."`
   - Update `bashDescription()` (`bash.go:119`) to describe the sandbox and the escalation parameter.
5. **Other spawn sites**, via a shared helper `sandbox.WrapCmd(cmd, sandbox.PurposeX)`:
   - ACP client terminals (`mesnada/acp/client.go:401`)
   - skill CLI tools (`skills/tool_bridge.go:111`)
   - the embedded runtime (`runtime/embedded_runtime.go:258`)
   - opt-in per MCP server: `MCPServer.Sandbox bool`, rewriting `resolved.Command` and `Args` in `mcpclient/client.go:65`
   - mesnada spawners: opt-in `Mesnada.SandboxSubagents`
   - Lua: in v1, document that it is unsandboxed. P2: replace the `sh` and `cmd` modules with wrappers that call `WrapCmd`.
6. **Telemetry and logging:**
   - slog events `sandbox.applied`, `sandbox.unavailable`, `sandbox.denied`, `sandbox.escalation.requested|granted|denied` with `session_id` and backend. They flow into the Better Stack telemetry already wired (KB `plan_remote_telemetry_betterstack`), with commands **redacted** through `internal/redact`.
   - Counters in `internal/stats` or `savings`.
   - JSONL at `.pando/data/sandbox-events.jsonl`, written by Pando, never by the sandboxed child.
   - The TUI footer/sidebar and the WebUI chat info show a badge: `sandbox: workspace-write (landlock v5)` / `unavailable`.

### C.6 Config keys (TOML section `[Sandbox]`, JSON `sandbox`)

```toml
[Sandbox]
Disabled       = false          # the UI toggle. Zero value = ON (same as the OutputFilterDisabled precedent)
Mode           = ""             # "" == "workspace-write" | read-only | strict
Network        = ""             # "" == "allowed" | restricted
AutoAllowBash  = ""             # "" == true while enforced; "false" to keep prompts
WritableRoots  = []             # extra dirs (absolute or ~)
ReadOnlyRoots  = []             # used by strict
DenyPaths      = []             # read+write deny, globs allowed (macOS exact, Linux launch-time expansion)
AllowCacheDirs = true           # ~/.cache, go mod, npm, cargo registries...
UseBwrap       = "auto"         # auto|always|never (Linux)
ExtendTo       = []             # "acp-terminals","skills","mcp","subagents"  (default: ["acp-terminals","skills"])
[Sandbox.Env]
Inherit        = "all"          # all|core|none
ExcludeSecrets = true
Exclude        = []
```

- **Precedence:** enterprise overlay lock > env `PANDO_SANDBOX` (`off|workspace-write|...`) > project `.pando.toml` > global config > default. **Project config may only tighten** (following Grok's additive-only rule): a repo-local `.pando.toml` cannot set `Disabled=true` or add `WritableRoots` outside the workspace unless the global config allows it.
- **Model-write lock:** exclude `sandbox.*` from `pando_setup` (`internal/llm/tools/pando_setup.go`) and from any AG-UI or MCP config write path.

### C.7 Testing strategy
- **Unit (all OS):** policy resolution and precedence; the project-only-tightens rule; SBPL generation (golden files); seccomp BPF evaluated by a tiny interpreter (port of Grok `child_net.rs` tests: allowed clone, denied `CLONE_NEWUSER`, wrong arch, x32); detection classifier over a corpus of stderr samples.
- **Linux e2e** (`//go:build linux`, skip when `landlock ABI < 1` or in unprivileged CI without Landlock):
  - `touch $ws/x` works; `touch $HOME/x` gets EACCES
  - `echo >> $ws/.pando.toml` is denied (bwrap and fallback variants)
  - `curl`/`nc`/`bash -c 'exec 3<>/dev/tcp/1.1.1.1/80'` gets EPERM when restricted
  - `unshare -Ur` is denied
  - the persistent-shell cwd survives across commands
  - a policy toggle re-spawns the shell
- **macOS e2e** (`//go:build darwin`, run in the release CI macOS runner, following Grok's `deny_paths_e2e` self-skip pattern): the same matrix plus `/private/tmp` aliases.
- **Windows:** status reports unavailable, prompts are still required, and the Job Object kill works.
- **Integration:** `go test ./internal/llm/tools ./internal/llm/agent ./internal/api`. The bash tool with a fake permission service checks the auto-allow, escalation and denial flows.
- **Python black-box tests** in `tests/` (repo rule), driving `pando -p` / the API.
- **Manual:** TUI toggle, WebUI toggle, desktop app (notarized) on macOS, and WSL2.

### C.8 Risks
- **Breaking dev workflows.** Writes to `~/.gitconfig`, global npm prefix, `pip install --user`, docker socket and IDE caches will fail under workspace-write. Mitigations: the cache-dirs allowlist, the escalation UX, a clear model hint, and a one-click "Off" in settings.
- **The persistent shell is global and long-lived.** Its sandbox is fixed at spawn, so toggling must restart it, which loses the shell's exported vars. Surface this in the UI ("shell restarted").
- **Linux protected paths without bwrap.** Ubuntu 24.04+ restricts unprivileged user namespaces through AppArmor, so bwrap may fail. The Landlock-only fallback is weaker (best-effort self-protection). The file-hash check mitigates this.
- **Old kernels (<5.13) and containers/VMs without Landlock** (some Docker/gVisor hosts). Degrade with a warning.
- **macOS `sandbox-exec` is deprecated** by Apple. It still works on macOS 15 and later, and the whole industry depends on it. Keep the SBPL small and CI-tested.
- **Output-heuristic detection** gives false positives and negatives. Keep the hint phrased as "likely" and never auto-escalate.
- **Security-theatre risk on Windows.** Be explicit in the UI that it is not enforced.
- **ACP-served bash is not covered**, because the client runs it. Document this.
- **Performance** is negligible: one extra exec at shell spawn. Escalated commands spawn a fresh shell each time.

---

## D) Epic breakdown

### Epic: "Host command sandbox (on by default, container-free)"

**Description.** Pando runs agent-generated shell commands directly on the developer's host, with the full privileges of the user. Only a first-word banned list and approval prompts limit this. The epic adds an OS-native sandbox:
- Linux: Landlock + seccomp, with bwrap when available
- macOS: Seatbelt through `sandbox-exec`
- Windows: graceful degradation with Job Object containment

It confines commands the agent executes to the workspace and temp dirs, protects Pando's own configuration and git hooks, optionally blocks child network, and lets the user approve one-off unsandboxed runs when a command legitimately needs more access. The sandbox is on by default, applies per spawned process (Pando itself stays unconfined), and can be switched off or tuned from TUI settings, WebUI settings and config. The design follows Grok Build's `xai-grok-sandbox` crate (profiles, protected paths, seccomp set, event log), adapted to Go and to per-spawn wrapping.

**Epic acceptance criteria**
- [ ] On Linux (kernel ≥5.13) and macOS, a fresh install runs the bash tool sandboxed in `workspace-write` mode without any user action.
- [ ] Under the default mode, a sandboxed command cannot write outside the workspace, temp dirs or allow-listed caches, and cannot modify `.pando.toml`, `.pando/` or `.git/hooks`. This is verified by e2e tests on both OSes.
- [ ] The sandbox can be disabled or its mode changed from TUI settings, WebUI settings, and `[Sandbox]` in config. The change takes effect on the next command without restarting Pando.
- [ ] The agent cannot disable or loosen the sandbox (not through `pando_setup`, project config, or editing config files from a sandboxed command).
- [ ] A command blocked by the sandbox returns an explanatory hint to the model. An escalated re-run requires explicit user approval in TUI, WebUI and ACP permission flows.
- [ ] On unsupported platforms (Windows, old kernels), Pando shows "sandbox unavailable", keeps permission prompts, and logs the reason once.
- [ ] Sandbox status, denials and escalations are logged (slog plus optional telemetry, with commands redacted) and shown as a status badge.
- [ ] Docs and a KB entry are added. `go test ./internal/llm/agent ./internal/api ./internal/sandbox/... ./internal/llm/tools/...` passes.

---

#### Story 1: sandbox core package, policy model and config (5 pts)
**As a** Pando maintainer **I want** a platform-neutral `internal/sandbox` package with a policy model and config section **so that** every spawn site and UI shares one definition of "sandboxed".

Implementation:
- New `internal/sandbox/{policy.go,sandbox.go,detect_caps.go,other.go}`:
  - `Policy`, `Mode` (`workspace-write|read-only|strict|off`), `Network`
  - `Resolve(cfg *config.Config, workspace string) Policy`
  - `Capability{Backend, Version, Enforced, Reason}` and `Default() Wrapper`
- Writable roots: the workspace (from `config.WorkingDirectory()`), temp dirs (following Grok `paths.rs:45-68`), and cache allowlist dirs.
- Protected paths: `<ws>/.pando`, `<ws>/.pando.toml`, `$HOME/.pando.toml`, the global config dir, `<ws>/.git/hooks` and `<ws>/.git/config`.
- `internal/config/config.go`: add `Sandbox SandboxConfig` to `Config` (around `:1091`). Zero value means ON; `Disabled` is a bool, the rest are strings where empty means default. Add `UpdateSandbox(SandboxConfig) error` following `UpdateContainer` (`:5530`).
- Project-config tightening only: the merge logic in config load ignores `Disabled=true` and broader `WritableRoots` coming from a project-level file.
- Env override `PANDO_SANDBOX`.
- Honour `overlay.LockedKeys()` for `sandbox.*`.
- Update `pando-schema.json` and `schema/`.

Acceptance:
- [ ] `Resolve` unit tests cover each mode, precedence (lock > env > project > global > default) and the project-cannot-loosen rule.
- [ ] An empty config resolves to `workspace-write`, network allowed and auto-allow on.
- [ ] `config` tests call `isolateGlobalConfig(t)` (repo pitfall).

Dependencies: none.

#### Story 2: Linux backend with Landlock + seccomp helper re-exec (8 pts)
**As a** Linux user **I want** agent commands confined by the kernel **so that** a bad or injected command cannot damage files outside my project.

Implementation:
- Add deps `github.com/landlock-lsm/go-landlock` and `github.com/elastic/go-seccomp-bpf` (or a hand-rolled BPF using `golang.org/x/net/bpf`). Both are pure Go.
- `internal/sandbox/linux.go`: `Wrap(cmd, p)` rewrites the command to `os.Executable() __sandbox-exec -- <orig argv>`, passes the policy as JSON on `ExtraFiles[0]` (fd 3), and sets `SysProcAttr.Setpgid=true`.
- `internal/sandbox/helper.go`: `RunHelper(args)`, dispatched from `main.go` **before** cobra, config and logging init. Steps:
  1. `LockOSThread`
  2. `PR_SET_NO_NEW_PRIVS`
  3. go-landlock `BestEffort().RestrictPaths(...)` with a device allowlist and the `/dev/tty` ENXIO skip
  4. the namespace-lockdown seccomp filter
  5. optionally the network seccomp filter (syscall set from Grok `child_net.rs:178-199`)
  6. `unix.Exec`
- When `UseBwrap=auto|always` and the `bwrap` probe succeeds, prefix `bwrap --cap-drop ALL --bind / / --ro-bind <protected>... --dev-bind /dev /dev --proc /proc --` (following Grok `lib.rs:300-368`). Otherwise use the Landlock-only fallback, which grants RW per top-level workspace entry excluding protected ones.
- Capability probe via `landlock.ABI`/`landlock_create_ruleset(NULL,0,VERSION)` and `bwrap --version`, cached.

Acceptance:
- [ ] The e2e matrix passes on a Landlock kernel: workspace write allowed, `$HOME` write denied, `.pando.toml` write denied (bwrap and fallback), `unshare -Ur` denied, network denied only when `restricted`.
- [ ] A BPF interpreter unit test covers the syscall sets, wrong arch and x32.
- [ ] On a kernel without Landlock, `Capability.Enforced=false` with a reason, and commands still run.
- [ ] The helper never loads config or DB, verified by a startup-time test under 20 ms.

Dependencies: S1.

#### Story 3: macOS backend with a Seatbelt profile via sandbox-exec (5 pts)
**As a** macOS user **I want** the same confinement **so that** the default protection is equal on my platform.

Implementation:
- `internal/sandbox/darwin.go` and `sbpl.go`: generate the SBPL.
  - deny default; allow process, sysctl-read, mach-lookup, file-read*
  - allow file-write* on the writable roots plus `/dev/null`, `/dev/tty`, `/dev/ptmx`, `/dev/ttys*`
  - deny file-write* on protected paths, emitted after the allows
  - network* allowed, or outbound denied except unix sockets when restricted
- Parameters go through `-D` to avoid escaping bugs. Reject control characters (Grok `deny/mod.rs:26-35`) and add `/private` aliases (Grok `macos_deny_aliases`).
- `Wrap` sets `cmd.Path=/usr/bin/sandbox-exec` and `Args=[sandbox-exec -p <profile> -D ... -- orig...]`.
- Capability check: `/usr/bin/sandbox-exec` exists and a trivial `true` run under the profile succeeds.

Acceptance:
- [ ] Golden-file tests for SBPL output in each mode.
- [ ] Darwin e2e (build tag, skipped elsewhere): the same matrix as S2, plus `/tmp` vs `/private/tmp` alias denies and network blocked in `restricted`.
- [ ] Verified in the signed, notarized desktop build (`desktop/`) and the CLI zip.

Dependencies: S1.

#### Story 4: Windows degradation and Job Object containment (3 pts)
**As a** Windows user **I want** Pando to tell me honestly that commands are not sandboxed and still clean up child processes **so that** I am not given a false sense of safety.

Implementation:
- `internal/sandbox/windows.go`: `Capability{Backend:"none", Reason:"not supported on windows"}`.
- `Wrap` assigns the child to a Job Object (`golang.org/x/sys/windows` `CreateJobObject`, `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`) after start, through a post-start hook in the shell.
- Auto-allow is forced off so prompts remain.
- Detect WSL on the Linux side (`/proc/sys/fs/binfmt_misc/WSLInterop`) and report it as full Linux support.
- Add a spike ticket note for AppContainer.

Acceptance:
- [ ] On Windows, settings and badge show "On (not enforced on this OS)", and bash still prompts.
- [ ] Killing Pando kills the shell's process tree (manual test).
- [ ] Cross-compile succeeds with `GOOS=windows` for both CGO modes.

Dependencies: S1.

#### Story 5: bash tool and persistent shell integration (5 pts)
**As a** developer **I want** the bash tool's persistent shell to start inside the sandbox and restart when the policy changes **so that** the default is protected and toggles apply immediately.

Implementation:
- `internal/llm/tools/shell/shell.go` `newPersistentShell`:
  - apply env scrubbing (replacing `os.Environ()` at `:94`), then `sandbox.Default().Wrap(cmd, policy)`
  - store `policyHash`
  - `GetPersistentShell` re-spawns when `sandbox.CurrentPolicyHash() != shellInstance.policyHash`
  - `killChildren` (`:248`) switches to killing the process group, because a bwrap parent sits in between
- `internal/runtime/host.go`: no change beyond using the same shell. `embedded_runtime.go:258`: wrap its `exec.CommandContext`.
- `internal/llm/tools/bash.go`:
  - when `sandbox.Active() && policy.AutoAllowBash && !isDangerous`, skip `permissions.Request` (`:359`)
  - add `SandboxBackend`, `SandboxMode` and `ApprovedBySandbox` to `BashResponseMetadata`
  - update `bashDescription()` (`:119`)
- Skip wrapping when `Container.Runtime` resolves to docker or podman.

Acceptance:
- [ ] `go test ./internal/llm/tools/...` covers wrapping, the auto-allow floor (dangerous commands still prompt) and re-spawn on policy change.
- [ ] Manual: toggling Off in settings, the next `touch ~/x` succeeds; toggling On again, it fails.
- [ ] Provider API keys are absent from `env` inside the sandboxed shell by default.

Dependencies: S2 and/or S3, S1.

#### Story 6: violation detection, model hint and escalation approval (8 pts)
**As a** developer **I want** blocked commands explained to the agent, and a way to approve an unsandboxed re-run **so that** the sandbox never becomes a dead end.

Implementation:
- `internal/sandbox/detect.go`: `Classify(exitCode, stderr, policy) (Denial{Kind: fs|net, Evidence})`, with patterns for EACCES, EPERM, read-only FS and network errors, plus path cross-checks.
- `bash.go`:
  - on a denial, append the `[sandbox]` hint and set `metadata.SandboxDenied`
  - new params `sandbox_permissions` (`"use_default"|"require_escalated"`) and `justification`
  - an escalated request calls `permissions.RequestWithContext` with `Action:"execute_unsandboxed"` and `RequireExplicitApproval:true`, then runs a one-shot unsandboxed `$SHELL -c` in the persistent shell's cwd
- Dialogs show the "Run outside sandbox" wording, the justification and a warning style:
  - TUI `internal/tui/components/dialog/permission.go`
  - WebUI `web-ui/src/components/chat/PermissionDialog.tsx`
  - ACP permission options, following the existing ACP handler path
  - AG-UI HITL
- "Allow for session" stores the grant (`GrantPersistant`) by command prefix. **Global auto-approve and goal/autopilot mode must never auto-grant `execute_unsandboxed`** unless `Sandbox.AllowAutoEscalation=true`.

Acceptance:
- [ ] A classifier table test covers at least 20 stderr samples (Linux, macOS, and false-positive cases).
- [ ] Agent test: a command fails under the sandbox; the model is shown the hint; an escalated call raises exactly one explicit-approval prompt; on deny it returns `permission denied`; on allow it runs unsandboxed.
- [ ] Auto-approve and goal mode still prompt for escalation.
- [ ] `go test ./internal/llm/agent ./internal/api` passes.

Dependencies: S5.

#### Story 7: settings toggle in TUI and WebUI, plus the API (5 pts)
**As a** user **I want** to see sandbox status and turn it on, off or change its mode from the settings screens **so that** I control the trade-off without editing TOML.

Implementation:
- API: `internal/api/handlers_config.go` `handleConfigSandbox` (GET/PUT) returns `{config, capability}`. Route `/api/v1/config/sandbox` in `internal/api/routes.go` (near `:97`). It returns 403 when `sandbox.*` is a locked key.
- TUI: `internal/tui/page/settings.go` `buildSandboxSection(cfg)` in the "Tools" group, before Bash (`:1000-1002`). Fields:
  - `sandbox.enabled` (FieldToggle, `boolString(!Disabled)`)
  - `sandbox.mode` and `sandbox.network` (select)
  - `sandbox.autoAllowBash` (toggle)
  - `sandbox.writableRoots` and `sandbox.denyPaths` (text, comma list)
  - `sandbox.backend` (read-only status such as "landlock v5 + bwrap" or "unavailable: …")
  - save cases next to `case "bash.outputFilter":` (`:5925`)
- WebUI:
  - new `web-ui/src/components/settings/SandboxSettings.tsx`
  - `useSandboxStore` in `web-ui/packages/pando-client/src/stores/settingsStore.ts`
  - category entry in `SettingsView.tsx` (`:38-67`, render around `:349`)
  - i18n keys in all 7 locale files
- Remove `sandbox.*` from what `internal/llm/tools/pando_setup.go` can write.
- Status badge: TUI chat info sidebar and footer; WebUI chat info panel.

Acceptance:
- [ ] Toggling in the TUI or WebUI persists to config and changes the next command's behaviour without a restart.
- [ ] A locked key renders read-only in both UIs.
- [ ] `pando_setup` refuses sandbox keys (test).
- [ ] API handler tests exist (`internal/api`).
- [ ] The WebUI builds (`bun run build`) and mobile master-detail layout still works.

Dependencies: S1 (the UI can be built against the API before S2 and S3 land).

#### Story 8: extend coverage to other agent-driven spawn sites (5 pts)
**As a** security-conscious user **I want** commands the agent reaches indirectly (sub-agent terminals, skill tools, optionally MCP and sub-agent CLIs) sandboxed too **so that** the protection cannot be bypassed through another tool.

Implementation:
- `sandbox.WrapCmd(cmd, purpose)` applied at:
  - `internal/mesnada/acp/client.go:401` (ACP terminals Pando serves, default on)
  - `internal/skills/tool_bridge.go:111` (default on)
  - `internal/mcpclient/client.go:65` (rewrite `resolved.Command`/`Args` when `MCPServer.Sandbox` or `ExtendTo` contains "mcp"; default off)
  - `internal/mesnada/agent/spawner*.go` (opt-in `ExtendTo:"subagents"`, whose policy adds `~/.claude`, `~/.copilot` and similar as writable)
- Lua: a P2 follow-up to wrap the `sh`/`cmd` modules (`internal/luaengine/lua.go`). For now, document it as unsandboxed.
- Never wrap user terminals (`internal/api/terminal_pty.go`, TUI terminal) or `cliassist`.

Acceptance:
- [ ] Tests show an ACP sub-agent terminal writing outside the workspace is denied under the default policy.
- [ ] An MCP stdio server with `Sandbox=true` still completes the initialize handshake.
- [ ] The docs table lists each spawn site and its coverage.

Dependencies: S2/S3, S5.

#### Story 9: observability, events and CLI status (3 pts)
**As a** user or operator **I want** to see why and when the sandbox acted **so that** I can debug failures and audit escalations.

Implementation:
- `internal/sandbox/events.go`: slog Info `sandbox.applied`, `sandbox.unavailable` (once per process), `sandbox.denied`, `sandbox.escalation.{requested,granted,denied}` with `session_id` and backend. Commands are redacted through `internal/redact`.
- Ring buffer plus `.pando/data/sandbox-events.jsonl`, written by Pando only.
- Counters are exposed through `pando_stats`/`internal/stats`, and the events are forwarded to the existing Better Stack telemetry when opt-in is enabled.
- CLI `pando sandbox status` (capabilities, effective policy, protected paths) and `pando sandbox exec -- <cmd>` for manual testing.
- Include the sandbox block in `/doctor`-style diagnostics if such a command exists.

Acceptance:
- [ ] `pando sandbox status` prints the backend, ABI, mode, roots and reason on all three OSes.
- [ ] Denial and escalation events show up in the logs page (TUI and WebUI) and JSONL.
- [ ] No raw secrets appear in events (redaction test).

Dependencies: S1, S6.

#### Story 10: cross-platform test suite, CI and documentation (5 pts)
**As a** maintainer **I want** automated e2e coverage and user docs **so that** sandbox regressions are caught and users understand the defaults.

Implementation:
- Go e2e under `internal/sandbox/e2e_linux_test.go` and `e2e_darwin_test.go`, which self-skip without kernel support (following Grok `tests/deny_paths_e2e.rs`).
- Python black-box tests in `tests/sandbox/` (repo rule), driving the bash tool through the API.
- CI: add a Linux job with a Landlock-capable runner (GitHub ubuntu-latest has Landlock ABI ≥3) and a macOS job. Both are in the `digiogithub/ci-actions` workflows.
- Docs: `docs/` user guide "Sandbox" (modes table, protected paths, escalation, platform matrix, how to disable), and a README mention.
- Save the KB doc `pando/features/host-command-sandbox.md` and add a MEMORY index entry (project rule).

Acceptance:
- [ ] CI runs the Linux and macOS e2e suites on every PR touching `internal/sandbox`, `internal/llm/tools/shell` or `bash.go`.
- [ ] The docs cover the "off" path and the limitations (Windows, Linux without bwrap, ACP-client bash, Lua).
- [ ] The KB doc is written.

Dependencies: S2, S3, S5, S6, S7.

#### Optional follow-ups (not in the estimate)
- **Network allowlist proxy.** An in-process HTTP(S)/SOCKS proxy with a domain allowlist; the sandbox allows only loopback to the proxy (Claude Code sandbox-runtime / Codex style). About 8 pts.
- **Windows AppContainer spike.** About 5 pts.
- **Sandbox Lua `sh`/`cmd` modules.** About 3 pts.

**Total: 52 story points** (S1 5, S2 8, S3 5, S4 3, S5 5, S6 8, S7 5, S8 5, S9 3, S10 5).

**Critical path:** S1 -> (S2 ∥ S3 ∥ S4 ∥ S7) -> S5 -> S6 -> (S8 ∥ S9) -> S10.

---

### Key differences from Grok to keep in mind
- Grok ships the sandbox **off** by default and makes it process-wide and irreversible, with no escalation. Pando wants **on by default plus a toggle**, so it must use per-spawn wrapping and a Codex-style escalation flow.
- Grok's macOS network restriction is a no-op. Pando can do better with SBPL network rules.
- Grok has no Windows support. Pando should degrade honestly.
- Grok protects its own config and hook files from the agent. Pando must do the same for `.pando.toml`, `.pando/` and `.git/hooks`, and must block `pando_setup` from changing sandbox keys.
