---
created_at: 2026-07-16T21:24:39.165752091Z
updated_at: 2026-07-16T22:51:22.320938864Z
tags:
    - plan
    - lsp
    - pando
---
# Plan: LSP preset catalog audit + expansion

Related: [[lsp_auto_activation]], [[lsp_auto_activation_plan]], [[lsp_auto_activation_progress]], [[lsp_preset_catalog_corrections]]

Status base: activation engine (lazy, PATH-detect, per-file-extension gating,
LSPAutoActivate) already COMPLETE since 2026-06-18 (internal/app/lsp.go,
lsp_bootstrap.go, internal/config/lsp_registry.go). Verified 2026-07-16 by
reading the code: `EnsureLSPForFile` only starts a server when (a) its binary
resolves via `exec.LookPath`, (b) the edited/opened file's extension matches
one of the server's Languages, and (c) `LSPAutoActivate` is true. Nothing
starts eagerly except servers with explicit `Autostart=true` in user config.
This matches exactly what was requested: don't activate gopls unless a .go
file is being edited, don't load unconfigured/uninstalled servers.

This plan only touches the **catalogue of presets**
(internal/config/lsp_presets.go), which the user flagged as containing some
invented/incorrect entries.

## Phase 1 — Fix incorrect/invented preset commands (DONE 2026-07-16)
- `json-language-server`: Command `vscode-json-languageserver` -> `vscode-json-language-server`
- `html-language-server`: Command `vscode-html-languageserver` -> `vscode-html-language-server`
- `css-language-server`: Command `vscode-css-languageserver` -> `vscode-css-language-server`
  (real binaries ship in npm package `vscode-langservers-extracted`; the old
  no-hyphen names don't exist as installable binaries — confirmed via web
  search 2026-07-16)
- `elixir-ls`: kept `elixir-ls` (Mason/asdf shim name, common on dev PATHs) but
  documented that vanilla upstream ElixirLS release ships `language_server.sh`
  instead, in case a user's PATH only has that name.
- Verified remaining 18 presets against real binary names via web research:
  gopls, rust-analyzer, typescript-language-server, pyright, pylsp, clangd,
  lua-language-server, bash-language-server, yaml-language-server, marksman,
  jdtls, solargraph, zls, kotlin-language-server, intelephense, omnisharp,
  dartls all correct — no changes needed.

## Phase 2 — Expand catalogue with missing mainstream languages
SKIPPED by explicit user request (2026-07-17): do not add Swift/Terraform/
Scala/Haskell/Nix/TOML/Vue/GraphQL/Docker/SQL/Protobuf presets. Catalogue
stays at the 21 existing (corrected) entries.

## Phase 3 — Tests + docs (DONE 2026-07-17)
- Added `TestLSPPresets_Sane` in internal/config/lsp_registry_test.go:
  asserts unique preset names, non-empty/whitespace-free/path-free Command,
  at least one Languages entry, and each extension normalized (leading dot,
  lowercase). Catches the class of bug fixed in Phase 1.
- Added a "Built-in preset catalogue" table to README.md (Language Servers
  (LSP) section) listing all 21 presets with corrected commands, plus a note
  on vscode-langservers-extracted and the elixir-ls/language_server.sh caveat.
- Verification: `go test ./internal/config/...` green, `go vet
  ./internal/config/...` clean.

## Phase 4 — KB documentation
- [[lsp_preset_catalog_corrections]] — Phase 1 fix summary.
- This document updated in place with Phase 3 completion and Phase 2 skip.
