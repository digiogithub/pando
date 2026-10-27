# Native desktop: evaluator (self-improvement) area

Date: 2026-10-27. Part of P3-ops of [[native_desktop_mygo]] (workspace `/www/MCP/Pando/pando-native-desktop`, uncommitted).

## What
Area id `evaluator` (`/evaluator`, sidebar tools, title `nav.selfImprovement`) in `internal/nativeui/app/evaluator/evaluator.go`, port of `web-ui/src/components/evaluator`:
- Doctor banner (problem or disabled reason, show/hide the `pando evaluator doctor` text).
- 4 metric cards (sessions evaluated, templates, avg reward, judge usage 14d).
- Daily evaluations bar chart with MyGo `ui/chart` (band + linear scales, grid, tooltips with mean reward and judge calls).
- Tabs (Segmented): variants + skills, sessions.
- UCB ranking table (sorted by UCB score per section; `-` while unused; file removed marker).
- Learned skills with pending/approved/rejected filter, success-rate progress, approve/reject (`POST /evaluator/skills/{id}/{approve|reject}`, then reload).
- Evaluated sessions: expandable rows with reward badge, score components x weights, pattern hits, feedback, variants, skills, judge analysis; "Open" navigates to `/chat/s/{id}`.
- Header refresh button.

Client: `internal/nativeui/client/evaluator.go` (`EvaluatorMetrics/Templates/Skills/Sessions/Doctor`, `ReviewSkill`, DTOs) with tests in `evaluator_instances_test.go`. Registered with a blank import in `internal/nativeui/nativeui.go`.

## Verification
`go vet` and `go test -race ./internal/nativeui/app/evaluator ./internal/nativeui/client` pass (headless Tester against httptest: cards, UCB ordering, approve call, sessions tab expansion). Screenshots (Tester image; `PANDO_NATIVE_SHOT_DIR=<dir> go test ./internal/nativeui/app/evaluator -run Shots`; add `PANDO_NATIVE_IT_URL` + `PANDO_NATIVE_IT_CA` for a real server): `evaluator-seeded-*.png` (fake seeded server), `evaluator-real-variants.png` (real `pando serve`, isolated HOME, empty data) in the scratchpad `screens/` dir.

## Gaps
Evaluator data cannot be seeded in a real server from outside (pando.db uses the multiwriter driver, plain sqlite3 cannot write it), so populated views are verified with the fake server. Text is English like the web (no i18n keys exist there).
