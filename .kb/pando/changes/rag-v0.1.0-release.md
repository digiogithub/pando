---
created_at: 2026-11-08T21:45:18.376894862Z
updated_at: 2026-11-08T21:45:18.376894862Z
tags:
    - change
    - release
    - rag
---
# Release: rag/v0.1.0

**Date:** 2026-11-08. José confirmed it directly in the session; Pips relayed the request in agentd #96.

## What was published
- Annotated tag `rag/v0.1.0`, tag object c91dbc53, pointing at commit 96e6e6cf6313 (the tip of `rag-api`, which contains `rag/go.mod`).
- Branches pushed to origin (github.com/digiogithub/pando):
  - `rag-module` at 6f81f6a50642 (Phase 1)
  - `rag-api` at 96e6e6cf6313 (Phase 2 plus the ADR-044 memory fix)
- `main` was not touched. `.pando.toml` was not committed: it contains a private ollama IP and the repo is public.

## Verification
- Before tagging, in the root module: `go build ./...`, `go vet ./...` and `go test ./internal/db ./internal/rag/... ./internal/llm/tools ./internal/api ./internal/extensions`.
- Before tagging, in `rag/`: `go vet ./...` and `go test ./...`.
- After the push, `GOPROXY=https://proxy.golang.org go list -m github.com/digiogithub/pando/rag@v0.1.0` returned `v0.1.0`.

## Notes
- The pando-rag jj workspace has no `.git` of its own. Git commands there need `--git-dir=/www/MCP/Pando/pando/.git`. Push bookmarks with `jj git push --remote origin -b <name>`.
- gintrack was notified (agentd #100) and can now `go get github.com/digiogithub/pando/rag@v0.1.0` instead of using go.work or replace.

Related: [[pando/changes/rag-api-phase2.md]], [[pando/changes/rag-module-phase1.md]], [[pando/plans/gintrack-hub-rag-library.md]]
