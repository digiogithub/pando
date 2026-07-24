---
created_at: 2026-07-24T16:16:00.64721312Z
updated_at: 2026-07-24T16:16:00.64721312Z
tags:
    - feature
    - lsp
    - config
    - runtime
    - pando
---
# Feature: `LSPRunner` — choosing the package manager for npm language servers

Date: 2026-07-24. Status: IMPLEMENTED.

Closes the last open item of [[lsp_ondemand_runtime_install_plan]] (section 5 of
the config table), which was deliberately skipped when P1–P6 were implemented
(see [[lsp_ondemand_runtime_and_activation_triggers]],
[[lsp_preset_catalogue_expansion]], [[lsp_surfaces_settings_and_setup_tool]]).

## What was changed

A new global setting, `LSPRunner`, selects the JavaScript toolchain Pando uses
to install and run npm-distributed language servers:

| Value | Behaviour |
|---|---|
| `auto` (default) | bun when installed, npm/npx otherwise — the previous, hardcoded behaviour |
| `bun` | only bun; npm is never used, even when it is the only toolchain present |
| `npm` | only npm/npx, even when bun is installed |
| `off` | neither; npm servers are only used when their binary is already on PATH or was staged by an earlier run |

`off` also disables automatic installation regardless of `LSPAutoInstall`:
without bun or npm there is nothing to install with. Servers that ship with a
language toolchain (gopls, rust-analyzer, clangd, …) are unaffected.

## Files and symbols touched

- `internal/config/config.go` — `Config.LSPRunner` (`toml:"LSPRunner"`),
  `viper.SetDefault("lspRunner", LSPRunnerAuto)`.
- `internal/config/lsp_activation.go` — constants `LSPRunnerAuto|Bun|Npm|Off`,
  `Config.LSPRunnerMode()` (normalizes case/whitespace, unknown values fall back
  to `auto`), `LSPAutoInstallEnabled()` now also returns false for `off`,
  `LSPActivationSettings.Runner` and its validation + rollback in
  `UpdateLSPActivation`.
- `internal/lsp/runtime/runtime.go` — **the detection was restructured**. It used
  to memoize a single `NodeRuntime` with `sync.Once`; it now probes *both*
  toolchains once (`probeRuntimes`/`probeBun`/`probeNpm` into `hostRuntimes`)
  and applies the preference on every call, because the user can change
  `LSPRunner` while Pando runs. `Detect()` reads
  `config.Get().LSPRunnerMode()`; the new `DetectFor(preference)` is the explicit
  form. `SetForTests` now installs an `override` pointer (still honoring `off`)
  and `resetDetection` clears both the probe and the override.
- `internal/app/lsp_resolve.go` — `resolveLSPCommand` returns a specific reason
  when the runner is `off`, and the new `noRunnerReason(cfg)` names the pinned
  manager (`LSPRunner = "bun"`) instead of the generic
  "neither bun nor node is available".
- `internal/tui/page/settings.go` — new "Package manager" select
  (`lsp.settings.runner`) in the LSP section; `saveLSPActivation` gained the
  `runner` case.
- `internal/llm/tools/pando_setup.go` — `renderSetupLSPActivation` prints
  `runner: <mode>`, so `pando_setup lsp` shows it.
- `web-ui/src/types/index.ts`, `stores/lspStore.ts`,
  `components/settings/LSPSettings.tsx` — `LSPActivation.runner`,
  `RUNNER_OPTIONS`, a select in the activation card (the REST endpoints needed
  no change: they serialize `config.LSPActivationSettings` as a whole).
- `internal/config/init.go` and `README.md` — documented, including a new
  "Choosing the package manager" table.

## Why

`auto` is right for most hosts, but not all: a corporate registry may only be
configured for npm, a platform may have a broken bun build, and sandboxed or
offline machines want no package manager touched at all. Before this, the only
way to stop Pando from reaching for bun/npm was `LSPAutoInstall = false`, which
also disabled the ephemeral-runner path and gave a misleading reason message.

## Verification

- `go build ./...` clean; `gofmt -l` clean on every touched file;
  `go vet ./internal/lsp/runtime ./internal/config ./internal/app` clean.
- New tests: `TestDetectForRunnerPreference`,
  `TestDetectForPinnedManagerMissing`, `TestDetectHonorsConfiguredRunner`
  (`internal/lsp/runtime`); `TestLSPRunnerMode`,
  `TestLSPAutoInstallRequiresARunner`, runner cases in
  `TestUpdateLSPActivationValidates` (`internal/config`);
  `TestResolveLSPCommand_RunnerOff`,
  `TestResolveLSPCommand_RunnerPinsMissingManager` (`internal/app`).
- `go test ./internal/lsp/runtime ./internal/config ./internal/app ./internal/api ./internal/llm/tools ./internal/tui/...` → all ok.
- `go test -race ./internal/lsp/runtime ./internal/app ./internal/config` → all ok.
- `npx tsc --noEmit` clean, `npm run build` succeeded in `web-ui/`.

With this, the whole `lsp_ondemand_runtime_install_plan` is implemented. The
only remaining deliberate omission is the `oxlint` preset, left out for lack of
a verified stdio LSP entrypoint.
