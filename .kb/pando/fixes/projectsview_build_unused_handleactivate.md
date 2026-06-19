---
created_at: 2026-06-19T09:24:37.955212602Z
updated_at: 2026-06-19T09:24:37.955212602Z
tags:
    - fix
    - web-ui
    - build
    - projects
---
# Fix: Web-UI build broken by unused `handleActivate` in ProjectsView.tsx

## Date
2026-06-19

## Symptom
`bun run build:embedded` (and thus the overall `build-webui` step) failed during `tsc -b`:

```
src/components/projects/ProjectsView.tsx(111,9): error TS6133: 'handleActivate' is declared but its value is never read.
```

## Root cause
The recent "Web-UI Projects stop-instance" feature introduced `handleToggle(proj)`,
which now handles both starting (calls `activateProject`) and stopping
(`stopProject`) a project's child instance. The previous `handleActivate(id)`
helper was left in place but no longer referenced anywhere in the component,
so TypeScript's `noUnusedLocals` flagged it as a build error.

## Change
Removed the dead `handleActivate` function from
`web-ui/src/components/projects/ProjectsView.tsx`. `handleToggle` already covers
the activation path via `activateProject`.

## Verification
- `cd web-ui && bunx tsc -b` now completes with no output (success).

## Related
- See memory `feature_webui_projects_stop_instance.md` for the feature that
  introduced `handleToggle` and superseded `handleActivate`.
