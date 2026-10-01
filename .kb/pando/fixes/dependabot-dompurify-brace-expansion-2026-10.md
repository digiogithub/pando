---
created_at: 2026-10-01T08:45:31.97440077Z
updated_at: 2026-10-01T08:45:31.97440077Z
tags:
    - fix
    - security
    - dependencies
---
# Dependabot: dompurify and brace-expansion patch bumps (2026-10-01)

Three open Dependabot alerts on `main`, all transitive npm dependencies with a patch release available.

| Alert | Package | Manifest | Advisory | Fix |
|---|---|---|---|---|
| 128 (low) | dompurify (via monaco-editor) | web-ui/package-lock.json | GHSA-p98j-92pf-mc4p | 3.4.16 |
| 127 (medium) | brace-expansion (via minimatch: eslint, workbox-build) | web-ui/package-lock.json | GHSA-q2hr-2g5m-vwhr | 5.0.12 |
| 124 (low) | dompurify (via @copilotkit/react-core > streamdown > mermaid) | examples/copilotkit/package-lock.json | GHSA-p98j-92pf-mc4p | 3.4.16 |

## Changes
- `web-ui/package.json` `overrides`: `brace-expansion` `>=5.0.6` -> `>=5.0.12`, `dompurify` `>=3.4.0` -> `>=3.4.16`; `npm install` regenerated `web-ui/package-lock.json`.
- `examples/copilotkit`: `npm update dompurify` (lockfile only).

## Side finding
`web-ui/package-lock.json` was out of sync with `package.json`: the dev dependencies added with the model auto mode tests (vitest, @playwright/test, @testing-library/*, jsdom and their trees) were missing from the lockfile. The install added them (additions only, no package entries removed), which is why the lockfile diff is ~1700 lines. `npm ci` would have failed before this.

## Verification
- `npm ls`: dompurify 3.4.16 and brace-expansion 5.0.12 in web-ui; dompurify 3.4.16 in the example.
- web-ui `npx vitest run`: 14 passed; `npm run build`: OK.
- The copilotkit example was not built or run.
