---
id: PANDO-US-0134
type: story
title: "tools: read-guard fallback to session history (same session, after compaction, content check, mtime)"
status: done
parent: PANDO-EP-0023
author: mcp
labels: [tools, edit]
created: 2026-11-03T12:02:14Z
updated: 2026-11-03T12:06:20Z
started: 2026-11-03T12:02:22Z
closed: 2026-11-03T12:06:20Z
---

## Description
In `internal/llm/tools`: a single helper used by every read-before-edit check (`edit`, `patch`, `write`). On an in-memory miss it asks a pluggable session-history source for the latest successful `view`/`read` of the resolved path in the current session after the compaction point, verifies the recorded content is still in the file, and caches the read time. Fails closed.

## Acceptance Criteria
- Unit tests cover: restart success; no view; other session; view before compaction; mtime later; content changed; error result; relative vs absolute path.
