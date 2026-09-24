---
id: PANDO-US-0056
type: story
title: "P6 Secondary views: editor, terminal, orchestrator, logs, snapshots and the rest"
status: done
priority: medium
parent: PANDO-EP-0010
milestone: PANDO-M-0003
author: mcp
labels: [webui, design]
estimate: 8
created: 2026-09-24T21:01:11Z
updated: 2026-09-24T21:41:01Z
started: 2026-09-24T21:19:14Z
closed: 2026-09-24T21:41:01Z
---

## Description

Migrate the remaining views to the tokens, primitives and lucide icons:
- editor (the Monaco theme is derived from tokens and switches live)
- terminal (the xterm theme comes from tokens)
- orchestrator, logs, snapshots, evaluator, design, projects, instances, extensions, agentvcs
- overlays/QuickMenu, splash, auth
- `components/shared/*`

## Acceptance Criteria

- [ ] Every route renders cleanly in light and dark mode.
- [ ] The Monaco and xterm themes follow a mode switch without a reload.
- [ ] typecheck passes.
