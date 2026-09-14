---
created_at: 2026-09-14T15:54:09.965366897Z
updated_at: 2026-09-14T15:54:09.965366897Z
tags:
    - fix
    - sdk
    - typescript
    - agui
    - vite
    - browser
---

# PANDO-US-0006 — Make the SDK package browser-safe for a Vite build

Status: implemented and verified (2026-09-14). Story: `.kb/.pmngr/stories/PANDO-US-0006-make-the-sdk-package-browser-safe-for-a-vite-build.md`. Epic: [[PANDO-EP-0001]] (typescript-sdk-browser-first-ag-ui-client).

## What changed

All changes are inside `sdk/typescript/` (its own git repo, separate remote `pando-typescript-sdk`, gitignored `/sdk/` at the pando monorepo root — that's why coordinator scoped this task to that directory only). No Go files touched.

1. **`sdk/typescript/src/http.ts`** — removed the static `import * as https from "node:https"`. Replaced with:
   - `import type * as NodeHttps from "node:https"` (type-only, erased at compile time — zero runtime footprint).
   - `isNodeRuntime()` — `typeof process !== "undefined" && process.versions?.node` guard.
   - `insecureHttpsAgent()` — `await import(specifier)` where `specifier` is a **variable**, not a string literal (same defeat-static-analysis trick `agui/copilotkit.ts`'s `importOptional` already used for its optional peers). This stops bundlers from resolving/bundling `node:https` at build time even for the main entry.
   - `buildFetchInit` is now `async`; only calls `insecureHttpsAgent()` when `!rejectUnauthorized && isNodeRuntime()`. `fetchJSON` now does `await buildFetchInit(...)`.
   - Behaviour under Node is unchanged; all pre-existing `tests/http.test.ts` cases pass unmodified.

2. **`sdk/typescript/src/agui/client-entry.ts`** (new) — the source for the new `./agui/client` deep export. Re-exports only `client.ts` (`PandoAguiClient`, `PandoAguiError`, `parseSSE`, `randomId`, `DEFAULT_AGUI_PATH`, `PERMISSION_TOOL_NAME`, and their option/type interfaces) plus `export type * from "./types.js"`. Deliberately does **not** import `copilotkit.ts`.

3. **`sdk/typescript/src/agui/copilotkit.ts`** — unchanged in content; now also built as its own tsup entry, exported at the new `./agui/copilotkit` subpath (it was already self-contained: only imports `client.ts` + `types.ts`).

4. **`sdk/typescript/src/agui/index.ts`** — unchanged exports (backward compatible: still re-exports both the client and CopilotKit surfaces at `./agui`). Only the module doc-comment was extended to point consumers at the two new narrower subpaths.

5. **`sdk/typescript/package.json`**:
   - Added `"sideEffects": false`.
   - Added `"files": ["dist", "README.md", "LICENSE"]` — was previously missing, which meant a real `npm publish`/`file:` install had no explicit inclusion rule and would have fallen back to `.gitignore` (which excludes `dist/`!). This was also required for the fixture's `file:../..` dependency to reliably carry `dist/` regardless of npm's copy-vs-symlink strategy for local dependencies.
   - `exports` map gained `./agui/client` and `./agui/copilotkit`, each with `types` first (TS resolver requires this key order), then (for `.` and `./agui/client` only) a `browser` condition, then `import`/`require`.
   - `build`/`dev` scripts now build 4 tsup entries: `src/index.ts`, `src/agui/index.ts`, `src/agui/client-entry.ts`, `src/agui/copilotkit.ts`.
   - New scripts: `typecheck:agui-browser` (DOM-only lib check) and `test:browser-build` (the Vite fixture smoke test). `typecheck` now runs both the normal and the DOM-only check.

6. **`sdk/typescript/tsconfig.agui-browser.json`** (new) — extends the base tsconfig but sets `"lib": ["ES2022", "DOM"]` and `"types": []` (excludes `@types/node` from auto-inclusion, which is what let the DOM-less config silently "work" before — `@types/node` was supplying `fetch`/`ReadableStream`/etc.). Scoped to `src/agui/**/*.ts`. `npx tsc --noEmit -p tsconfig.agui-browser.json` passes clean.

7. **`sdk/typescript/tests/browser-build/`** (new fixture, private, not published) — a real Vite 8 + React 18 app: `package.json` (depends on `@pando-ai/sdk` via `"file:../.."`, `react`/`react-dom` `^18.3.1`, devDeps `vite ^8.3.0`, `@vitejs/plugin-react ^6.1.1`), `vite.config.ts`, `index.html`, `src/main.tsx` (imports `PandoAguiClient`, `parseSSE`, `randomId`, `DEFAULT_AGUI_PATH` and the `AguiEvent`/`PandoState` types from `@pando-ai/sdk/agui/client`, renders inside a `useEffect`), `tsconfig.json`, and `run.mjs` — the CI-ready smoke-test driver (`npm run test:browser-build` from `sdk/typescript`). `run.mjs`: builds the SDK, `npm install`s the fixture, runs `vite build`, and fails (non-zero exit) if the build errors, if the combined output matches any warning pattern (`warn`, `(!)`, "externalized for browser compatibility", "could not resolve", "module level directives"), if the output mentions `node:`, or if any built `.js`/`.html` file contains the literal string `copilotkit` (case-insensitive) or an unresolved `node:xxx` specifier.

8. **`sdk/typescript/tests/agui-client-entry.test.ts`** (new, 6 jest tests) — exercises `client-entry.ts` end to end (a mocked `run()` through `PandoAguiClient`, `parseSSE`/`randomId` re-exports, a `PandoState` type-level check, `PandoAguiError`), asserts `createPandoAgent`/`discoverPandoAgents`/`registerPandoCopilotKit` are `undefined` on that module, asserts the source has no `from "./copilotkit.js"` import, and a regression guard that `src/http.ts` has no static (non-`import type`) `node:` import.

## Why

Browser app developers (starting with git-in-track's GIT-US-0053, which is blocked on this) need `@pando-ai/sdk/agui` importable from a Vite + React app. Three defects blocked it: a static `node:https` import in the main entry, CopilotKit's dynamic-specifier `import()` reachable from `./agui`'s re-exports (dead weight + Vite warning), and no `sideEffects`/`browser` export condition.

## Verification

All from `sdk/typescript/`:
- `npm run build` — tsup, 4 entries, clean. `dist/agui/client-entry.js` (and its shared chunks) contain no reference to `copilotkit` (checked with `grep -il`).
- `npm run typecheck` — `tsc --noEmit` (main, unchanged) **and** `tsc --noEmit -p tsconfig.agui-browser.json` (DOM-only, no `@types/node`, scoped to `src/agui/`) — both clean.
- `npm test` — Jest, **96/96 passed** (90 pre-existing + 6 new in `tests/agui-client-entry.test.ts`). No existing test was modified.
- `npm run test:browser-build` — real `vite build` of the `tests/browser-build/` fixture against the just-built `dist/`, twice (repeatability check): **PASS**, zero warnings, 16 modules transformed, built bundle contains neither `copilotkit` nor `node:`.
- Manually verified all four `exports` subpaths (`.`, `./agui`, `./agui/client`, `./agui/copilotkit`) resolve correctly through Node's own resolver in both ESM (`import`) and CJS (`require()`) from inside the fixture's `node_modules/@pando-ai/sdk` symlink.

## Acceptance criteria status

All 5 satisfied, with one caveat:
- ✅ `http.ts` has no static `node:*` import; Node self-signed-cert behaviour unchanged.
- ✅ `./agui/client` exports types/import/require, bundle has no `copilotkit` reference, `sideEffects: false` + `browser` condition present.
- ✅ Vite + React 18 fixture under `tests/browser-build/` builds with zero warnings/`node:` resolutions and no `copilotkit` string in the bundle.
- ⚠️ **"That build runs in CI as a smoke test"** — the fixture + `npm run test:browser-build` driver are CI-ready (deterministic, non-zero exit on any violation), but I did **not** add a `.github/workflows` job: the coordinator's task scope was explicitly restricted to `sdk/typescript/` (+ `examples/` if required) because other agents were editing Go files concurrently, and `.github/workflows/` is outside that. Also relevant: `sdk/typescript` is its own separate git repo (remote `pando-typescript-sdk`), `/sdk/` is gitignored at the pando monorepo root, and neither `.github/workflows/build-matrix.yml` nor any other pando workflow currently references `sdk/typescript` at all — there was no existing CI hook to extend. **Follow-up needed**: wire `cd sdk/typescript && npm ci && npm run test:browser-build` into CI for whichever repo is meant to own it.
- ✅ `tsc --noEmit` against DOM-only lib (no `@types/node`) passes for `src/agui/` (`tsconfig.agui-browser.json`, exercised via `npm run typecheck:agui-browser`).

## Things deliberately not done (per story's "MUST NOT")

- `PandoAguiClient` itself untouched — still isomorphic, transport behaviour unchanged.
- `copilotkit.ts` not deleted — still reachable, now at its own `./agui/copilotkit` subpath (and still via `./agui` for back-compat).
- No new runtime dependencies added to the SDK package itself (the fixture's `vite`/`react`/`@vitejs/plugin-react` are devDependencies of the fixture only, not of `@pando-ai/sdk`).

Related: [[fix_agui_tool_calls_missing_from_event_stream.md]] (earlier AG-UI SDK fix, same `sdk/typescript` area).
