---
created_at: 2026-10-21T18:19:57.854966934Z
updated_at: 2026-10-21T18:19:57.854966934Z
tags:
    - pando
    - security
    - dependencies
    - dependabot
---
# Dependabot triage 2026-10-21 (14 high, 11 moderate, 1 low)

Related: [[pando_repo_pitfalls]].

Most alerts were Go, not web-ui. All high + moderate resolved with patch-level / same-minor bumps:

## Go (go.mod)
- `github.com/klauspost/compress` 1.18.5 -> 1.18.7 (1 high).
- `github.com/xuri/excelize/v2` 2.11.0 -> `v2.11.1-0.20261003002531-6258dcebc4e2` (9 high, 6 moderate). No tagged release contains the fixes; pinned the oldest master commit that covers every advisory (not latest master). Pulled `richardlehane/mscfb` 1.0.8, `xuri/efp` 0.0.2. Only used via `conductor-oss/markitdown` in `internal/convert` (xlsx conversion). Replace with v2.11.1+ tag when published.
- `go get` regrouped go.mod require blocks (large diff, formatting only).

## npm
- `web-ui`: vite ^6.4.2 -> ^6.4.4 (resolved 6.4.5) in package.json; refreshed BOTH package-lock.json and bun.lock. `bun audit` / `npm audit`: 0.
- `examples/vite-react`: vite ^8.3.0 -> ^8.3.3. 0 vulns.
- `examples/copilotkit`: hono ^4.13.9 -> ^4.13.11 (resolved 4.13.13); `npm audit fix` (non-force) -> follow-redirects 1.16.1, plus patch bumps 1.79.0->1.79.1, 1.0.2->1.0.3.

## Left open
- katex (low, examples/copilotkit) and 8 related low: fix requires `--force` / breaking @copilotkit/react-core downgrade. Skipped.

## Verification
`go mod verify`, `go build ./...`, `go test ./internal/convert/... ./internal/llm/agent ./internal/api` ok (convert has xlsx test); `bun run build` in web-ui ok.
