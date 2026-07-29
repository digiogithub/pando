---
created_at: 2026-07-29T09:48:31.89730931Z
updated_at: 2026-07-29T09:48:31.89730931Z
tags:
    - fix
    - tools
    - view
    - utf8
    - encoding
---
# Fix: View tool reports UTF-8 text files as binary

Date: 2026-07-29

## Symptom

`View` intermittently refuses plain text files with a "binary content" error.
Reported case: `/www/digio-ai-costs-management/AGENTS.md`, which `file(1)`
identifies as `Unicode text, UTF-8 text`.

## Root cause

`isBinaryContent` in `internal/llm/tools/view.go` scanned the 4096-byte sample
byte by byte and counted **every byte above 0x7E** as non-printable:

```go
if b < 0x09 || (b > 0x0D && b < 0x20) || b > 0x7E {
    nonPrintable++
}
```

Every UTF-8 multi-byte character (em dash `—`, curly quotes, `·`, accents, CJK,
emoji) contributes 2-4 such bytes, so any file with roughly one non-ASCII
character per three ASCII characters crosses the 30% threshold. The reported
file measured **exactly 0.31** over its first 4096 bytes — just past the limit,
which is why the failure looked intermittent (it depends on where the dense
Unicode falls inside the sample, and on file size vs `MaxReadSize`).

The heuristic was the only copy in the codebase (`grep 0x7E` matched one line).

## Change

`isBinaryContent` now decodes the sample as UTF-8 with `utf8.DecodeRune` and
counts non-printable **runes**, not high bytes:

- null byte anywhere still means binary immediately (unchanged);
- counted as non-printable: C0 controls other than `\t \n \v \f \r`, `DEL`
  (0x7F), C1 controls (0x80-0x9F), and bytes that are not valid UTF-8;
- a multi-byte rune truncated by the fixed 4096-byte sample boundary (fewer
  than `utf8.UTFMax` bytes left) is ignored instead of counted as garbage;
- ratio threshold stays at 0.30, now over rune count.

Real binaries still trip it: ELF/PNG headers contain null bytes, and random
high bytes (`\xff\xfe\xfd`) are invalid UTF-8.

## Files

- `internal/llm/tools/view.go` — rewritten `isBinaryContent`, added
  `unicode/utf8` import.
- `internal/llm/tools/view_binary_test.go` (new) — table test: ASCII, null
  byte, ELF header, UTF-8 punctuation, CJK, emoji, truncated trailing rune,
  random high bytes, control characters.

## Verification

- `go build ./...` — OK.
- `go test ./internal/llm/tools` — ok (all cases pass, existing view tests
  unaffected).
- Ad-hoc test reading the actual `/www/digio-ai-costs-management/AGENTS.md`
  through `isBinaryContent`: passes after the fix, ratio was 0.31 before.
