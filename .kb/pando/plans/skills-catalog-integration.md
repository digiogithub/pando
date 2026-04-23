# Skills Catalog Integration Plan for Pando

## Overview

Integrate the [skills.sh](https://skills.sh) catalog (from vercel-labs/skills) into Pando's TUI settings panel, allowing users to search, browse, install, update and remove skills from the catalog directly from the settings panel.

## Context & Research

### skills.sh Platform (vercel-labs/skills)
- **CLI**: `npx skills find [query]` — interactive fzf-style search then install
- **Search API**: `GET https://skills.sh/api/search?q=<query>&limit=<n>`
- **Response**: `{ query, searchType, skills: [{id, skillId, name, installs, source}], count, duration_ms }`
- `source` = GitHub `owner/repo` (e.g. `"anthropics/claude-skills"`)
- `name` = skill identifier within repo (e.g. `"git-workflow"`)
- Install format: `owner/repo@skill-name`
- SKILL.md files located at `{repo}/skills/{name}/SKILL.md` or `{repo}/{name}/SKILL.md`
- Supports 40+ agent types including `crush` (Pando's upstream)

### Pando's Current Skills System
- Skills are SKILL.md files in `~/.pando/skills/` or `.pando/skills/`
- Settings page has `buildSkillsSection()` listing installed skills with toggle/status
- No search or install capability currently
- Settings uses `Section` + `Field` pattern with types: `FieldText`, `FieldToggle`, `FieldSelect`
- Settings page in `internal/tui/page/settings.go`
- Settings components in `internal/tui/components/settings/`

---

## Implementation Phases

### Phase 1: skills.sh API Client
**Fact ID**: `skills_catalog_phase1_api_client`

Create HTTP client package to query the skills.sh search API.

- **Files**: `internal/skills/catalog/client.go`, `client_test.go`
- **Key types**: `CatalogSkill`, `SearchResult`, `Client`
- **Key functions**: `NewClient()`, `Search(ctx, query, limit)`
- **API**: `GET https://skills.sh/api/search?q=<query>&limit=<n>`
- Configurable base URL, 10s timeout, context cancellation support

---

### Phase 2: Skill Downloader & Installer
**Fact ID**: `skills_catalog_phase2_downloader`

Download SKILL.md from GitHub repos and install into Pando's skills directory, with lock file tracking.

- **Files**: `internal/skills/catalog/downloader.go`, `installer.go`, `lock.go`
- Download: tries multiple paths in repo to find SKILL.md
- Install: writes to `~/.pando/skills/{name}/SKILL.md` or `.pando/skills/{name}/SKILL.md`
- Lock file: `{skillsDir}/catalog-lock.json` tracks source, install date, scope, checksum
- Functions: `FetchSkillContent()`, `InstallSkill()`, `UninstallSkill()`, `IsSkillInstalled()`

---

### Phase 3: TUI Skills Catalog Search Dialog
**Fact ID**: `skills_catalog_phase3_tui_dialog`

Bubbletea dialog component for searching and installing skills from the catalog.

- **Files**: `internal/tui/components/dialog/skills_catalog.go`, `skills_catalog_test.go`
- UI: title bar + search input + scrollable results list + footer keybindings
- Async search with 200ms debounce via `tea.Cmd`
- Keys: `up/down` navigate, `enter` install (project), `G` install (global), `esc` close
- Shows: skill name, source (owner/repo), install count badge (1.2K ↓)
- Tea messages: `OpenSkillsCatalogMsg`, `CloseSkillsCatalogMsg`, `InstallSkillMsg`, `SkillInstalledMsg`

---

### Phase 4: Settings Page Integration
**Fact ID**: `skills_catalog_phase4_settings_integration`

Wire the catalog dialog into the Settings page's Skills section.

- **Files modified**: `internal/tui/page/settings.go`, `internal/tui/components/settings/field.go`, `section.go`
- Add `FieldAction` type to settings fields — activates a command on enter/space
- Add "Browse & Install from Catalog" action field in `buildSkillsSection()`
- `settingsPage` gains `catalogDialog` state + `showCatalog` bool
- Dialog rendered as overlay via `layout.PlaceOverlay`
- Handle `SkillInstalledMsg` → refresh skills section, show success notification

---

### Phase 5: Skill Management (Remove, Update)
**Fact ID**: `skills_catalog_phase5_management`

Add per-skill management actions: uninstall and update from catalog source.

- **Files modified**: `internal/tui/page/settings.go`, `internal/skills/catalog/installer.go`
- Each installed skill (from catalog) gets "Uninstall" and "Update" action fields
- "Source" read-only field shows `owner/repo@skill-name` for catalog skills, "(local)" otherwise
- Action keys: `"action:uninstall_skill:{name}"`, `"action:update_skill:{name}"`
- Dispatched in `settingsPage.Update` via prefix matching on field key
- After action: reload `SkillManager`, refresh settings sections
- Tea messages: `UninstallSkillMsg`, `UpdateSkillMsg`, `SkillUninstalledMsg`, `SkillUpdatedMsg`

---

### Phase 6: Configuration, Lock File & Polish
**Fact ID**: `skills_catalog_phase6_config_lock`

Add `SkillsCatalogConfig` to Pando's config system and polish the UX.

- **Files modified**: `internal/config/config.go`, `internal/tui/page/settings.go`
- New config struct: `SkillsCatalogConfig { Enabled, BaseURL, AutoUpdate, DefaultScope }`
- New settings section: "Skills Catalog" with all config fields
- Default: enabled=true, baseURL="https://skills.sh", defaultScope="global"
- Lock file finalized with checksum field
- Polish: loading spinner, error retry, install count badges, keyboard shortcut hints
- Optional auto-update at startup (check lock timestamps, re-fetch if stale)

---

## Architecture Diagram

```
skills.sh API
    │
    ▼
internal/skills/catalog/
├── client.go        ← Phase 1: HTTP client
├── downloader.go    ← Phase 2: fetch SKILL.md from GitHub
├── installer.go     ← Phase 2: write files to disk
└── lock.go          ← Phase 2/6: catalog-lock.json

internal/tui/components/dialog/
└── skills_catalog.go  ← Phase 3: search dialog (Bubbletea)

internal/tui/components/settings/
├── field.go           ← Phase 4: add FieldAction type
└── section.go         ← Phase 4: handle FieldAction

internal/tui/page/
└── settings.go        ← Phase 4/5: integration, overlay, management

internal/config/
└── config.go          ← Phase 6: SkillsCatalogConfig
```

## Dependencies
- No new external Go dependencies required
- Uses stdlib `net/http` for HTTP client
- Bubbletea + lipgloss (already in project) for TUI
- `charmbracelet/bubbles/textinput` (already used) for search input

## Testing Strategy
- Unit tests: API client with mock HTTP server, installer with temp dirs
- Integration test: mock API → download → install → verify → uninstall
- TUI tests: dialog rendering, key handling, message dispatch

## Notes
- Pando appears as `crush` agent type in skills.sh catalog (upstream is Charmbracelet Crush)
- All skill installs ultimately produce SKILL.md files — no format differences from manually placed skills
- The skills.sh catalog has 40+ supported agent types; the `universal` type is most compatible
