---
id: PANDO-T-0001
type: task
title: Wire the SDK browser-build smoke test into CI
status: backlog
priority: medium
parent: PANDO-US-0006
milestone: PANDO-M-0001
author: claude
labels: [sdk, ci, agui]
estimate: 2
created: 2026-09-14T00:00:00Z
updated: 2026-09-14T00:00:00Z
---

## Description

PANDO-US-0006 shipped `npm run test:browser-build` in `sdk/typescript`: a real Vite + React 18
fixture under `tests/browser-build/` that consumes the packed SDK and exits non-zero on any build
warning or on a `node:` or `copilotkit` reference in the emitted bundle. The story's last
acceptance criterion — "that build runs in CI as a smoke test" — was deliberately left open.

Two facts decide where the job belongs: no existing workflow in this repository references
`sdk/typescript` at all, and `sdk/typescript` is a separate git repository (remote
`pando-typescript-sdk`) whose path is gitignored at the monorepo root. Pick the repository that
owns SDK CI, then add a job that runs `npm ci`, `npm run build`, `npm run typecheck`, `npm test`
and `npm run test:browser-build`.

## Acceptance Criteria

- [ ] A CI job runs `npm run test:browser-build` on every push and pull request that touches the SDK.
- [ ] The job fails the build when the bundle regains a `node:` builtin or a CopilotKit reference.
- [ ] The chosen repository and workflow file are recorded here, so the split is not rediscovered.

## Notes

Follow-up of PANDO-US-0006. Size XS, but it needs a decision about repository ownership first.
