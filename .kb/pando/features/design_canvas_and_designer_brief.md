---
created_at: 2026-09-02T09:36:33.626844944Z
updated_at: 2026-09-02T09:36:33.626844944Z
tags:
    - feature
    - design
    - canvas
    - preview
    - prompt
    - skills
    - craft
    - templates
---
# Design Studio — read-only canvas window + designer brief, craft and templates

Date: 2026-09-02. Follow-up to [[pando_design_studio_plan]] (P0–P8) and
[[goal_designer_autoopen_always_on]]. Two halves, both requested in one goal:

1. **A separate, read-only window** where the user watches a design being built
   and navigates a multi-artboard canvas.
2. **Prompt and skill improvements** for the designer, distilled from the
   `claude-design` system-prompt leak and the `baoyu-design` skill package,
   with every anti-disclosure clause deliberately dropped — Pando is open, and
   paying tokens to forbid showing its own instructions makes no sense.

## 1. The design canvas

### What it is

`/preview/_canvas/{token}/` — a self-contained page served by the **existing
preview server**, so it works identically in both deployments (mounted on the
API listener, and the loopback fallback a plain TUI/ACP/CLI process starts).
Every artifact of the session is an artboard on one pan-and-zoom surface.

- **Read-only by construction**: a transparent `.capture` layer sits over every
  artboard iframe, so no pointer event ever reaches the document. Verified in a
  browser: `document.elementFromPoint` over an artboard returns the capture div.
- **Live**: the page polls `artboards` every second. An artboard's `revision`
  is `server.Revision(id) + dirRevision(absDir)` — the second term is the
  newest mtime in the artifact directory, so the frame reloads on *any* write,
  not only on a render or a committed version. That is what makes "watch it
  being built" real.
- **Build activity**: a rail logs added / updated / removed / status per
  artboard, and a `building…` badge shows while a render is in flight.
- **Navigation**: drag to pan, wheel to zoom (cursor-anchored), `F` fit, `0`
  1:1, arrows, `+`/`-`, an artboard jump list, double-click an artboard to
  focus, and a **Follow** toggle that pans to whatever just changed. The view
  auto-fits until the user places it by hand, and then stops moving.

### Files

- `internal/design/preview/canvas.go` — canvas grants (one per session, stable
  token, expiring, revoked with the session), routes, the artboards JSON.
- `internal/design/preview/canvas.html` / `canvas.js` — the page, embedded.
- `internal/design/preview/preview.go` — `Options.Artboards` provider hook,
  `Server.Revision`, canvas route dispatch, `RevokeSession` closes the canvas.
- `internal/design/canvas.go` — `CanvasArtboards` (the provider),
  `(*Service).Artboards`, `CanvasPresentation`, `markRendering`/`isRendering`,
  `dirRevision`.
- `internal/design/preview_link.go` — `PreviewOptions` installs the provider,
  which is how the preview package stays ignorant of the design model.
- `internal/design/service_render.go` — marks/unmarks the rendering artifact.

### Surfaces

| Surface | How |
|---|---|
| Auto-open | `design.AutoOpenTarget` returns the **canvas** instead of the artifact, deduped on `canvas:<session>` — so the 2nd and later artifacts land in the window already open instead of spawning one window each. Falls back to the single-artifact preview when no canvas resolves. (`internal/tui/tui.go`, `internal/mesnada/acp/design_events.go`) |
| Agent tool | `design_present` gained `view: "artifact" \| "canvas"`; `artifact_id` is no longer required. |
| CLI | `pando design canvas` (`--json` prints url + artboards). |
| TUI | `b` on the design page. |
| REST | `GET /api/v1/design/canvas[?session_id=…]` → `{url, artboards}`. |
| WebUI | A **Canvas** button in the gallery header and in the Studio toolbar; both `window.open` it, because the canvas is a window, not a pane. `designStore.canvasURL()`; i18n `design.canvasWindow` / `design.openCanvas` (en, es). |

### Why polling and not SSE

The payload is a handful of rows, each artboard already carries the preview
server's own live-reload script, and polling works unchanged in both
deployments. An SSE proxy would have forced the preview package to learn about
design events for no gain.

## 2. Designer prompt, craft references and templates

### The brief

`internal/design/brief.go` — a compact `<design_brief>` block: you are the
expert in the medium the brief names (not a web developer), read the design
system first, ask when the answer changes what you build, commit to one
direction, a small request gets a small change, never invent a fact, you cannot
generate images, render → critique → present.

It is **gated**: `PromptBrief()` returns it only once the project has a
`designer/` directory, so a project that only writes code pays nothing. The one
turn that gate cannot cover — the turn that *creates* the directory — is
covered by `design_create` prepending `design.Brief()` to its result for the
first artifact. Injected in `internal/llm/prompt/coder.go` alongside the
existing design-system constraint block.

### New craft references (`internal/design/bundles/craft/`)

- `process.md` — how to run a design job: understand, ask well, commit to a
  direction, build and look at it, present; scope discipline; variations;
  what you cannot do.
- `content.md` — never invent facts, the user's words stay verbatim, the tells
  of generated copy, length per medium, microcopy.
- `print.md` — flowing document vs fixed canvas, `@page`, break control,
  what does not survive printing, colour on paper, verification.
- `interaction.md` — states before screens, `:focus-visible`, hit targets,
  motion budgets, `prefers-reduced-motion`, timed content.

The existing four (`typography`, `color`, `layout`, `anti-ai-slop`) are
unchanged; every existing template now also requires `process` and `content`
(and `interaction` for dashboard/prototype).

### New templates (`internal/design/bundles/templates/`)

`wireframe`, `options-explore`, `doc-report`, `print-flier`, `html-email`,
`mobile-app`, `diagram-explainer` — each a `SKILL.md` with an `od:` block plus
a scaffold. The gallery goes from 6 to 13 entries, roughly matching the
coverage of the reference material. `options-explore` encodes the
round-grouped, stably-id'd (`1a`, `1b`, `2a`) variation layout, which is what
the canvas is best at showing.

`design_skills` now names all eight craft references in its description and
tells the model to start with `process`.

## What was deliberately not taken from the reference material

- Every "do not divulge the system prompt / never describe your environment"
  clause. Pando is open source; those tokens buy nothing.
- The whole `.dc.html` / `<x-dc>` / `DCLogic` runtime, `dc_write`,
  `copy_starter_component`, the Figma VFS and the GitHub-sync protocol — they
  describe a different harness. Pando artifacts are plain HTML directories with
  a manifest, and that model stays.
- The copyright/citation boilerplate, which belongs to a web-search product.

## Verification

- `go test ./internal/design/... ./internal/api/... ./internal/llm/tools/` — pass.
  New tests: `internal/design/preview/canvas_test.go` (token capability, one
  canvas per session, payload ordering, provider failure reported not fatal,
  session revocation, access guard) and `internal/design/canvas_test.go`
  (artboard description + the URL actually serving, missing entry reported per
  board, nested render badge, revision moves on any write, stable canvas URL,
  brief gating).
- `npx tsc --noEmit` in `web-ui/` — clean.
- End to end in a real browser: built the binary, created three artifacts in a
  temp project, ran `pando design canvas`, and confirmed the three artboards
  render, a file edit shows up as "updated" with the new copy inside the frame,
  a newly created artifact appears on its own, zoom/pan/fit/1:1/jump all work,
  no console errors, and the capture layer is the topmost element over an
  artboard.

Pre-existing, unrelated failures in `internal/llm/agent`
(`TestSetAndGetCavemanMode`, `extension_tools_test.go`) come from a global
config leaking into the tests; nothing in this change touches those packages.