---
created_at: 2026-09-24T21:35:11.826710554Z
updated_at: 2026-09-24T21:35:11.826710554Z
---
# WebUI redesign P6 part B — Secondary views (PANDO-US-0056)

Part of [[webui_native_redesign_plan]] (epic PANDO-EP-0010). Builds on
[[webui_redesign_p1_p2_foundations]] and the `ui/*` primitives (`Button`,
`IconButton`, `Card`, `Badge`, `Dialog`, `Popover`, `Menu`, `Input`, `Select`,
`Switch`, `Textarea`, `Tabs`/`SegmentedControl`, `EmptyState`, `Spinner`,
`Tooltip`).

## Scope (my area of P6)

`web-ui/src/components/{orchestrator,logs,snapshots,evaluator,projects,
instances,extensions,shared}/*` (shared excluding `ThemePicker.tsx`, owned by
the settings agent), plus two new CSS files.

## What changed

### New CSS
- `web-ui/src/styles/views.css` — the shared "page" pattern for secondary
  views: `.view` shell, `.view-header` (title + description + right-aligned
  actions), `.view-toolbar`, `.view-table` (sticky header, hover
  `--bg-raised`, `data-selected` → `--accent-soft`), `.status-dot` (+ tone
  modifiers, `--pulse`), `.view-metrics`/`.metric-*` (Card-based metric row),
  `.filter-bar-search`, `.log-row-*`, `.view-detail` (side/bottom panels),
  `.code-block`, `.entity-card`/`.entity-row` (instance/session rows),
  `.split-pane-*`, `.centered-fill`, `.inline-form-*`.
- `web-ui/src/styles/shared.css` — `.toast-*`, `.banner-*` (danger/soft
  warning), `.dir-row`, `.model-combo-*` (trigger + popover panel + option
  rows + badge tones), `.persona-trigger`, `.kv-*`, `.tag-*`,
  `.masked-input-*`, `.pwa-prompt*`. Both imported once from `index.css`.
- `components/ui/icons.ts`: added `CloudUpload`, `FlaskConical`, `RotateCw`,
  `Trophy`, `UserRound`, `Wifi` (alphabetical, small targeted edits per the
  shared brief).

### Views (page pattern applied)
- **Orchestrator** (`OrchestratorView`, `TaskRow`, `TaskDetail`,
  `CreateTaskDialog`, `CronJobsPanel`): tab bar → `ui/Tabs`; toolbar → `Badge`
  running count + `Button`; task table → `.view-table`; detail side panel →
  `.view-detail`; create dialog → `ui/Dialog` with `form`/`form id` linking
  the footer submit button to the body `<form>`; cronjob enabled toggle →
  `ui/Switch` (was two FA icons); cronjob inline form → `.inline-form-grid`.
- **Logs** (`LogsView`, `LogFilters`, `LogTable`, `LogDetail`): level filter
  → `SegmentedControl` (was a native `<select>`); search → `Input` +
  `.filter-bar-search`; level badges → `Badge` `outline` with a
  danger/warning/info/neutral tone map; rows → `.view-table.log-table`
  (monospace); detail → `.view-detail--bottom`.
- **Snapshots** (`SnapshotsView`, `SnapshotTable`, `SnapshotRow`,
  `CreateSnapshotDialog`): table → `.view-table`; row actions →
  `IconButton`; create dialog → `ui/Dialog` + `TextInput`.
- **Evaluator** (`SelfImprovementView`, `MetricsCards`, `SkillCard`,
  `SkillsList`, `UCBRankingTable`): metrics row → `MetricCard` (restyled,
  `Card`-based) with lucide `FlaskConical`/`Layers`/`Trophy`; ranking table →
  `.view-table`.
- **Projects** (`ProjectsView`, `ProjectInitWizard`): status → `Badge` tone
  map (running=success, error=danger, initializing=warning,
  stopped/missing=neutral); external/auto/delegations chips → `Badge`
  `outline`/`warning`; add-project inline form → `Input`+`IconButton`+
  `Button`; init wizard → `ui/Dialog`.
- **Instances** (`InstancesPanel`, `InstanceCard`, `RemoteSessionView`):
  split-pane list/detail → `.split-pane-*`; instance card → `.entity-row`
  with mode `Badge` tone map (tui/desktop=accent, webui=info, acp=success);
  session list, live-stream event rows, message rows all restyled with
  tokens; live indicator → `.status-dot--success.status-dot--pulse`.
- **Extensions**: `ExtensionPanelPage`/`ExtensionPanel` restyled (error text
  → `text-danger`); `MemorySyncIndicator` FA → lucide
  `CloudUpload`/`TriangleAlert`, colour via `text-danger`/`text-muted`/
  `text-warning`. `ExtensionSlot` untouched (no styling).

