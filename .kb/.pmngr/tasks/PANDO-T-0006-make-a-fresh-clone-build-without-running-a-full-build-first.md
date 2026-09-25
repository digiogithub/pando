---
id: PANDO-T-0006
type: task
title: Make a fresh clone build without running a full build first
status: done
priority: medium
author: claude
labels: [build, dx, ci]
estimate: 2
created: 2026-09-15T00:00:00Z
updated: 2026-09-25T10:57:10Z
closed: 2026-09-25T10:57:10Z
---

## Description

`go build ./...` fails on a fresh clone of this repository:

```
internal/api/ui_assets_app.go:8:12: pattern webui/dist/**: no matching files found
internal/desktop/embed_binary.go:8:12: pattern bin/pando-desktop: no matching files found
```

Both embed build artifacts that are gitignored, so the patterns only resolve on a machine that has
already run a full build. Anyone cloning the repository hits this, and so does any CI job that
compiles the module, which is how it was found: the SDK's new HITL round-trip job failed on it
(PANDO-T-0001) while the same command was green on every developer machine in the session.

The `Makefile` already solves it — `make embed-stubs` writes placeholder files and never overwrites
a real asset — but nothing points a newcomer at it and nothing runs it automatically. The fix is to
make the stubs happen without being told: a `go:generate` step, a prerequisite on the build
targets, or at minimum a line in the README's build section.

## Acceptance Criteria

- [ ] `git clone && go build ./...` succeeds with no prior build and no manual step.
- [ ] A real webui or desktop asset is still never overwritten by whatever mechanism is chosen.
- [ ] The README's build instructions match what a newcomer actually has to do.

## Notes

Found 2026-09-15 while closing PANDO-T-0001. Not a regression: it has presumably been true for as
long as the embeds have existed, and stayed invisible because everyone who builds has built before.
