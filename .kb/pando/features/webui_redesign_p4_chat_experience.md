---
created_at: 2026-09-24T21:44:59.145644624Z
updated_at: 2026-09-24T21:44:59.145644624Z
tags:
    - feature
    - webui
    - redesign
---
# WebUI redesign P4: chat experience (PANDO-US-0054)

This is part of [[pando/plans/webui_native_redesign_plan.md]] and builds on [[pando/features/webui_redesign_p1_p2_foundations.md]].

## What changed
- New `web-ui/src/styles/chat.css`, imported from `index.css`. It now holds the shared `.markdown-content` prose rules, which moved out of `index.css`, and a highlight.js theme built from tokens (`--hl-*` variables). The static `highlight.js/styles/github-dark-dimmed.css` import is gone. The old `.msg-user-*` and `.msg-assistant-*` mobile rules were removed.
- **MessageBubble**:
  - No avatars. User messages are a right-aligned `--bg-raised` bubble, with image parts shown as thumbnails above it. Assistant messages are plain 15px prose.
  - New `AssistantTurn` merges consecutive assistant messages (one per agent-loop step) into one turn.
  - Consecutive tool calls and thinking collapse into `ActivityGroup` with a summary like "Ran 13 commands · searched 1 time · called 5 tools" (i18n plurals `chat.activity.*`), a chevron, a live spinner and a failure count. A single event renders as its own compact `EventRow`.
  - Code blocks get a header with the language and a copy IconButton.
  - Copy and time actions appear on hover.
  - The streaming indicator is a "Thinking" shimmer or animated dots.
- **MessageList**:
  - Groups messages into turns and uses a centered column (max-width 740px of text).
  - Auto-scroll follows only when the reader is at the bottom. A floating "Scroll to bottom" pill appears otherwise.
  - Exports `ChatEmptyHead` and `ChatSuggestions` for the empty state (木 mark, "What should we work on?", 4 suggestion chips that insert prompts through `chatDraftStore`).
- **ChatInput**:
  - Pill composer (`--radius-xl`, focus-within ring).
  - Model chip that opens ModelSwitcher.
  - Circular accent send button. While streaming, a stop button shows, plus send when there is text (steering).
  - Meta row: cwd, context meter or token count, char limit warning.
  - Slash commands and keyboard handling are unchanged.
- **Empty state**: ChatView and SimpleChatView render head, composer and suggestions in a `.chat-pane--empty` pane. The composer keeps the same React slot, so it never remounts and focus survives sending.
- **SlashCommandMenu**: menu-styled listbox with role option and aria-selected.
- **PermissionDialog and QuestionDialog**:
  - Both use the `Dialog` primitive: not dismissible, with Button footers.
  - QuestionDialog strings were hardcoded Spanish; they are now i18n (`chat.question.*`).
- **Restyled with primitives and tokens**:
  - PlanView, GoalStatus (Badge), FileChangesBar and ChatInfoSidebar.
  - ChatInfoSidebar sits on `--bg-shell` with quiet section headers. Its collapsed tab is an IconButton. Mobile drawer CSS moved into chat.css.
  - DiffViewer: the Monaco theme is built from live tokens and remounts when the theme changes.
- **Icons and i18n**:
  - All FontAwesome imports were removed from `components/chat`. `icons.ts` gained VenetianMask, Bug, FilePlus, FilePen, PanelRightClose, Hourglass, Lightbulb and Gavel.
  - New `chat.*` i18n keys were added in all 7 locales.

## Bug fix (ChatView)
Sending the first message of a new session blanked the chat until reload.

- **Cause:** `onNewSession` set `activeSessionId`. The load effect then called `setActiveSession()`, which cleared the messages and loaded a server copy that had not been persisted yet.
- **Fix:** a new `createdSessionRef` skips that reload for the session created by our own run.

## Verification
- `bun run typecheck` clean. `bun run lint` shows no errors in `components/chat`.
- Checked with headless Chrome through puppeteer-core in light, dark and 390px mobile views:
  - The advanced chat on real sessions, including grouped tool calls, an expanded edit diff, code blocks and tables.
  - The empty state, the simple view, the slash menu, and the permission and question dialogs (injected store state).
  - A live send with openrouter qwen, covering streaming and the done state.
