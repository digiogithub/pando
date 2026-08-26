---
created_at: 2026-08-27T06:19:57.8020503Z
updated_at: 2026-08-27T06:19:57.8020503Z
tags:
    - feature
    - design
    - design-system
    - extractor
    - prompt
    - webui
    - tui
    - acp
    - cli
---
# Design Studio P6 — Design system: contract, extractor, applier, prompt constraint

Phase 6 of [[pando_design_studio_plan]], completed 2026-08-27. Builds on
[[design_p5_surfaces_cli_tui_acp_desktop]], [[design_p4_webui_studio]],
[[design_p3_preview_server_surfaces]], [[design_p2_tools_patch_engine]],
[[design_p1_renderer_inspector]] and [[design_p0_foundations]].

## What was built

The design system stopped being a token file and became a **contract with a way
to build it**: extract tokens from something that already looks right, hold the
designer to them in the prompt, and link every artifact to the same stylesheet.

### 1. The contract — `internal/design/contract.go` (new)

- `SystemContractFile = "DESIGN.md"`. The system is now **three** committed
  files in `designer/_system/`: `tokens.json` (source of truth), `system.css`
  (generated), `DESIGN.md` (the written contract).
- `DesignSystem.Contract()` renders a full DESIGN.md for a project that has
  none; `DesignSystem.TokenSection()` renders the generated half, fenced by
  `<!-- pando:tokens:begin -->` / `<!-- pando:tokens:end -->`.
- `(*Service).writeContract` rewrites **only** what is between the markers, so
  prose a person wrote around the table survives every token edit. A DESIGN.md
  with no markers gets the section appended rather than being overwritten.
- `LoadSystemAt(layout)` reads a system without a `Service`, which is what the
  prompt builder needs (it runs before any session exists).
- `DesignSystem.ConstraintBlock(stylesheet, contract)` renders the hard
  constraint; `PromptConstraints()` is the package-level entry point.
- `SortedTokenGroups` / `SortedTokenNames` exported so every surface renders the
  token table in the same order.

### 2. The extractor — `internal/design/extract.go` + `tokens.go` (new)

`(*Service).ExtractSystem(ctx, ExtractOptions{Source, Target, Name, MaxFiles})`
returns an `ExtractResult{System, Source, Target, Scanned, Notes}` **without
writing anything**; persisting is a separate, explicit step.

Four sources:

| Source | How |
|---|---|
| `code` | walks a directory for `.css/.scss/.sass/.less/.html/.vue/.svelte/.jsx/.tsx` + `tailwind.config.*`; skips `node_modules`, `.git`, `dist`, `vendor`, dotdirs, files > 512 KiB, **and the design output dir itself** |
| `url` | reuses `Renderer.Render` with `RenderOptions{URL: …}` and a wider `urlStyleProps`; no new chromedp code. Rejects non-http(s) targets before touching the browser |
| `image` | `image.Decode` + stride sampling + channel quantisation. Colours only |
| `text` | a written style guide: a file path, or a bundled example name |

`tokens.go` holds the colour arithmetic shared by all four: `parseColor`
(`#rgb`/`#rrggbb`/`#rrggbbaa`/`rgb()`/`rgba()`/keywords, rejecting fully
transparent), WCAG `luminance`, HSL `saturation`, `contrastRatio`, `buildPalette`
(rank, de-duplicate near colours, **re-rank after merging**) and the role
assignment.

**Roles are assigned from measurable evidence, never from source order:**

- Colours are tallied twice — once overall, and once **by the CSS property they
  appeared in** (`background`/`background-color`, `color`/`fill`,
  `border-color`). `assignRoles` prefers the by-property evidence, because
  `color` is inherited and on a rendered page appears on every node — raw
  frequency would make the *text* colour the background every time.
- `bg` = most common background. `surface` = the next background that is within
  distance 96 of it **or** unsaturated (otherwise the second-most-common
  background is a button, and the page gets painted in the CTA colour).
- `text` = most common foreground. `muted` = a low-saturation foreground whose
  luminance sits **between** bg and text.
