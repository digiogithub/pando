---
created_at: 2026-10-01T07:58:20.862355098Z
updated_at: 2026-10-01T07:58:20.862355098Z
tags:
    - model-routing
    - testing
    - gintrack
---
# Model auto mode — validation and gintrack close-out (2026-10-01)

Follow-up to [[pando/features/model-auto-mode-implementation.md]] (EP-0015, specs SP-0001..0004). Ran every traced test, ingested results into gintrack and closed what is verified.

## Result
- 21/22 requirements stamped `verified` (`gintrack spec verify --commit`) and set to `done`.
- Stories US-0077..0085 `done`; specs SP-0001, SP-0003, SP-0004 `done`.
- Still `in_review`: PANDO-SP-0002.R4, SP-0002, US-0086, EP-0015. Only missing test: `provider_remote_live_test.go#TestRemoteLive` (skipped: needs `TYPESAFE_API_KEY` or `PANDO_LIVE_JEV_BASEURL` + `PANDO_LIVE_JEV_KEY`).

## Test fixes made (the e2e/live tests had never been executed)
- `tests/model_auto_mode/test_playground_live.py`: skips TLS verification for loopback hosts (`pando app` is self-signed; `PANDO_E2E_INSECURE=1` for other hosts), sends `X-Pando-Token` (the API ignores `Authorization: Bearer` outside AG-UI), new `--junit <file>` option in plain mode.
- `web-ui/playwright.config.ts`: `ignoreHTTPSErrors: true`.
- `web-ui/e2e/model-auto-mode.spec.ts`: all four starter routes get a model before the playground runs (the playground validates the draft and returns "invalid modelAutoMode configuration" otherwise); waits for the layout before pressing Ctrl+O. The spec is not idempotent: it needs a server with auto mode not yet configured.
- `.gitignore`: `web-ui/test-results/`, `web-ui/playwright-report/`, `.kb/.pmngr/verify.json` (derived cache).

No product code changed.

## How to reproduce the validation
1. `go test -json <traced packages> > go.json`; `PANDO_LIVE_OLLAMA=1 go test -json -run Live ./internal/llm/systemone ./internal/llm/modelrouter > golive.json` (Ollama 0.35 + `tev1:0.8b`).
2. `cd web-ui && npx vitest run --reporter=json --outputFile=vitest.json`.
3. `make web-ui-embedded && go build -o pando-test .`; start `pando-test app --port N` with isolated `HOME`/`XDG_CONFIG_HOME` in an empty dir holding a `.pando.toml` with an `ollama` providerAccount and `Agents.coder.Model = 'ollama.tev1:0.8b'`; token from `GET /api/v1/token`.
4. Fake Jev server for the e2e (custom provider, model `fake-jev`): any HTTP server answering `GET /v1/models` and `POST /v1/systemone` (first criterion wins). `systemonetest` is `testing.TB`-only, so a small Python script was used.
5. `PANDO_E2E_BASE_URL=https://127.0.0.1:N PANDO_E2E_TOKEN=… PANDO_E2E_FAKE_JEV_URL=… npx playwright test --reporter=junit`; `python3 tests/model_auto_mode/test_playground_live.py --junit playground.xml` with `PANDO_E2E_ROUTE_MODEL=ollama.tev1:0.8b PANDO_E2E_ROUTER_MODEL=tev1:0.8b`.
6. `bench_router.py --require` (27/30 = 0.90, p50 64 ms) and `PANDO_LIVE_OLLAMA=1 live_providers.py` have no report format: a two-case JUnit file was written from their exit codes (`file=` attribute = script path).
7. Ingest: go reports `--format go`; vitest `--base web-ui`; Playwright JUnit `--base web-ui/e2e` (its classname is the bare spec file name); Python JUnit as is.

## gintrack gotchas
- With jj the working-copy commit id changes on every snapshot, so separate ingests land on different commits and `spec verify` answers `mixed-commits`. Ingest every report with the same explicit `--commit $(jj log -r @ --no-graph -T commit_id)`.
- `spec verify` needs `--by` (no `git.authorName` configured, stamps say "unknown").
- MCP `list_requirements` can return revs older than a CLI stamp; retry with `currentRev` from the `stale_revision` error.
