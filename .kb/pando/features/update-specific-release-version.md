---
created_at: 2026-09-28T21:40:41.859360613Z
updated_at: 2026-09-29T14:03:50.669073491Z
tags:
    - feature
    - cli
    - update
---
# Feature: `pando update [version]` installs a specific release (2026-09-28)

## What changed
`pando update` accepts an optional positional release version (`v0.311.0` or `0.311.0`) to force download and install of that exact release, even if older than (or equal to) the running binary — rollback / reinstall.

- `cmd/update.go`: `Use: "update [version]"`, `Args: cobra.MaximumNArgs(1)`. With a version: `updatecheck.DetectVersion`, prints `Installing|Downgrading|Reinstalling Pando: <cur> -> v<x>`; `--check` just shows the release URL. No-arg path unchanged (latest, requires semver build, skips when up to date). Pinned install works from dev (non-semver) builds too. Renamed `latest` -> `target`.
- `internal/updatecheck/release.go`: new `DetectVersion(ctx, requested)` using `Repositories.GetReleaseByTag` (tries `v<ver>` then `<ver>`; 404 means try next), reuses `selectReleaseForTargets` for the platform asset (skips drafts), verifies the resolved semver equals the requested one. Missing tag → `release vX not found in digiogithub/pando`. Helper `releaseTagCandidates`. `GetReleaseByTag` is used instead of `ListReleases` because the list only returns the first page (30) and would miss old versions.
- `internal/updatecheck/release_test.go`: `TestReleaseTagCandidates`.
- Shipped in commits 59ac5bcac (code) and c8dd7102e (test).

## Verification
- `go build ./cmd/... ./internal/updatecheck/`, `go vet`, `go test ./internal/updatecheck/` ok.
- Live: `pando update --check v0.311.0` → release URL; `pando update --check 0.0.1` → not-found error; `pando update a b` → cobra arg error.
- End-to-end downgrade (2026-09-29): built v1.1.0 copy in a scratch dir, ran `./pando update v0.311.0` → `Downgrading Pando: v1.1.0 -> v0.311.0` / `Updated Pando to v0.311.0`, exit 0, sha256 changed, `./pando --version` → `v0.311.0+dirty`. Real binary replacement via `applyUpdate` confirmed on linux/amd64.
