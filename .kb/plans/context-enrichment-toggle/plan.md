# Plan: Context Enrichment Toggle Command

## Overview

Add a user-facing **runtime toggle** to enable/disable the context enrichment feature
(the automatic pre-prompt injection of KB, code index, and events data from the
`ContextEnricher`). The toggle must be accessible from three surfaces:

| Surface | Mechanism |
|---------|-----------|
| **TUI** | Command palette (`⌘K` / `Ctrl+K`) → "Enable/Disable Context Enrichment" |
| **WebUI** | Quick Menu (action overlay) → "Toggle Context Enrichment" |
| **ACP** | Slash command `/enrichment` (toggle) / `/enrichment on` / `/enrichment off` |

The feature is considered **done** when:
1. A user can toggle context enrichment at runtime without restarting Pando.
2. The current state is visible as a status indicator in TUI and WebUI.
3. The ACP client (Zed, VS Code, JetBrains) sees the command in the available-commands list.
4. The persistent config (`ContextEnrichmentEnabled`) is honoured as the startup default.

---

## Architecture Analysis

### Current State

```
config.RemembrancesConfig.ContextEnrichmentEnabled  ← startup default (persisted)
       ↓ (read at app init in app/app.go)
agent.globalContextEnricher  ← package-level var, set via SetContextEnricher()
       ↓ (called inside processGeneration before user message is created)
rag.ContextEnricher.EnrichContext(ctx, query) → "<context>…</context>"
       ↓ appended to user content
LLM provider request
```

**Key facts:**
- `internal/llm/agent/agent.go` exposes `SetContextEnricher(e ContextEnricher)` and `globalContextEnricher`.
- Passing `nil` to `SetContextEnricher` silently disables enrichment (already handled).
- The `ContextEnricher` itself (`internal/rag/enricher.go`) is stateless; all state is in the `globalContextEnricher` variable.
- No runtime toggle or status query API exists yet.

### What Must Be Added

```
┌─────────────────────────────────────────────────────┐
│  agent package (internal/llm/agent)                  │
│                                                       │
│  var enrichmentEnabled atomic.Bool   ← NEW            │
│  var cachedEnricher *rag.ContextEnricher ← NEW        │
│                                                       │
│  func SetContextEnricher(e ContextEnricher) {         │
│      globalContextEnricher = e                        │
│      if e != nil { cachedEnricher = e }               │  (extended)
│  }                                                    │
│  func ToggleContextEnrichment() bool { … }  ← NEW    │
│  func SetContextEnrichmentEnabled(v bool)   ← NEW    │
│  func IsContextEnrichmentEnabled() bool     ← NEW    │
└─────────────────────────────────────────────────────┘
```

---

## Implementation Phases

### Phase 1 — Core Runtime Toggle (Backend Go)

**Files:** `internal/llm/agent/agent.go`

#### 1.1 Add atomic state + public API

```go
// contextEnrichmentEnabled tracks the *runtime* toggle.
// It is initialised from config when SetContextEnricher is first called.
var contextEnrichmentEnabled atomic.Bool

// cachedEnricher stores the enricher instance so it can be re-attached
// after being detached by a disable call.
var cachedEnricher ContextEnricher

// SetContextEnricher installs a ContextEnricher and marks enrichment as enabled.
// Pass nil to disable. (Existing callers unchanged.)
func SetContextEnricher(e ContextEnricher) {
    globalContextEnricher = e
    if e != nil {
        cachedEnricher = e
        contextEnrichmentEnabled.Store(true)
    }
}

// ToggleContextEnrichment flips the enrichment state and returns the new value.
func ToggleContextEnrichment() bool {
    if contextEnrichmentEnabled.Load() {
        contextEnrichmentEnabled.Store(false)
        globalContextEnricher = nil
        return false
    }
    contextEnrichmentEnabled.Store(true)
    globalContextEnricher = cachedEnricher
    return true
}

// SetContextEnrichmentEnabled sets enrichment state explicitly.
func SetContextEnrichmentEnabled(enabled bool) {
    if enabled {
        contextEnrichmentEnabled.Store(true)
        globalContextEnricher = cachedEnricher
    } else {
        contextEnrichmentEnabled.Store(false)
        globalContextEnricher = nil
    }
}

// IsContextEnrichmentEnabled reports whether context enrichment is active.
func IsContextEnrichmentEnabled() bool {
    return contextEnrichmentEnabled.Load()
}
```

