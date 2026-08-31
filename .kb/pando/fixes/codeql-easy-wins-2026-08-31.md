---
created_at: 2026-08-31T09:40:24.540516322Z
updated_at: 2026-08-31T09:40:24.540516322Z
tags:
    - fix
    - security
    - codeql
    - dependabot
---
# CodeQL / Dependabot triage and easy-win fixes (2026-08-31)

## Scope

Reviewed all 96 open CodeQL alerts and 7 open Dependabot alerts on `digiogithub/pando`
(`gh api repos/digiogithub/pando/code-scanning/alerts` and `.../dependabot/alerts`;
helper scripts exist at `.ai/scripts/code-analysis.sh` and `.ai/scripts/dependabot.sh`).
Only genuinely-true positives fixable in a few lines were changed. False positives were
left in place and are recorded here so the triage is not repeated.

## Alert inventory

| Rule | Count | Verdict |
|---|---|---|
| go/path-injection | 72 | Almost entirely false positives: paths come from the user's own config/CLI in a local dev tool |
| go/request-forgery | 9 | Provider base URLs from user config (Copilot, embedding models) — by design |
| go/incorrect-integer-conversion | 4 | 3 true (darwin uiauto, mesnada api) — FIXED |
| go/unsafe-unzip-symlink | 2 | False positive — sanitizer not recognized |
| go/uncontrolled-allocation-size | 2 | 1 false (`events.go` already clamped), 1 config-derived (`policy.go`) |
| go/command-injection | 2 | False positives: LSP/browser executables come from user config |
| go/zipslip | 1 | False positive — sanitizer not recognized |
| go/incomplete-url-scheme-check | 1 | TRUE — FIXED |
| go/clear-text-logging | 1 | Needs deeper analysis, not a small fix — see Deferred |
| actions/missing-workflow-permissions | 1 | TRUE — FIXED |

## Fixed

1. **`actions/missing-workflow-permissions` (#96)** — `.github/workflows/build-matrix.yml`
   had no `permissions` block, so `GITHUB_TOKEN` got the default (potentially write) scope.
   Added a top-level `permissions: contents: read`; the job only builds and tests a checkout.

2. **`go/incomplete-url-scheme-check` (#2)** — `internal/llm/tools/fetch_browser.go`
   `sanitizeJSHref` neutralized only `javascript:` hrefs in fetched HTML. Added a
   `unsafeHrefSchemes` list (`javascript:`, `data:`, `vbscript:`) and a new `hasUnsafeScheme`
   helper that strips whitespace/control characters before the prefix check, so
   `"\njava\tscript:"`-style evasions are also caught.

3. **`go/incorrect-integer-conversion` + uncontrolled allocation (#5)** —
   `internal/mesnada/server/api.go`: the `?limit=` query parameter was parsed with
   `strconv.ParseInt(v, 10, 64)` and fed straight into `make([]byte, int(...))`. Added
   `maxLogTailBytes = 8 * 1024 * 1024` and clamped with `min64(n, maxLogTailBytes)`.
   Beyond the truncation alert this closes a real memory-blowup vector: a big log file plus
   a big `limit` sized the read buffer directly.

4. **`go/incorrect-integer-conversion` (#107, #108, #109)** — `internal/uiauto/platform/darwin/`:
   `refFromElement` (element.go) and `decodeWindowID` (backend.go) narrowed a parsed pid to
   `int32` with no range check; both now reject `pid < 0 || pid > math.MaxInt32`.
   **This also uncovered a genuine panic bug**: `refFromElement` did
   `strconv.ParseUint(hexStr[2:], ...)` and only checked `len(hexStr) < 2` *afterwards*, so a
   handle shorter than the `0x` prefix panicked instead of returning a malformed-handle error.
   The length check now precedes the slice.

## Confirmed false positives (left as-is)

- **`internal/runtime/embedded/unpack.go` (#43 zipslip, #37/#38 unsafe-unzip-symlink)** — the
  extractor already has `safeJoin` (rejects entries escaping the root) and `checkLinkTarget`
  (resolves relative and absolute symlink targets against the root before `os.Symlink`).
  CodeQL simply does not recognize these as sanitizers.
- **`internal/runtime/events.go:65` (#44)** — `make([]ContainerEvent, 0, min(limit, l.size))`
  where `limit` is already clamped to `l.size` a few lines above.
- **`internal/lsp/client.go:71` (#1) and `internal/llm/tools/browser_session.go:369` (#47)
  command injection** — the executable comes from LSP/browser configuration the user writes
  themselves; running a user-specified binary is the feature. The browser one already carries
  `//nolint:gosec`.
- **The 72 `go/path-injection` alerts** — Pando is a local developer CLI that operates on the
  paths the user gives it; every flagged sink reads or writes inside the user's own workspace
  or `.pando/` directory.

## Deferred (not a small fix)

- **`go/clear-text-logging` (#71), sink `internal/logging/logger.go:34`** — CodeQL reports
  API-key and password config fields reaching `logging.Debug` through a long indirect flow.
  No direct `logging.*(..., APIKey)` call site exists (keys are masked via `maskAPIKey` in
  `internal/api/handlers_config.go` and age-encrypted in `internal/config/agecrypto.go`).
  The correct fix is a redaction layer inside the `logging` package rather than a blind
  edit; left open deliberately.
- **`go/uncontrolled-allocation-size` in `internal/llm/tooldiscovery/policy.go:88`** —
  `make(..., 0, p.cfg.MaxDirectTools)` is sized from the user's own config, not remote input.

## Dependabot

No actionable fix available: `github.com/docker/docker` (4 alerts, high/medium) and
`github.com/disintegration/imaging` have **no patched version** published. The two npm
alerts (`uuid`, `@ai-sdk/provider-utils`) are confined to `examples/copilotkit/package-lock.json`,
which is example code and not part of any shipped binary.

## Verification

- `go vet ./...` exit 0; `GOOS=darwin GOARCH=arm64 go vet ./internal/uiauto/platform/darwin/`
  clean apart from a pre-existing `unsafe.Pointer` note in `ax_darwin.go`.
- `GOOS=darwin GOARCH=arm64 go build ./internal/uiauto/...` OK (the darwin files cannot be
  built natively on Linux).
- `go test ./internal/mesnada/server/` -> `ok`.
- A throwaway table test confirmed `hasUnsafeScheme` accepts `https://`, `/rel`, `#anchor`,
  `mailto:`, `?q=data:x` and rejects `javascript:`, `JavaScript:`, `data:text/html;base64,…`,
  `vbscript:`, `java\tscript:` and leading-newline variants.
- The 3 `TestDesktop*` failures in `internal/llm/tools` are pre-existing and environment
  dependent (X11 `GetImage BadMatch`); verified identical on a pristine clone of HEAD.

Related: [[ci-build-matrix-embed-stubs-and-lostcancel]], [[feature_desktop_controller_uiauto]].
