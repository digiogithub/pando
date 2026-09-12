---
id: PANDO-M-0001
type: milestone
title: Embeddable AG-UI backend for web hosts
status: backlog
labels: [agui, sdk, web-host]
created: 2026-09-13T21:10:04Z
updated: 2026-09-13T21:10:04Z
due: 2026-10-31
---

## Description

Everything a product that embeds Pando as its agent backend needs before it can ship a browser chat panel over AG-UI. The first consumer is git-in-track (GIT-EP-0018): a Go companion proxies a React 18 + Vite app to one `pando agui-serve` per repository, with the product's own tools reached over MCP.

Gap analysis of 2026-09-13 (git-in-track `docs/research/`): the AG-UI surface is one `POST` route plus `/info`; a client disconnect cancels the run; agents are a closed list of seven names all built with the full coder toolset and no allow-list; the TypeScript SDK's AG-UI client is browser-safe but the package is not (`node:https` in `http.ts`, CopilotKit re-exported from the agui index).

## Acceptance Criteria

- [ ] A Vite/React 18 app depends on `@pando-ai/sdk/agui` with no Node polyfills and no build warnings, and can assemble a transcript, apply `STATE_DELTA`, resume an interrupt and answer a permission prompt with SDK helpers.
- [ ] A deployment restricts the AG-UI agent to an explicit tool allow-list that `tool_search` cannot bypass, with Mesnada off.
- [ ] A browser client can list its threads, reload a transcript, survive a dropped stream mid-run and cancel a suspended run.
- [ ] `agui-serve` exposes an unauthenticated health check, caps concurrent runs, drains on shutdown and reads its token from the environment or a file.

## Notes

Due date is a planning estimate. Decisions taken with the product owner on 2026-09-13: one `agui-serve` per repository (no multi-cwd process); identity and multi-tenancy stay in the embedding host, so no per-request principal in Pando; no CopilotKit runtime.