**Notes:**
- The existing `processGeneration` check `if globalContextEnricher != nil` remains correct — no changes needed there.
- Thread-safety is guaranteed because `globalContextEnricher` reads happen inside the agent's sequential request processing and the toggle only happens between requests. Using `atomic.Bool` for `contextEnrichmentEnabled` prevents data races on concurrent status reads.
- `cachedEnricher` must be set before any toggle so that re-enabling after a disable works.

---

### Phase 2 — TUI Command

**Files:**
- `internal/tui/tui.go` — where the command list is built
- (optionally) `internal/tui/components/dialog/commands.go` — if a new category is needed

#### 2.1 Add command to command palette

The TUI already has a `CommandCategoryGeneral` category. A new command is registered along the existing ones (e.g. compact, new session, etc.).

```go
// In tui.go, inside the function that builds []dialog.Command:

{
    ID:          "toggle-context-enrichment",
    Title:       contextEnrichmentCommandTitle(), // "Disable Context Enrichment" or "Enable Context Enrichment"
    Description: "Toggle automatic KB/code/events context injection into prompts",
    Category:    dialog.CommandCategoryGeneral,
    Handler: func(cmd dialog.Command) tea.Cmd {
        enabled := agent.ToggleContextEnrichment()
        label := "enabled"
        if !enabled {
            label = "disabled"
        }
        // Refresh command title so the palette reflects new state immediately.
        return tea.Batch(
            dialog.RefreshCommandsCmd(),
            app.NotifyCmd(fmt.Sprintf("Context enrichment %s", label)),
        )
    },
},
```

```go
func contextEnrichmentCommandTitle() string {
    if agent.IsContextEnrichmentEnabled() {
        return "Disable Context Enrichment"
    }
    return "Enable Context Enrichment"
}
```

**Notes:**
- The command title dynamically reflects the current state.
- `dialog.RefreshCommandsCmd()` (or equivalent rebuild of commands) must be called so the palette re-reads the dynamic title. Check existing pattern for how the compact command refreshes UI state.
- The command only appears when `HasRemembrances` is true (guard: `if app.HasRemembrances()`).

#### 2.2 Status indicator in chat header (optional / nice-to-have)

A small icon `⊕` / `⊗` next to the model indicator when enrichment is active/inactive, similar to how the agent mode is shown. This is a separate, lower-priority task.

---

### Phase 3 — WebUI Action

**Files:**
- `web-ui/src/components/overlays/QuickMenu.tsx`
- `web-ui/src/services/commandLauncher.ts` (or a new API hook)
- Backend: `internal/api/` (REST endpoint for enrichment toggle/status)

#### 3.1 Backend REST endpoint

Add two routes to the existing HTTP API:

```
GET  /api/enrichment        → { "enabled": true|false }
POST /api/enrichment/toggle → { "enabled": true|false }  (toggles and returns new state)
POST /api/enrichment        → body: { "enabled": bool }  (set explicitly)
```

Implementation in a new file `internal/api/enrichment.go`:

```go
func handleGetEnrichment(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, map[string]bool{"enabled": agent.IsContextEnrichmentEnabled()})
}

func handleToggleEnrichment(w http.ResponseWriter, r *http.Request) {
    enabled := agent.ToggleContextEnrichment()
    writeJSON(w, map[string]bool{"enabled": enabled})
}

func handleSetEnrichment(w http.ResponseWriter, r *http.Request) {
    var body struct{ Enabled bool `json:"enabled"` }
    json.NewDecoder(r.Body).Decode(&body)
    agent.SetContextEnrichmentEnabled(body.Enabled)
    writeJSON(w, map[string]bool{"enabled": body.Enabled})
}
```

Register in the existing mux (e.g. `internal/api/server.go`):

```go
mux.HandleFunc("GET /api/enrichment", handleGetEnrichment)
mux.HandleFunc("POST /api/enrichment/toggle", handleToggleEnrichment)
mux.HandleFunc("POST /api/enrichment", handleSetEnrichment)
```

#### 3.2 QuickMenu action

Add to the `VIEWS` or actions list in `QuickMenu.tsx`:

