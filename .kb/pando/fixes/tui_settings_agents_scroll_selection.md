---
created_at: 2026-07-15T22:55:58.993836459Z
updated_at: 2026-07-15T22:55:58.993836459Z
tags:
    - fix
    - tui
    - settings
    - scroll
---
# Fix: TUI Settings — Agents/Models section scroll jump & unselectable fields

## Symptom
In the TUI settings page, the **Agents/Models** section (long list) misbehaved:
- Left-clicking a field jumped the viewport toward the top and selected the wrong field.
- After mouse-wheel scrolling, arrow-key navigation jumped the view unexpectedly.
Other sections did not show it (or showed it less), because they have fewer/uniform fields.

## Root cause
Field rows do **not** all have the same height. In `section.go` `View`, a field with a
non-empty `Hint` renders as a 4-line bordered box (top border + row + hint + bottom border),
while a field without a hint is 3 lines. The Agents/Models section (`buildAgentsSection` in
`internal/tui/page/settings.go`) gives every agent a **Max Tokens** field with an always-present
`agentTokensHint`, so heights are mixed within one section.

Two places assumed a fixed geometry:
1. Click hit-testing in `settings.go` used `fieldIdx := contentY / 3` — wrong once any field is 4 lines,
   so clicks resolved to the wrong field.
2. `autoScrollToActiveField` located the active field by `strings.Contains(line, activeField.Label)`
   (fragile substring search) instead of real line offsets, and scrolled the field to the top line
   rather than just into view.

## Fix
Made field geometry the single source of truth, derived from the actual render.

**`internal/tui/components/settings/section.go`**
- Extracted the per-field render loop from `View` into `renderFields(width, active) []string`;
  `View` now joins its output.
- Added geometry helpers built on `renderFields` + `lipgloss.Height`:
  - `FieldHeights(width) []int`
  - `FieldLineOffset(width, idx) int`
  - `FieldAtLine(width, line) int` (maps a content line to a field index, -1 if none)
  - `ActiveFieldIdx()` / `SetActiveFieldIdx(idx)` (clamped)

**`internal/tui/components/settings/settings.go`**
- Click hit-test now uses `activeSection.FieldAtLine(m.viewport.Width, contentY)` +
  `SetActiveFieldIdx`, instead of `contentY / 3`.
- `autoScrollToActiveField` rewritten to use `FieldHeights` cumulative offsets: computes the active
  field's `targetLine` and `fieldHeight`, and only scrolls when the field is not fully visible
  (scroll up if above, down if below), clamped to `maxOffset`. No more top-jump.
- Removed now-unused `strings` import.

## Files touched
- `internal/tui/components/settings/section.go` (renderFields + geometry helpers)
- `internal/tui/components/settings/settings.go` (click hit-test + autoScrollToActiveField)
- `internal/tui/components/settings/section_geometry_test.go` (new regression test)

## Verification
- `go build ./internal/tui/...` — OK.
- `go test ./internal/tui/components/settings/ ./internal/tui/page/` — pass.
- New `TestFieldGeometryVariableHeights` asserts heights `[3,4,3]`, offsets `[0,3,7]`, and
  `FieldAtLine` line-to-field mapping including out-of-range -> -1.

## Notes
- Pre-existing deprecation warnings on `viewport.ViewDown/ViewUp` in settings.go are unrelated.
