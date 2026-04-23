# Pando CronJob Feature — Implementation Plan

## Overview
Pando will support configurable cronjobs. Each cronjob has a name, a Unix cron schedule,
and a prompt. When Pando is running, a background service checks the schedules and fires
Mesnada subagent tasks (engine=pando via ACP) automatically. Jobs can also be triggered
manually from CLI, TUI, and Web-UI. Logs appear in the standard Mesnada panel.

## Architecture Summary

```
.pando.toml
  [CronJobs]
  [[CronJobs.Jobs]] name, schedule, prompt, engine...
        │
        ▼
internal/cronjob/CronService   ← started in app.New(), stopped in app.Shutdown()
  uses github.com/robfig/cron/v3
        │  on trigger
        ▼
orchestrator.Spawn(SpawnRequest{Engine:"pando", Tags:["cronjob","cronjob:<name>"]})
        │
        ▼
Mesnada Task (runs pando ACP subagent, log to .pando/mesnada/logs/)
        │
        ▼
Visible in Mesnada panel (TUI + Web-UI) filtered by tag "cronjob"
```

## Phase Breakdown

---

### Phase 1 — Config & Model
**Fact key**: `cronjob_plan_phase1_config_model`

Files: `internal/config/config.go`
- New struct `CronJob`: Name, Schedule, Prompt, Enabled, Engine, Model, WorkDir, Tags, Timeout
- New struct `CronJobsConfig`: Enabled bool, Jobs []CronJob
- Add field `CronJobs CronJobsConfig` to `Config` struct
- Add `ParseCronExpression(s string) error` validation helper
- Dependency: `github.com/robfig/cron/v3`

Example TOML:
```toml
[CronJobs]
Enabled = true
[[CronJobs.Jobs]]
Name = "daily-review"
Schedule = "0 9 * * 1-5"
Prompt = "Review today's git log and summarize in DAILY_REPORT.md"
Enabled = true
Engine = "pando"
Tags = ["daily"]
Timeout = "10m"
```

---

### Phase 2 — CronJob Service (Background Scheduler)
**Fact key**: `cronjob_plan_phase2_service`

New package: `internal/cronjob/`
- `service.go`: CronService with Start/Stop/RunNow/Reload
- `runner.go`: dispatch logic → orchestrator.Spawn()
- `types.go`: OrchestratorClient interface (avoids import cycles)

Integration:
- `internal/app/app.go`: add `CronService` field, init in `New()`, stop in `Shutdown()`
- Hot-reload via config watcher → `CronService.Reload()`
- All cron-fired tasks tagged with `"cronjob"` and `"cronjob:<name>"`
- Concurrency guard: skip if same job already has a running task

Scheduler: `github.com/robfig/cron/v3` with standard 5-field Unix cron format

---

### Phase 3 — CLI Commands
**Fact key**: `cronjob_plan_phase3_cli`

New file: `cmd/cronjob.go`

Commands:
```
pando cronjob list                     # show all jobs with name/schedule/next-run
pando cronjob run <name>               # run immediately (bypass schedule)
pando cronjob install <name> [--dry-run]   # install in OS scheduler
pando cronjob uninstall <name>         # remove from OS scheduler
```

Install behavior:
- **Unix/macOS**: adds entry to user crontab via `crontab -l` + `crontab <tempfile>`
  Marker comment: `# pando-cronjob:<name>`
  Command: `cd <cwd> && <pando_binary> cronjob run <name>`
  
- **Windows**: generates and runs PowerShell `Register-ScheduledTask` with
  `-WorkingDirectory <cwd>` and `-Execute <pando_binary>` `-Argument "cronjob run <name>"`
  Task name: `pando-cronjob-<name>`
  Complex cron schedules → best-effort conversion with user warning

Uninstall:
- Unix: remove marker line + schedule line from crontab
- Windows: `Unregister-ScheduledTask -TaskName 'pando-cronjob-<name>' -Confirm:$false`

---

### Phase 4 — TUI Panel
**Fact key**: `cronjob_plan_phase4_tui`

Minimal integration strategy (cron tasks = normal Mesnada tasks with tag "cronjob"):

