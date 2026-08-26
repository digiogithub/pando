---
created_at: 2026-08-26T16:48:03.683856011Z
updated_at: 2026-08-26T16:48:03.683856011Z
tags:
    - feature
    - design
    - p0
    - database
    - snapshot
    - config
---
# Design Studio — P0 Foundations (implemented 2026-08-26)

Phase P0 of [[pando_design_studio_plan]] is implemented: the `internal/design` model,
its SQLite schema, and version history bound to **directory-scoped** snapshots.
No agent tools, no HTTP surface, no UI yet — those are P1-P4.

## What was changed

### 1. Scoped snapshots (`internal/snapshot`)

Versioning a design artifact must never be able to revert unrelated work, but the
snapshot service only knew how to capture the whole working directory.

- `internal/snapshot/scanner.go`: `Scan` was split into `Scan` (unchanged, keeps the
  `fileutil.IsSafeWorkingDirectory` guard) and a new `ScanScoped`, which walks an
  arbitrary sub-directory without that guard. An artifact directory carries no project
  marker of its own, so the guard would otherwise return an empty file list. Both share
  the private `scan`.
- `internal/snapshot/scoped.go` (new): `SnapshotTypeScoped`, `(*service).CreateScoped`
  and `(*service).RevertScoped`. A scoped snapshot records `WorkingDir = rootDir` and
  paths relative to it. `RevertScoped` refuses a non-scoped snapshot, takes its own
  safety snapshot first, restores the recorded files and deletes only files that
  appeared **inside** the root.
- `Service` interface gained `CreateScoped` / `RevertScoped`.

### 2. DB schema

`internal/db/migrations/20260826000001_add_design.sql`: `design_artifacts` (unique index
on `dir`), `design_versions` (PK `artifact_id, number`, `snapshot_id` points at a scoped
snapshot), `design_nodes` (PK `artifact_id, version, node_id`, slide-aware) and
`design_critiques` (own table so a version can be re-critiqued without rewriting
history). `session_id` on artifacts is deliberately **not** a foreign key: an artifact is
a committable deliverable that outlives its session.

sqlc is not installed in this environment and the design store does not go through
`db.Queries`; data access is hand-written in `internal/design/store.go` against `*sql.DB`,
the same shape `internal/agentvcs` uses.

### 3. `internal/design` package (new)

- `model.go` — `Kind` (v1: `web`, `deck`), `Artifact`, `Version`, `Node`, `Rect`,
  `Issue`, `Critique`, severity constants, `ValidKind`.
- `layout.go` — `Layout` resolves artifact directories and **rejects any path escaping
  the design output root**; `Slugify` (underscore preserved so `_system` stays
  recognisable and is reserved), `AvailableSlug`, `NewArtifactID` (`dsg_<hex>`),
  `NewCritiqueID`.
- `manifest.go` — `pando-design.json` read/write plus `Normalize`, which repairs a
  hand-edited manifest (unknown kind falls back to `web`, deck block added/dropped to
  match the kind) instead of failing the artifact.
- `scaffold.go` — minimal renderable placeholder entry per kind. The deck placeholder
  ships `@page` + `break-after: page` print styles on purpose: one-slide-per-page PDF
  export depends on them (P1 risk in the plan).
- `store.go` — artifacts / versions / node index / critiques. `ReplaceNodes` swaps a
  whole version's index in a transaction (the index is a render product, never merged).
  Missing rows return `ErrNotFound`.
- `service.go` — `Create` (directory + seed files + manifest + row + version 1),
  `Get`, `List`, `CommitVersion` (scoped snapshot then history row then manifest sync),
  `Versions` (attaches the latest critique per version), `Checkout` (scoped revert +
  current-version + manifest sync), `Diff`, `Delete` (metadata only — the files are the
  user's). Depends on a narrow `Snapshotter` interface, not the whole snapshot service.
  `NewServiceFromConfig` builds it from the loaded config.

### 4. Config

`config.DesignConfig` (`Design` field on `Config`): `OutputDir` (default `designer` —
not `design`, which already holds the bundled design-system examples), `SystemDir`
(`_system`), `DefaultKind` (`web`), and `Critique{Enabled, MaxRounds: 3, Threshold: 8}`.
Viper defaults registered alongside the rest.

## Verification

- `go test ./internal/snapshot` — new `scoped_test.go`: a scoped snapshot captures only
  the sub-tree; **`RevertScoped` restores the artifact, removes files added after the
  snapshot, and leaves an out-of-scope file untouched** (the regression guard the plan
  demands); a full snapshot is rejected by `RevertScoped`.
- `go test ./internal/design` — `design_test.go`: slugify cases, `AbsDir` escape
  rejection, manifest round trip + normalize repair, create/list/version/checkout/diff,
  duplicate-directory and unsupported-kind rejection, seed files escaping the artifact
  dir rejected, node index replace/slide filter/style round trip, critique attachment,
  `ErrNotFound` mapping, delete cascades metadata but keeps files on disk. The schema
  under test is read from the real migration file, so the two cannot drift.
- `go test ./internal/db` — new `TestMigrationsIncludeDesignSchema` runs the whole goose
  chain against a temp database and asserts the four tables exist.
- `go build ./...`, `go vet ./internal/design ./internal/snapshot`, `gofmt` clean.
- `go test ./internal/llm/agent ./internal/api ./internal/config` — unaffected, passing.

## Next

P1: artifact-scoped chromedp renderer/inspector (`data-pando-id` injection, node index
extraction, deck slide enumeration, canvas rasterization, print fixture).

Related: [[pando_design_studio_plan]] · [[project_snapshot_plan]] · [[fix_agentvcs_baseline_delta]]
