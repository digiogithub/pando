---
created_at: 2026-10-02T15:13:40.02558401Z
updated_at: 2026-10-02T15:13:40.02558401Z
tags:
    - fix
    - docs
    - pando-docs
---
# Fix: pando-docs links to the SDK section after it moved to /sdk/

Date: 2026-10-02. Repo: `../pando-docs`. Related: [[pando-docs-september-2026-roundup]].

## Problem
The SDK section was moved from `content/<lang>/docs/sdk/` to `content/<lang>/sdk/` (commit "feat: move sdk section"), but four links still pointed at `/docs/sdk/` and returned 404.

## Changes
- `content/en/docs/_index.md`, `content/es/docs/_index.md`: card `link="sdk"` -> `link="../sdk"`.
- `content/en/blog/introducing-pando-sdks.md`: `(/docs/sdk/)` -> `{{< relref "/sdk" >}}`.
- `content/es/blog/presentando-pando-sdks.md`: `(/es/docs/sdk/)` -> `{{< relref "/sdk" >}}` (resolves to `/es/sdk/`).

## Verification
Full-site check of every internal `href`/`src` and `#anchor` in the built output (URL-decoded, under the `/pando-docs/` base path): 17557 references, 0 broken. Also resolved correctly with a root base URL. External links were not checked. The accented tag URLs reported by an earlier check were false positives (that check did not URL-decode). Not committed.
