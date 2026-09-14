---
id: PANDO-US-0015
type: story
title: "Thread API: list threads, read a thread's messages, delete a thread"
status: done
priority: high
parent: PANDO-EP-0003
milestone: PANDO-M-0001
author: claude
labels: [agui, api]
estimate: 5
created: 2026-09-13T21:14:53Z
updated: 2026-09-13T21:14:53Z
---

## Description

As a browser client that reloaded the page, I want to list AG-UI threads, read one's transcript and delete it, so that I can rebuild a conversation without co-mounting the Web-UI REST API.

`internal/agui/server.go:26-33` registers only `GET {path}/info`, `OPTIONS {path}/` and `POST {path}/{agent}`. Add `GET {path}/threads` (paginated, newest-first), `GET {path}/threads/{id}/messages` and `DELETE {path}/threads/{id}`, all through `authorize()` (`server.go:47-71`) and all mounted on `Handler()` so they work on the dedicated `agui-serve` listener (`cmd/agui_serve.go:159`), which deliberately carries no REST API. Back them with a new `threadStore.list` over the `agui_threads` table (`internal/agui/threads.go:26-35`, `:110-128` — the table already has `thread_id`, `session_id`, `agent`, `updated_at`; it has `get`/`put`/`forget` at `:48`/`:67`/`:75` but no `list`) and with `deps.Messages` for the transcript. Messages come back in AG-UI `Message[]` shape — `role`, `content`, `toolCalls`, `toolCallId` — not Pando's internal message struct. Delete removes the session's messages, the session and the `agui_threads` row; the existing dangling-binding recovery (`internal/agui/runtime.go:126-137`) stays as the safety net.

Do NOT list threads that belong to another adapter or to non-AG-UI sessions — scope the query to the `agui_threads` rows this adapter owns; do not fall back to matching the `agui: ` title prefix (`runtime.go:186-198`). Do NOT require the REST API to be co-mounted.

## Acceptance Criteria

- [ ] All three routes go through `authorize()` and answer correctly on a dedicated `agui-serve` listener with no REST API mounted (`internal/agui/threads_test.go`).
- [ ] `GET {path}/threads` is paginated, newest-first, and returns only this adapter's threads.
- [ ] `GET {path}/threads/{id}/messages` returns AG-UI-shaped messages with `toolCalls`/`toolCallId` preserved and assistant/tool pairing intact.
- [ ] `DELETE {path}/threads/{id}` removes messages, session and the `agui_threads` row; a subsequent `GET` on it answers 404 and a subsequent run on that thread id starts a fresh session.
- [ ] An unknown thread id answers 404 on both read routes and is idempotent on `DELETE`.

## Notes

First story of PANDO-EP-0003; no dependencies. The interim workaround it replaces (git-in-track proxying `GET /api/v1/sessions*`, reusing a Pando session id as `threadId`, honoured by `runtime.go:138-144`) stays valid until this ships. Size **M** (~150 LOC, no new dependencies).
