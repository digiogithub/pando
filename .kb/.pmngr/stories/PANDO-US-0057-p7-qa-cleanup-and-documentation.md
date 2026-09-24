---
id: PANDO-US-0057
type: story
title: P7 QA, cleanup and documentation
status: done
priority: medium
parent: PANDO-EP-0010
milestone: PANDO-M-0003
author: mcp
labels: [webui, design, qa]
estimate: 3
created: 2026-09-24T21:01:11Z
updated: 2026-09-24T22:10:13Z
started: 2026-09-24T21:45:26Z
closed: 2026-09-24T22:10:13Z
---

## Description

- Remove the `@fortawesome/*` dependencies and any leftover usages.
- Drop the legacy token aliases once nothing references them.
- Run a visual review: screenshots of chat, settings, editor and terminal in light and dark mode, at desktop and mobile widths.
- Run the contrast script.
- Run typecheck, lint, build and build:embedded.
- Write docs and a KB summary.

## Acceptance Criteria

- [ ] No FontAwesome imports remain.
- [ ] All build and quality commands pass.
- [ ] Screenshots have been reviewed.
- [ ] The KB entry `pando/features/webui-native-redesign.md` exists.
