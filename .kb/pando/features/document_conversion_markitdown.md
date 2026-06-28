---
created_at: 2026-06-28T15:48:56.402657895Z
updated_at: 2026-06-28T15:48:56.402657895Z
tags:
    - feature
    - kb
    - cli
    - convert
    - markitdown
    - rag
---
# Feature: Document conversion to Markdown (`pando convert` CLI + on-the-fly KB indexing)

**Date:** 2026-06-28
**Status:** Functional core DONE (Phases 1–5); UI settings widgets deferred (Phase 5 sub-part), see follow-up.

## What was added

Ability to convert rich document formats (docx, pdf, xlsx, pptx, html, csv, epub, ipynb, xls,
rss/atom/xml, …) to Markdown in two ways:

1. **CLI command `pando convert <input> [-o out.md]`** — prints Markdown to **stdout** by
   default; `-o/--output` writes to a file; `--list-formats` lists supported extensions;
   `http(s)://` inputs are auto-detected and converted as URLs.
2. **On-the-fly KB conversion + indexing** — supported documents dropped inside the Knowledge
   Base directory (`[Remembrances] KBPath`) are converted to Markdown and indexed (chunked +
   embedded) **referencing the original file**. The KB document identity stays as the original
   relative path (e.g. `report.docx`); metadata stores `source_path`, `source_format` (ext
   without dot) and `converted: true`. Works for both the initial import (sync) and the
   real-time fsnotify watcher.

## Library

`github.com/conductor-oss/markitdown` v0.0.1 — **pure-Go** port of Microsoft markitdown, **no
CGO** (PDF via PDFium compiled to WebAssembly with wazero). Chosen over `chenjunqian/go-markitdown`
(which used `gen2brain/go-fitz`, a heavier MuPDF/native binding) per user decision. pando is on
Go 1.26 (lib requires 1.24). Its `html-to-markdown/v2` coexists with the existing v1.6.0
(distinct module paths). `go mod tidy` bumped goldmark to 1.7.11 and wazero to 1.11.0.

API used: `markitdown.New()`, `m.ConvertFile(path)`, `m.ConvertReader(r, StreamInfo)`,
`m.Convert(source)` → `*DocumentConverterResult{ Markdown, Title }`.

## Files

- **New `internal/convert/converter.go`** — `Converter` wrapper. Lazy engine init (PDFium WASM
  cost), all conversions serialized by a `sync.Mutex` (PDF backend not guaranteed concurrency-
  safe; KB conversion is not a hot path). `New()`, `NewWithConvertibleExtensions([]string)`,
  `ConvertFile`, `ConvertReader`, `Convert`, `IsSupported` (pkg func), `IsConvertibleDocument`
  (method, instance-overridable curated set), `SupportedExtensions`,
  `ConvertibleDocumentExtensions`. Curated KB set excludes `.md/.markdown/.txt/.json/.jsonl/.xml`.
  - `internal/convert/converter_test.go` + `testdata/sample.xlsx` (generated via excelize),
    `sample.csv`.
- **New `cmd/convert.go`** — cobra command, registered in its own `init()` (pattern of
  `cmd/db.go`).
- **`internal/rag/kb/kb.go`** — added `DocumentConverter` interface (`ConvertFile` +
  `IsConvertibleDocument`), `KBStore.converter` field, `SetDocumentConverter` /
  `documentConverter()` (guarded by existing `fsMu`). Decouples kb pkg from convert pkg (no
  import cycle); nil converter = legacy markdown-only behavior.
- **`internal/rag/kb/sync.go`** — new shared helpers `isIndexableFile(path, conv)`,
  `loadDocumentBody(absPath, conv)` (markdown read verbatim, convertible docs converted),
  `fileFormat(path)`. `syncResult` gained `format`/`converted`. WalkDir filter
  `isMarkdownFile` → `isIndexableFile`; worker `os.ReadFile` → `loadDocumentBody`; results loop
  skips front-matter parsing for converted content and writes `source_format`/`converted` meta.
- **`internal/rag/kb/watcher.go`** — event filter and `handleWatchEvent` add/update branches use
  `isIndexableFile` / `loadDocumentBody` with the same metadata.
  - `internal/rag/kb/convert_ingest_test.go` — tests for `isIndexableFile` and
    `loadDocumentBody` (markdown verbatim, csv converted, no-converter raw, format detection).
- **`internal/config/config.go`** — `RemembrancesConfig.KBConvertDocuments bool` (default true)
  + `KBConvertExtensions []string` (optional override). Viper default
  `remembrances.kb_convert_documents=true`.
- **`internal/config/init.go`** — generated TOML template adds `KBConvertDocuments = true`.
- **`internal/app/remembrances.go`** — `initRemembrancesKBSync` installs
  `convert.NewWithConvertibleExtensions(cfg.KBConvertExtensions)` via `SetDocumentConverter`
  when `KBConvertDocuments`, before import/watch.
- **`README.md`** — `pando convert` usage + "Document conversion in the Knowledge Base" section.
- `go.mod`/`go.sum`.

## Decisions

- No build tags (pure-Go lib, all formats always available).
- KB document references the **original file** (not a materialized sibling `.md`).
- CLI defaults to stdout, `-o` for file output.
- `.json/.jsonl/.xml` excluded from the curated KB auto-convert set to avoid converting
  legitimate data files unexpectedly (override via `KBConvertExtensions`).

## Verification

- `go test ./internal/convert/... ./internal/rag/kb/... ./internal/config/... ./cmd/... ./internal/api` → all pass.
- `go build ./...` → OK. `go vet` on touched pkgs → clean. gofmt clean on touched files.
- Manual CLI: `pando convert --list-formats`; `pando convert sample.xlsx` → Markdown table to
  stdout; `pando convert sample.csv -o out.md` → file written.

## Follow-up (deferred)

- Expose `KBConvertDocuments` (+ optionally `KBConvertExtensions`) in TUI/WebUI/API settings with
  7-locale i18n, following the project's settings pattern. The functional path works without it
  (config-file / viper default driven).
