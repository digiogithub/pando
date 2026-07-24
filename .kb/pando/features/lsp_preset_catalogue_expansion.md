---
created_at: 2026-07-24T15:33:06.918391562Z
updated_at: 2026-07-24T15:33:06.918391562Z
tags:
    - feature
    - lsp
    - presets
    - pando
---
# LSP preset catalogue expansion (P5) — filename matching + opt-in presets

Status: **IMPLEMENTED** (2026-07-24). P5 of
[[lsp_ondemand_runtime_install_plan]]. Builds on
[[lsp_ondemand_runtime_and_activation_triggers]] (P1–P4) and
[[lsp_preset_catalog_correction_plan]]. Remaining: P6 (TUI/WebUI surfaces,
`pando_setup`, README).

## What changed

### 1. Catalogue: 21 → 45 presets

New npm presets (auto-installable with bun/npm through `internal/lsp/runtime`):
`vue-language-server` (@vue/language-server + typescript),
`svelte-language-server` (bin `svelteserver`), `astro-ls` (@astrojs/language-server
+ typescript), `dockerfile-language-server` (bin `docker-langserver`),
`graphql-lsp` (graphql-language-service-cli + graphql),
`prisma-language-server`, `vim-language-server`.

New manual/toolchain presets: `terraform-ls`, `taplo`, `texlab`, `nixd`,
`clojure-lsp`, `ocamllsp`, `haskell-language-server`, `sourcekit-lsp`, `gleam`,
`metals`, `ruby-lsp`, `lemminx`, `cmake-language-server`.

`oxlint` from the plan was **not** added: no verified stdio LSP entrypoint for
the published package, and inventing a command would produce a preset that can
never start.

### 2. `LSPPreset.OptIn` — presets that stay off until declared

`internal/config/lsp_presets.go`: new `OptIn bool`. `resolveLSPServer` sets
`Disabled = preset.OptIn && !hasUser`, so an opt-in preset is invisible to
`LSPServersForFile` until the user writes `[LSP.<name>]` (an empty table is
enough).

Opt-in presets: `eslint-language-server`, `biome`, `sql-language-server`
(useless or noisy without project config) and `deno` (claims the same files as
`typescript-language-server`; only one can be right per project).

Motivation: without this, adding linter servers would spawn and auto-install
3–4 servers on every `.ts` edit.

### 3. Filename matching (files whose extension says nothing)

- `LSPConfig.Filenames []string` (`internal/config/config.go`, TOML/JSON tags)
  and `ResolvedLSPServer.Filenames`, inherited from the preset unless the user
  overrides it.
- `config.LSPHandlesFile(languages, filenames, path)` in
  `internal/config/lsp_registry.go` is the single matcher, used by both
  `ResolvedLSPServer.HandlesFile` and `lsp.Client.HandlesFile`
  (`internal/lsp/client.go`), so the registry and the running clients can no
  longer disagree.
  - Base name matched case-insensitively, in full or up to the first dot, so
    `Dockerfile` also claims `Dockerfile.dev`.
  - **Behaviour change**: an extensionless file is now claimed *by name only*.
    Previously `Client.HandlesFile` returned true for every extensionless file,
    so `Makefile` was answered by whatever server happened to be running (while
    activation ignored it entirely — the two halves disagreed).
  - A server declaring neither `Languages` nor `Filenames` is still a catch-all.
- `Config.LSPServersForFile` no longer delegates to `LSPServersForExt`; it walks
  the registry with `HandlesFile`. `LSPServersForExt` stays for extension-only
  callers.
- `internal/app/lsp.go`: `EnsureLSPForFileTrigger` uses `LSPServersForFile` and
  the renamed `hasRunningClientForFile(path)` (was `hasRunningClientForExt`), so
  editing a `Dockerfile` activates its server. `createAndStartLSPClient`
  propagates `Filenames` to the client.
- `internal/app/lsp_resolve.go`: new `lspFileLabel(path)` names the file type in
  user-facing messages (extension, else base name), and `UnavailableReason` no
  longer bails out with "no language server can be matched to a file without an
  extension".

### 4. Generated config documentation

`internal/config/init.go`: the `[LSP]` section documents the opt-in presets and
the `Filenames` key.

## Files touched

- `internal/config/lsp_presets.go` — 24 new presets, `LSPPreset.OptIn`.
- `internal/config/lsp_registry.go` — `Filenames`, `LSPHandlesFile`,
  `matchesLSPFilename`, `ResolvedLSPServer.HandlesFile`, opt-in disabling,
  path-aware `LSPServersForFile`.
- `internal/config/config.go` — `LSPConfig.Filenames`.
- `internal/config/init.go` — documentation.
- `internal/lsp/client.go` — `Client.Filenames`, `HandlesFile` delegates to config.
- `internal/app/lsp.go` — path-aware activation, `hasRunningClientForFile`,
  `Filenames` propagation.
- `internal/app/lsp_resolve.go` — `lspFileLabel`, `UnavailableReason`.
- Tests: `internal/config/lsp_activation_test.go` (name matching, opt-in gating,
  `LSPServersForFile` by name, catalogue well-formedness), `internal/app/lsp_test.go`
  (`TestHasRunningClientForFile`).

`TestPresetCatalogueIsWellFormed` guards the catalogue: unique names, unique
commands, every preset declares Languages or Filenames, npm presets have
package+bin (bin == configured command) + hint, manual presets have hint + URL.

## Verification

- `go build ./...` clean; `go vet ./internal/config ./internal/app ./internal/lsp ./internal/llm/tools` clean.
- `go test ./internal/app ./internal/config ./internal/llm/tools ./internal/lsp/... ./internal/api` — all ok.
- `go test -race ./internal/app ./internal/config` — ok.

## Notes for P6

The settings surfaces must show, per server: availability
(`installed | installable (bun/npm) | manual`), the install hint, and whether
the preset is opt-in (a disabled preset that the user never configured is
opt-in, not broken).
