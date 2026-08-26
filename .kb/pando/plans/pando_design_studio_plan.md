---
created_at: 2026-08-26T16:22:45.007330765Z
updated_at: 2026-08-26T16:31:35.799125528Z
tags:
    - plan
    - design
    - webui
    - tui
    - acp
    - desktop
    - skills
---

# Pando Design Studio — Implementation Plan

Goal: give Pando a **Design** capability comparable to Claude Design / OpenDesign, built on
Pando's *existing* runtime (tools, skills, subagents, memory/KB, snapshots, browser
automation), reachable from **every surface**: TUI, ACP (Zed/VS Code/JetBrains), WebUI and
the Wails desktop app.

Scope decision: this is a **focused `internal/design` package**, not a generic artifact
runtime. Design artifacts are HTML/CSS/JS. Generalizing into a universal Artifact Runtime
stays a possible later refactor, explicitly out of scope here.

Related: [[project_webui_implementation_plan]] · [[project_acp_implementation_plan]] ·
[[project_snapshot_plan]] · [[project_desktop_wails_plan]] · [[feature_image_optimization_tool_results]] ·
[[plan_mesnada_swarm_verifier_gate]] · [[project_persona_system]] · [[plan_kb_wiki_links]] ·
[[feature_external_access_footer_toggle]] · [[fix_agentvcs_baseline_delta]]

---

## 0. Locked direction decisions (2026-08-26)

| # | Question | Decision |
|---|---|---|
| 1 | Generic Artifact Runtime vs narrow feature | **`internal/design`**, narrow. No generic runtime in v1. |
| 2 | Direct manipulation canvas (drag/resize/inline edit) | **Not in v1.** Select-and-ask-the-agent only. |
| 3 | Version storage | **On top of `internal/snapshot` + agentVCS**, scoped to the artifact dir. |
| 4 | Where artifacts live | **In the user's working tree** (committable), not `.pando/`. |
| 5 | Remote/headless preview | **External-access path** (`0.0.0.0` toggle + basic auth), no IPC tunnelling. |
| 6 | Critic model | **The model selected as coder**; critic differs by persona/prompt, not by model. |
| 7 | Skill frontmatter namespace | **`od:` verbatim**, for format interop with OpenDesign templates. |
| 8 | Image generation | **Via browser canvas rendering** (headless Chromium rasterizes), no image-model provider. |
| 9 | Export formats v1 | **HTML, PNG, PDF only.** No PPTX, no MP4. |
| 10 | Artifact kinds in v1 | **`web` (prototype) and `deck`.** mobile / document / dashboard / diagram deferred. |
| 11 | Entry points | **`pando design` CLI subcommand** + `/design` slash command + **top-level "Design" nav entry** in the WebUI. |
| 12 | Skill/template bundles | **Pando-authored bundles only.** No vendoring of third-party bundles; we speak the `od:` format, we do not ship their content. |
| 13 | Default artifact directory | **`designer/<slug>/`** in the project root (avoids the existing `design/` examples dir). Design system in `designer/_system/`. |

---

## 1. What already exists in Pando (reuse inventory)

Verified in the tree (2026-08-26):

