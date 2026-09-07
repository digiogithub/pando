---
created_at: 2026-09-07T15:30:51.474645108Z
updated_at: 2026-09-07T15:30:51.474645108Z
tags:
    - change
    - browser
    - obscura
    - cdp
---
# Change: Obscura headless browser added to the shared browser detector (Phase 1)

**Date:** 2026-09-07
**Status:** Done
**Plan:** [[pando/plans/obscura_browser_support_plan.md]] — this completes P1 (Detection).

## What changed

`internal/browser/detect.go` (only file touched, plus its new test file):

1. `supportedBrowserCandidates`: added an `obscura` candidate entry right after `lightpanda`:
   - `Type: "obscura"`, `Label: "Obscura"`
   - `ExecNames: ["obscura", "obscura.exe"]`
   - `Paths`: `/usr/local/bin/obscura`, `/usr/bin/obscura`, `~/.local/bin/obscura`, `~/bin/obscura`,
     `~/.cargo/bin/obscura`, `/opt/obscura/obscura`, `/Applications/Obscura.app/Contents/MacOS/obscura`,
     `` `C:\\Program Files\\Obscura\\obscura.exe` `` (raw-string Windows path, matching the existing
     `chrome`/`msedge`/`chromium`/`opera` entries' quoting style).
   - `Profiles: []string{}` with comment "Obscura has no user profile concept" (same pattern as Lightpanda).
2. `NormalizeBrowserType`: added `case "obscura", "obscura-browser": return "obscura"`.
3. `IsRemoteBrowserType`: converted the single `== "lightpanda"` comparison into a `switch` returning `true`
   for both `"lightpanda"` and `"obscura"` (false otherwise). Updated its doc comment to name both as examples
   of CDP-server browser types.

## New tests (`internal/browser/detect_test.go`, file did not exist before)

Table-driven, stdlib `testing` only, no new dependencies:
- `TestNormalizeBrowserType`: obscura (lowercase/uppercase/`obscura-browser` alias), `light-panda` alias,
  `chrome`, `edge`→`msedge`, and an unrecognized value passthrough.
- `TestIsRemoteBrowserType`: true for `lightpanda` and `obscura`; false for `chrome`, `chromium`, `msedge`,
  `opera`, and `""` (defaults to chrome).
- `TestDetectObscuraOnPath`: writes a fake executable named `obscura` (mode 0755) into a `t.TempDir()`,
  prepends that dir to `PATH` via `t.Setenv`, then asserts `DetectInstalledBrowsers()` returns an entry with
  `Type == "obscura"` and `Label == "Obscura"`, and `ResolveBrowserInstall("obscura", "")` resolves it the
  same way. Skipped on `windows` (PATH/executable-bit semantics differ) to keep the suite hermetic and
  cross-platform-safe.

## Motivation

Obscura (https://github.com/h4ckf0r0day/obscura) is a Rust headless browser that exposes its own CDP
WebSocket server, architecturally identical to Lightpanda from Pando's point of view (a remote CDP server,
not a Chromium executable to launch with `chromedp.ExecPath`). This phase only makes the shared detector in
`internal/browser` aware of it (detection + type normalization + remote-type classification); later plan
phases (P2 CDP-server launch/lifecycle, P3 TUI/WebUI selector surfaces, docs) build on this and are not yet
implemented.

## Verification

- `go build ./internal/browser/` — succeeds.
- `go test ./internal/browser/ -v` — all pass, including the 3 new test functions
  (`TestNormalizeBrowserType`, `TestIsRemoteBrowserType`, `TestDetectObscuraOnPath`).
- `go vet ./internal/browser/` — clean.
- Diff scope confirmed limited to `internal/browser/detect.go` and the new `internal/browser/detect_test.go`;
  no other package touched.