```tsx
// Dynamic action — fetched from API or managed via React state
{
  id: 'toggle-enrichment',
  label: enrichmentEnabled ? 'Disable Context Enrichment' : 'Enable Context Enrichment',
  icon: enrichmentEnabled ? faBrainCircuit : faBrainCircuit, // or faMemory / faDatabase
  group: 'command',
  description: 'Toggle automatic KB/code/events context injection',
  action: async () => {
    const res = await fetch('/api/enrichment/toggle', { method: 'POST' })
    const { enabled } = await res.json()
    setEnrichmentEnabled(enabled)
    notify(`Context enrichment ${enabled ? 'enabled' : 'disabled'}`, 'success')
  },
}
```

State management:
- On mount, `GET /api/enrichment` to initialise `enrichmentEnabled`.
- Or use the existing WebSocket event bus to broadcast state changes from the backend.

**Notes:**
- The item should only appear when remembrances is configured (check from settings store or dedicated endpoint).
- Icon choice: `faDatabase`, `faMemory`, or `faBrain` from FontAwesome solid.

---

### Phase 4 — ACP Slash Command

**Files:**
- `internal/mesnada/acp/session_state.go` — `availableCommands()` list
- `internal/mesnada/acp/goal_commands.go` — `handleSlashCommand()` switch
- `internal/mesnada/acp/session_state.go` — constants

#### 4.1 Register the command

In `session_state.go`, add the constant and register in `availableCommands()`:

```go
const enrichmentCommandName = "/enrichment"
```

```go
func availableCommands() []acpsdk.AvailableCommand {
    return []acpsdk.AvailableCommand{
        // … existing commands …
        {
            Name:        enrichmentCommandName,
            Description: "Toggle context enrichment: /enrichment | /enrichment on | /enrichment off",
        },
    }
}
```

#### 4.2 Parse the command

In `session_state.go` (where `slashCommandKind` constants are defined), add:

```go
slashCommandEnrichment slashCommandKind = "enrichment"
```

In the `parseSlashCommand` function (or wherever it parses commands), add a case:

```go
case strings.HasPrefix(name, "/enrichment"):
    obj := strings.TrimSpace(text[len("/enrichment"):])
    return slashCommand{Kind: slashCommandEnrichment, Objective: obj}, true
```

#### 4.3 Handle the command

In `handleSlashCommand()` in `goal_commands.go`, add a case:

```go
case slashCommandEnrichment:
    var newState bool
    switch strings.ToLower(strings.TrimSpace(command.Objective)) {
    case "on", "enable", "1", "true":
        agent.SetContextEnrichmentEnabled(true)
        newState = true
    case "off", "disable", "0", "false":
        agent.SetContextEnrichmentEnabled(false)
        newState = false
    default:
        // No arg → toggle
        newState = agent.ToggleContextEnrichment()
    }
    status := "enabled"
    if !newState {
        status = "disabled"
    }
    if err := a.sendAgentText(acpSession,
        fmt.Sprintf("Context enrichment %s.\nUse `/enrichment on` or `/enrichment off` to control it.", status),
    ); err != nil {
        return "", err
    }
    return acpsdk.StopReasonEndTurn, nil
```

**Notes:**
- The command only makes sense when remembrances is configured. If `agent.cachedEnricher == nil`, return a helpful message instead of toggling.
- After toggling, the ACP client receives a text response confirming the new state.

---

## Cross-Cutting Concerns

### Thread Safety

`globalContextEnricher` is read in `processGeneration` (agent goroutine) and written by `SetContextEnricher`/`ToggleContextEnrichment` (UI goroutine or ACP request goroutine). Options:

**Option A (minimal change):** Use `atomic.Pointer[ContextEnricher]` for `globalContextEnricher` instead of a plain var. This avoids adding `contextEnrichmentEnabled` at all.

```go
var globalContextEnricher atomic.Pointer[ContextEnricher]

func SetContextEnricher(e ContextEnricher) {
    if e == nil {
        globalContextEnricher.Store(nil)
    } else {
        globalContextEnricher.Store(&e)
    }
}

// In processGeneration:
if e := globalContextEnricher.Load(); e != nil {
    enriched := (*e).EnrichContext(ctx, content)
    …
}
```

