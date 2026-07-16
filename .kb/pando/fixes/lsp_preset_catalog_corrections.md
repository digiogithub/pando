---
created_at: 2026-07-16T21:25:27.551577074Z
updated_at: 2026-07-16T21:25:27.551577074Z
tags:
    - fix
    - lsp
    - pando
---
# Fix: incorrect/invented LSP preset binary names

Related: [[lsp_preset_catalog_correction_plan]], [[lsp_auto_activation]]

## What changed
`internal/config/lsp_presets.go`: 3 of the 21 built-in LSP presets pointed at
binary names that never existed as installable executables (they were
malformed variants missing a hyphen before "language-server"):

- `json-language-server`: Command `vscode-json-languageserver` -> `vscode-json-language-server`
- `html-language-server`: Command `vscode-html-languageserver` -> `vscode-html-language-server`
- `css-language-server`: Command `vscode-css-languageserver` -> `vscode-css-language-server`

Real binaries are provided by the npm package `vscode-langservers-extracted`
(confirmed via web research 2026-07-16); the old names would always fail
`exec.LookPath` even when the package was installed, silently disabling
auto-activation for JSON/HTML/CSS.

Also documented (comment only, no behavior change) that `elixir-ls`'s preset
Command works for Mason/asdf installs (which expose an `elixir-ls` shim), but
a manual/vanilla ElixirLS release ships the launcher as `language_server.sh`
instead — users on that setup should override `Command` in `[LSP.elixir-ls]`.

Remaining 18 presets (gopls, rust-analyzer, typescript-language-server,
pyright, pylsp, clangd, lua-language-server, bash-language-server,
yaml-language-server, marksman, jdtls, solargraph, zls,
kotlin-language-server, intelephense, omnisharp, dartls) were verified
correct against real published binary names — no changes needed.

## Why
User noticed the catalogue contained "invented" entries while requesting the
LSP auto-activation feature. Investigation found the activation engine itself
(lazy per-file-extension start, gated on `exec.LookPath` + `LSPAutoActivate`,
internal/app/lsp.go + lsp_bootstrap.go, internal/config/lsp_registry.go) was
already implemented and correct (COMPLETE since 2026-06-18, see
[[lsp_auto_activation]]) — the only real defect was 3 wrong command strings in
the preset data.

## Verification
- `go build ./internal/config/...` — green.
- `go test ./internal/config/...` — green (no test regressions).
- `grep` confirmed no other file in the repo (code or README) referenced the
  old incorrect binary names.

## Next phases (see [[lsp_preset_catalog_correction_plan]])
Phase 2: add missing mainstream presets (Swift/sourcekit-lsp,
Terraform/terraform-ls, Scala/metals, Haskell/haskell-language-server-wrapper,
Nix/nil, TOML/taplo, Vue/vue-language-server, GraphQL, Docker, SQL/sqls,
Protobuf). Phase 3: registry tests + README table update.