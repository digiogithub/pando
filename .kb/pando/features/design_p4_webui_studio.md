---
created_at: 2026-08-26T20:47:48.57194793Z
updated_at: 2026-08-26T20:47:48.57194793Z
tags:
    - feature
    - design
    - webui
    - react
    - studio
    - sse
---
# Design Studio P4 — WebUI Design section (gallery + three-column Studio)

Implemented 2026-08-26. Phase P4 of [[pando_design_studio_plan]], on top of
[[design_p3_preview_server_surfaces]] (preview server, SSE, guard) and
[[design_p2_tools_patch_engine]].

## What was built

### Frontend (React + Zustand, `web-ui/`)

New `src/components/design/`:

- **`DesignView.tsx`** — the section. `/design` is the gallery, `/design/:id` the Studio;
  the **route drives the store**, never the reverse, so a Studio URL survives a reload and
  can be shared (the same property the preview URLs have). Renders a dedicated
  "subsystem is off" screen with the config snippet when `design.enabled` is false.
- **`ArtifactGallery.tsx`** — cards with a **live screenshot** rather than a stored
  thumbnail. Versions are directory snapshots, not image archives, so there is no thumbnail
  to store — and rendering on demand means a card can never show a picture of a design that
  no longer exists on disk. `loading="lazy"` plus an `onError` that hides the image keeps
  the card usable on a machine with no headless browser (503).
- **`DesignStudio.tsx`** — the three-column workspace (chat | canvas | inspector), toolbar
  with Render / open-external / export, and a `(max-width: 1024px)` breakpoint that gives one
  pane at a time with tab switching, matching the existing WebUI mobile rules
  (cf. [[fix_webui_mobile_settings_master_detail]]). The right column tabs between the
  inspector and the version timeline.
- **`PreviewFrame.tsx`** — the canvas. An iframe over the preview server's own document, not
  a re-rendering in React: what the user sees is exactly what a browser opened at that URL
  would show. Speaks the `pando-design` postMessage protocol both ways — `selected`/`slide`
  in, `select`/`goToSlide`/`clearSelection` out. `key={url}#{nonce}` remounts the frame on a
  new version instead of trusting the browser to re-request a document it thinks it has.
- **`InspectorPanel.tsx`** — the stored structure index with a text/selector/role filter, the
  selected node's computed styles (fetched lazily per node, since styles are the largest part
  of a node), and the button that turns a selection into prompt context.
- **`VersionTimeline.tsx`** — history newest-first with checkout. The button is labelled and
  hinted plainly rather than being a hover-only undo: a checkout rewrites files in the user's
  tree, even though the revert is directory-scoped and cannot touch anything else.
- **`SlideStrip.tsx`** — deck navigation. **Numbers, not thumbnails, on purpose**: a
  thumbnail per slide would mean one screenshot round trip per slide on every render, the
  most expensive thing the Studio could do to a deck under active iteration.
- **`ExportMenu.tsx`** — HTML/PNG/PDF. Export and download are two requests: routing a
  multi-megabyte PDF through the JSON response that reports where the file landed would make
  the Studio feel hung.

New stores in `packages/pando-client/src/stores/`:

- **`designStore.ts`** — status, artifacts, versions, nodes, selection, slide, render,
  checkout, export, and the `design.*` SSE subscription. A `design.version`/`design.render`
  event for the open artifact re-reads versions **and** the node index and bumps
  `reloadNonce`: the document on disk moved under the canvas, and a click would otherwise
  resolve against stale ids.
- **`chatDraftStore.ts`** — a one-shot mailbox so a surface outside the composer can push
  text into it without owning it. This is how a preview click becomes
  `design://n42 (#hero > h1)` in the prompt. Queued fragments accumulate rather than
  overwrite, and `ChatInput` **appends** rather than assigns, so a half-written message is
  never destroyed by a click somewhere else. `ChatInput.tsx` gained ~8 lines; zero behaviour
  change when nothing is queued.

Wiring: `App.tsx` routes (`design`, `design/:id`), `Sidebar.tsx` top-level "Design" entry
(`faPalette`, placed before Code Editor), i18n `nav.design` in all 7 locales plus a full
`design.*` block in `en` and `es` (the rest fall back to `en`, which is the configured
`fallbackLng`).

### Backend additions

- **`GET /api/v1/design/status`** — the one design route registered **unconditionally**. The
  Web UI has to be able to ask whether the section exists, and a 404 is not an answer it can
  tell apart from an older server. Reports `enabled`, `preview` + `preview_reason` (so an
  exposed-without-auth listener explains itself in the canvas instead of showing a blank
  frame), `renderer`, `kinds`, `output_dir`.
