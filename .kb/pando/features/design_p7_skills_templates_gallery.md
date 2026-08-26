---
created_at: 2026-08-27T07:39:28.57530664Z
updated_at: 2026-08-27T07:39:28.57530664Z
tags:
    - feature
    - design
    - skills
    - templates
    - gallery
    - od
    - webui
    - tui
    - acp
    - cli
---
# Design Studio P7 — Skills, templates & gallery

Phase 7 of [[pando_design_studio_plan]], after [[design_p6_design_system]]. Status: complete,
verified. Ships the Pando-authored design bundles, `od:` frontmatter support in the skills
parser, `/api/v1/design/skills`, the gallery UI and the `Try it` starter prompts.

## What was changed

### 1. `od:` frontmatter, in a leaf package

OpenDesign's `od:` namespace is kept verbatim (plan decision 7). It lives in a new leaf
package **`internal/skills/od`** rather than in `internal/skills`, because the design package
also has to read it and the dependency chain is
`internal/skills` → `internal/llm/tools` → `internal/design`: putting the types in
`internal/skills` and importing them from `internal/design` is an import cycle.

- `od.Metadata` (`Mode`, `Surface`, `Category`, `Scenario`, `ExamplePrompt`), plus
  `od.Preview{Type, Viewport{Width,Height}}`, `od.DesignSystem{Requires}`,
  `od.Craft{Requires []string}`, `od.Critique{Policy}`.
- `od.SplitFrontmatter` (moved out of `skills/parser.go`) and `od.Strip`.
- `skills.SkillMetadata` gains `OD *od.Metadata` — a **pointer**, so "no od block" is
  distinguishable from "od block with zero values". Absent block = zero config: any existing
  Claude Code skill keeps working unchanged.
- `skills.SkillMetadata.IsDesignTemplate()`: has an od block, mode is not `reference`, and a
  surface is declared.
- `skills.ParseSkillContent(content, defaultName)` extracted from `ParseSkillFile` so a
  SKILL.md that is not on disk (an embedded bundle) can be parsed.
- **Resilience**: a third-party `od:` block whose shape we do not understand (e.g. a scalar)
  used to fail `yaml.Unmarshal` and make the whole skill invisible, because `DiscoverSkills`
  swallows parse errors. `ParseSkillFile` now retries with the od key stripped and keeps the
  skill, dropping only the block.

### 2. Bundled design bundles (`internal/design/bundles/`, embedded)

