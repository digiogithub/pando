---
created_at: 2026-11-06T12:27:43.719936431Z
updated_at: 2026-11-06T12:27:43.719936431Z
tags:
    - fix
    - agui
    - provider
    - pr
---
# PR #31 integrated: SSE keepalive comments + AG-UI resume event order

Date: 2026-11-06. Author: José Antonio Garrido (josegarridodigio), found while building an AG-UI app (CopilotKit -> `pando agui-serve` -> openai-compatible account on the Claudio desktop `/api/v1`). Merged into main with rebase; head commit 46eccf61.

## Bug 1: SSE keepalive comments (": ping") broke every OpenAI-protocol provider
- Symptom: `failed to process events: unexpected end of JSON input` once a slow model let the server send a ping (Claudio pings every 15 s).
- Cause: blank line closing a comment-only block was dispatched as an empty event. Present in openai-go v0.1.0-beta.2 ssestream AND in Pando's copied `largeBufferSSEDecoder` (`internal/llm/provider/copilot_sse.go`), which replaces the SDK decoder globally. Upgrading openai-go alone would not fix it.
- Fix: `largeBufferSSEDecoder.Next()` skips a blank line when the data buffer is empty (WHATWG spec). `newOpenAIClient` (`internal/llm/provider/openai.go`) now installs `sseNormalizeMimeMiddleware`, so `text/event-stream; charset=utf-8` still reaches Pando's decoder. Covers openai, openai-compatible, groq, openrouter, xai, ollama, llama.cpp, local, azure; Copilot shares the decoder.

## Bug 2: attached AG-UI response opened with STATE_SNAPSHOT
- Symptom: CopilotKit rejected resume after a frontend tool result with `First event must be 'RUN_STARTED'`; final answer never reached the page.
- Cause: `attachRun` (`internal/agui/server.go`, `first=false` branch: resumes, POST reattach, `GET /threads/{id}/stream`) wrote STATE_SNAPSHOT and the `pando.replayLossy` marker before the replay. First segment's RUN_STARTED never enters the replay buffer (runPrelude writes it directly).
- Fix: write RUN_STARTED first (moved from buffer front on resume, else rebuilt via `run.currentTranslator().Start()`), then STATE_SNAPSHOT, then lossy marker, then replay. Matches runPrelude order and PANDO-US-0016.

## Tests
- `internal/llm/provider/copilot_sse_test.go`: six SSE shapes x two content types, malformed payload still errors, end-to-end through `newOpenAIClient` with httptest server.
- `internal/agui/resume_order_test.go`: frontend-tool resume, reattach during first segment, reattach to lossy buffer.

## Verification at integration
- `go build ./...` ok; `go test -race ./internal/agui ./internal/llm/provider ./internal/llm/agent ./internal/api` ok on PR branch.
- New tests reverted against main code: all five fail, confirming they exercise the bugs.
- CI Build Matrix (none/enterprise) green on the PR.

Related: [[fix_agui_tool_calls_missing_event_stream]], [[fix_resumed_run_live_updates_webui_acp]], [[analysis_copilotkit_agui_integration]].
