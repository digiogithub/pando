---
id: PANDO-M-0002
type: milestone
title: Search fidelity and transport hardening
status: done
labels: [rag, kb, mcp, security]
created: 2026-09-13T21:10:04Z
updated: 2026-09-13T21:10:04Z
due: 2026-11-30
---

## Description

Make the knowledge base trustworthy as an external host's semantic index and close two transport holes. Findings of 2026-09-13: front matter other than `tags`/`aliases` is silently dropped on import; the KB watcher writes no metadata at all and `UpdateDocument` replaces metadata wholesale, so the first edit erases a document's tags for good (523 of 539 documents in Pando's own database have already lost theirs); search is reachable only over MCP or an agent run; `pando mcp-server`'s HTTP transport has no authentication, answers `Access-Control-Allow-Origin: *` and auto-approves every tool; vector search materialises every document body per query.

## Acceptance Criteria

- [ ] A document's front-matter keys survive import, watcher edits and `kb_add_document` mirroring, and come back in `kb_search_documents` results.
- [ ] KB and code search, document upsert/delete and a KB reindex are reachable over authenticated REST.
- [ ] The MCP HTTP transport requires a bearer token and echoes only allow-listed origins.
- [ ] Vector search no longer selects document bodies; a path-prefix filter is pushed into SQL; an embedding-dimension mismatch is detected and reported.

## Notes

Due date is a planning estimate. git-in-track (GIT-EP-0019) does not block on this milestone: it exports its corpus with `KBWatch=false`, resolves hits back to its own index for authoritative fields, and speaks MCP until the REST routes exist.