**Option B (as described above):** Keep `globalContextEnricher` as `ContextEnricher` interface var but protect reads/writes with `sync/atomic.Bool` + simple nil set under the assumption single-writer (one UI action at a time). This is simpler but technically has a narrow race.

**Recommendation: Option A** — use `atomic.Pointer` for `globalContextEnricher`. It avoids the race entirely with minimal code change.

### Persistence

The runtime toggle is **session-scoped** (in-process only). Restarting Pando resets to `config.ContextEnrichmentEnabled`.

If persistence across restarts is desired, `ToggleContextEnrichment()` can optionally persist the new value to config via `config.UpdateField("context_enrichment_enabled", enabled)`. This is a **phase 2 / optional enhancement** and not part of the initial implementation.

### Guard: Enrichment Not Configured

All three surfaces must handle the case where `ContextEnricher` was never set (remembrances not configured). In this case:
- TUI: command is not shown.
- WebUI: menu item is greyed out or hidden.
- ACP: command returns `"Context enrichment is not available (remembrances not configured)."`.

Check: `agent.IsContextEnrichmentConfigured()` (new helper returning `cachedEnricher != nil`).

---

## File Change Summary

| File | Change |
|------|--------|
| `internal/llm/agent/agent.go` | Add `atomic.Pointer` for `globalContextEnricher`, `ToggleContextEnrichment()`, `SetContextEnrichmentEnabled()`, `IsContextEnrichmentEnabled()`, `IsContextEnrichmentConfigured()` |
| `internal/tui/tui.go` | Register toggle command in command palette, guard with enrichment configured check |
| `internal/api/enrichment.go` | New file: REST handlers for GET/POST enrichment toggle |
| `internal/api/server.go` | Register new routes |
| `web-ui/src/components/overlays/QuickMenu.tsx` | Add toggle menu item with dynamic label, fetch state on mount |
| `internal/mesnada/acp/session_state.go` | Add `enrichmentCommandName` constant + entry in `availableCommands()`, add `slashCommandEnrichment` kind |
| `internal/mesnada/acp/goal_commands.go` | Add `slashCommandEnrichment` case in `handleSlashCommand()` |

---

## Testing

### Unit Tests

1. **`internal/llm/agent/agent_enrichment_test.go`** (new):
   - `TestToggleContextEnrichment_TogglesState`
   - `TestToggleContextEnrichment_NilCachedEnricher_DoesNotPanic`
   - `TestSetContextEnrichmentEnabled_ExplicitOn`
   - `TestSetContextEnrichmentEnabled_ExplicitOff`
   - `TestIsContextEnrichmentEnabled_ReflectsToggle`

2. **`internal/mesnada/acp/agent_pando_test.go`** (extend):
   - `TestHandleSlashCommand_EnrichmentToggle`
   - `TestHandleSlashCommand_EnrichmentOn`
   - `TestHandleSlashCommand_EnrichmentOff`
   - `TestHandleSlashCommand_EnrichmentNotConfigured`
   - `TestAvailableCommands_IncludesEnrichment`

3. **`internal/api/enrichment_test.go`** (new):
   - `TestGetEnrichment_ReturnsCurrentState`
   - `TestToggleEnrichment_FlipsState`
   - `TestSetEnrichment_ExplicitValues`

### Integration

- Run: `go test ./internal/llm/agent ./internal/api ./internal/mesnada/acp`

---

## Implementation Order

1. **Phase 1** — Core backend toggle (`agent.go`). Self-contained, testable. ← Start here.
2. **Phase 4** — ACP slash command. Lowest surface area, easiest to test in isolation.
3. **Phase 2** — TUI command. Requires Phase 1 complete.
4. **Phase 3** — WebUI action. Requires Phase 1 + API endpoint.

---

## Open Questions

1. **Persistence**: Should toggling via ACP or TUI persist to config file?
   - Suggested default: No (runtime only). Add a flag `/enrichment on --save` later.
2. **Per-session vs global**: For ACP, should the toggle be per-ACP-session or global?
   - Suggested default: Global (mirrors how `SetContextEnricher` works today).
   - If per-session is needed, store `enrichmentEnabled bool` in `ACPServerSession` and pass it to `processGeneration` via context.
3. **Icon choice for WebUI**: `faDatabase`, `faBrain`, or `faMemory`?
   - Suggested: `faDatabase` (already imported in similar contexts).