1. **Existing Mesnada panel**: add quick-filter key `c` to filter tasks by "cronjob" tag
2. **New dialog**: `internal/tui/components/dialog/cronjobs.go`
   - List: Name | Schedule | Enabled | Next Run
   - Actions: Enter (view tasks), `r` (run now), `e` (enable/disable)
3. **Keybinding**: open dialog from TUI (follow pattern of existing dialogs)
4. **New PubSub event**: `CronJobFired{JobName, TaskID}` in `internal/pubsub/events.go`
   → show ephemeral toast notification when a job fires automatically

"Run Now" from TUI → `app.CronService.RunNow(ctx, name)` → navigates to Mesnada panel filtered

---

### Phase 5 — Web-UI Panel + REST API
**Fact key**: `cronjob_plan_phase5_webui`

Backend: `internal/api/handlers_cronjobs.go` (new), registered in `internal/api/routes.go`

Endpoints:
```
GET    /api/cronjobs              — list jobs (name, schedule, enabled, nextRun, lastTaskId)
POST   /api/cronjobs              — create job (persists to .pando.toml)
PUT    /api/cronjobs/:name        — update job (enable/disable, prompt, schedule)
DELETE /api/cronjobs/:name        — remove job from config
POST   /api/cronjobs/:name/run    — trigger manual run → returns {taskId}
```

Config persistence helper: `config.SaveCronJobs(jobs []CronJob) error`
- Reads current TOML, replaces CronJobs section, writes back, triggers CronService.Reload()

Frontend: `web-ui/src/components/orchestrator/CronJobsPanel.tsx`
- New tab "CronJobs" inside the Orchestrator/Mesnada section
- Table: Name | Schedule | Enabled | Next Run | Run Now button | Toggle
- "+ New CronJob" button → modal form (Name, Schedule w/ cron hint, Prompt textarea, Engine, Model, Tags, Timeout)
- Click job → filter Mesnada tasks panel by `tag=cronjob:<name>`

Store: `web-ui/src/stores/cronJobsStore.ts`
- `fetchJobs()`, `runJob(name)`, `toggleEnabled(name)`, `createJob()`, `deleteJob()`

---

### Phase 6 — Integration, Tests & Polish
**Fact key**: `cronjob_plan_phase6_integration`

Tests in `tests/`:
- `test_cronjob_config.py`: validation (invalid name, invalid schedule, duplicate names)
- `test_cronjob_service.py`: scheduling with mocked time, RunNow tags, Reload, Stop
- `test_cronjob_cli.py`: list output, run unknown job error, install --dry-run per OS

Edge cases:
- Concurrency guard: skip if job already running (log warning with task_id)
- spawn failure: log but continue scheduler
- Relative WorkDir: resolve against base workDir
- Non-existent WorkDir: log error, skip job execution

Structured logging (slog):
```
cronjob_event=scheduled name=<name> schedule=<schedule>
cronjob_event=fired name=<name> task_id=<id>
cronjob_event=skipped name=<name> reason=already_running
cronjob_event=error name=<name> error=<err>
cronjob_event=reload jobs_count=<n>
```

Config init template: update `internal/config/init.go` to include commented CronJobs example.
ACP stdio mode: also starts CronService if configured (headless background jobs).

---

## Implementation Order
1. Phase 1 (Config) — foundation, no runtime changes
2. Phase 2 (Service) — core background scheduler
3. Phase 3 (CLI) — enables standalone/system integration immediately
4. Phase 4 (TUI) — minimal TUI integration
5. Phase 5 (Web-UI) — full management UI
6. Phase 6 (Tests & Polish) — quality and robustness

## Key Design Decisions
- **Mesnada-native**: cronjobs are just Mesnada tasks with special tags → zero new UI for log viewing
- **engine=pando default**: dispatches to the local Pando ACP instance (same machine, same config)
- **No new DB tables**: config in .pando.toml, task state in existing Mesnada store
- **Platform install**: user-level cron (not system-level) on Unix; limited privilege task on Windows
- **Hot-reload**: changing .pando.toml reschedules jobs without restarting Pando
