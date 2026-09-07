---
created_at: 2026-09-07T15:31:49.922779074Z
updated_at: 2026-09-07T15:31:49.922779074Z
tags:
    - change
    - browser
    - obscura
    - lightpanda
    - cdp
    - internal-tools
---
# Change: engine-neutral CDP-server browser launcher (Phase 2 of Obscura support)

**Date:** 2026-09-07
**Status:** Done
**Plan:** [[pando/plans/obscura_browser_support_plan.md]] (Phase 2 of the multi-phase plan)
**Builds on:** [[pando/changes/obscura_browser_detection_p1.md]] (Phase 1 — `internal/browser` detection)

## What changed

`internal/llm/tools/browser_session.go` — generalized the Lightpanda-only CDP-server launch path so it
also starts Obscura, without touching `internal/browser` (that package was being edited concurrently by
another agent for Phase 1/detection and already exposes `IsRemoteBrowserType` returning true for both
`"lightpanda"` and `"obscura"`, and `ResolveBrowserInstall` returning an install with `Type: "obscura"`,
`Label: "Obscura"`).

Symbols touched:
- Renamed `startLightpandaProcess(executable string)` → `startRemoteBrowserProcess(install BrowserInstall)`.
  Same return signature (`context.Context, context.CancelFunc, context.Context, context.CancelFunc,
  *exec.Cmd, error`); unchanged free-port allocation, `waitForCDPEndpoint` polling, and
  `chromedp.NewRemoteAllocator` connect logic. Only the executable path and the `serve` args are now
  resolved from the `BrowserInstall` instead of a bare executable string, and error messages use
  `install.Label` instead of the hardcoded string "lightpanda".
- Added `remoteBrowserServeArgs(browserType, host string, port int) []string`, a pure helper (no process
  spawned) that normalizes `browserType` via the existing `NormalizeBrowserType` and switches on it:
  - `"obscura"` → `{"serve", "--host", H, "--port", P, "--allow-private-network", "--allow-file-access",
    "--quiet"}`
  - default / `"lightpanda"` → `{"serve", "--host", H, "--port", P}` (unchanged behaviour)
- `GetOrCreateBrowserSession` (~line 117): now calls `startRemoteBrowserProcess(resolvedInstall)` instead
  of `startLightpandaProcess(resolvedInstall.Executable)`; the wrapping error is
  `fmt.Errorf("%s startup failed: %w", resolvedInstall.Label, err)` (was hardcoded "lightpanda startup
  failed"). Comments above the remote-vs-local branch and on the `serverProcess` field updated to say
  "Lightpanda, Obscura, and other CDP-server browsers" instead of naming only Lightpanda.

## Why

Obscura (https://github.com/h4ckf0r0day/obscura, Rust headless browser, own CDP server) is architecturally
identical to Lightpanda from Pando's point of view — a remote CDP server reached via
`chromedp.NewRemoteAllocator`, not a Chromium executable launched with `chromedp.ExecPath`. Phase 1 taught
the shared `internal/browser` detector about it; this phase makes the actual process-launch code stop
assuming "the only CDP-server browser is Lightpanda" so `obscura serve` can be spawned the same way.

The two `--allow-*` flags are passed for Obscura specifically because it defaults them OFF
(`--allow-private-network` blocks navigation to loopback/RFC1918 addresses, i.e. local dev servers;
`--allow-file-access` blocks `file://` URLs). The server here is bound to `127.0.0.1` only and the agent
driving it already has local shell/file access, so those restrictions would only break the primary use
cases (open `http://localhost:*` dev servers, inspect local files) without adding any real security
boundary. `--quiet` keeps Obscura's own stdout/stderr out of the pando process logs. Lightpanda has no
equivalent flags/needs, so its args are unchanged.

## Verification

- `go build ./...` — succeeds, no errors.
- `go vet ./internal/llm/tools/...` — clean.
- `go test ./internal/llm/tools/ -run 'Browser|Remote' -count=1 -v` — all pass, including the new
  `TestRemoteBrowserServeArgs` (obscura, obscura with alias casing "Obscura-Browser", lightpanda, lightpanda
  with alias casing "Light-Panda", and an unknown browser type falling back to the lightpanda-style args)
  and the pre-existing `TestBrowserRegistryInit`, `TestBrowserSessionCleanup`, `TestBrowserMaxSessions`,
  `TestCloseAllBrowserSessions`, `TestIsBrowserProfileLockError`.
- `grep -rn "startLightpandaProcess"` across the repo returns no hits — the rename left no dangling
  references.
- No browser process is actually launched in the new test; it only exercises the pure
  `remoteBrowserServeArgs` argument-builder.

## Files touched

- `internal/llm/tools/browser_session.go` (edited)
- `internal/llm/tools/browser_remote_test.go` (new)

## Scope note

Only `internal/llm/tools/browser_session.go` and the new test file were touched, per the phase 2
instructions — `internal/browser/detect.go` was left untouched since another agent was concurrently
working on it for the detection phase.