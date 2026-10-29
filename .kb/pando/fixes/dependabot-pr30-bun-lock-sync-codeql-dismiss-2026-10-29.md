---
created_at: 2026-10-29T08:16:55.084051891Z
updated_at: 2026-10-29T08:16:55.084051891Z
tags:
    - fix
    - security
    - dependabot
    - codeql
    - dependencies
---
# Dependabot PR #30 merge, bun.lock sync, CodeQL dismissals (2026-10-29)

## What
- Merged Dependabot PR #30 (squash, main 47c64e51): npm_and_yarn group bumps in `examples/copilotkit` (next 16.4.1 critical RCE fix, body-parser, dompurify, fast-uri, ip-address, qs) and `web-ui` (zod, dompurify, fast-uri, serialize-javascript, undici).
- Dependabot only edits `web-ui/package-lock.json`; build uses bun. `bun install` alone only picked up direct/override changes (dompurify, serialize-javascript). Transitive undici/fast-uri/zod stayed old in `web-ui/bun.lock`.
- Fix: `web-ui/package.json` overrides: `fast-uri >=4.2.3`, `undici >=8.12.3`, `zod >=4.6.7`; regenerated `bun.lock` and `package-lock.json` (`npm install --package-lock-only`).
- `examples/copilotkit/package.json`: new `overrides` `katex ^0.18.2` (rehype-katex wants ^0.16, alert #136) and `zod@^4 ^4.6.6` (alert #180).
- jj commit nqmzwnxs 9d0cf812 "chore(deps): sync bun.lock and patch transitive npm advisories" on top of main.

## Known leftovers
- zod advisory GHSA-mrhx-c7xc-m3w5 range is `<4.6.6`, so zod 3.25.76 copies pulled by `@ag-ui/*` / `@copilotkit/*` (even latest @ag-ui/client 1.0.3 needs zod ^3.25.76) remain vulnerable; no 3.x fix. Alert #180 may stay open.
- deepmerge #204 (web-ui, low): no patched version.
- examples/copilotkit: turbopack cannot resolve `@pando-ai/sdk/agui` (file: symlink outside root); `next build --webpack` compiles but fails tsc on pre-existing `app/page.tsx(60,27) Property 'id' does not exist on type 'PandoTodo'` — unrelated to deps.

## CodeQL
Dismissed alerts 134-140 as false positive: api.go:186 (bounded by maxLogTailBytes), policy.go:88 and kb.go:1193 (min() cap), mesnada/store path-injection, search/ignore.go, fswatch/exclude.go (local workspace/config paths). See [[codeql-easy-fixes-2026-10]].

## Verification
- web-ui: `bun run build` OK, `vitest run` 28 files / 133 tests passed.
- examples/copilotkit: `npm ci` + `next build --webpack` compiled successfully (type error pre-existing).
