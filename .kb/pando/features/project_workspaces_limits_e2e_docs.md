# Project workspaces: limits, E2E, and docs

## What

Completed the final build/QA/documentation slice for project workspaces:

- added configurable project-workspace limits under `[Projects]`
- surfaced the workspace-cap error through the API and WebUI i18n
- replaced the stub Playwright scenario with a runnable end-to-end project-tabs flow
- added English and Spanish user-facing docs for project workspaces

## Files

### Backend / config

- `internal/config/config.go`
- `internal/config/init.go`
- `internal/config/config_test.go`
- `cmd/schema/main.go`
- `internal/app/app.go`
- `internal/project/errors.go`
- `internal/project/manager.go`
- `internal/project/web_instance.go`
- `internal/project/web_instance_test.go`
- `internal/api/handlers_projects.go`
- `internal/api/handlers_projects_test.go`
- `internal/api/handlers_settings.go`

### WebUI

- `web-ui/packages/pando-client/src/types/index.ts`
- `web-ui/packages/pando-client/src/stores/settingsStore.ts`
- `web-ui/packages/pando-client/src/stores/projectTabsStore.ts`
- `web-ui/packages/pando-client/src/stores/projectTabsStore.test.ts`
- `web-ui/src/components/settings/GeneralSettings.tsx`
- `web-ui/src/components/projects/ProjectsView.tsx`
- `web-ui/src/components/projects/ProjectWorkspace.tsx`
- `web-ui/src/components/layout/ProjectTabBarControls.ts`
- `web-ui/src/i18n/locales/{en,es,fr,de,pt,ja,zh}.json`
- `web-ui/e2e/project-tabs.spec.ts`
- `web-ui/playwright.config.ts`
- `web-ui/package.json`
- `web-ui/scripts/e2e-project-tabs.sh`

### Docs

- `../pando-docs/content/en/docs/features/project-workspaces.md`
- `../pando-docs/content/es/docs/features/project-workspaces.md`
- `../pando-docs/content/en/docs/features/web-ui.md`
- `../pando-docs/content/es/docs/features/web-ui.md`
- `../pando-docs/content/en/docs/features/_index.md`
- `../pando-docs/content/es/docs/features/_index.md`

## Why

The project workspace feature needed operational guardrails, a reproducible live test, and public documentation before the story could be considered complete:

- `Projects.MaxWebInstances` prevents unbounded background child growth
- `Projects.WebStartupTimeout` makes startup health-check timing configurable
- the API/store/i18n path keeps workspace-cap failures explicit and user-readable
- the Playwright flow proves tab open/restore/keep-alive/close-stop behavior against a real `pando app`
- the docs explain workflow, shortcuts, configuration, and the parent/child security model

## Verification

- `go build ./...`
- `go vet ./...`
- `go test ./internal/project/... ./internal/api/... ./internal/config/... ./internal/app/... ./cmd/...`
- `go test -race ./internal/project/... -count=1`
- `cd web-ui && bun run typecheck`
- `cd web-ui && bun run lint`
- `cd web-ui && bun run test`
- `cd web-ui && bun run build`
- `cd web-ui && ./scripts/e2e-project-tabs.sh`
- `cd ../pando-docs && hugo build`
