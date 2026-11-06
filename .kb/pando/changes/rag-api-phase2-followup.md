---
created_at: 2026-11-08T19:36:15.007380233Z
updated_at: 2026-11-08T19:36:15.007380233Z
tags:
    - change
    - rag
    - gintrack
    - phase2
---
# Change: rag library Phase 2, review follow-up (gintrack memories)

**Date:** 2026-11-08. **Bookmark:** `rag-api` in `/www/MCP/Pando/pando-rag`. Main notes: `pando/changes/rag-api-phase2.md`.

Pips's review (#70) accepted Phase 2, with deviations 1–4 and 6 approved. (5), Markdown only, is OK for the PoC and is recorded as debt in `pando/plans/gintrack-hub-rag-library.md` ("Deuda técnica (Fase 2)"). For hub pdf/docx, `ApplyChanges` will need `Include` plus the `Converter`, and the converter will need to work from `fs.FS` or a reader.

## Fix: commit 45eeca20, `fix(kb): index gintrack memory files by their front matter`
- `rag/kb/api.go` `normalizeDoc`: kind comes from `kind`, else `type`. A gintrack ADR-044 memory file (`type: memory`, `status: active|proposed|...`) therefore indexes as kind=memory with its status, and `Search(Kinds: [memory], Statuses: [active])` finds it without going through `UpsertMemory`.
- `rag/kb/chunker.go` `parseDocument` reads the front matter only as a `map[string]any`. Before, Pando's typed `FrontMatter` decode failed on ADR-044's `source: {kind, ref}` (a map going into `Source string`), and the whole front matter was dropped.
- `rag/kb/frontmatter.go`: the delimiter logic was extracted into `splitFrontMatter`, which `ParseFrontMatterWithRaw` also uses. This is a pure refactor, so Pando's behaviour is unchanged.
- Test: `TestApplyChangesIndexesGintrackMemories` in `rag/kb/apply_test.go`. It uses the ADR-044 example front matter verbatim and checks:
  - kind, status, author, Meta.key and labels-as-tags;
  - Search in hybrid, FTS and vector modes;
  - the proposed memory is found only with `Statuses: [proposed]`;
  - front matter is stripped from chunks, and the line range starts at 19.

## Verification
- Root module: `go build ./...` and `go vet ./internal/...`, both clean.
- Root module: `go test ./internal/rag/... ./internal/llm/tools ./internal/api ./internal/extensions` passes.
- `rag/` module: `go vet ./...` and `go test ./...` pass.

Status: on hold for José's review of `rag-module` and `rag-api`. No `rag/vX` tag without his OK, and no push.
