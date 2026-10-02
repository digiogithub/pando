---
created_at: 2026-10-02T20:17:27.308462636Z
updated_at: 2026-10-02T20:17:27.308462636Z
tags:
    - change
    - tui
    - settings
---
# TUI settings: typed row components and WebUI section names (2026-10-02)

Implements [[pando/plans/tui-settings-visual-components-proposal.md]]. Builds on [[pando/fixes/tui_settings_agents_scroll_selection.md]] (geometry single source of truth). Status: implemented, uncommitted, not yet checked by hand in a real terminal.

## Motivation
Every settings field was the same 3-4 line bordered box; separators and notes were faked with read-only text fields (about 43); repeated entities were flattened with label prefixes; section names differed from the WebUI.

## What changed
### Components (`internal/tui/components/settings/`)
- Borderless one-line rows, typed value rendering: toggle `● on / ○ off`, select `value ▾`, editable text with edit marker, action as `[ Label ]` button, read-only muted. Active row: `▌` gutter + highlight; hint shown only for the active row. ASCII fallback for glyphs.
- New `FieldType` values `FieldHeader` and `FieldNote` (non-focusable), `NoteLevel` (`NoteLevelInfo|Warning|Error`), `Field.Focusable()`.
- Cards: `Field.Card`, `Field.CardStatus`, `Field.CardID`. Consecutive fields with the same `CardID` (or `Card` when unset) share one rounded titled border; status is the first non-empty `CardStatus` of the run. Actions are one row each (vertical).
- Navigation skips non-focusable rows; auto-scroll also reveals decoration rows around the active row (leading header, trailing notes).
- `renderFields` plan remains the single source for `FieldHeights` / `FieldLineOffset` / `FieldAtLine`; invariant test: View height == sum of heights for widths 1..120 in both glyph modes.
- Files: `field.go`, `section.go`, `settings.go`, new `render_helpers.go`, tests `section_geometry_test.go`, `section_render_test.go`, `section_rebuild_test.go`.

### Page (`internal/tui/page/`)
- Section titles/order/groups match WebUI `CATEGORY_KEYS`: ungrouped General, Appearance, Providers, Agents, Persona Auto-Select, Auto mode, Decision model, MCP Servers, MCP Gateway, LSP, Tools, Container Runtime, Sandbox, Bash, Token Optimization, Skills, Skills Catalog, Lua Engine, Self-Improvement; group "Services": Mesnada, Remembrances, Snapshots, API Server, WebUI Access, OpenLit Observability.
- Renames: Agents/Models -> Agents, Internal Tools -> Tools, Subagents -> Mesnada, KB & Code Index -> Remembrances. New sections: Appearance (`tui.theme`), WebUI Access (`server.basicAuth.*`).
- User decision: no merges. Persona Auto-Select, OpenLit Observability and Skills Catalog have no WebUI settings counterpart and stay as their own TUI sections.
- Builders migrated to headers, notes and cards (providers, MCP servers, skills, agents, LSP servers, auto-mode routes). New helper file `settings_rows.go` (header/note/card helpers, card-aware labels for "Setting saved: <Card> › <Label>").
- `applyFieldPolicy` prunes headers with no focusable rows and drops sections with zero focusable rows.
- No `Field.Key` renamed. Removed read-only keys (moved to `CardStatus`): `providerAccount.<id>.status`, `skills.status.<name>`, `lsp.<name>.status`, `mcpServers.<name>.authStatusInfo`.
- Docs: `docs/desktop-controller.md`, and in the sibling repo `pando-docs/content/{en,es}/guides/web-browser-desktop-tools.md` ("Internal Tools" -> "Tools").

## Process
Orchestrated with mesnada subagents: names (task-d9ba0113) and components (task-44adf705) in parallel on disjoint directories, migration (task-23195b4e), independent review (task-de9a9ca5, 6 confirmed bugs, save path clean), fixes (task-42840460).

## Verification
`gofmt -l internal/tui` empty; `go build ./...`; `go vet ./internal/tui/...`; `go test -count=1 ./internal/tui/... ./internal/config/... ./internal/llm/agent ./internal/api` all pass. A sample section was rendered to text and inspected. Not done: manual run of the TUI in a terminal (mouse clicks, scroll, editing in cards).
