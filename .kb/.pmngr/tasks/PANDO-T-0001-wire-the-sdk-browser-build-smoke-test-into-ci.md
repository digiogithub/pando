---
id: PANDO-T-0001
type: task
title: Wire the SDK browser-build smoke test into CI
status: done
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

## Resolution (2026-09-15)

SDK CI lives in the SDK repository: `.github/workflows/ci.yml` in
`madeindigio/pando-typescript-sdk`, added by PANDO-US-0009. It runs on every push to `main` and on
every pull request, in three jobs:

- `build, typecheck, test` — `npm ci`, build, both typechecks, the test suite and
  `npm run test:browser-build`, which is the smoke test this task was opened for. It fails the
  build when the bundle regains a `node:` builtin or a CopilotKit reference.
- `AG-UI protocol drift check` — checks out `internal/agui` from the monorepo and runs
  `check:agui-drift` against the live Go source.
- `HITL round trip` — added while closing this task; see below.

The open question about a token secret is answered: `digiogithub/pando` is **public**, so the
default `GITHUB_TOKEN` checks it out and no `PANDO_MONOREPO_TOKEN` is needed. The workflow keeps a
commented-out `token:` line and a note for the day that changes.

Three things had to be fixed before the workflow was actually green, none of which were visible
locally:

1. The HITL integration suite guarded only on `go version`. A GitHub runner has Go preinstalled,
   but the SDK is checked out on its own there, so the default `REPO_ROOT` — three directories up,
   where the monorepo sits during local development — did not exist. The suite tried to build and
   failed instead of skipping. It now also requires a `go.mod` declaring
   `github.com/digiogithub/pando`.
2. A fresh checkout of the monorepo **cannot be compiled at all**: `internal/api/ui_assets_app.go`
   embeds `webui/dist/**` and `internal/desktop/embed_binary.go` embeds `bin/pando-desktop`, both
   gitignored build artifacts. They exist on a machine that has run a full build, which is why
   `go build ./...` was green locally throughout. The job now runs the monorepo's own
   `make embed-stubs` first. This affects any contributor cloning the repository, not just CI.
3. The job cloned the monorepo's remote `main`, which did not yet carry the session's Go commits,
   so the fixture agent was missing and the server answered `unknown agent "fixture-hitl"`. Fixed
   by publishing the Go side.

Rather than let the suite skip silently in CI — the coverage loss PANDO-US-0008 and US-0010 sat in
review over — a dedicated `agui-integration` job checks out the monorepo, sets Go up from its
`go.mod` and points `PANDO_REPO_ROOT` at it, so the round trip really runs.

All three jobs green on run 34986903978.

## Notes

Follow-up of PANDO-US-0006, advanced by PANDO-US-0009. Related to PANDO-T-0002: both are about
where SDK integration tests are allowed to run.
