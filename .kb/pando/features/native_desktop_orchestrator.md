# Native desktop: orchestrator area (MyGo)

Date: 2026-10-27. Phase P3-ops. Plan: [[native_desktop_mygo]]. Foundation: [[native_desktop_p0_foundation]].

## What
- `internal/nativeui/app/orchestrator/` (id `orchestrator`, `/orchestrator`): `orchestrator.go` (view, pollers, task table, delegation-metrics bar), `detail.go` (task detail panel, collapsible tool-call cards), `create.go` (create-task dialog), `cron.go` (CronJobs tab: table, enable switch, run now, delete confirm, create form).
- `internal/nativeui/client/orchestrator.go` (+test): tasks list/get/create/cancel/delete, delegation metrics, cronjobs list/create/enable/run/delete.
- Registered via blank import in `nativeui.go`.

## Behaviour notes
- Polls tasks+metrics every 5 s and cron every 30 s, only while the view was built in the last 2 s (no traffic when another area is shown).
- Create task sends `{prompt, model, background}`: the server requires `prompt` (the web dialog sends `description`, which the server ignores -> 400; the native one sends name + description as prompt). Model select lists `/api/v1/models` ids.
- Selected table row paints the accent colour (ui.Table Selected), so status badges render as plain text on the selected row.
- Detail panel is fixed 400 DIP (no resize handle yet); output and tool results are truncated to the last 20k/4k chars.
- Tab labels and most strings are English literals as in the web; hero and delegation metric labels use i18n keys.

## Verification
`go test ./internal/nativeui/app/orchestrator ./internal/nativeui/client` and `go vet` pass: fake-server headless tests (list, metrics, detail, tool-call expand, cancel, create, cron run/create). Real server (isolated HOME, Mesnada + cronJobs enabled, tasks failing because no `claude` binary): screenshots in scratchpad `screens/orchestrator-{tasks,detail,create,cron,cron-form}.png`; `TestRealServerScreens` (env `PANDO_NATIVE_IT_URL`, `PANDO_NATIVE_IT_SHOTS`) renders them headlessly.

## Gaps
No task title edit, no resizable detail, no ACP/conclusion fields, cron edit (update prompt/schedule) not offered (web neither).
