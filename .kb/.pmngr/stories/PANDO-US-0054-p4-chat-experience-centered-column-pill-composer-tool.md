---
id: PANDO-US-0054
type: story
title: "P4 Chat experience: centered column, pill composer, tool activity rows, no watermark"
status: done
priority: high
parent: PANDO-EP-0010
milestone: PANDO-M-0003
author: mcp
labels: [webui, design, chat]
estimate: 8
created: 2026-09-24T21:01:11Z
updated: 2026-09-24T21:45:26Z
started: 2026-09-24T21:19:14Z
closed: 2026-09-24T21:45:26Z
---

## Description

Redesign `components/chat/*`:
- **Messages**: no avatars. User messages are a rounded bubble, right-aligned, on the raised surface. Assistant messages are plain prose (15px, line height 1.65) in a centered column of about 740px.
- **Tool calls**: collapse them into muted summary rows with a chevron to expand, e.g. "Ran 3 commands · edited 2 files".
- **Scrolling**: add a floating "Scroll to bottom" pill.
- **Composer (ChatInput)**: a rounded pill with a textarea, a model or agent chip, attach, and a circular send/stop button. Below it, faint meta text for the workspace and branch.
- **Empty state**: a centered greeting with a small Pando mark and the composer centered. Remove the mascot watermark from MainLayout and SimpleChatView.
- Also restyle Permission and Question dialogs, SlashCommandMenu, PlanView, FileChangesBar, DiffViewer, GoalStatus and ChatInfoSidebar.
- Markdown and code blocks get a header bar with the language and a copy button. highlight.js colours come from tokens.

## Acceptance Criteria

- [ ] Chat in both SimpleChatView and ChatView matches the reference look in light and dark mode.
- [ ] No existing chat behaviour regresses: streaming, permissions, questions, slash commands, attachments, mobile.
- [ ] typecheck passes.
