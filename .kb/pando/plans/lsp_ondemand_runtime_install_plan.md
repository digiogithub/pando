---
created_at: 2026-07-24T14:03:55.567470157Z
updated_at: 2026-07-24T14:03:55.567470157Z
tags:
    - plan
    - lsp
    - diagnostics
    - pando
---
# Plan: LSP on-demand runtime + auto-install (bun/npx) + strict edit-driven activation

Related: [[lsp_auto_activation]], [[lsp_auto_activation_plan]], [[lsp_preset_catalog_correction_plan]],
[[lsp-diagnostics-wait-for-lazy-start]], [[lsp_auto_activation_progress]]

Date: 2026-07-24. Status: PLANNED (not implemented).

## 1. Current state of Pando (verified by reading the code)

- `internal/config/lsp_presets.go` — 21 built-in presets: `{Name, Description, Config{Command, Args, Languages}}`.
  Binary-on-PATH only; no package name, no install metadata.
- `internal/config/lsp_registry.go` — `LSPRegistry()` merges presets + user `[LSP.*]` into
  `ResolvedLSPServer{Name, Command, Args, Languages, Disabled, Autostart, Source}`;
  `LSPServersForExt/ForFile`, `LSPAutostartServers`.
- `internal/app/lsp.go` — `EnsureLSPForFile(ctx, path)`: ext match, skip running/spawning/broken,
  `exec.LookPath` gate (var `lspLookPath`, stubbable), spawn in goroutine, only the FIRST installed
  preset per extension starts (user-configured servers always honored). `WaitForFile` polls 50 ms up
  to a hard 3 s. `createAndStartLSPClient` registers the client in `app.LSPClients` only AFTER
  `InitializeLSPClient` + `WaitForServerReady`.
- `internal/app/lsp_bootstrap.go` — workspace-wide fsnotify watcher that calls `EnsureLSPForFile` on
  ANY external write/create. Started from `initLSPClients` whenever `LSPAutoActivate` is true.
- Activation triggers today: `view.go`, `edit.go`, `write.go`, `patch.go`, `diagnostics.go`
  (`lspProvider.EnsureForFile`), `internal/tui/page/chat.go:811` (file selected in tree/editor),
  plus the bootstrap watcher.
- `internal/llm/tools/diagnostics.go` — on no matching client it FALLS BACK to `Clients()` (all
  running servers, unrelated diagnostics), else returns one generic sentence; no install guidance.
- Config: `LSPAutoActivate bool` (default true), per-server `Disabled`, `Autostart`.

## 2. Reference implementations

### OpenCode (`packages/opencode/src/lsp/`, TypeScript)
- `server.ts` (~2 kLOC): ~40 server definitions as `Info{id, extensions, root(file,ctx), spawn(root,ctx,flags)}`.
  `root` = nearest-marker resolution (`NearestRoot(["package-lock.json","bun.lock",…], exclude ["deno.json"])`),
  so one server instance PER PROJECT ROOT, not per workspace.
- Binary resolution per server: `which(cmd)` (PATH + `Global.Path.bin`) → else `Npm.which(pkg[, bin])`.
- `packages/core/src/npm.ts` — `Npm.which` installs the package into a Pando-equivalent global cache
  (`<cache>/packages/<pkg>/node_modules/.bin/<bin>`) using `@npmcli/arborist`, then returns the staged
  binary path. Not `bunx`/`npx` — a persistent staged install, file-locked, dedup'd.
- Non-npm servers download release archives (zips) into `Global.Path.bin` (eslint, jdtls, zls, lua-ls…),
  or use the toolchain (`deno lsp`, `dart language-server`, `go`/`gopls`).
- `flags.disableLspDownload` kills every download path.
- `lsp.ts` `getClients(file)`: extension filter → `root()` → `broken` set keyed `root+serverID` →
  `spawning` map holds the in-flight promise, so concurrent callers AWAIT the same spawn instead of
  racing it. Callers always get a ready client.

