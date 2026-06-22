---
created_at: 2026-06-22T10:47:30.300035238Z
updated_at: 2026-06-22T10:47:30.300035238Z
tags:
    - fix
    - lsp
    - diagnostics
    - tools
---
# Fix diagnostics tool racing lazy LSP startup

## What changed
Updated the diagnostics tool and LSP provider integration so `diagnostics` no longer fails immediately when a matching language server is being lazily started for the requested file.

Changes made:
- Extended `internal/llm/tools/lsp_provider.go` with `WaitForFile(ctx, path)` so tools can wait briefly for a lazy LSP startup to settle.
- Updated `internal/llm/tools/diagnostics.go` to use `WaitForFile` instead of checking `ClientsForFile` immediately after `EnsureForFile`.
- Improved the diagnostics error messages so they explain whether the issue is likely missing server binary, startup still in progress, or disabled auto-activation.
- Added `App.WaitForFile` and `lspStartupSettled` in `internal/app/lsp.go` to poll for a matching client or detect that startup has settled as broken/timed out.
- Improved LSP startup logging in `createAndStartLSPClient` when a binary cannot be launched.
- Added tests in `internal/app/lsp_test.go` covering waiting for a spawned client and returning empty when startup settles broken.

## Files and symbols touched
- `internal/llm/tools/lsp_provider.go`
  - `LSPProvider`
  - `staticLSPProvider.WaitForFile`
- `internal/llm/tools/diagnostics.go`
  - `diagnosticsTool.Run`
- `internal/app/lsp.go`
  - `App.WaitForFile`
  - `App.lspStartupSettled`
  - `App.createAndStartLSPClient`
- `internal/app/lsp_test.go`
  - `TestWaitForFile_WaitsForMatchingClient`
  - `TestWaitForFile_ReturnsEmptyWhenStartupSettlesBroken`

## Why
The diagnostics tool was racing the background lazy-start mechanism: it requested LSP startup and then checked for running clients immediately, which often produced `no LSP clients available` even when `gopls` was installed and would have started a moment later.

## Verification
- Confirmed `gopls` is installed in the environment (`gopls version`)
- Ran `go test ./internal/app ./internal/llm/tools ./internal/config`
