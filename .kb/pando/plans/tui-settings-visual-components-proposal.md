---
created_at: 2026-10-02T19:39:39.162846726Z
updated_at: 2026-10-02T19:39:39.162846726Z
tags:
    - plan
    - tui
    - settings
    - proposal
---
# Proposal: TUI settings — typed visual components and WebUI section names

Status: proposal, awaiting user evaluation (2026-10-02). Nothing implemented.

## Problem
- `internal/tui/components/settings/section.go` `renderFields` draws every `Field` as the same bordered box (3-4 lines), whatever its `FieldType` (text, toggle, select, action) or state (read-only info).
- Separators and notes are faked with read-only `FieldText` fields: about 43 `ReadOnly: true` fields in `internal/tui/page/settings*.go` ("Info", "Warning", "Validation", "Memory System" with a literal `── Memory System ───` value, "Security Defaults"...). They are focusable and look like editable fields.
- Repeated entities (provider accounts, MCP servers, skills, agents, LSP) are flattened to prefixed labels (`[id] Name`, `<server> Command`), truncated by the 14-24 column label width.
- Section names and groups differ from the WebUI (`web-ui/src/components/settings/SettingsView.tsx` `CATEGORY_KEYS`).

## Section naming (WebUI is the reference)
Order and names: General, Appearance, Providers, Agents, Auto mode, Decision model, MCP Servers, MCP Gateway, LSP, Tools, Container Runtime, Sandbox, Bash, Token Optimization, Skills, Lua Engine, Self-Improvement; group "Services": Mesnada, Remembrances, Snapshots, API Server, WebUI Access.

Renames: "Agents/Models" -> "Agents"; "Internal Tools" -> "Tools"; "Subagents" -> "Mesnada"; "KB & Code Index" -> "Remembrances". Merges: "Skills Catalog" becomes a group inside "Skills"; "Persona Auto-Select" becomes a group inside "Agents"; "OpenLit Observability" becomes a group inside "API Server". Splits: theme fields out of General into "Appearance"; "WebUI Access Users" out of API Server into "WebUI Access". Sidebar groups Core/AI/Extensions/Integrations/Tools/Services replaced by the WebUI layout (ungrouped + "Services"). "Design system" has no TUI counterpart and is left out.

## New components
1. Group header: non-focusable title + rule, replaces fake separator fields.
2. Note / callout: non-focusable, icon + colored left bar, levels info/warning/error; replaces "Info"/"Warning"/"Validation" fields.
3. Card: one titled border around the rows of a repeated entity, with a status badge in the title; removes the label prefixes.
4. Borderless one-line rows with a typed value: toggle `● on / ○ off`, select `value ▾`, text `value` with edit affordance, action rendered as a button `[ Add provider ]`, read-only value dimmed. Active row marked with `▌` and background, hint shown only for the active row.
5. Status badge for `... Status` fields.

## Design notes
- Model: `Section.Fields []Field` stays as the editable list; add `Field.Group`, `Field.Card` and a `FieldNote`/`FieldHeader` kind, or a parallel `Rows` slice. Navigation skips non-focusable rows.
- `renderFields` must stay the single source of truth for `FieldHeights` / `FieldAtLine` / `FieldLineOffset` (see `pando/fixes/tui_settings_agents_scroll_selection.md`); it becomes a row renderer returning height and field index (-1 for decoration).
- Save path is keyed by `Field.Key`, unaffected. `SaveFieldMsg.SectionTitle` and `SetActiveField(sectionTitle, ...)` use titles: audit before renaming; prefer adding a stable `Section.ID`.
- Density: 3-4 lines per field today, 1 line proposed.

## Phases
1. Row model + borderless typed rows + geometry tests.
2. Group header and note components; migrate the read-only pseudo-fields.
3. Card component; migrate Providers, MCP Servers, Skills, Agents, LSP.
4. Section renames, merges, splits, sidebar order; stable `Section.ID`.
5. Tests (`internal/tui/components/settings`, `internal/tui/page`) and visual check.
