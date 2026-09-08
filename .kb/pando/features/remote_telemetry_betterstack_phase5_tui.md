---
created_at: 2026-09-10T20:40:44.585876824Z
updated_at: 2026-09-10T20:40:44.585876824Z
tags:
    - feature
    - telemetry
    - tui
    - settings
    - clipboard
---
# Feature: opt-in remote logs/telemetry to Better Stack — Phase 5 (TUI) implementation log

Plan: [[remote_telemetry_betterstack_plan]] (`pando/plans/remote_telemetry_betterstack_plan.md`).
Sibling entries: [[remote_telemetry_betterstack]] (Phase 2: redaction + shipper),
[[remote_telemetry_betterstack_phase0_1]] (Phase 0: build-time secret plumbing; Phase 1: config
model/global persistence). This entry covers **Phase 5: TUI** — the Bubble Tea settings page and
the shared clipboard helper. Implemented in `internal/tui/**` only, per the concurrent-agent
ownership split in the plan (Phase 3 owns `internal/logging`/`internal/app`/`cmd`/
`internal/config/config.go`/`internal/telemetry`; Phase 4 owns `internal/api/**`/`web-ui/**`).

## What changed

### 1. Shared clipboard helper (`internal/tui/util/clipboard.go`, new)
Extracted the unexported `copyToClipboard` (atotto/clipboard best-effort OS write + always-emitted
OSC 52 escape sequence, with `TMUX` wrapping) from `internal/tui/components/chat/list.go:47` into
an exported `util.CopyToClipboard`. Behavior is byte-for-byte unchanged (same clipboard.WriteAll +
osc52 + tmux logic, same "false only on empty/whitespace input" contract) — only the location and
exported name changed.

It is declared as a **package variable** (`var CopyToClipboard = copyToClipboard`), not a plain
function, specifically so settings-page tests (and any future caller) can swap in a fake and assert
what text was "copied" without touching a real terminal or the OS clipboard. This is the "make the
helper injectable via a package var" requirement from the phase brief; no equivalent test-seam
pattern existed elsewhere in the repo to copy, so this introduces it.

`internal/tui/components/chat/list.go` was updated to call `util.CopyToClipboard` in
`copyAndFeedback`; its own `copyToClipboard` function and the now-unused `os`, `atotto/clipboard`,
`go-osc52/v2` imports were removed (`strings` stays — used elsewhere in the file). No behavior
change for chat's copy actions (copy message, copy selection, etc.) — confirmed by the pre-existing
`internal/tui/components/chat` test suite passing unchanged.

New tests: `internal/tui/util/clipboard_test.go` — empty/whitespace input returns false without
touching the OS clipboard or stdout; the package var is swappable and receives the exact text
passed in.

### 2. Settings page: General section (`internal/tui/page/settings.go`)
`buildGeneralSection` gained 5 fields right after the existing "Debug" toggle (all in the `General`
section, `Core` group):

- **`telemetry.enabled`** (`FieldToggle`, label "Remote Telemetry") — `Disabled: !telemetry.Available()`.
  Hint explains what is sent (logs/crashes/version info tagged with the anonymous debug id,
  secrets redacted, off by default) when available, or "Not available in this build." when not.
- **`telemetry.debug_id`** (`FieldText`, `ReadOnly: true`, label "Debug ID") — value is
  `config.TelemetryDebugIDDisplay()` or `"—"` when empty (never enabled yet). Hint: "Share this ID
  when reporting a problem."