| Capability | Where | Reuse for Design |
|---|---|---|
| Headless Chromium (chromedp v0.14) with per-session allocator, console + network buffers | `internal/llm/tools/browser_session.go`, `browser_*.go` | render / screenshot / DOM inspect / PDF export / canvas rasterization |
| Screenshot → model images pipeline (`imageopt`, tool-result images in TUI + WebUI SSE) | `internal/imageopt`, `handlers_chat.go` | critic loop feedback, TUI preview |
| Skills subsystem (Claude-Code-compatible `SKILL.md`, 3 load levels, user/project roots, catalog installer + `skills-lock.json`) | `internal/skills/` | design skills, design templates, craft rules (`od:` frontmatter) |
| KB + memory (`kb_add_document`, wiki `[[links]]`, hybrid search) | `internal/rag`, kb tools | design-system persistence, brand memory, precedents |
| Subagents/orchestration (mesnada, swarm blackboard, verifier gate, conclusion gate) | `internal/mesnada/` | designer ↔ critic ↔ a11y passes |
| Snapshots + agentVCS | `internal/snapshot`, `internal/agentvcs` | **artifact versioning / `checkout v2`** (decision 3) |
| HTTP API + SSE + embedded WebUI + basic auth + external-access toggle | `internal/api/`, `web-ui/` | Design page, preview server, live reload, remote preview (decision 5) |
| ACP server with rich tool render, `ResourceLinkBlock`, `ImageBlock` | `internal/mesnada/acp/` | preview URL + screenshots inside Zed |
| Extensions with frontend overlay FS + panels | `internal/extensions/frontend.go` | Design ships as a core page, extensible with plugin panels |
| Design-system markdown already in repo (`design/claude.md`, `clay.md`, `starbucks.md`) | `design/` | seed corpus for the `DESIGN.md` contract |
| Desktop shell (Wails) embedding the same WebUI | `desktop/`, `internal/desktop/` | Design page for free once WebUI has it |

Gaps: no design-artifact model, no version binding to snapshots, no preview server, no
element selection protocol, no design-system compiler, no design skills, no Design UI.

## 2. Target architecture

```text
   TUI          ACP client (Zed)      WebUI / Desktop      pando design (CLI)
    │                  │                     │                     │
 open URL     resource_link + images   "Design" nav page     create/list/export
    └──────────────────┴──────────┬──────────┴─────────────────────┘
                                  │
                    ┌─────────────▼──────────────┐
                    │  Design Surface (HTTP+SSE) │  internal/api
                    │  /design, /preview/{id}/*  │
                    └─────────────┬──────────────┘
                                  │
        ┌─────────────────────────▼─────────────────────────┐
        │              internal/design                      │
        │  model · workspace · versions(snapshot) · patches │
        └───┬────────────┬───────────────┬──────────────┬───┘
            │            │               │              │
     Design System   Renderer        Inspector      Exporter
     (system.go)     chromedp        DOM / a11y /   html · png ·
                     render+canvas   boxes/styles   pdf
            │            │               │
            └────────────┴───────────────┴──────► design_* tools
                                                        │
                            ┌───────────────────────────┴────────┐
                            │  designer + critic (coder model)   │ mesnada
                            └────────────────────────────────────┘
```

Key decisions:

1. **An artifact is a directory in the user's working tree** (decisions 4, 13). Default
   `designer/<slug>/` under the project root (configurable, `design.output_dir`),
   containing `index.html`, assets and a small `pando-design.json` manifest. Agents edit
   those files with the *existing* `edit`/`write` tools — every current permission, LSP,
   diff and agentVCS mechanism keeps working, and the result is committable.
2. **Versions are snapshots** (decision 3). Each accepted iteration takes a snapshot through
   `internal/snapshot`/agentVCS **scoped to the artifact directory**; `design_versions
   checkout` is a scoped revert; diff is a snapshot diff. No parallel copy-per-version store.
3. **SQLite holds only design metadata + the node index**: artifact row (id, dir, kind,
   skill, design system, current version), version rows (snapshot id ↔ ordinal ↔ summary ↔
   critique), node rows for element selection.
4. **Preview = sandboxed iframe over a local HTTP origin** served by the API server
   (`/preview/{artifact}/…`), CSP-locked, bridge injected only on request. Same rendering
   path for WebUI, Desktop, and any external browser (TUI/ACP just open the URL).
5. **Structured mutation before regeneration**: the agent patches nodes
   (`design_patch(selector, props)`); full rewrite is the fallback. The node index makes
   `selection = design://<nodeId>` possible and keeps context small.
6. **The design system is a contract, not a prompt**: `DESIGN.md` + compiled
   `design-system.json` tokens in `designer/_system/`, mirrored in the KB so brand knowledge
   is searchable and reusable across sessions.

## 3. Data model (draft)

