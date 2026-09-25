---
created_at: 2026-09-25T09:14:41.525367205Z
updated_at: 2026-09-25T09:14:41.525367205Z
tags:
    - feature
    - webui
    - api
    - update
    - version
---
# WebUI version display + update availability (v1.0.1, 2026-09-25)

## Motivation
The native redesign ([[webui-native-redesign]]) moved the version into a title-bar tooltip only. The user wanted it visible again, but not in the header:
- at the top of the chat info sidebar (right panel);
- in Settings > General > Diagnostics, next to the Debug ID.

Both places also tell the user when a newer release exists. There is no download button, only the hint "Run `pando update`".

## Changes
- **New package `internal/updatecheck`**
  - Release detection moved out of `cmd/update.go`: `DetectLatest`, `Result`, `RepoSlug`, arch aliases, release selection and tag parsing. The matching tests moved from `cmd/root_test.go` to `release_test.go`.
  - `cmd/update.go` now calls `updatecheck.DetectLatest`, both in `pando update` and in the background startup check.
  - `status.go`: `CurrentStatus(ctx) Status` returns {version, latest, update_available, update_command, release_url, checkable}.
    - The GitHub lookup is cached for 6h on success and 15m on failure, with a 10s timeout and mutex-serialized calls.
    - Dev builds without a semver return `checkable:false` and skip the lookup.
- **API**: `GET /api/v1/version` (`internal/api/handlers_base.go` `handleVersion`, registered in `routes.go`) is token-authenticated like every `/api/` path. It uses `context.WithoutCancel` so a client disconnect does not cache a failure.
- **WebUI**
  - `packages/pando-client/src/stores/versionStore.ts` (zustand): `fetchVersion` loads once and retries after 2s, 5s and 15s, because the first call can 401 before the session token is set.
  - `ChatInfoSidebar.tsx`: a "Version" section is the first block of `.chat-info-scroll`, showing `Pando vX` plus the update note (`.chat-info-update` in `chat.css`).
  - `GeneralSettings.tsx`: a "Pando version" row is first in Diagnostics, with a code value and copy button. Its description shows the update hint or "Up to date".
  - i18n keys `chat.info.version`, `settings.general.pandoVersion` and `version.{updateAvailable,upToDate}` added in all 7 locales.

## Gotchas
- A plain `go build` embeds whatever is already in `internal/api/webui/dist`. Use `make build` (target `web-ui-embedded`) so a WebUI change is actually embedded.
- With `go build` inside the repo, Go stamps the VCS tag into build info, and `version.init()` prefers that over `-ldflags -X`. To fake an older version for testing, use `-buildvcs=false`.

## Verification
- `go test ./internal/updatecheck ./internal/api ./cmd`: pass. New test `TestCurrentStatusDevBuildSkipsLookup`.
- `bun run typecheck`, `lint` (0 errors), `check:contrast` and `build`: OK. `make build`: OK.
- Live run of `pando app` built as v0.700.0:
  - the API returned `update_available:true, latest v1.0.0`;
  - Diagnostics showed "Pando version v0.700.0 — Update available: v1.0.0. Run pando update";
  - the chat sidebar showed the Version section first.
