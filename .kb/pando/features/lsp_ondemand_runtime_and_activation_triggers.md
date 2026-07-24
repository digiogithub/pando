---
created_at: 2026-07-24T14:21:00.74876336Z
updated_at: 2026-07-24T14:33:25.641891342Z
tags:
    - feature
    - lsp
    - diagnostics
    - pando
---
# LSP on-demand: runtime provisioning, activation triggers, ready-gated diagnostics

Status: **P1, P2, P3, P4 IMPLEMENTED** (2026-07-24). P5 (catalogue expansion)
and P6 (TUI/WebUI surfaces, README) of
[[lsp_ondemand_runtime_install_plan]] remain pending.

Implements the first four phases of
`pando/plans/lsp_ondemand_runtime_install_plan.md`, derived from the OpenCode
`lsp/` + `core/npm.ts` design and Hermes' `agent/lsp/install.py`.
Builds on [[lsp_auto_activation]] and [[lsp_preset_catalog_correction_plan]].

## P1 — `internal/lsp/runtime` (new package)

Detects the host JavaScript toolchain and provisions npm-distributed language
servers, so a server missing from PATH is no longer a dead end.

- `runtime.go`: `Manager` (`ManagerNone|ManagerBun|ManagerNpm`),
  `NodeRuntime{Manager, Install, Exec, ExecArgs}`, `Available()`.
  `Detect()` memoizes with `sync.Once`: probes `bun` first, then `node`.
  Without the `bunx` shim it falls back to `bun x` (`ExecArgs=["x"]`); `node`
  alone (no npm, no npx) is reported unusable. `lookPath` is a package variable
  for tests; `SetForTests(rt)` pins detection for other packages.
- `install.go`: `Package{Spec, Bin, Extra}` (Extra covers peer dependencies,
  e.g. `typescript`), `Result{Command, Args, Installed}`.
  Layout `RootDir()`=`<GlobalConfigDir()>/lsp`, `BinDir()`=`<RootDir>/bin`,
  `packageDir(spec)`=`<RootDir>/packages/<sanitized>` — one isolated
  `node_modules` per package.
  `Staged(p)` is two stat calls. `Ensure(ctx, p, timeout)` resolves staged →
  install → ephemeral runner, using `bun add --cwd` or
  `npm install --prefix --no-audit --no-fund --loglevel error` with a generated
  stub `package.json`, then symlinks the bin into `BinDir()`.
  `Runner(rt, p)` builds `npx -y --package <spec> -- <bin>`, or `bunx <bin>`
  only when the bin matches the unscoped package name (bunx resolves the
  package from the executable name). `installOnce` deduplicates concurrent
  installs and memoizes outcomes; a failed install is never retried in-process.
  `DefaultInstallTimeout = 300s`.

## P2 — install metadata and command resolution

- `internal/config/lsp_presets.go`: new `LSPInstallStrategy`
  (`none|npm|manual`) and `LSPInstall{Strategy, Package, Bin, ExtraPackages,
  Hint, URL}`; `LSPPreset.Install` filled for all 21 presets. npm: pyright
  (bin `pyright-langserver`), typescript-language-server (+`typescript`),
  bash/yaml/json/html/css (`vscode-langservers-extracted`), intelephense.
  Manual with a concrete hint: gopls, rust-analyzer, pylsp, clangd, lua-ls,
  marksman, jdtls, solargraph, zls, kotlin-ls, omnisharp, dartls, elixir-ls.
- `internal/config/lsp_registry.go`: `ResolvedLSPServer.Install` propagated from
  the preset, and **cleared when the user overrides `Command`** — Pando never
  installs a recipe for a binary the user chose. Toggling only
  `Autostart`/`Disabled` keeps the recipe.
- `internal/app/lsp_resolve.go` (new): `LSPAvailability`
  (`LSPAvailable|LSPInstallable|LSPManual`), `LSPResolution{Command, Args,
  Availability, Reason}`, `resolveLSPCommand(s)` (PATH → staged binary →
  installable, honoring `LSPAutoInstall` and runtime availability),
  `installPackage`, `manualReason`, `installHint`, `installLSPServer` (runs the
  install off the request path, tracked in `app.lspInstalling`),
  `lspInstallInProgress`, `UnavailableReason(path)`.
