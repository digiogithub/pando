---
created_at: 2026-07-16T23:01:18.809462272Z
updated_at: 2026-07-16T23:06:23.387017859Z
tags:
    - fix
    - tui
    - settings
---
# Fix: TUI settings view jumps back to the first field after saving a setting

## Problem
In the TUI settings page (reported for the "Agents/Models" section, but affecting every
section with enough content to scroll), changing a value — e.g. picking an agent model
via the model dialog — snapped the view back to the first field, losing scroll position
and focus.

## Root cause (the real one)
`persistSetting` writes the config file. `internal/config/watcher.go` watches that file
and, ~200ms after the write, calls `Reload()` and `Bus.Publish(ConfigChangeEvent{Source:
"file"})`. The settings page subscribes to that bus, so *its own save* comes back to it
as `configExternalChangeMsg`:

```go
case configExternalChangeMsg:
    p.settings.SetSections(buildSections(p.app))
    p.settings.SetSize(p.width, p.height)
```

That branch rebuilds sections but never calls `SetActiveField`. `buildSections()` returns
freshly constructed `Section` values whose `activeFieldIdx` is always 0, and the old
`SetSections` copied them verbatim, so focus reset to field 0 and the trailing
`autoScrollToActiveField()` drove `viewport.YOffset` to 0. The explicit
`SetActiveField` that `saveField` does immediately after saving was simply overwritten
by the asynchronous watcher rebuild that landed ~200ms later.

## Fix
`internal/tui/components/settings/settings.go`:

1. `SetSections` now preserves state across a rebuild: it records the active section's
   `Title` and each section's active field `Key` before replacing `m.sections`, then
   remaps them onto the new sections by title/key. `syncViewport` already clamps and
   preserves `YOffset`, so scroll survives too and the following
   `autoScrollToActiveField` becomes a no-op when the field is still visible. This fixes
   every rebuild call site at once (~15 of them), not just the watcher branch.
2. Added `syncActiveFieldToScroll()`, called after manual scrolling (`pgdown`/`ctrl+f`,
   `pgup`/`ctrl+b`, and mouse wheel via `tea.MouseMsg`): when the active field scrolls
   fully out of view, focus re-anchors to the field at the top visible line
   (`Section.FieldAtLine`). Without it, a later edit's `autoScrollToActiveField` would
   yank the viewport back to a stale off-screen field.

## Verification
- New regression test `internal/tui/components/settings/section_rebuild_test.go`
  (`TestSetSectionsPreservesFocusAndScroll`) reproduces the watcher rebuild: scroll to
  field 30, rebuild sections without `SetActiveField`, assert focus and `YOffset` hold.
  Confirmed it FAILS against the pre-fix code with exactly the reported symptom
  (`active field after rebuild = 0, want 30` / `YOffset = 0, want 77`) and passes after.
- `go build ./internal/tui/...` clean (pre-existing `ViewDown`/`ViewUp` deprecation
  warnings are unrelated).
- `go test ./internal/tui/components/settings/ ./internal/tui/page/` — pass.
- Not exercised in a live interactive TUI session; verification is the regression test
  plus tracing the `saveField` → config write → watcher → `configExternalChangeMsg` path.

## Note
A first attempt fixed only the manual-scroll desync (item 2) and did not resolve the
reported bug, because the actual trigger was the config-watcher rebuild.