Six templates and four craft references, all Pando-authored (plan decision 12 — we speak the
format, we ship nobody else's content):

- templates: `landing-page`, `web-prototype`, `dashboard-page` (web); `deck-basic`,
  `magazine-deck` (deck); `design-system-extract` (`od.mode: workflow`, no surface, so it is
  listed but **not** startable).
- craft references: `typography.md`, `color.md`, `layout.md`, `anti-ai-slop.md`.
- Each template that scaffolds ships `scaffold/index.html` + `scaffold/style.css` with
  `{{TITLE}}` substituted at creation; every scaffold links `../_system/system.css` and styles
  exclusively through `var(--…)` tokens, and both deck scaffolds carry the `@page` /
  `break-after: page` print styles PDF export depends on.

### 3. `internal/design/templates.go`

- `Template` (the gallery entry), `TemplateFromSkill(name, description, *od.Metadata)`,
  `BundledTemplates`, `BundledTemplate`, `BundledTemplateContent`, `CraftReferenceNames`,
  `CraftReference`, `Scaffold(name, title)`, `InstallBundle(name, targetDir, force)`,
  `Gallery(discovered []Template)`.
- `Gallery` takes `[]Template`, not `[]*skills.Skill` — the caller converts, which is what
  keeps `internal/design` free of the cycle described above.
- An **installed** copy of a bundle wins over the embedded one: the installed file is what the
  agent actually reads, so listing our description would show rules that are not in force.
- A template declaring a surface this build does not support stays **listed but not
  startable** rather than silently scaffolding as web.
- `InstallBundle` copies the craft references **into** the skill directory rather than sharing
  one copy: `skills.resolveResourceLocation` refuses to read a resource outside the skill
  directory, and a bundle that reaches outside itself breaks when copied elsewhere.
- `InstallBundle` refuses to overwrite an existing SKILL.md (`ErrBundleInstalled`) unless
  `force` is passed — the installed copy is the user's to edit.

### 4. Creation from a template

`design.Service.Create` resolves the template **before** defaulting the kind, so
`Create{Title, SkillID: "deck-basic"}` produces a deck, seeded with the template scaffold, with
the template's preview viewport on the manifest. A scaffold that does not carry the manifest
entry still gets the placeholder so the artifact is always renderable. The same ordering fix
was applied in `design_create` (tool) and `pando design create` (CLI `--kind` default changed
from `"web"` to `""`).

### 5. Surfaces

- **Tool**: new `design_skills` (`internal/llm/tools/design_skills.go`) with
  `list | show | install`. It reads embedded content, so `list`/`show` answer with no project,
  no database and no design subsystem wired; only `install` writes and asks for permission.
  Registered in `builtin_names.go`, `DesignToolNames`, `agent.DesignTools` (so MCP exports it).
- **CLI**: `pando design skills`, `… show <name> [--craft ref]`, `… install <name>
  [--global] [--force]`; `pando design create --skill <name>`. The skills subcommands
  deliberately do **not** load the configuration or the database — they only copy embedded
  files, so they resolve the working directory themselves (an earlier version panicked with
  `config not loaded`).
- **API**: `GET /api/v1/design/skills` (gallery + craft list), `POST
  /api/v1/design/skills/{name}/install` (`scope`, `force`). `ErrBundleInstalled` maps to 409.
- **WebUI**: `TemplateGallery.tsx` plus an Artifacts/Templates tab in `DesignView.tsx`; store
  gains `templates`, `craftReferences`, `fetchTemplates`, `installTemplate`. **`Try it`** pushes
  the template's starter brief into the chat composer through the existing `chatDraftStore` and
  navigates to `/chat` — it creates nothing, because a template is only half the input.
  Locale keys added to `en.json` and `es.json` (the other five have no `design` block at all
  and fall back to English).
- **TUI**: `g` toggles a read-only template panel on the design page (mutually exclusive with
  the `y` system panel — they share the detail column).
- **ACP**: `/design-templates`, read-only, same reasoning as `/design-system`.

## Files touched

New: `internal/skills/od/od.go`, `internal/skills/od_test.go`,
`internal/design/templates.go`, `internal/design/templates_test.go`,
`internal/design/e2e_p7_test.go`, `internal/design/bundles/**` (6 templates + 4 craft refs),
`internal/llm/tools/design_skills.go`, `internal/llm/tools/design_skills_test.go`,
`internal/api/design_skills_test.go`, `web-ui/src/components/design/TemplateGallery.tsx`.

Modified: `internal/skills/types.go`, `internal/skills/parser.go`,
`internal/design/service.go`, `internal/llm/tools/design.go`,
`internal/llm/tools/builtin_names.go`, `internal/llm/agent/tools.go`, `cmd/design.go`,
`cmd/design_test.go`, `internal/api/handlers_design.go`, `internal/api/routes.go`,
`internal/mesnada/acp/{session_state,slash_commands,goal_commands,design_commands}.go`,
`internal/mesnada/acp/agent_pando_test.go` (22→23 commands),
`internal/tui/page/design.go`, `internal/tui/page/design_test.go`,
`web-ui/packages/pando-client/src/stores/designStore.ts`,
`web-ui/src/components/design/DesignView.tsx`, `web-ui/src/i18n/locales/{en,es}.json`.

## Exit criterion

"A user picks a template, types a brief, gets the artifact." The brief is prose no unit test
can assert, so `TestPickATemplateAndGetTheArtifact` asserts everything the *template* is
responsible for: creating with `SkillID: "deck-basic"` and no kind produces a **deck**, seeded
with the template files (not the placeholder), with the title substituted, the template's
viewport on the manifest, exactly one stylesheet link, and `ApplySystem` reporting
`Linked == false` with **zero** findings — the scaffold already obeys the design system.

## Verification

- `go build ./...`, `go vet` on every touched package, `gofmt` clean on all touched files
  (the 18 unformatted files `gofmt -l internal/ cmd/` reports are pre-existing and untouched).
- Green: `./internal/design/...`, `./internal/skills/...`, `./internal/api`,
  `./internal/llm/tools`, `./internal/llm/prompt`, `./internal/mesnada/acp`,
  `./internal/tui/page`, `./cmd`, `./internal/app`, `./internal/config`, `./internal/db`.
- Frontend: `bun run build` clean; `bun run lint` → 4 warnings, all pre-existing in
  `KeyValueEditor.tsx` / `ModelCombobox.tsx`.
- **Real-binary smoke test** in a temp project: `design skills` table + `--json`;
  `design create "Incident Review" --skill deck-basic` → deck, scaffold seeded, title
  substituted, system linked; `design create --skill vibes` → one-line note, artifact still
  created; `design skills install magazine-deck` → SKILL.md + 4 craft refs; re-install →
  `design: template already installed … (pass force to replace it)`; `--force` → replaced;
  `design system apply` on both artifacts → already linked, zero findings; unknown template
  and unknown craft reference → one-line errors naming what exists.

## Things found while building

- **Bundled frontmatter must quote values containing `: `.** Four SKILL.md files failed
  `yaml.Unmarshal` (`mapping values are not allowed in this context`) and, because
  `DiscoverSkills` swallows parse errors, would have vanished from the gallery silently.
  `TestBundledTemplatesAreComplete` now fails loudly instead.
- `bun run typecheck` (`tsc --noEmit`) is still not equivalent to `bun run build` (`tsc -b`) —
  see [[design_p6_design_system]]. The build is the one to trust.
- Still open from P5: the WebUI's Wails bindings are broken (`web-ui/wailsjs/` does not exist,
  root `wailsjs/` is stale); `desktopRuntime.ts` sidesteps it.

## Next — P8 — Critic loop & quality gates

Critic + a11y passes on the coder model, structured critique, score threshold + max rounds
config, inspector issue list, per-skill `od.critique.policy` (already parsed and carried on
`Template.CritiquePolicy`; the bundles declare `strict` / `standard` / `none`). Regression
fixtures: N briefs × golden screenshots, perceptual-diff check in CI. Exit: measurable score
improvement v1→vN on the fixture set.
