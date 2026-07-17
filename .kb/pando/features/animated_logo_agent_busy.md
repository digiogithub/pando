---
created_at: 2026-07-17T13:26:48.515051658Z
updated_at: 2026-07-17T13:26:48.515051658Z
tags:
    - feature
    - tui
    - webui
    - logo
    - animation
---
# Feature: animated Pando logo while the agent is busy (2026-07-17)

## Motivation
Give a passive, always-visible activity indicator: when the coder agent (or one
of its subagent runs, which keep the parent run active) is working, the Pando
logo cycles through the "growth of a pando colony" glyphs instead of resting on
the static tree icon. Idle = tree icon.

## Frames
`本` (root/base) · `枝` (branch) · `葉` (leaf) · `林` (grove) · `森` (dense forest).
Idle glyph: TUI `styles.PandoIcon`, WebUI `木`.
Random frame every 2–3s; the next frame is drawn from the frames other than the
current one, so every tick is visibly different.

## TUI
- `internal/tui/page/maintabs.go`
  - `logoAnimFrames`, `logoAnimMinInterval`/`logoAnimMaxInterval` (2s/3s),
    `logoGlyphWidth = 2`.
  - `logoTickMsg` + `logoTickCmd()` — `tea.Tick` at a random delay in
    [2s, 3s], re-scheduled on every tick.
  - `mainTabBar.logoFrame` (-1 = idle), `AdvanceLogo(busy bool)`,
    `logoGlyph()` — pads the glyph to `logoGlyphWidth` because the CJK
    ideographs are 2 cells wide while the Nerd Font icon may be 1; without the
    pad the workspace tabs shift sideways on every frame swap.
  - `logoSegment` renders `b.logoGlyph()` instead of `tuistyles.PandoIcon`.
- `internal/tui/page/chat.go`
  - `Init()` batches `logoTickCmd()`.
  - `Update()` handles `logoTickMsg`: `AdvanceLogo(app.CoderAgent.IsBusy())`
    then re-schedules the tick.

## WebUI
- New `web-ui/src/hooks/useAnimatedLogo.ts` — exports `LOGO_IDLE_GLYPH`,
  `LOGO_ANIM_FRAMES`, `useAnimatedLogo()`. Busy = `sessionStore.isStreaming ||
  sessions.some(s => s.is_running)`. `setTimeout` self-rescheduling loop,
  cleared when busy flips false (frame reset to -1 = idle).
- `web-ui/src/components/layout/Header.tsx` — logo `<span>` renders
  `{logoGlyph}` and gets a 200ms opacity transition.

## Verification
- `go build ./...`, `go vet ./internal/tui/page/` clean.
- `npx tsc --noEmit` clean (web-ui).
- New `internal/tui/page/maintabs_test.go`: idle restore, no back-to-back frame
  repeat + all frames reachable, stable glyph cell width. `go test
  ./internal/tui/page/ -run 'TestAdvanceLogo|TestLogoGlyph'` passes.

Related: [[tui_icon_fallback]] (the icon set `PandoIcon` comes from).
