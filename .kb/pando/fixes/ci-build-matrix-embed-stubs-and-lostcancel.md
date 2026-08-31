---
created_at: 2026-08-31T09:31:18.041382234Z
updated_at: 2026-08-31T09:31:18.041382234Z
tags:
    - fix
    - ci
    - build
    - github-actions
---
# Fix: Build Matrix CI failing on go:embed patterns + go vet lostcancel (2026-08-31)

## Symptom

`Build Matrix` workflow failed on every push to `main` (runs 33377571004, 33366918230,
33149498157, 32953501395...), both matrix legs (`MODULE_TAGS=none` and `MODULE_TAGS=enterprise`),
at the **Vet** step:

```
internal/api/ui_assets_app.go:8:12: pattern webui/dist/**: no matching files found
internal/desktop/embed_binary.go:8:12: pattern bin/pando-desktop: no matching files found
Process completed with exit code 1.
```

## Root cause

Two `go:embed` targets are **generated build artifacts and gitignored**:

- `internal/api/webui/dist/**` — produced by `make web-ui-embedded` (`.gitignore:53` `dist/`)
- `internal/desktop/bin/pando-desktop` — produced by `make desktop-embed` (`.gitignore:68`)

A fresh CI checkout has neither, so `go vet ./...` / `go build` fail before anything else runs.
It works locally only because developers have already built those assets.
(The darwin bundle embed `bin/Pando.app/**` was already safe: `bin/.keep` and
`bin/Pando.app/.keep` are committed placeholders.)

A **second, independent** failure was hidden behind the first: `go vet` reports a real
`lostcancel` in `internal/mesnada/agent/spawner_template.go` — `context.WithCancel`'s
`cancel` was overwritten by a `context.WithTimeout` reassignment, and the `buildEnv`
error path returned without calling `cancel()`.

## Changes

- `Makefile`: new phony target `embed-stubs` that creates the missing embed placeholders
  (`internal/desktop/bin/pando-desktop` empty file, `internal/api/webui/dist/index.html`)
  **only when absent**, so a real local build is never overwritten. Added to `.PHONY`.
- `.github/workflows/build-matrix.yml`: new step `Prepare embed placeholders` running
  `make embed-stubs` right after `Set up Go`, before `Vet`.
- `internal/mesnada/agent/spawner_template.go` (`TemplateSpawner.Spawn`): replaced the
  `WithCancel` + conditional `WithTimeout` reassignment with a single if/else that creates
  exactly one context, and added the missing `cancel()` on the `buildEnv` error path.

## Verification

Reproduced and verified in a clean shallow clone of HEAD in `/tmp` (same state as CI):

1. Before fix: `go vet ./internal/api/ ./internal/desktop/` reproduced both embed errors.
2. After `make embed-stubs`: `go vet ./...` exit 0, and `go vet -tags enterprise ./...` exit 0.
3. `make build-fast MODULE_TAGS=` -> `./pando --version` = `v0.700.0` (no variant, as the
   "Check the variant stamp" step requires).
4. `make build-fast MODULE_TAGS=enterprise` -> `./pando-enterprise --version` =
   `v0.700.0 (enterprise)`.
5. `make xpando` OK; `go test ./cmd/xpando ./pkg/extension ./internal/extensions` all `ok`.

i.e. the full Build Matrix job sequence passes end to end from a virgin checkout.

Related: [[project_desktop_wails_plan]], [[project_webui_implementation_plan]],
[[analysis_pando_enterprise_extension_system]].
