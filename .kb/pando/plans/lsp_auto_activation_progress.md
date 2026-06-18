---
created_at: 2026-06-18T09:29:09.632247223Z
updated_at: 2026-06-18T09:49:50.017055934Z
tags:
    - plan
    - lsp
    - pando
    - progress
---
# LSP On-Demand Activation — Progress

ALL PHASES COMPLETE (2026-06-18)

- Phase 1 — Config & registry: DONE. `Autostart`, `LSPAutoActivate`, viper default,
  internal/config/lsp_registry.go + tests, init.go default updated.
- Phase 2 — Lazy app manager: DONE. internal/app/lsp.go rewritten (EnsureLSPForFile,
  ensureLSPServer, lspSpawning/lspBroken, snapshots) + lsp_test.go.
- Phase 3 — LSPProvider interface: DONE. tools/lsp_provider.go + NewStaticLSPProvider.
- Phase 4 — Wire tools: DONE. edit/write/view/patch/diagnostics + agent/tools.go,
  agent-tool.go, app.go, cmd/mcp_server.go migrated to LSPProvider. Build+tests green.
- Phase 5 — Triggers: DONE.
  - TUI editor/file-tree: chat.go ensureLSPCmd on FileSelectedMsg + OpenEditableFileMsg.
  - Workspace watcher: internal/app/lsp_bootstrap.go (fsnotify, started from
    initLSPClients when LSPAutoActivate) + bootstrap test.
  - Settings: settings.go buildLSPSection shows install status + Autostart toggle +
    auto-activation info; saveLSP handles autostart.
- Phase 6 — Docs: DONE. README.md "Language Servers (LSP)" section (on-demand model,
  catalogue, LSPAutoActivate/Disabled/Autostart table, settings page). KB feature
  doc pando/features/lsp_auto_activation.md. Final build/vet/tests green.
