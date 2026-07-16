---
created_at: 2026-07-16T20:57:52.090646882Z
updated_at: 2026-07-16T21:16:48.523705346Z
tags:
    - pando
    - feature
    - tui
    - sidebar
    - mesnada
    - orchestrator
---
## Feature: TUI chat info sidebar — subagent count + scrollable content

Builds on [[pando/features/rich_token_usage_panel.md]] (same sidebar file).

### What changed

1. Added a "Subagents" line to the Chat page's right info sidebar
   (`internal/tui/components/chat/sidebar.go`), showing the live count of
   Mesnada delegated tasks as `running/unfinished`, e.g. `Subagents: 2/5`
   meaning 2 tasks currently `TaskStatusRunning` out of 5 not yet
   completed/failed/cancelled (`unfinished = Pending + Running + Paused`).
   Computed live at render time from `orchestrator.Orchestrator.GetStats()`
   (`internal/mesnada/orchestrator/orchestrator.go:1252`) — no caching, no new
   tick loop, consistent with how `usageSection` already reads `m.session`
   live. Hidden entirely (`subagentsSection()` returns `""`) both when
   Mesnada is disabled (nil orchestrator) AND when it is enabled but
   `unfinished == 0` — the row must only appear while there is at least one
   subagent pending or running, never as an idle "0/0" placeholder.

2. Made the sidebar scrollable via mouse wheel using `bubbles/viewport`: the
   joined sections are now set as viewport content and rendered through
   `m.viewport.View()` instead of being rendered directly, so content taller
   than the panel (Usage + Subagents + LSPs + Plan + Modified Files) no
   longer gets clipped/hidden without any way to reach it.

### Files/symbols touched

- `internal/tui/components/chat/sidebar.go`:
  - `ChatInfoSidebar` interface gained `SetOrchestrator(o *orchestrator.Orchestrator)`.
  - `sidebarCmp` struct gained `orchestrator *orchestrator.Orchestrator` and
    `viewport viewport.Model` fields.
  - New `subagentsSection()` method: returns `""` when `m.orchestrator == nil`
    OR when `unfinished == 0` (Pending+Running+Paused), so the row is only
    ever visible while there's real in-flight work.
  - `View()` now builds the viewport content and renders `m.viewport.View()`;
    also inserts the subagents row between Usage and the LSP list (only when
    non-empty).
  - `Update()` gained a `tea.MouseMsg` case forwarding wheel events into
    `m.viewport.Update`.
  - `NewSidebarCmp` / `NewChatInfoSidebar` now construct the viewport with
    `MouseWheelEnabled = true`.
  - New `SetOrchestrator` method implementing the interface addition.
- `internal/tui/page/chat.go`:
  - After constructing `infoSidebar` (`NewChatPage`, ~line 1862), calls
    `infoSidebar.SetOrchestrator(app.MesnadaOrchestrator)` (may be nil).
  - `routeInfoSidebar` gained a `tea.MouseMsg` case: forwards only wheel
    events, only when `chatSidebarVisible()` is true, and only when the
    pointer X is at or past the sidebar's left edge
    (`int(float64(p.width) * p.chatSidebarRatio())`) — mirrors the existing
    x-range gating pattern already used for the file tree panel in
    `routeMessage`. This was necessary because the info sidebar is NOT part
    of `p.chatLayout` (the inner chat split) and previously only received a
    hardcoded whitelist of message types via `routeInfoSidebar` — mouse
    events were never forwarded to it at all before this change.

### Why

User request: show subagent running/pending count abbreviated as
`Subagents: 2/5`, but ONLY while there are unfinished subagents — an idle
system (no pending/running/paused tasks) must not show the row at all, and
make the sidebar scrollable since it can now overflow (token usage panel +
subagents + plan + modified files).

### Verification

- `go build ./...` clean.
- `go vet ./internal/tui/... ./internal/mesnada/orchestrator/...` clean.
- `go test ./internal/tui/... ./internal/mesnada/orchestrator/...` — all
  packages `ok` (or no test files).
- No manual live-session verification with a real running subagent was
  performed (build/test-only, per session conventions); the stats logic
  reuses the already-tested `Orchestrator.GetStats()` code path used by the
  existing Orchestrator TUI page and the `get_stats` Mesnada tool.
