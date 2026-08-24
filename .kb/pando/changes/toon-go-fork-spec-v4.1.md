---
created_at: 2026-08-24T12:24:43.675005643Z
updated_at: 2026-08-24T13:31:00.892244887Z
tags:
    - changes
    - toon
    - serialization
    - spec
    - dependency
---
# toon-go fork upgraded from TOON spec 1.4 to spec 4.1

## What changed

`/www/MCP/Pando/toon-go` (fork `madeindigio/toon-go` of `toon-format/toon-go`, module path kept as
`github.com/toon-format/toon-go`) implemented only TOON spec **1.4**. It was brought up to spec
**4.1**, the version the reference TypeScript implementation `@toon-format/toon` v4.1.1 targets.

Baseline before the work: **147 failing** conformance cases. After: **561 passing / 0 failing**
(`go test ./...`, `go vet ./...` clean).

The `tests/spec` submodule pointer was moved from `v1.4.0` (51fe1e9) to `v4.1.1` (62f16b3).

### Important correction to the original assumption

TOON 4.x does **not** provide key folding / dotted-path flattening. Spec 4.0 *removed*
`keyFolding`, `flattenDepth` and `expandPaths` entirely. The v4 mechanism for flattening nested
objects is instead:

- **Nested field groups** in tabular headers (S6, S9.3):
  `orders[2]{id,customer{name,country},total}:` with rows staying flat delimiter-separated
  primitives, laid out by a depth-first walk, unbounded depth.
- **Keyed tabular form** (S6, S9.5): `users[2:]{age,city}:` with one `entrykey: cells` row per
  entry, and `[N:]{...}:` at the document root.

Both are implemented.

## Files and symbols touched

- `internal/format/format.go` - rewritten. `Context` collapsed to a single `Delimiter`;
  `NeedsQuoting` completed for S7.2 (leading `#`, leading `+` numeric-like, control chars,
  ASCII-only key pattern); `QuoteString` emits `\uXXXX`; `ValidateString` rejects unpaired
  surrogates (S3); new `FormatNumber` (canonical S2 form plus JSON exponent notation outside the
  canonical range) and `ParseNumberToken` (normative S4 decoder grammar).
- `internal/parse/parse.go` - rewritten. `UnquoteString` handles `\uXXXX` and rejects surrogate
  escapes; new `ValidateQuotedToken` (S7.4 quoted-token boundary), `IndexUnquoted`,
  `SplitDelimited` (preserves empty tokens, trims only U+0020).
- `internal/codec/encoder.go` - rewritten. New `fieldNode` tree; `detectColumns`,
  `detectTabular`, `detectKeyedTabular`; `encodeKeyedTabular`, `emitRows`, `rowCells`,
  `renderHeader`, `renderFieldList`; list-item objects now render the first field on the hyphen
  line by emitting it at depth+1 and rewriting its opening line (S10).
- `internal/codec/decoder.go` - rewritten. `prepareLines` (BOM removal, CRLF, trailing-space
  strip, comment pre-pass S5.1); `parseHeaderSyntax` with `headerStatus`
  (notHeader/malformed/ok), `parseBracketSegment`, `parseFieldList`, `splitFieldEntries`,
  `matchBrace`, `checkForeignDelimiters`; `parseTabularRows`, `parseKeyedRows`, `parseListItems`,
  `parseListItem`, `parseObjectInto`, `buildFromCells`; `spanState` stack plus `checkBlank` for
  the S12 header-span blank-line rule; `isScalarLine` for the S5.2 scalar-line error.
- `internal/codec/options.go` - single `delimiter` option. `WithDelimiter` added;
  `WithDocumentDelimiter`, `WithArrayDelimiter` become deprecated aliases; `WithLengthMarkers`
  and `WithDecoderDocumentDelimiter` become deprecated no-ops.
- `internal/codec/object.go` - added unexported `Object.get`.
- `internal/codec/normalize.go` - numbers now formatted through `format.FormatNumber`.
- `api.go` - exposes `WithDelimiter`, documents the deprecations.
- `tests/v4_test.go` - new: nested field groups, deep nesting, keyed tabular in field and root
  position, single-entry object does not collapse, comment stripping, `#`-leading string quoting,
  empty-array forms, `[#N]` rejection.
- `tests/spec_fixtures_test.go` - fixture option key renamed `indent` to `indentSize`;
  `lengthMarker` handling dropped.
- `tests/arrays_test.go`, `tests/errors_test.go`, `tests/options_test.go`,
  `tests/decode_test.go`, `examples/basic`, `examples/delimiters`, `README.md` - updated for the
  removed `[#N]` syntax, the single delimiter option, and S7.4 (decoders accept any unquoted key
  token, so `1invalid: value` is valid input).

## Pando dependency switch

Pando now consumes the fork through a `replace` in `/www/MCP/Pando/pando/go.mod`:

```
replace github.com/toon-format/toon-go => github.com/madeindigio/toon-go v0.0.0-20260824122047-953870f65a68
```

The `require` line still names `github.com/toon-format/toon-go` and every import path is
unchanged, because the fork deliberately keeps the upstream module path so the work can be
offered upstream as-is. `go.sum` carries the `madeindigio/toon-go` hashes.

## Motivation

Pando converts tool output to TOON through `internal/llm/tools/json_output.go`
(`toon.Marshal`) and `internal/agui/subagents.go` (`toon.DecodeString`). The vendored library
was three major spec revisions behind, so it emitted non-conforming documents (legacy `key[0]:`
empty arrays, unquoted `#`-leading strings that read as comments under v4, no nested field
groups or keyed tabular form) and rejected valid input.

## Verification

- `go test ./...` in `/www/MCP/Pando/toon-go`: all green, 561 spec fixture subtests pass.
- `go vet ./...`: clean. `gofmt -l .`: clean.
- Fork pushed to `madeindigio/toon-go` `main` as commit `953870f`.
- Pando after the `replace`: `go build ./...` succeeded, `go test ./internal/llm/tools
  ./internal/agui` passed. Pando uses only `toon.Marshal` and `toon.DecodeString`, so no removed
  option affects it.

Related: [[toon-first-serializer-refactor]], [[tools-output-format-table]]
