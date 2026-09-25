---
created_at: 2026-09-25T11:10:03.035204968Z
updated_at: 2026-09-25T11:10:03.035204968Z
tags:
    - fix
    - desktop
    - build
    - embed
---
# Fix: `go build` fails with "cannot embed directory bin/Pando.app: contains no embeddable files"

Date: 2026-09-25

## Symptom
`xc build-and-copy` (make build) failed on Linux:
`internal/desktop/embed_binary.go:14:12: pattern bin/**: cannot embed directory bin/Pando.app: contains no embeddable files`

## Root cause
The uncommitted PANDO-T-0006 change (fresh-clone buildable) replaced `//go:embed bin/pando-desktop` with `//go:embed bin/**` (an `embed.FS` plus `EmbeddedDesktopBinary()`). In go:embed `**` is just `*`, so the pattern also matched the `bin/Pando.app` directory. `make desktop-embed` / `scripts/embed_desktop_artifact.py` / `make desktop-clean` delete and recreate `Pando.app` holding only `.keep`; dotfiles are not embeddable, so the directory had no embeddable files. The `Pando.app/desktop-placeholder` added by T-0006 was wiped by that tooling.

## Fix
- `internal/desktop/embed_binary.go`: pattern `//go:embed bin/*desktop*` — matches the tracked `bin/desktop-placeholder` and the generated `bin/pando-desktop`, never `Pando.app` or dotfiles.
- `scripts/embed_desktop_artifact.py` `restore_app_placeholder()` and `Makefile` `desktop-clean` now also create `internal/desktop/bin/Pando.app/desktop-placeholder`, so darwin's `bin/Pando.app/**` pattern stays valid.
- Recreated `internal/desktop/bin/Pando.app/desktop-placeholder` (jj/git working copy).

## Verification
`go build .` OK; `go vet ./internal/desktop` OK on linux (even without the Pando.app placeholder) and with `GOOS=darwin GOARCH=arm64`; `go test ./internal/desktop` OK.