```go
// internal/design
type Kind string // v1: "web" | "deck"   (deferred: mobile, document, dashboard, diagram)

type Artifact struct {
    ID        string    // dsg_<ulid>
    SessionID string
    ProjectID string
    Title     string
    Slug      string    // directory name under design.output_dir ("designer/")
    Dir       string    // repo-relative, in the user's tree
    Kind      Kind
    SkillID   string    // design skill / template that produced it
    DesignSystemID string
    CurrentVersion int
    CreatedAt, UpdatedAt time.Time
}

type Version struct {
    ArtifactID string
    Number     int
    SnapshotID string     // internal/snapshot id scoped to Dir  (decision 3)
    Summary    string     // agent-written changelog line
    Critique   *Critique  // last critic pass on this version
    CreatedAt  time.Time
}

type Node struct { // structure index, filled by the inspector after each render
    ArtifactID string
    Version    int
    NodeID     string   // stable: data-pando-id attribute, injected at render time
    Selector   string   // "#hero-cta"
    Role       string   // a11y role
    Text       string
    Box        Rect
    Styles     map[string]string // computed subset (font, color, spacing…)
    ParentID   string
    Slide      int      // deck only: owning slide index
}
```

`pando-design.json` (in the artifact dir, portable, committable):

```json
{
  "id": "dsg_8f91", "kind": "deck", "version": 4,
  "entry": "index.html",
  "designSystem": "pando-brand",
  "skill": "magazine-deck",
  "preview": {"viewport": {"w": 1440, "h": 900}},
  "deck": {"slides": 12, "navigation": "horizontal"}
}
```

## 4. Agent tool surface (`design_*`)

Registered as builtin tools (`internal/llm/tools`), also exported through the MCP server
(`cmd/mcp_server.go`) so external agents — Claude Code as a Pando subagent included — can
drive the same design mode.

| Tool | Purpose |
|---|---|
| `design_create(kind, title, skill?, design_system?, slug?)` | create `designer/<slug>/` + v1 scaffold from a skill/template |
| `design_patch(selector, props \| html)` | structured mutation of one node; snapshot taken on version bump |
| `design_render(viewport?, slide?, wait?)` | render current state in chromedp; returns console/network errors |
| `design_screenshot(selector?, slide?, full_page?)` | screenshot → image tool-result (reuses `imageopt`) |
| `design_inspect(selector?, slide?, depth?)` | DOM + a11y tree + computed styles + boxes (token-budgeted, paginated) |
| `design_versions(op: list\|checkout\|diff)` | version history over snapshots; `checkout` = scoped revert |
| `design_export(format: html\|png\|pdf, slides?)` | export pipeline (decision 9); deck PDF = one page per slide via chromedp print |
| `design_canvas(spec)` | rasterize a `<canvas>`/SVG scene rendered in the browser to PNG — the image-generation path (decision 8) |
| `design_system(op: extract\|get\|apply)` | build/read/apply the design system (§6) |
| `design_present(open?)` | publish the preview URL to the active surface (TUI/ACP/WebUI) |

