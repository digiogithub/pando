---
created_at: 2026-08-19T11:53:23.336705282Z
updated_at: 2026-08-19T11:53:23.336705282Z
tags:
    - change
    - webui
    - tui
    - sessions
    - orchestrator
---
# Fix: session default titles from prompt + full prompt in orchestrator task detail

## What changed
Sessions with placeholder titles ("", "New Chat", "New Session", "delegated task", "Generate a title") now get a prompt-derived fallback title, and the full task prompt is visible in orchestrator task details (TUI + WebUI).

## Backend
- `internal/session/session.go` — new exported helpers `IsDefaultTitle(title)` (placeholder set) and `TitleFromPrompt(prompt)` (first line, whitespace-collapsed, ≤100 runes + "…").
- `internal/llm/agent/agent.go` (`generateTitle`) — seeds the session title with `TitleFromPrompt(content)` immediately when the title is a placeholder, so a failed/missing title provider no longer leaves "New Chat"; a successful LLM title still overwrites it. Sessions titled `delegated: …` are left untouched.
- `internal/ipc/bridge/delegation_runner.go` — delegated session title is now `delegated: <corrId12> — <prompt snippet>` (snippet via `TitleFromPrompt`), falling back to whichever part exists.
- `internal/api/handlers_sessions.go` — `GET /api/v1/sessions` now returns `prompt_preview` (≤100-char first user prompt) and replaces/augments display titles retroactively: placeholder titles become the snippet; legacy `delegated: xxx` titles get ` — <snippet>` appended. Only queries messages for such sessions in the page.
- `internal/api/handlers_orchestrator.go` — `TaskResponse` gains `prompt` (full prompt; `name` stays the 80-char truncation).

## Frontend
- `web-ui/src/types/index.ts` — `Session.prompt_preview?`, `OrchestratorTask.prompt?`.
- `Sidebar.tsx`, `SimpleChatView.tsx` — session buttons carry `title={prompt_preview || title}` so hover shows up to 100 chars of the initial prompt.
- `orchestrator/TaskDetail.tsx` — new "Prompt" user-message-style box (border-left primary, `var(--selected)` bg) under the task header showing the full prompt.

## TUI
- `internal/tui/page/orchestrator.go` (`buildDetailContent`) — the prompt is now rendered inside a rounded-border box (full in detail mode, truncated in overview), below the truncated table column.

## Verification
- `go build ./...`, `go vet` on touched packages — clean.
- `go test ./internal/api ./internal/session ./internal/ipc/bridge ./internal/tui/page ./internal/llm/agent` — pass.
- `npx tsc --noEmit` in web-ui — clean.
