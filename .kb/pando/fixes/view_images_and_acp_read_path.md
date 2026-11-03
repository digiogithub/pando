---
created_at: 2026-11-03T12:02:56.664384571Z
updated_at: 2026-11-03T12:06:39.926277337Z
tags:
    - fix
    - tools
    - view
    - edit
    - agent
---
# View images, ACP read path, and read-before-edit guard surviving restarts (EP-0023)

Gintrack: [[PANDO-EP-0023]] (stories US-0133, US-0134, US-0135). Status: done, released in v1.5.3 (commit d6340c51).

## Reported
macOS user on WebUI (Pando v1.5.2, debug_id 7402-2673-1745-4679):
1. `read` on png/jpg returned `This is an image file of type: JPEG. Use a different tool to process images`.
2. `edit` often failed with `you must read the file before editing it. Use the View tool first` and worked on the second try.

## Root causes
- Images: `view` rejected image files on purpose (`TODO: handle images`), although the agent already supports image tool results (`agent.buildImageToolResult`).
- ACP: `runWithACP` recorded the read with the raw (possibly relative) path; `edit`/`patch` check the resolved absolute path.
- WebUI (most likely cause of the report): the guard map `fileRecords` (`internal/llm/tools/file.go`) lives only in process memory. A server/app restart wipes it, but the persisted session history still shows the read, so the model edits and the guard rejects. The `read` alias (`aliases.go`) is not involved.

## Changes
- `internal/llm/tools/view_image.go` (new): `readImageForView`, 20 MiB cap, MIME from content (`imageopt.DetectMIME`), returns `ToolResponseTypeImage`. SVG stays text.
- `internal/llm/tools/view.go`: image branch uses it; `runWithACP` resolves the path with `resolveToolPath` before reading and recording.
- `internal/llm/tools/read_history.go` (new): `SessionToolRead`, `SessionReadSource`, `SetSessionReadSource`, `lastReadTimeForEdit(ctx, absPath)`.
  - In-memory record wins.
  - On a miss: same session only (`SessionIDContextKey`), latest successful `view`/`read` of the resolved path, `ViewResponseMetadata.Content` must still be in the current file, then the read time (assistant message `CreatedAt`, before the real read) is cached via `recordFileReadAt`. Any doubt fails closed; older entries are not consulted. Dedup re-reads store no metadata and fail.
  - The callers' mtime check is unchanged.
- `internal/llm/tools/file.go`: `recordFileReadAt`.
- `internal/llm/tools/edit.go`, `patch.go`, `write.go`: guards call `lastReadTimeForEdit`. Error messages unchanged.
- `internal/llm/agent/read_source.go` (new): `newSessionReadSource(sessions, messages)` returns tool calls paired with results, only after `session.SummaryMessageID` (compaction point); fails closed if the summary id is not in the list. Installed in `NewAgent` (`agent.go`).
- Note: `messages.created_at` is Unix seconds (`strftime('%s')`); the migration comment saying milliseconds is wrong.

## Guard semantics vs before
Equal or stricter: previously the map was process-global (a read in session A allowed an edit in session B). The fallback is per session, cut at compaction, and checks content as well as mtime, so mtime-preserving changes (`rsync -t`, `cp -p`, tar) during a restart are caught when they touch the read window.

Known limit (fails closed): after a restart, if the model's own `edit`/`write` happened after its last `view`, the mtime is newer than that view and one re-read is required. Possible follow-up: accept successful edit/write results as reads.

## Verification
- Tests: `view_image_test.go`, `read_history_test.go` (9 tests incl. edit end to end), `internal/llm/agent/read_source_test.go`.
- `gofmt` clean on touched files; `go build ./...`; `go vet ./internal/llm/tools/ ./internal/llm/agent/`; `go test ./internal/llm/tools/ ./internal/llm/agent ./internal/api -count=1` all pass.
- Implemented with mesnada subagents (claude/sonnet) and reviewed in the main session.

## Better Stack
Source `pando` (id 2751484) only has startup logs for the reporter; tool traces are debug level and not shipped, so the logs could not confirm the cause.
