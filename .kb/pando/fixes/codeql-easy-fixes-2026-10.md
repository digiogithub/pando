---
created_at: 2026-10-13T12:36:02.754570873Z
updated_at: 2026-10-13T12:36:02.754570873Z
tags:
    - fix
    - security
    - codeql
---
# CodeQL easy fixes (2026-10-13)

Reviewed open CodeQL alerts with `gh api repos/{owner}/{repo}/code-scanning/alerts` (110 open on main @ 493243dff). Most are `go/path-injection` / `go/request-forgery` false positives: paths and URLs come from local user config in a local dev tool.

## Changed
- `web-ui/packages/pando-client/src/services/api.ts`: new `trimTrailingSlashes()` loop helper replaces `/\/+$/` regex (polynomial ReDoS, alerts 131-133) in `normalizeBaseURL`, `getConfiguredBasePath`, `isChildModeRestrictedPath`.
- `web-ui/packages/pando-client/src/services/uiPolicy.ts` `pathsOverlap`: removed `/^\.+|\.+$/g` (alerts 112-113); `split('.').filter(Boolean)` already drops empty segments, same result.
- `internal/config/overlay.go` `changedPaths`: removed `len(before)+len(after)` map capacity hint (alerts 110-111).
- `internal/rag/kb/kb.go` `rrfFuse`: capacity `min(limit, len(fused))` (alert 116).
- `internal/llm/tooldiscovery/policy.go` `Apply`: capacity `min(MaxDirectTools, total+1)` (alert 69).
- `internal/mesnada/server/api.go` `readGrowingFile`: explicit clamp to `maxLogTailBytes` before `int()` conversion (alert 5).

## Not fixed (false positives / wontfix candidates)
- darwin uiauto `uintptr(ParseUint(..,64))` (108, 109): darwin is 64-bit only.
- `internal/llm/systemone/health.go` sha256 of API key (130): cache-key fingerprint, not password storage.
- `internal/runtime/embedded/unpack.go` zipslip/symlink (37, 38, 43): already guarded by `safeJoin` + `checkLinkTarget`.
- `internal/runtime/events.go` (44): limit already clamped to ring size.
- logging clear-text (71), path-injection, request-forgery, command-injection: inputs from local user config/tool args by design.

## Verified
`go build ./internal/...`; `go test ./internal/config ./internal/rag/kb ./internal/llm/tooldiscovery ./internal/mesnada/server` ok; `bunx vitest run packages/pando-client/src` (20 passed).
