---
created_at: 2026-06-30T12:40:36.447650074Z
updated_at: 2026-06-30T12:40:36.447650074Z
tags:
    - fix
    - config
    - toml
---
# Fix: duplicate [InternalTools] table in DefaultConfigTemplate

## Symptom
TUI (and any reload after a fresh config generation) failed with:
`failed to parse toml config file: table internalTools already exists`
The user deleted `.pando.toml` and it "kept regenerating wrong" — because the
regenerated file came from a broken template.

## Root cause
`internal/config/init.go` `DefaultConfigTemplate` (the annotated `.pando.toml`
written by `GenerateLocalConfigFile` / on init when no local config exists)
declared the `[InternalTools]` table **twice**: once at ~line 374 (short block,
defaults matching the code: FetchMaxSizeMB=10, searches enabled, browserHeadless=true)
and again at ~line 456 (a fuller block listing all keys but with everything
disabled / FetchMaxSizeMB=0). go-toml/v2's seen-tracker (`internal/tracker/seen.go`,
case-sensitive `bytes.Equal`) rejects a second explicit table with
`toml: table InternalTools already exists`, so every parse of a freshly generated
config aborted.

The existing on-disk user files (`.pando.toml`, `~/.pando.toml`) each had only one
`[InternalTools]`, so plain round-trips passed — the bug only manifested when the
template itself was (re)generated, which matched the user's report.

## Fix
Merged into a single `[InternalTools]` block in `internal/config/init.go`:
- Kept the first block's position (right after `[ToolDiscovery]`, under the
  "Internal Tools" header) and its code-aligned default values (matching the
  `viper.SetDefault("internalTools.*", ...)` in config.go: FetchMaxSizeMB=10,
  Google/Brave/Perplexity/Context7 enabled, browserHeadless=true, browserType=chrome,
  browserTimeout=30, browserMaxSessions=3).
- Expanded it to list all `InternalToolsConfig` fields so users see every option
  (added GoogleAPIKey, GoogleSearchEngineID, BraveAPIKey, PerplexityAPIKey,
  ExaSearchEnabled/ExaAPIKey, SourcegraphEnabled/SourcegraphToken, BrowserExecutable,
  BrowserUserDataDir).
- Deleted the duplicate second `[InternalTools]` block (and its repeated header
  comment) that sat after `[MCPGateway]`.

## Files touched
- `internal/config/init.go` — `DefaultConfigTemplate` const.

## Verification
- Added a temporary test parsing `DefaultConfigTemplate` with `toml.Unmarshal`:
  before → `toml: table InternalTools already exists`; after → PASS, and a
  duplicate-top-level-table-header scan found none.
- `go build ./internal/config/` succeeds.

## Note for the user
The fix is in the embedded template, so a rebuilt binary is required; existing
single-table config files are unaffected. A previously generated, corrupted
`.pando.toml` (with two `[InternalTools]`) should be deleted so the corrected
template regenerates.