File writes use the **existing** `write`/`edit` tools (artifacts live in the user's tree),
so there is no `design_write`. Existing `browser_*` tools stay generic; `design_*` are thin,
artifact-scoped wrappers over the same chromedp session registry (no second browser stack).

## 5. Surfaces

**One canvas, four doors plus a CLI** (decision 11). The Design UI is a WebUI route
(`/design`, `/design/:id`) with a **top-level "Design" nav entry**, served from the embedded
static FS:

- **WebUI**: new `web-ui/src/components/design/` — Studio layout: chat pane | canvas |
  inspector. Live reload over the existing SSE channel (`design.version`, `design.render`,
  `design.critique`). Click-to-select on the iframe through a `postMessage` bridge;
  selection becomes `selection = design://<nodeId>` appended to the next prompt. Deck kind
  adds a slide strip (thumbnails) and slide navigation. **Read-only canvas in v1**
  (decision 2): select, inspect, ask — no drag/resize/inline editing.
- **Desktop (Wails)**: nothing beyond WebUI, plus a native menu item "Design" and
  open-external for exports.
- **TUI**: new `internal/tui/page/design.go` — artifact list + version history + inline
  screenshot preview (existing `internal/tui/image` renderer) + `o` opens the live URL in
  the system browser (`auth.OpenBrowser`), `d` version diff. If no HTTP server is running
  (plain `pando` TUI), the preview server starts on demand (§7).
- **ACP**: on each new version the agent emits a `resource_link` block with the preview URL
  plus an `ImageBlock` screenshot, so Zed/VS Code show a clickable live preview and a
  thumbnail. Slash commands `/design`, `/design open`, `/design versions`. `design` tool
  kind added in `tool_render.go`.
- **CLI**: `pando design` subcommand — `create`, `list`, `open`, `versions`, `export`,
  `system extract`. Machine-readable output (`--json`) for scripting; it drives the same
  service layer as the tools, no second implementation.

## 6. Design system

- `designer/_system/DESIGN.md` + compiled `design-system.json` (tokens: color, typography,
  spacing scale, radii, shadows, components). In the user's tree, committable.
- `design_system extract` builds it from: existing code (Tailwind config, CSS vars,
  component files), a reference URL (crawl + computed-styles extraction via chromedp), or
  uploaded images/PDF (existing `convert` + vision).
- Compiled tokens are injected into the designer prompt as a **hard constraint block**
  ("never invent colors/typography/spacing; prefer existing components").
- The extracted system is mirrored into the KB (`pando/design-systems/<name>.md`) so it is
  searchable, wiki-linked and reusable across projects — Pando's edge over per-folder
  registries.
- The three existing `design/*.md` files ship as bundled example systems (Pando-authored
  content, decision 12).

## 7. Preview server & sandboxing

- Preferred path: the API server already running (`pando app|serve|desktop`).
- Fallback: `internal/design/preview` starts an HTTP listener on `127.0.0.1:0` when Pando
  runs as TUI/ACP/CLI without the API server; token in the URL path (`/preview/{token}/…`),
  bound to the session, expiring with it.
- **Remote/headless preview (decision 5)**: served through the existing external-access
  toggle (bind `0.0.0.0`) and gated by the existing basic auth
  (`internal/api/basicauth.go`). `/preview` must never be reachable on a non-loopback bind
  with auth disabled — enforced in the route guard, with a regression test.
- Security: strict CSP on preview responses (`default-src 'self' data:`, `frame-ancestors`
  limited to the Pando origin), no cookies, no access to `/api/*`, files served from the
  artifact directory only with path-escape checks.
- The bridge script (selection, hover outline, slide sync) is served separately
  (`/preview/_bridge.js`) and injected only for `?bridge=1` requests from the Pando UI, so
  exported HTML stays clean.

## 8. Agent loop (designer ↔ critic)

Uses mesnada subagents and the existing verifier-gate machinery. **Both roles run on the
model currently selected as coder** (decision 6) — no separate provider, no extra cost
surface; the difference is persona/prompt, not model.

```
brief → designer (skill + design system) → v1 files
      → render + screenshot + inspect
      → critic pass (coder model, critic persona) → scored critique + issue list
      → a11y/perf checks (rules over the a11y tree, console/network errors)
      → designer patches → vN+1 … until score ≥ threshold or max rounds
```

- Critique is structured (`Critique{score, issues[]{severity, nodeId, slide, message, fix}}`),
  rendered in the inspector panel; each issue is clickable → selects the node/slide.
- Rounds and threshold are config (`design.critique.{max_rounds,threshold,policy}`),
  per-skill override via `od.critique.policy`.
- Guardrails already built: circuit breaker/respawn guard, conclusion gate, blackboard GC.

## 9. Skills & templates

- Ship **Pando-authored** `skills/design/*` bundles (decision 12) in the Claude-Code
  `SKILL.md` format. v1 set matches the v1 kinds (decision 10): `landing-page`,
  `web-prototype`, `dashboard-page` (web kind), `deck-basic`, `magazine-deck` (deck kind),
  `design-system-extract`, plus craft references (`typography.md`, `color.md`, `layout.md`,
  `anti-ai-slop.md`).
- **Frontmatter uses OpenDesign's `od:` namespace verbatim** (decision 7): `od.mode`,
  `od.surface`, `od.preview.type`, `od.design_system.requires`, `od.craft.requires`,
  `od.critique.policy`, `od.example_prompt`, `od.scenario`, `od.category`. Format interop
  only — a user *may* drop a third-party bundle into their skills root and it will load, but
  Pando ships none. Absent `od:` block = zero-config defaults, so any existing Claude Code
  skill works unchanged. Parser extension lands in `internal/skills/parser.go` + `types.go`.
- Skills resolve through the existing discovery roots and catalog installer; the Design page
  gets a gallery reading `/api/v1/design/skills`.

## 10. Phases

Each phase ends buildable, tested, and documented in the KB.

### P0 — Foundations
- `internal/design`: model, artifact directory layout (`designer/<slug>/`),
  `pando-design.json` read/write, slug/ID scheme, `design.output_dir` config.
- DB migration + sqlc queries: `design_artifacts`, `design_versions` (snapshot-id backed),
  `design_nodes`, `design_critiques`.
- Version service bound to `internal/snapshot`/agentVCS: create version = **dir-scoped**
  snapshot; checkout = scoped revert; diff = snapshot diff.
- Exit: create/list/checkout versions from Go tests; artifacts show up in `git status`;
  scope test proves an unrelated file is never reverted.

### P1 — Renderer + inspector core
- Artifact-scoped chromedp wrapper reusing `browserSession`: render, screenshot, PDF,
  canvas rasterization (`design_canvas` backend), deck slide enumeration.
- `data-pando-id` injection + node index extraction (DOM + a11y + computed styles + boxes,
  slide attribution for decks).
- Token-budgeted `Inspect()` output (paginated, configurable style subset).
- Exit: `go test ./internal/design/...` renders web + deck fixtures, indexes nodes,
  rasterizes a canvas scene.

### P2 — Agent tools
- `design_create/patch/render/screenshot/inspect/versions/export/canvas/system/present`
  builtin tools, permission-aware, registered in the base tool set and in the MCP server.
- Patch engine: selector → file edit (HTML attr/style/text) with a deterministic formatter;
  version bump (snapshot) on first mutation after a render.
- Exit: an agent builds and iterates a landing page and a deck end-to-end from the CLI.

### P3 — Preview server + surface plumbing
- `/preview/{id}/…` in `internal/api` + loopback fallback server.
- CSP/sandbox/token rules; **external-access + basic-auth guard** and its regression test.
- SSE events `design.created|version|render|critique`; export download endpoints.
- `design_present` publishes the URL; `auth.OpenBrowser` from TUI.
- Exit: URL opens in a real browser from `pando` TUI, from `pando app`, and remotely with
  external access + basic auth on.

### P4 — WebUI Design Studio
- Top-level "Design" nav entry + routes; Studio layout (chat | canvas | inspector), artifact
  gallery, version timeline with thumbnails, export menu (HTML/PNG/PDF).
- **Both v1 kinds**: web prototype canvas + deck mode (slide strip, slide navigation,
  per-slide export).
- iframe bridge: click-to-select, hover outline, selection → prompt context. **No direct
  manipulation** (decision 2).
- Mobile/responsive pass consistent with existing WebUI mobile rules.
- Exit: full create→iterate→export loop in the browser for web and deck.

### P5 — Desktop + TUI + ACP + CLI surfaces
- `pando design` subcommand (`create/list/open/versions/export/system extract`, `--json`).
- Wails: menu entry, open-external for exports, save dialog.
- TUI `design` page: list, versions, inline screenshot, `o` open live URL, `d` diff.
- ACP: `resource_link` + `ImageBlock` per version, `/design*` slash commands, `design` tool
  kind in `tool_render.go`.
- Exit: the same artifact reachable from all four surfaces plus the CLI in one session.

### P6 — Design system
- `designer/_system/DESIGN.md` + `design-system.json` schema, extractor (code / URL /
  images), applier, prompt-constraint injection, KB mirroring, bundled examples.
- Settings UI (WebUI + TUI) to pick the active design system per project.
- Exit: two artifacts generated with different systems are visually consistent with each.

### P7 — Skills, templates & gallery
- Ship the Pando-authored design bundles + craft references; `od:` frontmatter support in
  the skills parser; `/api/v1/design/skills` and the gallery UI; `Try it` starter prompts.
- Exit: a user picks a template, types a brief, gets the artifact.

### P8 — Critic loop & quality gates
- Critic + a11y passes on the coder model, structured critique, score threshold + max rounds
  config, inspector issue list, per-skill `od.critique.policy`.
- Regression fixtures: N briefs × golden screenshots, perceptual-diff check in CI.
- Exit: measurable score improvement v1→vN on the fixture set.

### P9 (optional, post-MVP)
- Remaining kinds: mobile, document, dashboard, diagram (decision 10 deferral).
- PPTX export via the office/zenskill path; motion artifacts + video export (decision 9
  deferral).
- Figma import/export; direct-manipulation canvas (decision 2 revisited).

## 11. Risks

- **Chromium footprint**: chromedp needs a browser; handled by `browser_detect.go` (system
  Chrome / Lightpanda). The user-facing live preview is the user's own browser, so a missing
  Chromium degrades only screenshots/inspect/export/canvas, not the preview.
- **Artifacts in the user's tree** (decision 4): agent-generated files land in the repo —
  `designer/` default dir, an offered `.gitignore` hint, and dir-scoped snapshots so design
  versioning never touches unrelated files.
- **Snapshot-backed versions** (decision 3): snapshot scope must be the artifact dir only; a
  global snapshot would make `checkout v2` revert unrelated work. Enforce scope + test.
- **Deck fidelity in PDF**: slide-per-page printing depends on `@page`/print CSS in the deck
  skill; the bundled deck skills must ship print styles, and P1 needs a print fixture test.
- **Token blow-up on inspect**: node-index paging, style subsets, existing cache/pagination
  interceptor.
- **Patch fidelity**: selector patching is brittle on generated markup; `data-pando-id`
  anchors + "regenerate this section" fallback bound the damage.
- **Sandbox escape via generated JS**: strict CSP, separate origin path, no API cookies,
  bridge only on request; non-loopback bind requires basic auth.
- **Scope creep into a Figma clone**: hard rule — Pando renders *real* HTML/CSS artifacts;
  no vector canvas editor.

## 12. Implementation-level calls (decide during the phase, not before)

Direction is fully settled (§0). These remain as engineering details:

1. Default `design.critique.{max_rounds,threshold}` values — tune against the P8 fixture set.
2. Deck slide model: single `index.html` with in-page sections (OpenDesign-style) vs one
   file per slide. Single file is the working assumption; revisit if PDF/print or patching
   fidelity suffers (P1/P4).
3. Node-index refresh policy: full re-index per render vs incremental diff after a patch.
4. `.gitignore` behavior for `designer/` — offer, never write silently.

## 13. Verification strategy

- Unit: artifact model, snapshot-backed version service (scope enforcement), patch engine,
  node indexer, design-system compiler, `od:` frontmatter parsing.
- Integration: `go test ./internal/design ./internal/api` — render web + deck fixtures,
  assert node index, preview route, CSP headers, external-access/basic-auth guard, export
  bytes (HTML/PNG/PDF).
- E2E: scripted session (brief → artifact → critique → export) behind a build tag, plus
  perceptual-diff fixtures for P8.
- Manual matrix: TUI open-URL, ACP in Zed (resource link + image), WebUI, Desktop,
  `pando design` CLI, remote preview over external access.