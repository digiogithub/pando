---
created_at: 2026-08-26T19:38:38.97675581Z
updated_at: 2026-08-26T19:38:38.97675581Z
tags:
    - feature
    - design
    - tools
    - patch
    - mcp
---
# Design Studio P2 — agent tools, selector→source patch engine, MCP export

Date: 2026-08-26. Phase P2 of [[pando_design_studio_plan]], continuing
[[design_p0_foundations]] and [[design_p1_renderer_inspector]].

## What was built

### 1. The patch engine (the core of P2)

Selector → file edit, implemented as **byte splicing, never reserialisation**.
Artifact files live in the user's repository: they are committed, reviewed and
hand-edited, so a patch must leave every byte outside the targeted element
identical. `html.Render` would have reformatted whole documents, so it is not
used.

- `internal/design/htmldoc.go` — an offset-preserving tree built with
  `html.NewTokenizer`. Each element records `outerStart/outerEnd`,
  `startTagStart/startTagEnd`, `innerStart/innerEnd`, and each attribute records
  the range of `key="value"` **and** of the raw value inside the quotes, so
  `set_attr` rewrites only the value. `parseStartTag` scans attribute offsets by
  hand because the tokenizer exposes key/value but not positions. An
  `autoClosing` table (li, p, dt, dd, option, tr, td, th, thead, tbody) makes the
  source tree match the DOM the renderer indexed when authors leave tags open.
- `internal/design/selector.go` — the selector subset the renderer's own
  `selectorFor` emits: tag / `#id` / `.class` / `[attr]` / `[attr=value]` /
  `:nth-of-type(n)`, joined by `>` or descendant whitespace. Matching is
  right-to-left and the leading part is **not** anchored to the root, because
  `selectorFor` caps generated selectors at six components. Selector lists and
  every other pseudo-class are rejected with a clear message.
- `internal/design/patch.go` — ten operations: `set_text`, `set_html`,
  `set_attr`, `remove_attr`, `set_style`, `add_class`, `remove_class`,
  `insert_html` (before/after/prepend/append), `replace_outer`, `remove`. Every
  operation resolves to an `edit{start,end,replace}`; edits are sorted and
  **overlapping ranges are refused** rather than silently mangled. A selector
  matching more than one element is refused unless the operation sets
  `"all": true`.
- Deterministic formatter: `mergeStyle` keeps existing declarations in place and
  in order, updates them where they are, and appends new ones **sorted by
  property** — Go map iteration order would otherwise make the same patch
  produce different bytes on different runs. A declaration with an empty value
  is removed; emptying `class`/`style` drops the attribute rather than leaving
  `class=""`.

### 2. Service layer

- `internal/design/service_patch.go` — `PreparePatch` / `ApplyPatchPlan` /
  `Patch`. The split exists so the tool layer can show a **real unified diff in
  the permission prompt before anything touches the working tree**;
  `PreparePatch` provably writes nothing. Operations addressed by `node_id` are
  resolved through the node index of the artifact's current version, which is
  what closes the `design://<node_id>` selection loop. `commit` optionally
  records a version (a directory-scoped snapshot).
- `internal/design/export.go` — `html` / `png` / `pdf`. HTML export produces a
  **single self-contained file**: local stylesheets, scripts and images are
  inlined (images as data URIs); remote references are deliberately left alone
  and reported in `Note`, since rewriting them would change what the design
  depends on. PDF export of a deck re-checks `SlideBreaks` and warns when no
  slide carries `break-after`/`page-break-after`, because a deck exported as one
  long page is invisible until someone opens the file.
- `internal/design/system.go` — the shared design system as two committed files
  under `designer/_system/`: `tokens.json` (source of truth) and the
  `system.css` it generates as `--<group>-<name>` custom properties. Groups and
  names are sorted so the stylesheet is byte-stable and a single token change
  gives a one-line diff.
- `internal/design/present.go` — `Presentation` (title, kind, version, entry,
  URL, slide count, selection) plus `SelectionURI`/`ParseSelectionURI` and
  `WriteWorkspaceFile` (path-escape guarded; backs image generation). The URL is
  `file://` today; every surface reads it from here, so pointing them at the P3
  preview server is a change in one place.
- `internal/design/provider.go` — `Provider` owns the DB handle, the snapshot
  service and **one shared headless browser**, handing out cheap per-session
  `*Service` values. Installed process-wide with `SetDefaultProvider`; the tools
  resolve it per call (`design.ServiceFor(sessionID)`) so a tool constructed
  before wiring still works, and report `ErrNoProvider` as actionable text when
  it is absent.

