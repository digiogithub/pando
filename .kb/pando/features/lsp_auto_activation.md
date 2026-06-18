---
created_at: 2026-06-18T09:49:47.209878631Z
updated_at: 2026-06-18T09:49:47.209878631Z
tags:
    - feature
    - lsp
    - pando
---
# LSP On-Demand Activation (Pando)

Status: COMPLETE (Phases 1-6, 2026-06-18)

## Goal
Mimic OpenCode: detect the language of edited files and start the matching
language server only if its binary is on PATH, automatically. Disable the eager
gopls-at-boot behaviour (a reminiscence of Pando's Go origins). Pando's built-in
22 LSP presets act as a default catalogue, usable even without user config; user
config only overrides/extends/disables.

## Design decisions (confirmed with user)
- Q1: "Lazy salvo autostart" — everything lazy by default; opt-in `Autostart=true`
  per server restores eager boot startup.
- Q2: ALL THREE triggers — agent tools, TUI editor/file-tree, workspace watcher.

## Config
- `LSPAutoActivate bool` (global, default true via viper) — on-demand activation.
- `LSPConfig.Autostart bool` (per server) — eager start at boot.
- `LSPConfig.Disabled` — never start.

## Key files
- internal/config/config.go — `Autostart` field, `LSPAutoActivate` field + viper default.
- internal/config/lsp_registry.go (NEW) — merges presets + user config into
  `ResolvedLSPServer` candidates. APIs: `LSPRegistry()`, `LSPServersForExt(ext)`,
  `LSPServersForFile(path)`, `LSPAutostartServers()`, `HandlesExt`, `resolveLSPServer`
  (user non-empty fields win; booleans always from user). `Source` field marks
  "preset" vs user.
- internal/config/init.go — default config now documents on-demand activation
  (commented [LSP.gopls] example, gopls no longer eager).
- internal/app/lsp.go — lazy manager: `initLSPClients` starts only
  `LSPAutostartServers()`; `EnsureLSPForFile(ctx, path)` derives ext, starts only
  first installed PRESET per ext (user-configured always honored) via
  `presetSatisfied`/`hasRunningClientForExt`; `ensureLSPServer` skips
  disabled/running/spawning/broken, `lspLookPath` (test seam = exec.LookPath)
  marks missing binaries broken (no retry); snapshot methods `ClientsForFile`,
  `Clients`, `EnsureForFile`. `var _ tools.LSPProvider = (*App)(nil)`.
- internal/app/app.go — `lspSpawning`, `lspBroken` map[string]struct{} guarded by
  clientsMutex; passes `app` (not `app.LSPClients`) to agent tools.
- internal/app/lsp_bootstrap.go (NEW) — lightweight workspace-wide fsnotify
  watcher started from initLSPClients when LSPAutoActivate; on Write/Create of a
  file with an extension calls EnsureLSPForFile (idempotent/cheap). Skips
  dot-dirs + node_modules/vendor/dist/build/target/out/.git. Registers cancel +
  watcherWG for clean shutdown.
- internal/llm/tools/lsp_provider.go (NEW) — `LSPProvider` interface
  {EnsureForFile, ClientsForFile, Clients} + `NewStaticLSPProvider(clients)`
  adapter (wraps a fixed map, no spawn).
- internal/llm/tools/{edit,write,view,patch,diagnostics}.go — field renamed
  `lspClients map` -> `lspProvider LSPProvider`; Run calls EnsureForFile(ctx,path)
  then ClientsForFile(path).
- internal/llm/agent/tools.go, agent-tool.go — params take tools.LSPProvider;
  diagnostics tool gated on `lspProvider != nil` (not len>0, incompatible with
  lazy boot).
- cmd/mcp_server.go — passes `appSvc` to view/write/edit/patch tools.
- internal/tui/page/chat.go — `ensureLSPCmd(path)` tea.Cmd fired on
  FileSelectedMsg and OpenEditableFileMsg (off the UI goroutine).
- internal/tui/page/settings.go — buildLSPSection shows per-server `Status`
  (installed/not installed via lspBinaryStatus), an `Autostart` toggle, a
  read-only `Auto-activation` info row, and preset rows annotated with install
  status. saveLSP handles `autostart` + read-only `status`/`info`. ReadOnly
  fields never trigger SaveFieldMsg (section.go:179,224).

## Tests
- internal/config/lsp_registry_test.go — 4 tests (defaults from presets, override
  inherits preset, disable preset, user-only server + ext normalization).
- internal/app/lsp_test.go — 5 tests (binary missing marks broken, disabled/no
  command, already running/spawning, hasRunningClientForExt, ClientsForFile
  snapshot filters by language). Uses newLSPTestApp + withStubLookPath.
- internal/app/lsp_bootstrap_test.go — bootstrapShouldExcludeDir cases.

## Verification
go build ./... clean; go vet clean; tests green for app, config, agent, tools, api.
Result: non-Go project no longer starts gopls; touching a .py file starts
pyright if installed; missing binary marked broken without retry loop.
