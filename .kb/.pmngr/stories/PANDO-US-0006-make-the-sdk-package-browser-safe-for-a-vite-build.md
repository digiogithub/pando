---
id: PANDO-US-0006
type: story
title: Make the SDK package browser-safe for a Vite build
status: backlog
priority: critical
parent: PANDO-EP-0001
milestone: PANDO-M-0001
author: claude
labels: [sdk, typescript, agui, build]
estimate: 5
created: 2026-09-13T21:14:35Z
updated: 2026-09-13T21:17:17Z
---

## Description

As a browser app developer, I want to `npm i @pando-ai/sdk` and import the AG-UI client from a Vite + React 18 app, so that I get Pando streaming in the browser without Node polyfills, bundler warnings or dead CopilotKit weight.

Three concrete defects, all in `sdk/typescript`:

1. `src/http.ts:20` does `import * as https from "node:https";` at module scope, used only at `src/http.ts:88-91` to build an `https.Agent` for self-signed certs. It sits in the *main* entry (`.` export), so any bundler resolving the package root fails. Move that branch behind a lazy `await import("node:https")` guarded by a runtime check, or drop it from the browser path — everything else in `http.ts` is plain `fetch`.
2. `src/agui/index.ts:54-66` statically re-exports `createPandoAgent`, `discoverPandoAgents`, `registerPandoCopilotKit` and the CopilotKit types from `./copilotkit.js`. `copilotkit.ts:279-288` does `import(/* @vite-ignore */ specifier)` with a variable specifier, which makes Vite emit a dynamic-import warning and keeps dead weight in the bundle. Add a `./agui/client` deep export that re-exports only `client.ts` + `types.ts`, and move the CopilotKit surface to its own `./agui/copilotkit` subpath.
3. `package.json:6-20` declares no `"sideEffects": false` and no `"browser"` export condition; `engines` (`package.json:21-25`) lists node/bun/deno only. Add `"sideEffects": false`, a `browser` condition steering away from the `node:https` branch, and keep the existing `.`/`./agui` exports working so current consumers do not break.

Also validate the emitted `.d.ts` against a DOM-only lib set: `tsconfig.json` currently targets ES2022 with `"lib": ["ES2022"]` and no `DOM`, and the agui client only typechecks because `@types/node` supplies `fetch`/`ReadableStream`.

Do NOT rewrite `PandoAguiClient` itself — it is already isomorphic (`globalThis.fetch`, `response.body.getReader()`, `AbortController`, `globalThis.crypto.randomUUID` with a fallback) and its transport behaviour must not change in this story. Do NOT delete `copilotkit.ts`; it stays reachable on its own subpath. Do NOT add runtime dependencies.

## Acceptance Criteria

- [ ] `src/http.ts` contains no static `node:*` import; `PandoHttpClient` still honours the self-signed-cert option under Node (existing Node tests pass unchanged).
- [ ] `package.json` exports `./agui/client` (types/import/require) resolving to a bundle that contains no reference to `copilotkit.ts`, and declares `"sideEffects": false` plus a `browser` condition.
- [ ] A fixture Vite + React 18 app under `sdk/typescript/tests/browser-build/` imports `@pando-ai/sdk/agui/client`, runs `vite build`, and the build finishes with zero warnings and zero `node:`/polyfill resolutions; the built bundle does not contain the string `copilotkit`.
- [ ] That build runs in CI as a smoke test and fails the job on any bundler warning.
- [ ] `tsc --noEmit` against a DOM-only lib set (no `@types/node`) passes for `src/agui/`.

## Notes

Size: S/M. First story of PANDO-EP-0001 and the one that unblocks downstream work: git-in-track story GIT-US-0053 depends on this story directly — the git-in-track product owner chose on 2026-09-13 to depend on `@pando-ai/sdk/agui` rather than vendor `client.ts` + `types.ts`, so GIT-US-0053 is blocked until this ships and a version is published; nothing is vendored meanwhile.

Blocks PANDO-EP-0001's other stories only loosely (they can be written against `src/`), but nothing should be published until this lands. Evidence: `report-sdk-agui-client.md` §1.1-§1.2 and §5. Decision already taken: CopilotKit is out of scope for the browser path, and `@ag-ui/client` was rejected as an alternative client (its `RunFinishedEventSchema` has no `outcome`).
