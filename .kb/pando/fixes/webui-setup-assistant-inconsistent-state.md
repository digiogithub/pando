---
created_at: 2026-09-29T14:34:27.524436773Z
updated_at: 2026-09-29T15:02:06.977504508Z
tags:
    - fix
    - webui
    - setup
    - onboarding
---
# Fix: first-run setup assistant could fail at startup and leave inconsistent state

Date: 2026-09-29. Builds on [[webui-first-run-setup-assistant]].

## Problems found (review) and fixes
1. **No startup retry** — `SetupWizard` fetched `/api/v1/setup/status` once; if the API was not ready the error was swallowed and the assistant never opened. Now retries with backoff 1/2/4/8/15s (~30s), stops on success/unmount.
2. **500 during Reload** — `Reload()` sets `cfg=nil` briefly; `GetSetupStatus` returned 500. New `config.ErrConfigNotLoaded`; `handleSetupStatus` maps it to 503.
3. **`PrepareSetupScope` not atomic** — created `.pando.toml` / global file then `Reload()`; on failure the file stayed (global choice refused forever) and viper stayed reset. Now tracks files it created and `rollbackSetupScope` removes only those and reloads again.
4. **Stale status after scope** — store now has `setStatus`; `applyScope` stores `r.status`, so after choosing project `firstStep` becomes `provider` and the impossible "global" choice is not reachable via Back.
5. **Stuck `providerBusy`** — ProviderStep unmounted (Copilot login success) before its busy effect re-ran, leaving Back/Skip disabled. Unmount cleanup calls `setBusy(false)`; wizard resets it on open.
6. **Open init effect** only on `[open]` — banner could open before status loaded. Now `[open, status]` with `initialisedRef` once per open.
7. **Partial model writes** in `handleSetupModels` — new `config.ValidateAgentModel` (lock + registered model) pre-validates all agents; previous models captured and restored on midway failure (coder via `s.setCoderModel`).
8. **`complete()` failure closed silently** — store `completeError`, `complete()` returns bool, wizard stays open with `setup.done.completeFailed` (en/es).

## Files
internal/config/setup.go, internal/config/config.go (ValidateAgentModel), internal/config/setup_test.go, internal/api/handlers_setup.go, internal/api/handlers_set_model_test.go, web-ui/packages/pando-client/src/stores/setupWizardStore.ts, web-ui/src/components/setup/SetupWizard.tsx, ProviderStep.tsx, i18n en.json/es.json.

## Verification
- `go build ./...`, `go vet`, `go test ./internal/config ./internal/api ./internal/llm/agent` OK; `bun run typecheck` and `bun run build` OK.
- Unit tests: rollback helper removes only created files; GetSetupStatus returns ErrConfigNotLoaded when cfg nil; bad fast model returns 400 with no coder write.
- **E2E (2026-09-29)**: `make web-ui-embedded` + binary, `pando app --port 8799` with isolated HOME/XDG dirs (empty project dir), driven with Playwright (playwright-core + /usr/bin/google-chrome headless; Pando's own browser_* tool failed: no $DISPLAY in the agent shell). Note `pando serve` does NOT serve the UI (404), use `pando app`.
  - Scenario 1 (global): first 2 status calls forced 503 → wizard opened on 3rd; global Continue → Back → Continue again no error; add openai-compatible → models step Back/Skip enabled; skip → skip → done; `/setup/complete` forced 500 → error notice, wizard stays open; Finish again → closes, marker written; reload → does not reopen; no page errors.
  - Scenario 2 (project): cancel + reopen from banner starts at scope; project scope creates .pando.toml and hides Back; Save models → remembrances; API `POST /setup/models` with invalid fastModel → 400 `fast model (summarizer): model … not supported`, .pando.toml md5 unchanged, coder unchanged; `POST /setup/scope global` refused with project config present.
- Not tested: real Reload failure, midway rollback in handleSetupModels, Copilot device flow (needs GitHub).

## Observations (not fixed, by design)
- Provider step lists auto-detected Ollama as "1 provider account" while `status.providerAccounts` is 0, so Skip is hidden there; user must "Add another provider" or "Use these accounts".
- After a project .pando.toml exists, the config banner disappears, so a cancelled wizard can't be reopened from the banner.
- Global scope reuses `~/.pando.json` auto-created by Load on fresh machines, not `~/.config/pando/.pando.toml`.

## Caveats
`validateAgent` reverts bad models silently, so pre-validation cannot catch provider-credential failures; those hit the rollback path. Client retry treats any error as retryable (does not inspect 503).
