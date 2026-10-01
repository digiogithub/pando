---
created_at: 2026-10-01T13:32:00.445857395Z
updated_at: 2026-10-01T13:32:00.445857395Z
tags:
    - fix
    - webui
    - e2e
    - delegation
    - sse
    - api
    - pando
---
# Fix + E2E: WebUI missed short resumed runs; reattach now driven by `run_seq` (2026-10-01)

Follow-up to [[resumed-run-live-updates-webui-acp-2026-10-01]] (epic PANDO-EP-0016, story PANDO-US-0087). Related: [[reference_webui_e2e_playwright]].

## What the first live test showed
`pando app` + Pando `browser_*` tools. Parent agent spawned a subagent (`sleep 30`), ended its turn; ~35 s later the supervisor resumed the session. Backend was correct (run through `bgRunner`, `is_running` true ~4 s, messages persisted) but the open chat never changed: no `GET /sessions/{id}/stream` request. The WebUI only reattached when the 4 s `/pending` poll saw `running == true`, so a resumed run shorter than the poll interval was lost. Unit tests had not covered this.

## Fix
- `internal/api/background_runner.go`: monotonic per-session `runSeq` (`RunSeq(id)`), bumped on each accepted `Submit`, never GC'd.
- `run_seq` exposed in `/pending` (`handlers_questions.go`), session list and detail (`handlers_sessions.go`); new SSE event `run` `{sessionId, runSeq}` at the start of `streamSessionEvents` (`handlers_chat.go`), for both `POST /chat/stream` and `GET /sessions/{id}/stream`.
- Frontend: `seenRunSeq` per session in `sessionStore`, helper `shouldReattach` + `RUN_SEQ_STALE` in `web-ui/packages/pando-client/src/services/runSeq.ts`. `ChatView.tsx` reattaches when server `run_seq` > seen and not streaming, regardless of `is_running`; `reconnectSession` replays the buffered run. `finishedSessionRef`, its timer and `reconnectedSessionRef` removed (redundant). A dropped stream marks the baseline stale so the next poll reattaches. Empty replay (buffer GC'd) reloads history.
- Side fix: `mapSession` read `raw.IsRunning` but the list endpoint sends `is_running`; the sidebar running state was always dropped.

## E2E result after the fix (same scenario, no navigation)
- t=9.6 s "waiting for subagent" shown, turn ended.
- t=52.2 s "Resuming after a delegated task" marker appeared; t=52.6 s "RESULT RECEIVED: PINEAPPLE".
- Network: `GET /api/v1/sessions/<id>/stream` issued by the page at resume time; server `run_seq` went 1 -> 2.
- No duplicated messages in the transcript.

## E2E setup notes
- Build: `make web-ui-embedded` + `go build -o <scratch>/pando-test .`; run `pando-test app --port 8799` from a scratch dir holding a copy of `.pando.toml` (separate dir so it is not an IPC secondary of a running instance).
- Pando `browser_navigate` rejects the self-signed cert (`ERR_CERT_AUTHORITY_INVALID`) and `pando app` has no plain-HTTP flag: put a tiny Go reverse proxy (HTTP -> HTTPS, `InsecureSkipVerify`, `FlushInterval = -1`) in front.
- `browser_fill` fails on an empty textarea (`does not have child #text node`); set the value with the native setter + `input` event via `browser_evaluate`.
- `browser_screenshot` output is too large for the tool result; use `browser_get_content` / `browser_evaluate`.
- After rebuilding, unregister the service worker / reload so the new bundle is used.

## Still unverified
ACP clients (Zed, Xcode); cancel and steer of a resumed run in the live UI; long resumed runs streaming token by token (the tested run was ~4 s, shown via replay). Detection latency is still up to the 4 s poll; no push signal.
