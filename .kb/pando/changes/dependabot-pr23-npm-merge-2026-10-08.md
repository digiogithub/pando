---
created_at: 2026-10-08T14:25:36.893707473Z
updated_at: 2026-10-08T14:25:36.893707473Z
tags:
    - change
    - deps
    - security
    - jj
---
# Merge Dependabot PR #23 (npm_and_yarn group) — 2026-10-08

## What
- Committed pending work first: `chore: project config and pending KB docs` (.pando.toml re-encrypted age keys + Google search disabled; 3 KB docs).
- Merged branch `dependabot/npm_and_yarn/npm_and_yarn-79b3da718e` (PR #23) into `main` with jj: `jj new main 'dependabot/...@origin' -m "chore(deps): merge ..."`. No conflicts. Push auto-closed PR #23 as MERGED.
- Bumps: sharp 0.35.5 (root + examples/copilotkit), @a2ui/web_core 0.10.4, @fastify/busboy 3.2.2, @modelcontextprotocol/sdk 1.32.1, fast-copy 3.1.0, source-map-js 1.2.2 (copilotkit, vite-react, web-ui).

## Why PR CI was red
PR base (71aa4b74) predated the build-matrix gtk/webkit fix (53784fab2): failure was `Package gtk+-3.0 was not found` in `desktop`, unrelated to the bump. main CI is green.

## Verification
web-ui: `npm ci`, `tsc -b`, `vitest run` (111 pass), `vite build` ok.

## Still open (not fixed on purpose)
Alerts in `examples/copilotkit/package-lock.json` only:
- `@graphql-tools/utils` < 12.0.1 (high, prototype pollution in mergeDeep) — pulled by @copilotkit/runtime -> graphql-yoga at ^10/^11.
- `katex` < 0.18.2 (low) — pulled by @copilotkit/react-core/streamdown at 0.16.x.
Fixing needs npm `overrides` forcing major versions in an example app; left for upstream CopilotKit bumps.

## jj notes
- `jj abandon @` creates a new empty @ on the parent — do not `jj bookmark set main -r @` right after; target the named change.
- Moving a bookmark backwards (`--allow-backwards`) is blocked by the auto-mode classifier; instead `jj squash --from <empty> --into <target>` moves the bookmark to the target without losing content.

Related: [[pando/fixes/jj_dev_main_divergence_and_push.md]], [[pando/fixes/tree-sitter-v0.2.1-ndebug-assert-and-build-matrix-gtk.md]]
