---
id: PANDO-US-0052
type: story
title: P2 UI primitives library and Lucide icon module
status: done
priority: high
parent: PANDO-EP-0010
milestone: PANDO-M-0003
author: mcp
labels: [webui, design]
estimate: 5
created: 2026-09-24T21:01:11Z
updated: 2026-09-24T21:17:37Z
started: 2026-09-24T21:01:56Z
closed: 2026-09-24T21:17:37Z
---

## Description

Create `web-ui/src/components/ui/` with these primitives:
- Button (primary, secondary, ghost, danger; sm and md sizes)
- IconButton, Input, Textarea, Select, Switch, Checkbox
- Card, Dialog (overlay with focus trap), Popover/Menu, Tabs/SegmentedControl
- Badge, Tooltip, Kbd, Spinner, Divider
- SettingsSection/SettingsRow, EmptyState

Style them with token-based classes in `styles/ui.css` or with Tailwind utilities, not inline style objects.

Add `lucide-react` behind a small `components/ui/icons.ts` re-export module. That module is the single place icons are imported from.

## Acceptance Criteria

- [ ] All primitives are keyboard accessible and show visible focus rings.
- [ ] They look correct in every family, in both light and dark mode.
- [ ] They are exported from `components/ui/index.ts`.
- [ ] typecheck and lint pass.
