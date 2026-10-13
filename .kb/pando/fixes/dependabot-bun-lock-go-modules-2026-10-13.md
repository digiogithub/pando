---
created_at: 2026-10-13T11:55:37.154077648Z
updated_at: 2026-10-13T11:55:37.154077648Z
tags:
    - fix
    - security
    - dependencies
---
# Dependabot: 23 alerts after v1.3.1 — stale web-ui/bun.lock + Go modules (2026-10-13)

Previous rounds: [[pando/fixes/dependabot-dompurify-brace-expansion-2026-10.md]], [[pando/changes/dependabot-alerts-2026-09-25.md]].

## Finding
- GitHub reported 23 alerts (11 high, 10 moderate, 2 low). `gh` could not list them because the `digiogithub` token is invalid, so they were triaged locally.
- **`web-ui/bun.lock` alone held 23 advisories (13 high, 8 moderate, 2 low)**, while `npm audit` on `web-ui/package-lock.json` was clean.
  - Earlier rounds updated `package-lock.json` (including `overrides`) but never `bun.lock`, and Dependabot reads both.
  - Stale packages: react-router 7.13.2 (10 advisories), vite 6.4.2, source-map-js 1.2.1, @babel/core, browserslist, nanoid, postcss, @humanfs/node, baseline-browser-mapping.
- Only git-tracked manifests count: root package-lock, web-ui package-lock + bun.lock, examples/copilotkit, examples/vite-react, go.sum, examples/acp-client/go. `sdk/` is not tracked, so its npm audit findings (jest 29 chain, handlebars) do not reach Dependabot.

## Changes
- web-ui:
  - `bun audit fix` fixed 13.
  - `bun update react-router-dom` moved 7.13.2 to 7.18.4 (`package.json` `^7.18.4`).
  - `npm install` synced `package-lock.json`.
  - `bun audit` and `npm audit` both report 0.
- examples/copilotkit: `npm audit fix` (no --force) bumps next (Draft Mode leak) and the graphql-yoga / @graphql-tools chain.
  - 8 lows remain (katex / mermaid / streamdown via @copilotkit/react-core and react-ui). Their only "fix" is a semver-major downgrade to 1.54.1, so they were skipped.
- Go (`go.mod`/`go.sum`):
  - golang.org/x/net 0.58.0 → 0.60.0 (HTTP/2 GO-2026-6603/6610/6611/6612/6617).
  - golang.org/x/image 0.41.0 → 0.45.0.
  - github.com/yuin/goldmark 1.7.11 → 1.7.17.
  - Pulled in transitively: x/crypto 0.57.0, x/term 0.46.0, x/text 0.42.0.
  - Still reported by govulncheck with no fixed version: docker/docker v28.5.2+incompatible (2) and x/crypto (1).
  - Stdlib advisories (net/http, crypto/tls, mime) are fixed in go1.27.2. CI uses `go-version-file: go.mod` (`go 1.26.0`), so that depends on which patch setup-go resolves.

## Verification
- web-ui: `bun run typecheck`; lint (0 errors, 6 existing warnings); vitest 111/111; `bun run build:embedded`.
- Go: `go build ./...`; tests of every package importing goldmark, x/image or x/net, plus `./internal/llm/agent` and `./internal/api`; govulncheck re-run.
- examples/copilotkit: only the `npm audit` result was checked; it was not built.
