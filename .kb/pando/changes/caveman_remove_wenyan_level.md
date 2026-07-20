---
created_at: 2026-07-20T07:26:34.073200051Z
updated_at: 2026-07-20T07:26:34.073200051Z
tags:
    - change
    - caveman
    - slash-commands
    - cleanup
---
# Caveman — remove `wenyan` level

Part of [[caveman-persistent-output-brevity-mode]]. Removed the `wenyan`
(Classical Chinese prose) output-brevity level: no real utility in Pando.
Remaining levels: `lite`, `full` (default), `ultra`, plus off.

## What changed
- `internal/caveman/caveman.go`: dropped `ModeWenyan` const, its `ParseMode`
  case, `Description` case, `SettingOptions` entry, `level` map snippet, and all
  `[lite|full|ultra|wenyan]` usage/help strings to `[lite|full|ultra]`. `wenyan`
  now parses as unrecognized (`ModeOff, false`).
- `internal/config/config.go`: `CavemanDefaultMode` + `UpdateCaveman` no longer
  accept `wenyan`; error/comment text updated.
- `internal/config/init.go`, `internal/commands/registry.go`,
  `internal/api/handlers_settings.go`, `internal/mesnada/acp/{caveman_commands,
  slash_commands,types_interfaces}.go`, `internal/tui/page/{chat,settings}.go`:
  help/hint/error strings updated to drop wenyan.
- `web-ui/src/components/settings/GeneralSettings.tsx`: removed the
  `Wenyan (文言文)` option; `web-ui/src/types/index.ts` comment updated. Rebuilt
  embedded dist (`bun run build:embedded`).
- `README.md`: table row + all `[lite|full|ultra|wenyan]` references + "four
  levels" to "three levels".
- Tests: removed `TestWenyanKeepsCodeVerbatim`; wenyan turned into a negative
  case in `caveman_test.go` / `config/caveman_test.go`; sample values that used
  `wenyan` switched to `ultra` in ACP + API + TUI settings tests.

## Verification
- `go build ./...` clean.
- `go test` over caveman/config/api/acp/agent/tui-page/commands all pass.
- `grep -rniI wenyan` over source: only the two intentional negative-test cases
  remain; embedded dist clean.
