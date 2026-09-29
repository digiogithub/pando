---
created_at: 2026-09-29T15:19:02.984709Z
updated_at: 2026-09-29T15:19:02.984709Z
tags:
    - fix
    - security
    - dependencies
---
# Dependabot triage 2026-09-29

## Fixed
- Alerts #122, #123 (high): `fast-uri` host confusion / authority injection (vulnerable 4.1.3 in package-lock, 3.1.2 in bun.lock). Override in `web-ui/package.json` raised from `>=3.1.2` to `>=4.1.4`; both lockfiles resolve `fast-uri@4.2.1` (pulled by ajv via workbox/vite-plugin-pwa).
- `web-ui/package-lock.json` was stale versus package.json (still listed fontawesome/prop-types; missing lucide-react and @fontsource-variable from the WebUI redesign); `npm install --package-lock-only` resynced it.
- Verified: `npm` reports 0 vulnerabilities, `bun run build` OK.

## Not fixed (no patched version upstream)
- `github.com/docker/docker` #23, #29, #30, #31 (Moby plugin privilege off-by-one, docker cp races, archive PUT). No first_patched_version for the v1 module path; fix lives in `github.com/moby/moby/v2` — a module migration, not trivial. These are daemon-side bugs; Pando only uses the client.
- `github.com/disintegration/imaging` #1 (low): crafted TIFF crash; library unmaintained, no patch. Would need a replacement or dismissing.