- `internal/app/app.go`: `lspBroken map[string]struct{}` replaced by
  `lspUnavailable map[string]lspUnavailableEntry` (availability + reason), plus
  `lspInstalling`.
- `internal/app/lsp.go`: `ensureLSPServer` resolves outside the lock, records
  the reason on `LSPManual`, and for `LSPInstallable` installs in the spawn
  goroutine before starting. `restartLSPClient` re-resolves too, so a
  Pando-installed server (which lives in the staging directory, not on PATH)
  can be restarted.

## P3 — activation triggers (`LSPActivateOn`)

No language server may start at boot when nothing has been edited.

- `internal/config/lsp_activation.go` (new): `LSPTrigger` (`edit`, `read`,
  `workspace`, `explicit`), modes `off|edits|reads|workspace`,
  `LSPActivationMode()`, `LSPActivationAllows(trigger)`,
  `LSPWorkspaceWatchEnabled()`. `LSPAutoActivate=false` disables everything;
  `off` blocks even an explicit diagnostics request.
- `EnsureLSPForFileTrigger` is the new entry point; `EnsureLSPForFile` is the
  edit-trigger shorthand. `EnsureForFileTrigger` added to `tools.LSPProvider`.
- Call sites: `view.go`=read, `diagnostics.go`=explicit,
  `lsp_bootstrap.go`=workspace, TUI `ensureLSPCmd`=read;
  edit/write/patch keep the edit trigger. The workspace fsnotify watcher only
  starts in `workspace` mode.

## P4 — wait for a ready server, actionable failures

- `WaitForFile` now waits for `lsp.StateReady` (new `readyClientsForFile`), not
  merely for a registered client. Budget = `LSPStartupTimeout` (20s), extended
  once to `LSPInstallTimeout` (120s) when `lspInstallInProgress(path)` — the
  previous hard 3s deadline could not cover a cold gopls, let alone an install.
- `diagnostics.go`: dropped the "fall back to all clients" branch, which
  reported another language's diagnostics; on failure it returns
  `UnavailableReason(path)`, e.g.
  `no language server available for .py files: pyright-langserver is not
  installed and neither bun nor node is available to install it (run: npm
  install -g pyright)`. The project-wide form points at `file_path`.
- New config: `LSPAutoInstall` (default true), `LSPStartupTimeout` ("20s"),
  `LSPInstallTimeout` ("120s", clamped to be ≥ startup), documented in the
  generated `.pando.toml` (`internal/config/init.go`).

## Motivation

1. A server missing from PATH was permanently marked broken with no install
   path and no message telling the user what to do.
2. Servers started for files Pando never touches (bootstrap watcher, `view`
   tool, TUI file tree), wasting memory on read-only sessions.
3. `diagnostics` raced startup (3s), then silently reported unrelated servers'
   diagnostics when the right one was missing.

## Verification

- `go build ./...`, `go vet` on every touched package — clean.
- `go test ./internal/app ./internal/config ./internal/llm/tools
  ./internal/lsp/runtime ./internal/api` — pass.
- `go test -race ./internal/app ./internal/lsp/runtime ./internal/config` — clean.
- Tests: `internal/lsp/runtime/runtime_test.go` (detection matrix, runner
  construction, install + symlink + no reinstall, 8-way concurrent dedupe,
  runner fallback, memoized failure); `internal/app/lsp_resolve_test.go`
  (PATH hit, toolchain server stays manual even with bun present, npm server
  installable, no-runtime and `LSPAutoInstall=false` fall back to manual with
  the exact command, previously staged binary reused, preset→package mapping);
  `internal/app/lsp_test.go` (trigger/mode gating, `StateError` client excluded
  from `WaitForFile`, install-extended budget, `UnavailableReason` per state);
  `internal/config/lsp_activation_test.go` (mode normalization, trigger matrix,
  install metadata propagation and clearing, timeout helpers);
  `internal/llm/tools/diagnostics_test.go` (install instructions surfaced,
  no fallback to unrelated servers, explicit trigger used).

## Next

P5 catalogue expansion (vue, svelte, astro, deno, graphql, prisma, eslint,
biome, terraform, texlab, taplo, ...), P6 surfaces (TUI/WebUI settings showing
`installed | installable | manual`, `pando_setup`, README).
