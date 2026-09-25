---
created_at: 2026-09-25T10:57:15.142660542Z
updated_at: 2026-09-25T10:57:15.142660542Z
tags:
    - build
    - dx
    - gintrack
---
# Fresh-clone Go build support (2026-09-25)

## Change
- Updated `internal/api/ui_assets_app.go` to embed `webui/**` and serve generated `webui/dist` when present, falling back to a tracked placeholder page in a clean checkout.
- Updated `internal/desktop/embed_binary.go` to embed available `bin/**` entries through an `embed.FS`; the desktop launcher reads the generated binary at runtime, while placeholders allow clean builds. Added tracked placeholders for the binary path and macOS bundle path.
- Updated `cmd/desktop.go` to use the runtime embedded-binary reader.
- Updated README build-from-source commands to run `go build ./...` before building the executable.
- Closed Gintrack `PANDO-T-0006` as done and added a verification comment.

## Motivation
Both `go:embed` patterns previously required generated, ignored artifacts to exist. A new checkout could not run `go build ./...`; the tracked placeholders and broader embed patterns remove that prerequisite while preserving generated assets.

## Verification
- Built from an isolated tree assembled from tracked files plus this change: `go build ./...` passed without generated WebUI or desktop artifacts.
- `go test ./internal/api ./cmd ./internal/desktop` passed with isolated `HOME`/`XDG_CONFIG_HOME`.
- `git diff --check` passed.
- Attempted macOS cross-build, but project tree-sitter dependencies fail under `GOOS=darwin CGO_ENABLED=0`; not a regression attributed to this change.
- Existing ignored generated files `internal/api/webui/dist/` and `internal/desktop/bin/pando-desktop` were left untouched.