- **`telemetry.min_level`** (`FieldSelect`, options `debug/info/warn/error`, label "Telemetry Min
  Level") — always present (not hidden), `Disabled` when unavailable or not currently enabled,
  following the same "keep the row, grey it out" convention `buildOpenLitSection` already uses for
  its dependent fields rather than hiding rows conditionally.
- **`action:telemetry_copy_id`** (`FieldAction`, `ReadOnly: true`, label "Copy Debug ID") —
  `Disabled` when there is no debug id yet.
- **`action:telemetry_regenerate_id`** (`FieldAction`, `ReadOnly: true`, label "Regenerate Debug
  ID") — `Disabled` when the build has no token; usable while telemetry is disabled (matches the
  plan: "a user may want a fresh id ready before re-enabling").

The two `action:`-prefixed keys deliberately follow the established `FieldAction` convention in
this file (grepped every existing `FieldAction` entry — `action:add_provider`,
`action:open_skills_catalog`, `action:lsp_preset:%s`, `action:delete_mcp_server:%s`, etc. — **all**
of them use an `action:` key prefix and are special-cased in `settingsPage.Update`'s `SaveFieldMsg`
handling, bypassing `persistSetting` entirely) rather than being literal dotted keys, since they run
a side effect (clipboard write / id regeneration) instead of persisting a field value.

### 3. Persistence (`persistSetting`, `saveTelemetry`)
`persistSetting`'s prefix-switch gained `case strings.HasPrefix(field.Key, "telemetry."): return
saveTelemetry(field)`, next to the existing `openlit.` case. New `saveTelemetry`:
- `telemetry.enabled` → refuses with an error when `!telemetry.Available()` (build has no token),
  else parses the bool and calls `config.UpdateTelemetry(value)` (which generates the debug id on
  first enable, persists to the **global** config file only, per Phase 1).
- `telemetry.min_level` → `config.UpdateTelemetryMinLevel(field.Value)`.
- `telemetry.debug_id` → no-op (kept explicit rather than falling into "unsupported setting", even
  though the field's `ReadOnly: true` already makes `Section.Editable()` refuse to ever emit a
  `SaveFieldMsg` for it — mirrors the existing `server.basicAuth.users`/`server.info.disabled`
  read-only-informational precedent in `saveServer`).

New `settingsPage` methods (added next to `indexWorkingDirectory`, routed from `Update`'s
`SaveFieldMsg` case alongside the other `action:` prefix checks):
- `copyTelemetryDebugID()` — errors ("no debug ID yet: enable remote telemetry first") when
  `cfg.Telemetry.DebugID` is empty without touching the clipboard; otherwise copies the **grouped**
  display id (`config.TelemetryDebugIDDisplay()`, e.g. `1234-5678-9012-3456` — same format shown in
  the field) via `util.CopyToClipboard` and reports `"Debug ID copied to clipboard"` through the
  existing `util.ReportInfo`/`util.ReportError` status-bar mechanism (same one every other settings
  action in this file uses).
- `regenerateTelemetryID()` — errors when `!telemetry.Available()`; otherwise calls
  `config.RegenerateTelemetryID()` and, on success, rebuilds sections
  (`p.settings.SetSections(buildSections(p.app)); p.settings.SetSize(...)`) before reporting
  `"Debug ID regenerated"`.

### 4. Immediate refresh after enabling (no dependency on config.Bus)
The brief asked to verify the debug-id field updates immediately after enabling, and to add a local
rebuild regardless of whether Phase 3's `config.Bus` wiring also triggers one. Confirmed: the
**existing** `saveField` method (used by every ordinary `FieldToggle`/`FieldText`/`FieldSelect`
save, including `telemetry.enabled`/`telemetry.min_level`) already calls `persistSetting` then
unconditionally `p.settings.SetSections(buildSections(p.app))` before returning — and
`buildSections` re-reads `config.Get()` fresh each call. Since `config.UpdateTelemetry` updates the
package-level in-memory config synchronously before the file write returns, the rebuilt
`telemetry.debug_id` field already reflects the new id by the time `saveField` redraws, with no
dependency on the `config.Bus` subscription (`waitForConfigChange`) the page also holds. The
`action:telemetry_regenerate_id` handler adds its own explicit rebuild for the same reason (actions
bypass `saveField`). Both paths were exercised directly by test, not just inspected.

## Files touched
- `internal/tui/util/clipboard.go` (new), `internal/tui/util/clipboard_test.go` (new)
- `internal/tui/components/chat/list.go` (removed local `copyToClipboard` + 3 now-unused imports;
  `copyAndFeedback` now calls `util.CopyToClipboard`)
- `internal/tui/page/settings.go` (`buildGeneralSection`, `persistSetting`, new `saveTelemetry`,
  new `copyTelemetryDebugID`/`regenerateTelemetryID` methods, new `action:telemetry_copy_id` /
  `action:telemetry_regenerate_id` routing in `Update`, new `internal/telemetry` import)
- `internal/tui/page/settings_telemetry_test.go` (new)

## Verification
- `go build ./...` — clean (whole repo, including the other phases' concurrently-landed code).
- `go vet ./internal/tui/...` and `go vet ./...` — clean.
- `go test ./internal/tui/... -count=1` — all packages `ok`, including the new
  `internal/tui/util` and the 11 new tests in `internal/tui/page/settings_telemetry_test.go`
  (unavailable → all telemetry fields disabled with the right hint/placeholder; available+enabled →
  fields enabled, debug id grouped to 19 chars, min-level options correct; `persistSetting` enable
  refused without a token and leaves `Enabled=false`; enable with a token generates and persists a
  16-digit id and the immediately-rebuilt field shows it grouped; min-level save + rejection of an
  unknown level; copy action invokes the injected clipboard fake with the grouped id and leaves it
  untouched when there is no id yet; regenerate changes the id, rebuilds the section, and is refused
  without a token).
- `go test ./internal/llm/agent ./internal/api -count=1` (CLAUDE.md's verified command) —
  `internal/api` PASS; `internal/llm/agent` has the same 4 pre-existing failures already documented
  in the Phase 0/1/2 KB entries (`TestSetAndGetCavemanMode`, `TestCavemanActivatesTheSessionPolicyPath`,
  `TestCavemanSessionPolicyInstructions`, `TestApplyToolDiscoveryWithoutManagerIsUnchanged`, in
  `caveman_session_test.go`/`extension_tools_test.go` — never touched by this phase, root-caused
  there to the developer's real `~/.pando.toml` leaking into an un-isolated test's `HOME`).
- Test isolation: `t.Setenv("HOME", t.TempDir())` + `t.Setenv("XDG_CONFIG_HOME", "")` +
  `config.SetForTests(&config.Config{...})`, the same pattern `settings_caveman_test.go` already
  uses — verified (via a throwaway scratch test, deleted before finishing) that `buildSections`
  needs a non-nil `*pandoapp.App{}` (a bare empty struct is fine; a literal `nil` panics in an
  unrelated section) — `newTestSettingsPage()` in the new test file always constructs one.

## Deviations / open items
- Field keys for the two actions are `action:telemetry_copy_id` / `action:telemetry_regenerate_id`
  rather than the literal `telemetry.copy_id`/`telemetry.regenerate_id` shorthand named in the
  phase brief — deliberate, to match the codebase's exceptionless `action:`-prefix convention for
  every other `FieldAction` in this file (see point 2 above). `telemetry.enabled` and
  `telemetry.min_level` do use the literal dotted keys from the brief, since those two go through
  the ordinary `persistSetting` value-save path like every other non-action field.
- Did not add a CLI-visible `pando telemetry` gate check or touch `internal/api`/`web-ui` — out of
  this phase's ownership (Phase 4/6).
- No network request to Better Stack was made; no token was read, printed, or fetched; no
  jj/git mutating commands were run, per the task's hard constraints.

Related: [[remote_telemetry_betterstack_plan]], [[remote_telemetry_betterstack]],
[[remote_telemetry_betterstack_phase0_1]]