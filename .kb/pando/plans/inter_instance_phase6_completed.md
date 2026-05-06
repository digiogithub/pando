# Inter-Instance Communication — Phase 6 Completed

**Date:** 2026-05-06  
**Status:** COMPLETED ✓

## What was implemented

### Phase 6: TUI Instance Browser

**Goal achieved:** New TUI page accessible via `Ctrl+Alt+I` that shows running instances, their sessions, and a live view of remote events.

---

## Files Created

### `internal/tui/components/instances/instances.go`
- `Model` type with 3-pane state: `paneInstances`, `paneSessions`, `paneLiveView`
- Bubble Tea messages: `tickMsg`, `instancesUpdatedMsg`, `sessionsUpdatedMsg`, `liveEventMsg`, `liveSubCancelMsg`
- Commands: `fetchInstancesCmd`, `fetchSessionsCmd`, `subscribeLiveCmd`, `interruptSessionCmd`
- `formatEnvelope()` converts ZMQ envelopes to readable lines with prefixes `[LLM]`, `[Tool]`, `[Msg]`, `[Asst]`, `[Err]`
- Methods: `Width()`, `Height()`, `SetSize()`, `Init()`

### `internal/tui/components/instances/update.go`
- Key handling: Tab (switch pane), ↑/k / ↓/j (navigate), Enter (select/activate live), Esc (back), i (interrupt)
- Navigation: selecting instance → loads sessions via RPC `session.list`; Enter on session → starts ZMQ PUB subscription
- Subscription lifecycle management (cancel/restart on new selection)

### `internal/tui/components/instances/view.go`
- Lipgloss 3-pane render with highlighted border on active pane
- Left pane (30%): instances list with `▸` selector, truncated path + PRIMARY/2nd role
- Top-right pane (40% height): sessions with title and relative time (e.g. "2m ago")
- Bottom-right pane (60% height): colored event lines by type, `▌` streaming cursor
- Helpers: `truncatePath()`, `relativeTime()`

### `internal/tui/page/instances.go`
- `InstancesPage PageID = "instances"` constant
- `instancesPageModel` wrapping `instances.Model`, implements `tea.Model`, `layout.Sizeable`, `layout.Bindings`
- `NewInstancesPage()` constructor

---

## Files Modified

### `internal/tui/keys.go`
- Added `InstancesBrowser key.Binding` to `GlobalKeys` with `ctrl+alt+i`
- Added to `FullHelp()` alongside Projects/CronJobs

### `internal/tui/tui.go`
- Added `page.InstancesPage: page.NewInstancesPage()` to pages map
- Added `key.Matches(msg, a.keys.Global.InstancesBrowser)` → `a.moveToPage(page.InstancesPage)`
- Added ESC handler for `page.InstancesPage` → navigates back to `page.ChatPage`
- Added help section for `page.InstancesPage`

---

## Architecture after Phase 6

```
TUI (Ctrl+Alt+I)
  └── instances.Model
        ├── Pane 1: Instance list (polls instanceregistry every 2s)
        │     └── instanceregistry.Registry.List() → []Entry
        ├── Pane 2: Session list (fetches on instance select)
        │     └── ipc.Client.Call(rpcAddr, "session.list") → []SessionPayload
        └── Pane 3: Live view (subscribes on session select)
              └── ipc.Client.SubscribeTo(pubAddr) → chan Envelope
                    → formatEnvelope() → colored lines

Keyboard shortcuts:
  Tab   → switch active pane
  ↑/k, ↓/j → navigate list
  Enter → select / activate live view
  i     → interrupt (RPC session.interrupt)
  Esc   → back to chat page
```