- `accent` = the *recurring* saturated colour, not the most saturated one: a
  palette usually contains one very saturated colour used once for a focus ring
  or an error state. `isAccentCandidate` also requires mid luminance (0.05–0.85)
  because HSL reports a parchment `#f5f4ed` at saturation 0.29.
- `border-color` from a rendered page is only counted when `border-width` is
  non-zero — browsers report the text colour as the border colour of every
  element with no border.
- A role with no plausible candidate is **left out**, so the default keeps its
  accessible value instead of being filled with a colour nobody chose.
- Declared CSS custom properties (`--color-accent: …`) are recorded separately
  and **override everything inferred** — the project already stated its answer.
  They are deliberately *not* counted in the frequency palette: a declaration is
  a definition, not a usage.
- Prose sources call `dropRoleHints()`: a style guide quoting `background:
  white` in an example is not stating that the system background is white.

`Notes` carry what the source could not tell us (an image has no typography, a
page cannot name its own tokens, prose states colours in an order we can only
guess at). That is the point — the caller sees where a human still decides.

### 3. Bundled examples — `internal/design/examples.go` + `examples/*.md` (new)

The three style guides moved from the repo-root `design/` directory into
`internal/design/examples/` and are `go:embed`ed: `claude.md`, `clay.md`,
`starbucks.md` (Pando-authored prose, locked decision 12). `ExampleSystemNames`,
`ExampleSystem` (rejects `/`, `\`, `.` so a name cannot traverse) and
`ExampleSystemTitle`. The root `design/` directory is gone.

### 4. The applier — `internal/design/apply.go` (new)

`(*Service).ApplySystem(ctx, artifactID) (ApplyResult, error)` does two jobs that
are deliberately kept apart:

- **Links** `system.css` into the entry document, once, before `</head>`, using a
  path relative to the artifact directory (`../_system/system.css`). Idempotent.
  A document with no `</head>` is an **error**, not a silent no-op.
- **Audits** the artifact for literals a token already covers, reporting
  `SystemFinding{File, Line, Property, Value, Token}` — never rewriting them. The
  same hex can be the brand colour in one place and a deliberate one-off in
  another, so that is a design judgement. Lines already using `var(--…)` are
  skipped, and the generated stylesheet is never audited against itself. Capped
  at 60 findings.

### 5. Knowledge-base mirroring — `internal/design/mirror.go` (new)

`SystemMirror` is a one-method interface (`AddDocument`) so `internal/design`
does not depend on the RAG stack. `*kb.KBStore` satisfies it exactly; wired in
`internal/app/app.go` from `app.Remembrances.KB` via `Provider.SetMirror`.
`MirrorSystem` writes `pando/design-systems/<slug>.md` carrying the token table
**and** the generated CSS (the document is read from other projects, where the
file does not exist). Best-effort throughout: a missing or failing knowledge base
never fails a token edit or undoes a system already on disk.

### 6. Prompt constraint — `internal/llm/prompt/coder.go`

`CoderPrompt` now appends `design.PromptConstraints()`, mirroring the existing
`lspInformation()` pattern. It returns the empty string unless **both**
`design.enabled` is on **and** the project committed a system — an unused
subsystem costs zero prompt tokens, and a default nobody chose is not a
constraint worth stating.

### 7. Surfaces

- **Tool** `design_system` gained `extract`, `apply` and `examples` alongside
  `get`/`init`/`set`, plus `source`/`target`/`artifact_id` parameters.
  `examples` is answered **before** the service is resolved, so it works in a
  process where the subsystem was never wired.
- **CLI**: `pando design system extract [target] --from code|url|image|text
  [--target …] [--name …] [--dry-run]`, `pando design system apply [artifact]`,
  `pando design system examples`. All honour `--json`.
- **API**: `GET|PUT /api/v1/design/system`, `GET /api/v1/design/system/examples`,
  `POST /api/v1/design/system/extract`, `POST
  /api/v1/design/artifacts/{id}/apply-system`.
- **WebUI**: new Settings category **Design system**
  (`web-ui/src/components/settings/DesignSystemSettings.tsx`) — source picker,
  target, Extract / Preview (dry run), bundled-guide chips, extraction notes, a
  token editor with a colour swatch for `#rrggbb` values, Save/Reset. Store
  actions added to `designStore.ts`: `fetchSystem`, `fetchSystemExamples`,
  `saveSystemTokens`, `extractSystem`, `applySystem`. `designSystem` label added
  to 7 locale files.
- **TUI**: `y` toggles a design-system panel on the `design` page (tokens, paths,
  bundled guides). Digits `1`–`9` adopt a bundled guide — the only write the page
  performs, and they do nothing at all while the panel is closed.
- **ACP**: `/design-system`, read-only. Changing a system rewrites the look of
  every artifact at once, and a slash command typed into an editor is the wrong
  place for that: the tool asks permission, the CLI shows a dry run.

## Deviations from the plan, and why

1. **`tokens.json`, not `design-system.json`.** The plan and a config comment
   named the compiled file `design-system.json`; P0 shipped `tokens.json` and
   P0–P5 document it. Renaming buys nothing but churn. The stale config comment
   in `internal/config/config.go` was corrected instead.
2. **No vision model for image extraction.** The plan suggested `convert` +
   vision. Colour quantisation needs no model and no provider, so images yield a
   palette and say so in `Notes`; typography and spacing stay at their defaults
   rather than being hallucinated from pixels.
3. **The applier reports, it does not rewrite.** See §4.

## Verification

- `go build ./...`, `go vet` on every touched package, `gofmt` clean
  (`internal/llm/tools/aliases.go`, `lua_tools.go`, `remembrances_code_test.go`
  and `cmd/test_ollama_main/main.go` were already unformatted and untouched).
- `go test` green: `./internal/design`, `./internal/design/preview`,
  `./internal/api`, `./internal/llm/tools`, `./internal/llm/prompt`,
  `./internal/mesnada/acp`, `./internal/tui/...`, `./cmd`, `./internal/app`,
  `./internal/config`, `./internal/db`, `./internal/desktop`.
- New tests: `internal/design/system_p6_test.go` (17 cases: colour parsing, role
  assignment from measured properties, code scan skipping dependencies and its
  own output, bundled-guide extraction, image notes, unknown source/target/URL
  rejection, contract prose preservation, constraint-block contents, apply
  idempotence + audit, mirror publish/no-op, embedded-example traversal),
  `internal/design/e2e_p6_test.go` (the **P6 exit criterion**),
  `internal/design/prompt_constraints_test.go`, `internal/api/design_system_test.go`
  (full HTTP loop + 400 on an unknown target), TUI panel tests, CLI surface
  tests, `design_system` tool action-advertising test.
- **Exit criterion** made checkable without a browser
  (`TestTwoArtifactsShareOneSystemAndFollowItWhenItChanges`): two artifacts of
  different kinds resolve through the *same* relative stylesheet, audit clean,
  and replacing the system changes the generated CSS **without touching either
  artifact's files** — the mechanism that produces visual consistency.
- **Real-binary smoke test** in a temp project: `system examples`; code
  extraction (declared `--color-accent`/`--space-md` win, `node_modules` and
  `designer/` skipped); text extraction from `claude` (bg `#f5f4ed`, accent
  `#c96442`); URL extraction against a local page (bg `#0b1020`, surface
  `#161c33`, text `#e8eaf2`, accent `#ff5a3c`); `system apply` linking
  `../_system/system.css` once and reporting two hardcoded literals; re-apply as
  a no-op; `--json`; and clean one-line errors for a bad source, an unknown
  guide (listing the available ones) and a `file://` URL.

## Found, not fixed

`bun run typecheck` (`tsc --noEmit`) did **not** catch a missing member in the
`SettingsCategory` union that `bun run build` (`tsc -b`) rejected. The two are
not equivalent; the build is the one to trust. Also still open from P5: the
WebUI's Wails bindings are broken (`web-ui/wailsjs/` does not exist, the root
`wailsjs/` is stale) — `desktopRuntime.ts` sidesteps it.

## Next — P7

Skills, templates & gallery: ship the Pando-authored design bundles + craft
references, `od:` frontmatter in the skills parser, `/api/v1/design/skills` and
the gallery UI, `Try it` starter prompts. Exit: a user picks a template, types a
brief, gets the artifact.