### Shared components (API kept backward-compatible — other areas import
these)
`StatusBadge`→`Badge` tone map; `EmptyState`/`LoadingSpinner`/`Tooltip` now
thin wrappers around the `ui` primitives of the same name; `ProgressBar`
restyled with tokens; `MetricCard`→`Card`; `CopyButton` restyled (kept
`text`/`label`/`title`/`className`/`size`/`style` props); `ConfirmDialog`→
`ui/Dialog`; `Toast`/`ToastContainer` restyled (`.toast-*`, icon-by-tone);
`ErrorBoundary`/`NotFound` restyled, lucide icons; `NetworkErrorBanner`→
`.banner--danger` + `.banner-action` (new token-based overlay class, avoids
raw `white/opacity` Tailwind utilities); `RestartRequiredBanner`→
`.banner--soft-warning`; `PWAInstallPrompt`→`.pwa-prompt`; `DirBrowserDialog`→
`ui/Dialog` + `.dir-row`; `FormInput.tsx` (`TextInput`/`SelectInput`/
`Textarea`/`MaskedInput`/`Toggle`) now wrap `ui/Input`/`Select`/`Textarea`/
`Switch` — note the native HTML `size?: number` attribute had to be omitted
from the prop types (`Omit<...HTMLAttributes<...>, 'size'>`) since it
collides with the `ui` primitives' own `size?: 'sm'|'md'`; standalone
`MaskedInput.tsx` (different props: `value`/`onChange`/`actionLabel`)
restyled separately; `KeyValueEditor`/`TagListEditor` restyled with
`ui/Input`+`ui/Button`; `ModelCombobox` rewritten on top of `ui/Popover`
(manual `getBoundingClientRect` positioning removed) — kept exports
`formatTokenLimit`/`modelMetaLine`/default export and all keyboard nav
(arrow/Enter/Escape, `provider.model` search syntax); `PersonaSelector`
rewritten on `ui/Menu`+`MenuItem`.

### Mid-task correction (shell agent)
The shell agent reported `PersonaSelector` was rendered in the title bar and
was the loudest element there (FA icon + gold `--primary` text). Adjusted:
lucide `UserRound`/`ChevronDown` at 14px, `text-sm`, idle state
`text-fg-muted`, hover `--bg-raised`, active persona `text-fg` (not accent) —
a quiet ghost trigger, `Menu` popover unchanged.

## Bugs found in the `ui` primitives while wiring them up
- `ui/Input`'s CSS sets `padding` as a shorthand in `ui.css`, which — because
  `ui.css` is `@import`ed after Tailwind in `index.css` — always wins over a
  Tailwind `pr-*` utility on the same element regardless of class order. Any
  input needing extra padding for a trailing icon/button (e.g. the
  show/hide toggle in `MaskedInput`) must override via a **two-class**
  selector (`.masked-input-wrap .ui-input { padding-right: 34px }`) for the
  specificity to win, not a Tailwind utility class. Documented here in case
  another area hits the same thing.

## Verification
- `cd web-ui && bun run typecheck` — 0 errors in my area (2 unrelated
  pre-existing errors in `components/design/IssuePanel.tsx` and
  `components/editor/FileExplorer.tsx`, owned by other concurrent agents).
- `bunx eslint <my area dirs>` — 0 errors, 4 pre-existing
  `react-refresh/only-export-components` warnings (same pattern existed
  before: `KeyValueEditor.tsx`/`ModelCombobox.tsx` export helper functions
  alongside their default component, unchanged by this pass).
- `grep -rl '@fortawesome'` and a hex-colour grep over the area: both empty
  — no FontAwesome imports, no hardcoded hex colours left.
- Visual screenshots (light/dark, `/orchestrator` `/logs` `/evaluator`
  `/projects` `/instances`) could **not** be captured: the shared
  `browser_navigate`/`browser_evaluate` MCP tools returned `context
  canceled` on every retry (multiple attempts, increasing waits, including
  against `https://example.com`), most likely due to contention from the
  other concurrent redesign agents sharing the same browser session. Code
  review + typecheck/lint were used as the verification substitute; a
  follow-up visual pass is recommended once the shared browser tool is free.

## Leftovers / requests for other areas
- Visual QA screenshots still owed (see above) — whoever gets browser access
  next should sanity-check `/orchestrator`, `/logs`, `/evaluator`,
  `/projects`, `/instances` in light + dark.
- The `ui/Input` padding-shorthand-vs-Tailwind-utility precedence issue
  above may affect other areas using trailing icons/buttons inside `Input`.
