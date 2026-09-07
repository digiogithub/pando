---
created_at: 2026-09-07T15:33:30.028471508Z
updated_at: 2026-09-07T15:33:30.028471508Z
tags:
    - change
    - browser
    - obscura
    - cdp
    - internal-tools
    - webui
    - tui
---
# Change: Obscura browser support — P3 (selector/config surfaces)

**Date:** 2026-09-07
**Status:** Done
**Related plan:** [[obscura_browser_support_plan]] (P1 detection + P2 session launch were being done concurrently
by other agents in `internal/browser/detect.go` and `internal/llm/tools/browser_session.go`; this change only
covers P3 — the selector/config UI surfaces and docs — and deliberately did not touch those two files).

## What changed

1. `internal/tui/page/settings.go`
   - `buildInternalToolsSection` (~line 2696): verified the base `browserOptions` list
     (`chrome, msedge, chromium, opera`) already picks up any detected `obscura` install via the existing
     `ensureOption(browserOptions, install.Type)` merge loop — no base-list change needed, matching the plan's
     note that undetected engines stay out of the list and detected ones always surface.
   - Same loop: guarded the "auto-fill `BrowserUserDataDir` from the detected install" branch with
     `!llmtools.IsRemoteBrowserType(install.Type)` so a detected `obscura`/`lightpanda` install (which has no
     profile/user-data-dir concept) never seeds that field.
   - Apply path for key `internalTools.browserType` (~line 4970): kept the existing auto-fill of
     `itCfg.BrowserExecutable` (still needed so Pando knows which binary to launch as the CDP server for a remote
     type), but guarded the nested `BrowserUserDataDir` auto-fill with `!llmtools.IsRemoteBrowserType(itCfg.BrowserType)`.

2. `web-ui/src/components/settings/InternalToolsSettings.tsx`
   - Verified detected browsers already merge into `browserOptions` via the existing dedupe loop
     (~line 137-151) fed by `/api/v1/config/browsers` — no change needed there.
   - Added helper `isRemoteBrowserType(type: string): boolean` (true for `'lightpanda'` and `'obscura'`).
   - When `config.browserType` is remote, render one muted hint line directly under the browser `SelectInput`
     ("Launched by Pando as a local CDP server; profile, user-data-dir and headless options do not apply."),
     styled `{ fontSize: 12, color: 'var(--fg-muted)' }` to match the existing "Detected: …" hint line just below
     it in the same card.

3. Config templates/docs
   - `cmd/init.go` (~line 346) and `internal/config/init.go` (~line 492): extended/added the `BrowserType`
     inline comment in the generated TOML template to list `lightpanda` and `obscura` and explain that they are
     CDP-server browsers Pando launches as a background process and drives over a WebSocket, instead of a
     Chromium executable.
   - `README.md`: checked for existing `browserType`/browser-internal-tools documentation; none exists (the only
     browser-related mentions are the unrelated Desktop Controller CDP-backend feature and general "access via
     browser" prose), so left unchanged per the conditional instruction rather than force an unrelated addition.

## Files touched
- `internal/tui/page/settings.go`
- `web-ui/src/components/settings/InternalToolsSettings.tsx`
- `cmd/init.go`
- `internal/config/init.go`

## Motivation
Phase 3 of the Obscura headless-browser support plan: once `internal/browser/detect.go` (P1, concurrent) can
detect an installed Obscura binary and classify it as a remote/CDP-server browser type, and
`internal/llm/tools/browser_session.go` (P2, concurrent) can launch its `serve` CDP server, the selector UIs and
config docs must (a) surface it as a choice and (b) stop offering/auto-filling browser-profile concepts (user
data dir, headless) that don't apply to a CDP-server engine.

## Verification
- `go build ./...` — clean, no output/errors.
- `go vet ./internal/tui/page/...` — clean.
- `cd web-ui && npx tsc --noEmit` and `npm run typecheck` (project's actual typecheck script, `tsc --noEmit`) —
  both clean, no errors.
- Read back the full diff (`jj diff --git -- internal/tui/page/settings.go
  web-ui/src/components/settings/InternalToolsSettings.tsx cmd/init.go internal/config/init.go`) to confirm only
  the intended four hunks landed, and that the two off-limits concurrent-edit files
  (`internal/browser/detect.go`, `internal/llm/tools/browser_session.go`) were never touched.
- Did not run a live manual check with a real `obscura` binary launching a session (out of scope for P3; that is
  P4's manual-check step in the plan, owned by whichever phase finishes last).