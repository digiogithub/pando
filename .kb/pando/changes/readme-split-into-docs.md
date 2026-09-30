---
created_at: 2026-09-30T16:45:48.345874398Z
updated_at: 2026-09-30T16:45:48.345874398Z
tags:
    - change
    - docs
    - readme
---
# README slimmed down; technical content moved to docs/ (2026-09-30)

## What changed
- `README.md` cut from 1247 to ~285 lines: header/logo/overview/screenshots, one-line headline features with links, short install, quick start, documentation index table, acknowledgments, license.
- `## Tasks` section KEPT at the end of README.md: `xc` reads its tasks (tag, build-webui, build-desktop, build, build-and-copy, release, release-osx) from README.md. Moving it would break `xc build` and the release-osx flow referenced by `.github/workflows/release.yml`. An HTML comment above it explains why.
- New docs (content moved verbatim, headings demoted, relative links fixed):
  - `docs/installation.md` (binaries, go install, build from source, extension builds; fixed clone URL to digiogithub)
  - `docs/configuration.md` (config locations, env var table — now also lists PANDO_TELEMETRY_*, PANDO_BETTERSTACK_TOKEN and PANDO_DELEGATION_* vars that were scattered — JSON/TOML samples, legacy DB migration, related guides)
  - `docs/usage.md`, `docs/webui.md` (features + terminal security note + WebUI Access basic auth), `docs/acp.md` (fixed broken link to non-existent docs/acp-server.md), `docs/agui.md`, `docs/lsp.md`, `docs/telemetry.md`, `docs/model-catalog.md`, `docs/knowledge-base.md`, `docs/pando-setup.md`, `docs/slash-commands.md` (built-ins, caveman, superpowers, learning, custom commands), `docs/delegation.md` (subagent conclusions, warm reuse, hot-peer IPC, durable event log, claim-lease dispatch), `docs/architecture.md`.
- Dropped README summaries whose full versions already lived in docs: MCP auth (`docs/mcp-authentication.md`), output filters (`docs/output-filters.md`).

## Verification
- Script check: every relative markdown link in README.md and docs/*.md resolves to an existing file.
- `xc -short` still lists all 7 tasks.

Related: [[pando/features/release-pipeline]]
