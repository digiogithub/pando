---
created_at: 2026-09-08T20:26:48.557535889Z
updated_at: 2026-09-08T20:26:48.557535889Z
tags:
    - fix
    - webui
    - api
    - config
    - desktop
---
# Fix: WebUI/Desktop "Tools" settings section crashed with `null is not an object (evaluating 'e.desktopAllowedApps.join')`

Date: 2026-09-08

## Symptom

From v0.700 onwards, opening the **Tools** section of the settings view in the
WebUI / desktop (Wails) app blanked the whole screen with the error boundary:

```
Something went wrong
null is not an object (evaluating 'e.desktopAllowedApps.join')
```

Reported on macOS, but the bug is platform independent — it depends only on the
shape of the user's config file.

## Root cause

`ToolsConfigResponse` in `internal/api/handlers_config.go` declares the desktop
app lists **without** `omitempty`:

```go
DesktopAllowedApps []string `json:"desktopAllowedApps"`
DesktopDeniedApps  []string `json:"desktopDeniedApps"`
```

A config written before those options existed (any pre-v0.700 `.pando.toml`, and
also a fresh config that simply never set them) leaves the slices `nil`, and Go
marshals a nil slice as JSON `null`, not `[]`. So `GET /api/v1/config/tools`
returned `"desktopAllowedApps": null`.

The WebUI store merged the response over its defaults with a plain spread:

```ts
const merged = { ...TOOLS_DEFAULTS, ...data }   // data.desktopAllowedApps === null
```

A present-but-null key *wins* over the default `[]`, so `config.desktopAllowedApps`
became `null` and `InternalToolsSettings.tsx` threw on `.join(', ')`, taking the
entire Tools section down via the error boundary. No config migration was
involved — nothing had to migrate; the API contract was simply violated for nil
slices.

Other config endpoints were checked and are safe: `BashConfig.BannedCommands` /
`AllowedCommands` and `outputFilterPaths` all carry `omitempty`, so the key is
absent and the client default applies.

## Changes

Backend (root cause):
- `internal/api/handlers_config.go`: new helper `nonNilStrings([]string) []string`
  returning `[]string{}` for nil; applied to `DesktopAllowedApps` and
  `DesktopDeniedApps` in `handleGetConfigTools`. `handlePutConfigTools` delegates
  its response to `handleGetConfigTools`, so the PUT path is covered too.

Frontend (defense in depth — protects older backends and other clients):
- `web-ui/packages/pando-client/src/stores/settingsStore.ts`: new
  `normalizeToolsConfig()` which merges over `TOOLS_DEFAULTS` and coerces both
  desktop app lists with `Array.isArray(...) ? ... : []`. Used by both
  `fetchTools` and `saveTools`.
- `web-ui/src/components/settings/InternalToolsSettings.tsx`: the two inputs now
  read `(config.desktopAllowedApps ?? []).join(', ')` /
  `(config.desktopDeniedApps ?? []).join(', ')`.

Tests:
- `internal/api/handlers_config_nilslice_test.go`:
  `TestToolsConfigResponseNeverEmitsNullAppLists` marshals the response with nil
  lists and asserts both keys decode to JSON arrays;
  `TestNonNilStringsKeepsValues` asserts non-nil input is passed through.

## Verification

- `go test ./internal/api -run 'ToolsConfig|NonNilStrings'` — ok
- `go build ./...` — clean
- `go vet ./internal/api` — clean
- `npx tsc --noEmit` in `web-ui/` — clean

## Rule of thumb

Any `[]string` / slice field in an API DTO that a client indexes, joins or maps
over must either carry `omitempty` (so the client default applies) or be
normalized to an empty slice before encoding. Never let it reach the wire as
`null`.

Related: [[fix_fresh_install_provider_agent_model_defaults]],
[[feature_desktop_controller_uiauto]], [[project_webui_settings_plan]]
