---
created_at: 2026-06-22T10:42:05.123130574Z
updated_at: 2026-06-22T10:42:05.123130574Z
tags:
    - change
    - config
    - tui
    - defaults
---
# Default config enables Pando preferred settings

## What changed
Updated the generated default configuration template in `internal/config/init.go` so new `.pando.toml` files start with Pando's preferred out-of-the-box settings enabled. The generated config now:
- sets the default TUI theme to `pando-nobg`
- enables SkillsCatalog with `DefaultScope = 'global'`
- enables TUI convenience defaults like `ShowHiddenFiles` and `NerdFonts`
- enables `Permissions.AutoApproveTools`
- enables Mesnada delegation defaults
- enables Remembrances context enrichment, memory, and auto-capture defaults
- writes explicit enabled sections for `LLMCache`, `ToolDiscovery`, `InternalTools`, and `Evaluator`

Also added `TestDefaultConfigTemplateEnablesPandoPreferredDefaults` in `internal/config/config_test.go` to lock these generated defaults in place.

## Files and symbols touched
- `internal/config/init.go`
  - `DefaultConfigTemplate`
- `internal/config/config_test.go`
  - `TestDefaultConfigTemplateEnablesPandoPreferredDefaults`

## Why
The goal is to make first-run setup easier when Pando is not yet configured, so the generated config immediately activates the main Pando enhancement features instead of leaving many useful options disabled or implicit.

## Verification
- Ran `go test ./internal/config`
- Added template-content assertions covering the new generated defaults
