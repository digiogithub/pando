---
created_at: 2026-08-04T07:07:04.299930956Z
updated_at: 2026-08-04T07:07:04.299930956Z
---

# Documentation Update: July 2026 Features (2026-08-04)

## Date
2026-08-04

## What Was Done

Completed comprehensive documentation for all features developed in July 2026 that were not yet documented in pando-docs. Updated existing token-optimization.md and created new feature docs.

## Files Modified
- `content/en/docs/configuration/token-optimization.md` — Added pando gain CLI, pando_stats MCP, bounce tracker details, environment variable override
- `content/es/docs/configuration/token-optimization.md` — Same changes in Spanish

## Files Created
- `content/en/docs/features/caveman-mode.md` — Caveman output brevity mode documentation
- `content/en/docs/features/superpowers-mode.md` — Superpowers specs-driven development documentation
- `content/en/docs/features/learning-mode.md` — Learning mode knowledge-capture documentation
- `content/en/docs/features/pando-setup-tool.md` — Agent self-service pando_setup tool documentation
- `content/en/docs/features/mcp-authentication.md` — MCP OAuth/mTLS enterprise authentication documentation
- `content/en/docs/features/lsp-auto-activation.md` — LSP auto-activation documentation
- `content/en/docs/features/config-discovery.md` — Configuration file discovery documentation
- `content/en/blog/pando-july-2026-roundup.md` — July 2026 blog post
- `content/es/blog/pando-july-2026-roundup.md` — July 2026 blog post (Spanish)

## Documentation Coverage

### Token Optimization (lean-ctx P1-P5)
- File Read Optimization (Full/Auto/Signatures/Map modes)
- Content-Hash Deduplication (F-references)
- Bounce Tracker (adaptive auto-mode safety)
- Code Property Graph (impact analysis, related files)
- Savings Ledger (pando gain CLI, pando_stats MCP)
- Token Optimization settings section (WebUI/TUI/TOML)

### New Session Modes
- Caveman Mode (output brevity: lite/full/ultra)
- Superpowers Mode (specs-driven development workflow)
- Learning Mode (knowledge-capture policy)

### MCP Authentication
- Static auth (bearer, basic, header)
- OAuth 2.1 (browser-based login flow)
- Client credentials (server-to-server)
- Enterprise mTLS (client certificates, encrypted keys, custom CA)

### Infrastructure
- LSP Auto-Activation (22 language presets, on-demand startup)
- Configuration Discovery (upward directory search)
- pando_setup agent self-service tool
- Models.dev integration

## Verification
- Hugo build successful: 122 EN pages, 120 ES pages
- No build errors
- All new docs follow existing style and format