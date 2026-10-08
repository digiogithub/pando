---
created_at: 2026-10-06T21:04:45.252874272Z
updated_at: 2026-10-06T21:04:45.252874272Z
tags:
    - fix
    - treesitter
    - ci
    - release
---
# Fix: go-tree-sitter v0.2.1 (NDEBUG assert) + Build Matrix GTK deps (2026-10-06)

Follows [[pando/changes/tree-sitter-v0.2.0-language-coverage.md]].

## Problem 1 — release v1.2.10 failed (Linux + Windows job)
`make release-linux-arm64` (zig cc cross build) failed:
`# github.com/madeindigio/go-tree-sitter/csharp scanner.c:107:5: error: call to undeclared function 'assert'` (same for ruby, scala).
Cause: tree-sitter 0.25 runtime `array.h` reaches `<assert.h>` only via `ts_assert.h` when NDEBUG is unset; zig cc with -O2 defines NDEBUG; grammar scanners include `../array.h` and call `assert()` directly. Native gcc on linux/amd64 and macOS clang were unaffected (macOS job passed).
Fix (fork, tag v0.2.1, commit ca9ba30): `#include <assert.h>` in root `array.h`; `_automation/treesitter_updater/main.go` re-applies it on runtime refresh. Verified with a native build `CGO_CFLAGS="-O2 -DNDEBUG -Werror=implicit-function-declaration" go build ./...`.
Pando: go.mod -> go-tree-sitter v0.2.1 (commit 53784fab).
Tag v1.2.10 exists without a published release.

## Problem 2 — Build Matrix failing on main (pre-existing)
`go vet ./...` includes `./desktop` (wails + `decorations_linux.go` with `#cgo pkg-config: gtk+-3.0`); runner lacked GTK: `Package gtk+-3.0 was not found`. Wails linux needs webkit2gtk-4.0 unless tag `webkit2_41`; ubuntu-latest ships only 4.1.
Fix: `.github/workflows/build-matrix.yml` installs `libgtk-3-dev libwebkit2gtk-4.1-dev` and vets with `-tags webkit2_41` (combined with `enterprise` in that matrix entry). Same choice as Makefile `WAILS_TAGS`. actionlint clean; desktop vet/tests pass natively.

## User guidance
Do NOT cross-compile locally (zig) — builds/signing happen in the CI pipeline (macOS runner + Windows signing runner). goreleaser is not used (`.goreleaser.yml` left untouched by request); release = Makefile + scripts via `.github/workflows/release.yml`.
