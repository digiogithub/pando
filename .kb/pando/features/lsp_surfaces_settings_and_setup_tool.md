---
created_at: 2026-07-24T15:49:24.607475869Z
updated_at: 2026-07-24T15:49:24.607475869Z
tags:
    - feature
    - lsp
    - tui
    - webui
    - api
    - pando
---
# LSP surfaces (P6): status report, settings pages, REST API, pando_setup, README

Status: **IMPLEMENTED** (2026-07-24). P6 — the last phase — of
[[lsp_ondemand_runtime_install_plan]]. Follows
[[lsp_ondemand_runtime_and_activation_triggers]] (P1–P4) and
[[lsp_preset_catalogue_expansion]] (P5). **The plan is now complete.**

## What changed

### 1. One status report for every surface — `internal/app/lsp_status.go`

`App.LSPServerStatuses() []LSPServerStatus` walks the whole registry (presets +
user servers) and, for each one, reports: description, configured command,
`ResolvedCommand` (the absolute binary Pando would spawn), languages/filenames,
`Configured`, `OptIn`, `Disabled`, `Autostart`, `Availability`
(`installed|installable|manual`) + `AvailabilityLabel` (e.g. "installable
(bun)"), `Reason`, `Hint`, `URL`, `RunState` (`stopped|starting|ready|error`)
and `Installing`. It reuses `resolveLSPCommand`, so "installed" means exactly
what the activation path means by it — read-only: it never installs or starts
anything. Runtime maps are snapshotted under one RLock. Configured servers sort
first, then the catalogue by name.

`Command` deliberately stays the *configured* command while `ResolvedCommand`
carries the absolute path: the web UI prefills its edit form from the status,
and writing a staged absolute path into the user's config would pin it.

`App.LSPCatalog() []tools.LSPCatalogEntry` is the same report in the shape the
setup tool consumes; `App.LSPServerStatusByName` and `App.LSPActivationSettings`
round out the API.

### 2. Global knobs are now writable — `internal/config/lsp_activation.go`

- `config.LSPActivationSettings` struct (`AutoActivate`, `ActivateOn`,
  `AutoInstall`, `StartupTimeout`, `InstallTimeout`) + `Config.LSPActivationSettings()`
  returning normalized/effective values.
- `config.UpdateLSPActivation(settings)` validates the mode and both durations
  (`lspTimeoutValue` rejects unparsable and non-positive values, empty falls
  back to the documented default) and persists via `updateCfgFile`, rolling the
  in-memory config back on failure.

### 3. TUI — `internal/tui/page/settings.go`

`buildLSPSection(app, cfg)` now opens with five editable global fields
(`lsp.settings.autoactivate|activateon|autoinstall|startuptimeout|installtimeout`,
`activateon` a select), persisted through the new `saveLSPActivation`. Each
configured server gained a `Filenames` field and a real status line
(`lspServerStatusLine`: availability · running/starting/installing/failed ·
`run: <hint>`). The "Add <preset>" actions come from the status report and show
availability plus an `opt-in: add it to enable` marker. `lspBinaryStatus` (a
bare `exec.LookPath`) is gone — it did not know about staged binaries.

### 4. REST API — `internal/api/handlers_config.go`, `routes.go`

- `GET /api/v1/config/lsp` now also returns `activation`, sorts its items, and
  exposes `filenames` + `autostart` (both accepted by `PUT` too).
- `GET /api/v1/config/lsp/catalog` — the full status report (503 without an app).
- `GET|PUT /api/v1/config/lsp/activation` — the global knobs.

### 5. Web UI — `LSPSettings.tsx`, `lspStore.ts`, `types/index.ts`

Activation card (toggles + mode select + the two timeouts), an availability
badge next to each configured server, and a collapsible "Built-in catalogue"
list with availability, handled files, the manual install command, and an
Enable/Customize button that prefills the modal from the preset. The modal
gained Filenames and Autostart. The stale hardcoded `LANGUAGE_PRESETS` (whose
keys — `go`, `typescript`, `c` — never matched a real preset name) is gone; the
selector is fed from the catalogue. "Test" now reports the resolved binary or
the reason instead of a fake client-side check.
`FormInput.TextInput` now chains a caller-supplied `onFocus`/`onBlur` instead of
swallowing them (its own handler used to override the spread props).

### 6. pando_setup — `internal/llm/tools/pando_setup.go`, `lsp_provider.go`

New `lsp` command: `lsp [name] [--all] [--missing]`. Default view shows the
activation settings plus servers that are configured, running or installed, and
says how many more exist; `--all` includes opt-in servers, `--missing` only what
needs a manual install, `lsp <name>` prints one server in detail.

Wiring: `NewPandoSetupTool(bridge, lspProvider)` (signature change, both call
sites in `internal/llm/agent/tools.go` pass the existing `lspProvider`). The
catalogue is reached through an **optional** interface `tools.SetupLSPCatalog`
(`LSPCatalog() []LSPCatalogEntry`), asserted at call time, so the existing
`LSPProvider` implementations and test stubs stay valid and a provider without a
catalogue degrades to a clear error.

### 7. README

The "Language Servers (LSP)" section was rewritten: on-demand model, auto-install
with bun/npm, the `LSPActivateOn` trigger table, all five global settings, the
per-server keys (including `Filenames`), the 41-row preset table with an
install column, the four opt-in presets with the reason each is opt-in, and the
`pando_setup lsp` commands.

## Files touched

- New: `internal/app/lsp_status.go`, `internal/app/lsp_status_test.go`,
  `internal/api/handlers_config_lsp_test.go`,
  `internal/llm/tools/pando_setup_lsp_test.go`.
- Modified: `internal/config/lsp_activation.go` (+settings struct/update),
  `internal/config/lsp_registry.go` (`Description`, `OptIn` on
  `ResolvedLSPServer`), `internal/api/handlers_config.go`, `internal/api/routes.go`,
  `internal/tui/page/settings.go`, `internal/llm/tools/pando_setup.go`,
  `internal/llm/tools/lsp_provider.go`, `internal/llm/agent/tools.go`,
  `web-ui/src/components/settings/LSPSettings.tsx`,
  `web-ui/src/components/shared/FormInput.tsx`, `web-ui/src/stores/lspStore.ts`,
  `web-ui/src/types/index.ts`, `README.md`,
  `internal/config/lsp_activation_test.go`, `internal/llm/tools/pando_setup_test.go`.

## Verification

- `go build ./...` clean; `go vet ./internal/...` clean for every touched
  package (the only findings are pre-existing, in `internal/mesnada/agent`).
- `go test ./internal/app ./internal/config ./internal/llm/tools ./internal/llm/agent ./internal/api ./internal/lsp/runtime ./internal/tui/...` — all ok.
- `go test -race ./internal/app ./internal/config ./internal/llm/tools` — ok.
- `npx tsc --noEmit` clean and `npm run build` succeeds in `web-ui/`.

New tests: availability/opt-in/run-state/installing reporting and
configured-first ordering (`lsp_status_test.go`), catalogue/status parity,
`UpdateLSPActivation` validation and round-trip (`lsp_activation_test.go`),
the three REST endpoints (`handlers_config_lsp_test.go`), and the four
`pando_setup lsp` views (`pando_setup_lsp_test.go`).
