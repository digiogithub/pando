---
created_at: 2026-08-31T09:58:44.554583718Z
updated_at: 2026-08-31T09:58:44.554583718Z
tags:
    - fix
    - config
    - desktop
    - uiauto
    - defaults
---
# Fix: internalTools sizing knobs stamped as 0 shadow their defaults (2026-08-31)

## Symptom

A live `.pando.toml` had every Desktop Controller sizing knob at zero:

```toml
DesktopEnabled = true
DesktopBackend = 'auto'
DesktopAllowPhysicalInput = true
DesktopMaxNodes = 0
DesktopDefaultDepth = 0
DesktopActionTimeout = 0
DesktopSnapshotTTL = 0
DesktopScreenshotScale = 0.0
```

`FetchMaxSizeMB = 0` too.

## Does it break desktop automation? No — but only by accident

`uiauto.NewManager` clamps every non-positive option to the documented default
(`MaxNodes` 500, `DefaultDepth` 3, `ActionTimeout` 10s, `SnapshotTTL` 60s,
`ScreenshotScale` 1.0), so the desktop tools behave exactly as if the knobs were set
correctly. `DesktopBackend = ""` is mapped to `"auto"` by `OptionsFromConfig`.
`fetch.go` likewise guards with `FetchMaxSizeMB > 0`.

`BrowserMaxSessions` is the counter-example that shows why relying on that is fragile:
`internal/llm/tools/browser_session.go:98` reads it **straight from the config** with no
clamp, so a zero there would reject every session with
`max browser sessions (0) reached`.

## Root cause

The defaults *are* declared (`viper.SetDefault("internalTools.desktopMaxNodes", 500)` and
friends at `internal/config/config.go`), but **a viper default only applies while the key is
absent from the file**. The persist path (`config.go`, `toml.Marshal(persistedCfg)`)
rewrites the whole `Config` struct on every config edit, and the TOML tags carry no
`omitempty` — so any knob that was zero in memory at that moment is stamped into the file as
an explicit `0`, which shadows its default on every subsequent load, permanently.

That is how a config written before the Desktop Controller existed ends up with explicit
zeros for it.

## Fix

New `normalizeInternalToolsDefaults()` in `internal/config/config.go`, called from
`applyDefaultValues()` alongside the existing `normalizeRemembrancesDefaults()` /
`normalizeMesnadaDelegationDefaults()` pattern. It replaces non-positive knobs with named
constants (`defaultDesktopMaxNodes`, `defaultBrowserMaxSessions`, ...) and fills an empty
`DesktopBackend` with `"auto"`, while leaving any value the user actually chose untouched.

A zero is never a meaningful setting for these: it means "never written" or "stamped by a
rewrite". Normalizing at load time keeps `config.Get()` honest for **every** consumer — the
TUI settings page and the REST API render the raw config, so before this they displayed
`0` for knobs that were really operating at 500/3/10/60/1.0.

## Files touched

- `internal/config/config.go` — default constants, `normalizeInternalToolsDefaults()`,
  wired into `applyDefaultValues()`
- `internal/config/internal_tools_defaults_test.go` — new

## Verification

- `TestNormalizeInternalToolsDefaultsReplacesZeros` (all-zero config gets every documented
  default) and `TestNormalizeInternalToolsDefaultsKeepsExplicitValues` (explicit values,
  including a deliberately small `DesktopScreenshotScale = 0.5`, survive untouched) both pass.
- `go test ./...` across the repository: no failures. `go vet ./...` exit 0.

Note: the underlying `omitempty`-free TOML persistence remains as-is; normalization at load
makes the written values correct instead, so the next config write stamps the real defaults
rather than zeros.

Related: [[feature_desktop_controller_uiauto]],
[[desktop-uiauto-inert-null-backend-and-atspi-conn-lifetime]].