- **`preview.Options.FrameAncestors`** — the CSP `frame-ancestors` list is now configurable,
  defaulting to `'self'`. Correct for the mounted deployment, where preview and UI share an
  origin; a shell that runs the UI on its own origin (the Wails desktop app, P5) must widen
  it, and the decision belongs to the caller that knows its own origin.

### Two real bugs the integration test found

1. **`GET …/nodes` answered 404 for an artifact that had never been rendered.** The design
   service deliberately wraps that condition in `ErrNotFound` so an agent is told to render
   first — but a UI panel wants "no index yet", not an error. Fixed with a distinct sentinel
   **`design.ErrNoIndex`** (in `store.go`); the tool layer maps it to its own message, and
   `handleDesignNodes` turns it into an empty `InspectResult` with 200. The two callers can
   only tell the cases apart if they are different errors.
2. **`GET …/versions` answered 200 with an empty list for an artifact that does not exist.**
   An empty list is a legitimate answer and was hiding the typo. `Service.Versions` now
   verifies the artifact first, which is also the right answer for `design_versions list`.

## Verification

- Frontend: `bun run typecheck` clean, `bun run lint` clean (the 4 remaining warnings are
  pre-existing, in `KeyValueEditor.tsx` and `ModelCombobox.tsx`, untouched), `bun run build`
  succeeds. There is **no frontend test harness in this repo** (no vitest/jest anywhere), so
  adding one was treated as out of P4's scope; the contract is tested from the Go side
  instead.
- Go: `go build ./...`, `go vet`, `gofmt` clean. `go test` green for `./internal/design`,
  `./internal/design/preview`, `./internal/api`, `./internal/llm/tools`, `./internal/app`,
  `./internal/config`, `./internal/db`, `./cmd`.
- **`TestDesignStudioHTTPLoop`** — the whole Studio loop over the real handlers, a real
  `design.Provider`, a real migrated SQLite DB and a real snapshot service in a temp project:
  status → gallery list → open (asserting the bridged URL and the entry) → fetch that URL
  through the mounted route and confirm it serves the artifact *with* the bridge → version
  timeline → checkout → invalid version refused (400) → empty index returns 200 → export HTML
  → download it and confirm `Content-Disposition: attachment` and the artifact's own content.
- **`TestDesignStudioDeckLoop`** (real Chrome, skipped without one) — the deck half: render
  fills the slide count the strip is built from, the render response does **not** repeat the
  node array the inspector pages separately, the artifact reports 3 slides, the index carries
  slide attribution, and a PDF export downloads starting with `%PDF` and with no
  missing-print-CSS warning.
- **`TestDesignRoutesRejectUnknownArtifacts`** — 404 on artifact/versions/nodes for an id
  that does not exist.
- **`TestDesignStatusReportsWhyThePreviewIsUnavailable`** — off reports off with the kinds
  still listed; on-but-exposed-without-auth reports `enabled: true`, `preview: false` and an
  actionable reason.
- **`TestFrameAncestorsIsConfigurable`** — default `'self'`, override honoured.

## P4 exit criterion

Met at the contract level for both v1 kinds: the create → iterate → export loop is exercised
end-to-end through the exact endpoints the Studio calls, with the deck path proven against
real Chrome. The UI layer on top is typechecked, linted and building.

## Known limits

- Gallery thumbnails cost one headless render per visible card (`Cache-Control: no-store` on
  the screenshot route). Lazy loading bounds it to what is on screen; a cached or persisted
  thumbnail is worth revisiting if a project accumulates many artifacts.
- No frontend unit tests, because the repo has no harness for them.
- The desktop shell frames the preview from its own origin, so P5 must pass
  `FrameAncestors` — the option exists and is tested, but nothing sets it yet.
- Artifact creation from the UI is deliberately absent: artifacts are created by asking the
  agent, which is decision 2's "select and ask" applied to the gallery as well.

## Next — P5

Desktop + TUI + ACP + CLI surfaces: `pando design` subcommand (`create/list/open/versions/
export/system extract`, `--json`), Wails menu entry + open-external + the `FrameAncestors`
widening, TUI `design` page (list, versions, inline screenshot, `o` open, `d` diff), and ACP
`resource_link` + `ImageBlock` per version with `/design*` slash commands.

Related: [[pando_design_studio_plan]] · [[design_p3_preview_server_surfaces]] ·
[[design_p2_tools_patch_engine]] · [[project_webui_implementation_plan]] ·
[[fix_webui_mobile_settings_master_detail]] · [[feature_realtime_context_token_counter]]
