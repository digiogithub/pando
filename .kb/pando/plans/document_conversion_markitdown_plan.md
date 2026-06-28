---
created_at: 2026-06-28T15:49:19.50724593Z
updated_at: 2026-06-28T15:49:19.50724593Z
tags:
    - plan
    - convert
    - markitdown
    - kb
    - cli
---
# Plan: Document conversion (docx/pdf/xlsx/…) → Markdown — `pando convert` CLI + on-the-fly KB indexing

Approved & implemented 2026-06-28. See feature doc
`pando/features/document_conversion_markitdown.md` for the as-built result.

## Goal
1. CLI `pando convert` converting docx/pdf/xlsx (and all formats the library supports) to Markdown.
2. Same formats placed in the KB folder are converted on the fly and indexed (Markdown chunks)
   referencing the ORIGINAL file.

Library: `github.com/conductor-oss/markitdown` (pure-Go, no CGO, PDF via PDFium WASM). Chosen by
the user over `chenjunqian/go-markitdown` (heavier go-fitz/MuPDF native binding).

## Decisions (with user)
- No build tags. KB doc references original file + metadata (`source_path`/`source_format`/
  `converted`). CLI prints to stdout by default, `-o` flag for file.

## Phases
1. `internal/convert` service (Converter wrapper, lazy engine, mutex, curated KB set) — DONE.
2. `cmd/convert.go` CLI (`-o`, `--list-formats`, URL auto-detect) — DONE.
3. KB sync on-the-fly conversion (`DocumentConverter` iface on KBStore, `isIndexableFile`,
   `loadDocumentBody`, metadata) — DONE.
4. KB watcher real-time conversion — DONE.
5. Config (`KBConvertDocuments` default true, `KBConvertExtensions` override) + app wiring in
   `remembrances.go` — DONE. UI settings widgets + i18n DEFERRED (follow-up).
6. Tests, README, KB doc — DONE.

## Verification
`go test ./internal/convert/... ./internal/rag/kb/... ./internal/config/... ./cmd/... ./internal/api`
all green; `go build ./...` OK; manual `pando convert` against xlsx/csv fixtures.
