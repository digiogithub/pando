# Native desktop: instances area

Date: 2026-10-27. Part of P3-ops of [[native_desktop_mygo]] (uncommitted).

## What
Area id `instances` (`/instances`, services section) in `internal/nativeui/app/instances/instances.go`, port of `web-ui/src/components/instances`:
- Instance list grouped by project path (`~` shortening, PRIMARY badge, mode badge, PID, short id), refresh button.
- Remote sessions of the selected instance (`/instances/{id}/sessions?limit=100&offset=`), paged with "Load more".
- Remote session view: history (`/messages`), live stream `GET /instances/{id}/sessions/{sid}/stream` (unnamed `data: {topic,payload}` frames; heartbeats filtered but mark the stream live; last 200 events kept; token/content shown; topic colours), follow-end list, Send Message box (Enter sends; `POST .../message`), Cancel (`DELETE .../cancel`). Streams are tagged by generation so superseded ones are dropped; events batched with `app.Batch`.

Client: `client/instances.go` (`Instances`, `RemoteSessions`, `RemoteMessages`, `SendRemoteMessage`, `CancelRemote`, `FollowRemoteSession`), tests in `client/evaluator_instances_test.go`. The stream uses the token header (the web used `?token=`).

## Verification
`go vet`, `go test -race ./internal/nativeui/app/instances ./internal/nativeui/client` pass (Tester + httptest: list, select instance, history, live token, noise filtered, send, cancel). Screenshots: `instances-real-list.png`, `instances-real-selected.png` (real `pando serve` x2, isolated HOME), `instances-seeded-stream.png` (fake server) in the scratchpad `screens/` dir.

## Gaps
Sessions cannot be created through the API without a model, so a populated remote stream is verified only against the fake server. No auto-refresh of the instance list (same as web).
