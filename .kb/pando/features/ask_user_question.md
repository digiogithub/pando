---
created_at: 2026-06-16T18:25:15.593872192Z
updated_at: 2026-06-16T18:25:15.593872192Z
tags:
    - feature
    - tools
    - tui
    - webui
    - acp
    - userinput
---
# AskUserQuestion Tool

Status: **Implemented (2026-06-16)** — all 6 phases complete. Same name/parity as Claude Code's `AskUserQuestion`.

## What it does
Lets the agent ask the user one or more questions with selectable options mid-task and wait
for the answer. Each question has a short `header` (chip label), the `question` text, 2-4
`options` ({label, description}), and an optional `multiSelect` flag. The user can pick
option(s) or type free text via an automatic "Other" answer. Returns structured selections
back to the model (not a bool — this is the key difference vs the permission system it mirrors).

Guidance baked into the tool description: use only for decisions the agent can't resolve itself
(no obvious default / unverifiable facts); 1-4 questions, 2-4 options each; header <= 12 chars.

## Architecture (mirrors internal/permission)
The tool blocks on a channel, publishes a pubsub event, the frontend shows an overlay and
responds. Service: `internal/userinput` (`Ask`/`Respond`/`Cancel`/`PendingRequests`/`Subscribe`,
`NewService()`). Types: `Option{Label,Description}`, `Question{ID,Header,Question,MultiSelect,Options}`,
`QuestionRequest{ID,SessionID,Questions}`, `Answer{QuestionID,Selected,OtherText}`,
`AskResponse{Answers,Cancelled}`, `CreateAskRequest`.

`App.UserInput = userinput.NewService()` in app.go, propagated to the agent and registered as the
tool. Tool lives at `internal/llm/tools/ask_user_question.go` (`AskUserQuestionToolName = "AskUserQuestion"`),
registered in `CoderAgentTools`/`CoderAgentToolsWithMesnada` and in `alwaysIncludedTools`.

## Per-frontend behavior
- **TUI**: blocking overlay `internal/tui/components/dialog/ask_question.go` (`AskQuestionDialogCmp`:
  ↑/↓ navigation, single/multi-select, "Other" via textinput, summary view, emits
  `QuestionResponseMsg`). Wiring in `internal/tui/tui.go` (overlay mirroring the permission one,
  key-blocking, mouse guard, help section). Subscriber `userinput` registered in `cmd/root.go`.
- **Web UI**: backend SSE event `question_request` in `handlers_chat.go` (`writeQuestionRequest`,
  subscribe + replay of `PendingRequests` on connect) and `POST /api/v1/questions/respond`
  (`handlers_questions.go`, route in `routes.go`). Frontend: types + SSE parser
  (`types/index.ts`, `services/sse.ts`), `pendingQuestions` store + actions in `sessionStore.ts`,
  handler in `useChat.ts`, component `QuestionDialog.tsx` mounted in `MainLayout.tsx`. Embedded
  bundle rebuilt with `bun run build:embedded` (dist gitignored).
- **ACP**: no blocking selection UI → text mode. The tool formats the questions as numbered
  markdown and ends the turn; the user answers in writing (`formatQuestionsAsText`). Detected via
  `acp.ACPModeContextKey{}` set in `internal/mesnada/acp/prompt_handler.go` before
  `agentService.Run` (flows through agent `genCtx` into each tool's Run). The tool also honors the
  legacy `tools.ACPClientConnContextKey` for parity with view/write/bash. NOTE: the acp package
  cannot import tools (tools→acp is the existing one-way dep), so the mode key is defined in the
  acp package and read from tools, avoiding an import cycle.

## Config
Enabled by default. Disable entirely via `[InternalTools] AskUserQuestionDisabled = true`
(field `InternalToolsConfig.AskUserQuestionDisabled`, default-false = enabled). Gated in
`tools.go` via `maybeAskUserQuestionTool(userInput)` / `askUserQuestionEnabled()`.

## Tests
`internal/llm/tools/ask_user_question_test.go`: ACP-conn mode, ACP-mode-key mode, UI blocking
+ structured metadata, cancel, validation. Plus `internal/userinput` service tests. Verified:
`go build ./...`, `go vet`, `go test ./internal/userinput ./internal/llm/tools ./internal/llm/agent ./internal/api ./internal/mesnada/acp`, web-ui `tsc --noEmit`.

KB plan: `pando/plans/ask_user_question_tool_plan.md`.
