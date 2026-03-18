# Plan: Browser Internal Tools (chromedp)

**Date:** 2026-03-18  
**Status:** Planned  
**Goal:** Implement Chrome DevTools-inspired internal tools in Go using `github.com/chromedp/chromedp`, activatable/deactivatable from the TUI configuration panel under "Internal Tools".

## Reference
- Inspired by: https://github.com/ChromeDevTools/chrome-devtools-mcp/
- Go library: https://github.com/chromedp/chromedp
- Pattern: follows existing `InternalToolsConfig` + `buildInternalToolsSection()` pattern

---

## Architecture Overview

```
.pando.toml [InternalTools.BrowserEnabled=true]
       ↓
app.go → InitBrowserRegistry(&cfg.InternalTools)
       ↓
CoderAgentTools() → registers 10 browser tools when BrowserEnabled=true
       ↓
browserSessionRegistry (global, like sessionCacheRegistry)
  └─ per pando sessionID → chromedp allocator + context
       ↓
session.EndSession() → CloseBrowserSession(sessionID)
```

---

## Phase 1: Dependencies & Configuration
**Fact key:** `browser_tools_phase1_deps_config`

- Add `github.com/chromedp/chromedp` to `go.mod`
- Extend `InternalToolsConfig` in `internal/config/config.go`:
  - `BrowserEnabled bool`
  - `BrowserHeadless bool` (default `true`)
  - `BrowserTimeout int` (seconds, default `30`)
  - `BrowserUserDataDir string`
  - `BrowserMaxSessions int` (default `3`)
- Update `pando-schema.json` with new fields
- Add commented browser section to `.pando.toml`

---

## Phase 2: Browser Session Manager
**Fact key:** `browser_tools_phase2_session_manager`

- New file: `internal/llm/tools/browser_session.go`
- `browserSessionRegistry` global registry (mirrors `sessionCacheRegistry` pattern)
- Per-session `browserSession` struct holding chromedp allocator context + browser context
- `GetOrCreateBrowserSession(sessionID)` — lazy-creates Chrome process
- `CloseBrowserSession(sessionID)` — kills Chrome process for that session
- `CloseAllBrowserSessions()` — app shutdown cleanup
- Helper `getBrowserCtxWithTimeout(ctx)` for use in all tools
- chromedp allocator configured from `InternalToolsConfig` (headless, userDataDir, etc.)

---

## Phase 3: Core Browser Tools
**Fact key:** `browser_tools_phase3_core_tools`

Four foundational tools, each in its own file:

| Tool name | File | Description |
|---|---|---|
| `browser_navigate` | `browser_navigate.go` | Navigate to URL, wait for load |
| `browser_screenshot` | `browser_screenshot.go` | Full page or element screenshot (returns image) |
| `browser_get_content` | `browser_content.go` | Get HTML/text/title/markdown from page |
| `browser_evaluate` | `browser_evaluate.go` | Execute JavaScript, return result |

- Screenshots use `ToolResponseTypeImage` (existing infrastructure)
- Large HTML content auto-cached via `InterceptToolResponse`
- All tools use `getBrowserCtxWithTimeout` from Phase 2

---

## Phase 4: Interaction & DevTools Tools
**Fact key:** `browser_tools_phase4_interaction_devtools`

Two files, seven more tools:

**`browser_interact.go`:**
| Tool name | Description |
|---|---|
| `browser_click` | Click element by CSS selector |
| `browser_fill` | Fill form input/textarea |
| `browser_scroll` | Scroll page or element |

**`browser_devtools.go`:**
| Tool name | Description |
|---|---|
| `browser_console_logs` | Capture JS console messages (buffered per session) |
| `browser_network` | Capture network request/response log |
| `browser_pdf` | Generate PDF of current page |

- Console and network tools use `chromedp.ListenTarget` for event capture
- PDF uses Chrome DevTools Protocol `page.PrintToPDF`
- All logs/events buffered per `browserSession`, clearable on demand

---

## Phase 5: TUI Configuration Integration
**Fact key:** `browser_tools_phase5_tui_config`

- Modify `internal/tui/components/settings/settings.go`
- Add to `buildInternalToolsSection()`:
  - "Browser Enabled" → toggle `InternalTools.BrowserEnabled`
  - "Browser Headless" → toggle `InternalTools.BrowserHeadless`
  - "Browser Timeout (s)" → text `InternalTools.BrowserTimeout`
  - "Browser User Data Dir" → text `InternalTools.BrowserUserDataDir`
  - "Browser Max Sessions" → text `InternalTools.BrowserMaxSessions`
  - "Browser Info" → readonly hint with tool list + Chrome requirement
- Update `persistSetting()` to handle new integer fields
- Input validation: timeout 5–300, maxSessions 1–10

---

## Phase 6: Agent Registration, Session Cleanup & Tests
**Fact key:** `browser_tools_phase6_agent_registration`

- `internal/llm/agent/tools.go`: conditional block adding 10 browser tools when `BrowserEnabled=true`
- `internal/app/app.go`: call `tools.InitBrowserRegistry()` at startup
- `internal/session/session.go`: call `tools.CloseBrowserSession(sessionID)` on session end
- `internal/app/app.go`: call `tools.CloseAllBrowserSessions()` on app shutdown
- Tests in `internal/llm/tools/browser_test.go` using `httptest.NewServer` for local pages
- Optional docs: `docs/browser-tools.md`

---

## Summary: New Files

```
internal/llm/tools/
├── browser_session.go       # Phase 2: session manager
├── browser_navigate.go      # Phase 3: navigate tool
├── browser_screenshot.go    # Phase 3: screenshot tool
├── browser_content.go       # Phase 3: get content tool
├── browser_evaluate.go      # Phase 3: evaluate JS tool
├── browser_interact.go      # Phase 4: click/fill/scroll tools
├── browser_devtools.go      # Phase 4: console/network/pdf tools
└── browser_test.go          # Phase 6: tests
```

## Summary: Modified Files

```
go.mod                                              # Phase 1: +chromedp
internal/config/config.go                          # Phase 1: +BrowserXxx fields
pando-schema.json                                   # Phase 1: +browser schema
.pando.toml                                         # Phase 1: +browser comments
internal/tui/components/settings/settings.go       # Phase 5: +browser UI fields
internal/llm/agent/tools.go                        # Phase 6: +browser tool registration
internal/app/app.go                                 # Phase 6: +init + shutdown
internal/session/session.go                         # Phase 6: +session cleanup
```

## Total Tools Added: 10

`browser_navigate`, `browser_screenshot`, `browser_get_content`, `browser_evaluate`,  
`browser_click`, `browser_fill`, `browser_scroll`,  
`browser_console_logs`, `browser_network`, `browser_pdf`