### 3. The ten `design_*` tools

`internal/llm/tools/design.go` (create, versions, system) and
`internal/llm/tools/design_render.go` (render, inspect, patch, screenshot,
export, canvas, present). Mutating tools (`create`, `patch`, `versions
commit|checkout`, `export`, `canvas`, `system set|init`) request permission
exactly like `write`/`edit`; `design_patch` passes the per-file diff as
`EditPermissionsParams` so the prompt shows what will change.
`design_screenshot` returns a real image block. `design_render` strips the node
array from its response (the index is read through `design_inspect`, which is
paged and omits computed styles unless asked).

### 4. Registration

- `internal/llm/tools/builtin_names.go` — all ten names, so the MCP gateway
  never tries to route them.
- `internal/llm/agent/tools.go` — new `DesignTools(permissions)` appended to
  both `CoderAgentTools` and the gateway branch of `CoderAgentToolsWithMesnada`.
- `internal/app/app.go` — provider wired before the agent is built, released in
  `Shutdown()`.
- `cmd/mcp_server.go` — a `design` tool group behind
  `[MCPServer.Design] Enabled` / `--design-tools`; turning it on also turns
  `design.enabled` on, since exposing the tools without the subsystem would
  answer every call with "not available".

## Decision: `design.enabled` defaults to **false**

Ten tools with substantial descriptions cost roughly 3–4k tokens in **every**
request. A project that never designs anything should not pay for that, and the
repo's convention for large opt-in subsystems (agui, swarm, caveman,
superpowers) is default-off. `config.DesignConfig.Enabled` gates the whole
group. Turning it on is one config line; the P5 `pando design` CLI will flip it.

## Files touched

New: `internal/design/{htmldoc,selector,patch,service_patch,export,system,present,provider}.go`,
`internal/llm/tools/{design,design_render}.go`.
Modified: `internal/config/config.go` (`Design.Enabled`,
`MCPServerDesignToolsConfig`), `internal/llm/tools/builtin_names.go`,
`internal/llm/agent/tools.go`, `internal/app/app.go`, `cmd/mcp_server.go`.
Tests: `internal/design/{patch_test,service_p2_test,e2e_p2_test}.go`,
`internal/llm/tools/design_tools_test.go`.

## Verification

- `internal/design/patch_test.go` (15 tests, no browser): byte preservation of
  untouched regions including irregular whitespace and the doctype, nth-of-type
  resolution, ambiguity refusal and `all`, attribute escaping and in-place
  insertion, whitespace-clean attribute removal, style merge semantics,
  determinism of `mergeStyle` over 20 runs, class ops, insert/remove, void
  element rejection, overlap conflict detection, `set_text` escaping markup,
  actionable no-match error, implicitly-closed `<li>`, selector grammar
  accept/reject, attribute range recording.
- `internal/design/service_p2_test.go` (10 tests): node-id resolution end to
  end, unindexed-node guidance, `PreparePatch` writing nothing, artifact-escape
  refusal, HTML export inlining, remote references left alone with a note,
  design-system CSS byte-stability over 5 saves, token merge/remove, presentation
  + selection round-trip, workspace write escape refusal.
- `internal/design/e2e_p2_test.go` (4 browser tests, skipped without Chromium):
  **create → render → inspect (find by text) → patch by node_id → re-render →
  confirm the text and the inline style reached the live DOM**; the deck path
  (render, patch a slide, export a real `%PDF` with no warning); the
  missing-print-CSS warning on a deck without print styles; canvas
  rasterisation written into the workspace. All passed against system Chrome.
- `internal/llm/tools/design_tools_test.go` (4 tests): every tool is a builtin,
  `DesignToolNames` matches the constructors, schemas well-formed and every
  `Required` key declared, missing-subsystem message, input validation.
- `go build ./...`, `go vet` on the touched packages, and
  `go test ./internal/design ./internal/llm/tools ./internal/llm/agent
  ./internal/api ./internal/config ./internal/snapshot ./internal/db ./cmd
  ./internal/app` — all ok. `gofmt` clean on every file touched
  (`cmd/test_ollama_main/main.go` is pre-existing and untouched).

## Known limitations (deliberate)

- The patch engine resolves selectors against the **source** tree. An element
  that exists only because a script created it cannot be patched; the error says
  so and points at editing the script.
- Implicit tag closing is handled for the common authoring cases only, not the
  full HTML5 tree-construction algorithm.
- `design_present` returns a `file://` URL until P3 ships the preview server.

## Next — P3

Preview server (CSP-locked), surface plumbing, and the external-access /
basic-auth guard, then P4 WebUI Design Studio.