### Hermes (`agent/lsp/`, Python)
- `servers.py` — ~28 `ServerDef` entries with `extensions`, root markers, install package.
- `install.py` — `INSTALL_RECIPES{pkg: {strategy: npm|go|pip|manual, pkg, bin, extra_pkgs}}`;
  `try_install(pkg, strategy)` installs into `<HERMES_HOME>/lsp/bin` (npm `--prefix`, `go install` with
  `GOBIN`, `pip --target`), symlinks the bin, per-package lock + memoized result, every failure
  non-fatal. `detect_status(pkg) -> installed|missing|manual-only` powers `hermes lsp status`.
  Notable: `typescript-language-server` gets `extra_pkgs: ["typescript"]` (peer dep npm won't pull).
  Heavy servers (rust-analyzer, clangd, lua-ls) are deliberately `manual`.
- Gating: LSP only runs inside a git workspace; diagnostics are baselined before a write and
  diff'd after, so only NEW diagnostics reach the model.

### langserver.org
Confirms the catalogue: bash/yaml/json/html/css/dockerfile/vim/graphql/xml/turtle/sparql are Node/npm
(installable via bunx/npx); gopls/terraform-ls/helm-ls/jsonnet/regal are Go; rust-analyzer/texlab/
marksman/gleam/glsl/svls are Rust binaries; jdtls/lemminx/groovy/ltex are Java; sourcekit-lsp ships
with Swift; solargraph/ruby-lsp are gems; pylsp/jedi/esbonio/robotframework are pip.

## 3. Gaps to close

1. No package-manager fallback: a server whose binary is not on PATH is permanently `broken`, even
   when `bun`/`node` could run it.
2. `diagnostics` gives no actionable "install X" message and pollutes output with unrelated servers.
3. Servers can start without Pando having edited anything (bootstrap watcher, `view`, TUI tree select)
   — user requirement: NOTHING starts until Pando actually edits/writes a file.
4. `WaitForFile`'s hard 3 s is too short for a cold `gopls`/`tsserver`, and hopeless for a first-run
   npm install; the tool then reports "no LSP clients".
5. Preset catalogue is small and carries no install metadata.

## 4. Design

### P1 — Runtime/package-manager detection (`internal/lsp/runtime/` new package)
- `DetectNodeRuntime() Runtime` — probes in order `bun`, then `node`+`npx`; memoized `sync.Once`,
  result `{Kind: bun|npm|none, Exec, RunnerExec}`. Also probe `go`, `pipx`/`uv`, `cargo` for the
  install-hint text only (never auto-run heavy toolchains).
- `BinDir()` = `<GlobalConfigDir()>/lsp/bin` (mirrors Hermes `<HERMES_HOME>/lsp/bin`), always
  prepended to the resolution path.
- `EnsurePackage(ctx, pkg string, bin string, extra []string) (path string, err error)`:
  1. `BinDir()/bin` exists+executable → return.
  2. `bun add --cwd <lspdir>` / `npm install --prefix <lspdir> --no-audit --no-fund` (timeout 300 s,
     `stdin=devnull`), then symlink `<lspdir>/node_modules/.bin/<bin>` into `BinDir()`.
  3. Fallback ephemeral runner: return `bunx -y <pkg>` / `npx -y <pkg>` as command+args (works, slower).
  - Single-flight per package (`golang.org/x/sync/singleflight` or a `map[string]*sync.Once`), memoized
    failures, never fatal.

### P2 — Preset metadata + resolution
- `LSPPreset` gains `Install LSPInstall{Strategy: npm|go|toolchain|manual, Package, Bin, ExtraPackages,
  Hint, URL}`. `ResolvedLSPServer` carries it through (user config may override `Command`/`Args` as today).
- New `app.resolveLSPCommand(s) (cmd string, args []string, status)` with status
  `available | installable | manual`:
  1. `lspLookPath(s.Command)`.
  2. `runtime.BinDir()/s.Install.Bin`.
  3. `Install.Strategy == npm` && node runtime present && `LSPAutoInstall` → `EnsurePackage` (async;
     first call returns `installing`).
  4. else → `manual`, keep `Install.Hint` for the diagnostics message.
- `app.lspUnavailable map[string]UnavailableReason` replaces the opaque `lspBroken` entry so the reason
  (missing binary / no node runtime / install failed / init failed) survives to the tool layer.

### P3 — Strict edit-driven activation (user requirement)
- New config `LSPActivateOn` (`edits` default | `reads` | `workspace` | `off`):
  - `edits` — only `edit`/`write`/`patch` (+ explicit `diagnostics` call) activate. `view.go` and the
    TUI file-tree selection stop activating; the bootstrap watcher is NOT started.
  - `reads` — today's tool behaviour (adds `view` + TUI open).
  - `workspace` — today's full behaviour incl. `lsp_bootstrap.go` watcher.
- `LSPAutoActivate=false` still means "nothing but `Autostart=true`".
- Consequence: a session that never edits a file spawns zero language servers — the requested
  optimisation.

### P4 — Tool calls wait for a READY server
- `LSPProvider.WaitForFile` gets a budget instead of the hard 3 s: `LSPStartupTimeout` (default 20 s),
  extended to `LSPInstallTimeout` (default 120 s) while an install for that extension is in flight;
  poll loop already exits early once startup settles.
- Wait on `client.GetServerState() == StateReady` (registration already implies ready today, but the
  install path makes the distinction real).
- `edit`/`write`/`patch` keep triggering activation fire-and-forget (no latency cost); only
  `diagnostics` blocks.
- `diagnostics.go`: drop the "fall back to all clients" branch when a `file_path` was given. On
  timeout/unavailable return a structured, actionable message, e.g.
  `no language server for .py: pyright is not installed. bun detected — Pando can install it
  automatically (set LSPAutoInstall=true), or run: bun add -g pyright` /
  `... install manually: go install golang.org/x/tools/gopls@latest`.
- Optional: baseline+diff diagnostics after a write (Hermes model) — separate follow-up, not this plan.

### P5 — Catalogue expansion (presets + install metadata)
npm strategy (auto-installable via bun/npx): typescript-language-server(+typescript), pyright,
vue, svelte, astro, yaml, json/html/css (vscode-langservers-extracted), bash, dockerfile, intelephense,
graphql, vim, prisma, eslint, oxlint, biome, sql.
Toolchain strategy: gopls (`go install`), deno lsp, dart language-server, zls, gleam lsp, metals,
solargraph/ruby-lsp (gem), pylsp/jedi/esbonio (pip/uv), texlab, marksman, taplo, terraform-ls, nixd,
clojure-lsp, ocaml-lsp, haskell-language-server, sourcekit-lsp, roslyn/omnisharp, jdtls, kotlin-ls,
lemminx, rust-analyzer, clangd, lua-language-server.
Heavy servers stay `manual` (Hermes precedent): rust-analyzer, clangd, lua-language-server, jdtls.

### P6 — Surfaces + docs
- Settings (TUI + WebUI) LSP section: per-server status `installed | installable (bun/npm) | manual`
  + the install hint, plus the new `LSPActivateOn` / `LSPAutoInstall` toggles.
- `pando_setup` tool + README "Language Servers (LSP)" section updated.
- KB summary doc after implementation (mandatory).

## 5. Config surface (new)

| Key | Default | Meaning |
|---|---|---|
| `LSPActivateOn` | `"edits"` | what triggers on-demand activation (`edits`/`reads`/`workspace`/`off`) |
| `LSPAutoInstall` | `true` | allow installing npm-based servers with bun/npm into `<config>/lsp/bin` |
| `LSPRunner` | `"auto"` | `auto` (bun→npm), `bun`, `npm`, `off` |
| `LSPStartupTimeout` | `20s` | diagnostics wait for a ready server |
| `LSPInstallTimeout` | `120s` | extended wait while installing |

`LSPAutoActivate` and per-server `Disabled`/`Autostart` keep their current meaning.

## 6. Risks
- Network installs in sandboxed/offline environments → every failure memoized and non-fatal; `LSPAutoInstall=false`
  fully disables.
- `npx -y` re-resolves on every spawn (slow); persistent staged install is the primary path, ephemeral runner
  the fallback.
- Concurrency: single-flight per package + existing `lspSpawning` dedupe.
- Blocking the `diagnostics` tool for up to 120 s on first install — mitigate by returning early with an
  "installing, retry" message once `LSPStartupTimeout` elapses while an install is still running.

## 7. Test plan
- `internal/lsp/runtime`: detection with stubbed `LookPath`; `EnsurePackage` with a fake package manager.
- `internal/config`: preset install metadata + registry resolution.
- `internal/app`: `resolveLSPCommand` matrix (PATH hit / bin-dir hit / installable / manual);
  `LSPActivateOn` gating (no server started for `view` under `edits`).
- `internal/llm/tools`: diagnostics message content for each unavailable reason.
- `go build ./... && go test ./internal/app ./internal/config ./internal/llm/tools ./internal/lsp/...`
