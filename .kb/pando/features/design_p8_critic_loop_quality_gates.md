---
created_at: 2026-08-27T11:07:49.623370357Z
updated_at: 2026-08-27T11:07:49.623370357Z
tags:
    - feature
    - design
    - critique
    - a11y
    - audit
    - quality-gate
    - webui
    - tui
    - cli
    - api
---
# Design Studio P8 — Critic loop & quality gates

Phase 8 of [[pando_design_studio_plan]], after [[design_p7_skills_templates_gallery]].
Implemented 2026-08-27. Everything builds, vets, is gofmt-clean and tested; the CLI path
was smoke-tested with a real binary and a real headless browser.

Plan scope (verbatim): *"Critic + a11y passes on the coder model, structured critique, score
threshold + max rounds config, inspector issue list, per-skill `od.critique.policy`.
Regression fixtures: N briefs × golden screenshots, perceptual-diff check in CI.
Exit: measurable score improvement v1→vN on the fixture set."*

## What was built

### 1. A deterministic audit — `internal/design/audit.go`

`Audit(AuditInput) AuditResult` runs every rule over one render and scores it. It calls no
model: this is the evidence a critic argues from, and evidence that changes between two
identical runs is not evidence. Rules carry **stable codes** so a UI can group them and a
caller can suppress one without matching on prose:

| Code | Severity | What it catches |
|---|---|---|
| `a11y.image-alt` | error | `<img>` with no alt attribute at all (`alt=""` is a decision, not an omission — it passes) |
| `a11y.control-name` | error | interactive element with no accessible name |
| `a11y.contrast` | error | WCAG AA: 4.5:1, 3:1 for large text (≥24px, or ≥18.66px bold) |
| `a11y.tap-target` | warning | interactive box below 24×24 (WCAG 2.5.8 AA, not the 44×44 of AAA) |
| `a11y.heading-order` | warning | heading level skipped |
| `a11y.missing-h1` | warning | headings but no `h1` |
| `a11y.document-title` | warning | empty `<title>` |
| `runtime.console-error` | error | console error/exception/assert (a `console.log` is not an error) |
| `runtime.network-failure` | error | failed request or ≥400 response |
| `layout.horizontal-overflow` | warning | scroll width beyond the viewport |
| `layout.empty-document` | blocking | the render produced no elements |
| `deck.no-slides` | blocking | deck kind, no slide containers |
| `deck.page-break` | error | a slide not followed by a page break — PDF export runs slides together |
| `system.unlinked` | error | committed design system not linked by the artifact |
| `system.hardcoded` | warning | a value a token already covers (reuses the P6 audit) |

Scoring: 10 minus severity weights (blocking 4, error 1.5, warning 0.5), with a **per-rule
cap of 3** that bounds *repetition* only — a rule whose single occurrence costs more than
the cap still does, so forty missing alts (7.0) never scores worse than a page that failed
to render (6.0). Issues are folded at 5 per rule but `Counts` keeps the real number, so the
score reflects the whole problem while the list stays readable.

Colour maths lives here too: `ParseCSSColor` (rgb/rgba/hex, composites a translucent
foreground over its background) and `ContrastRatio`. **Anything it cannot parse is unknown,
not failing** — a rule that guesses reports contrast failures that are not real, and readers
learn to ignore it.

### 2. Facts the node index deliberately does not store

The rules need the accessible name, heading level, interactivity, effective background and
font metrics. Putting those on `Node` would mean a migration *and* would cost tokens on every
`design_inspect` page. Instead `RenderResult.Facts []NodeFacts` is filled by the index script
(`render_script.go`), marked `json:"-"`, never persisted and never sent to a model. Facts are
emitted only for elements a rule can fire on.

The script resolves the **effective background** by walking ancestors to the first one that
actually paints: contrast is measured against what is behind the text, not against the
transparent background most elements declare.

### 3. The gate — `internal/design/critique.go`

- `CritiqueSettings{Enabled, MaxRounds, Threshold, Policy}` from `design.critique.*` config
  (new key: `design.critique.policy`, default `standard`), overridden per skill by
  `od.critique.policy` via `Service.CritiqueSettingsFor(skillID)`.
- Policies: `none` (scores, never blocks), `standard` (score alone), `strict` (also refuses
  to pass while any error-level finding remains, and raises the threshold floor to 9.0 — a
  project cannot configure a threshold of 5 and still call the result strict).
- An unrecognised policy falls back to standard rather than running a gate whose behaviour is
  undefined.
- `Gate(critique, round) GateDecision` answers `Pass` / `Iterate` / stop, with the numbers and
  a one-sentence reason. `Iterate` is not `!Pass`: a run that has spent its rounds stops
  without passing.
- `MergeIssues` folds the critic's own findings in and drops an echo of a rule already fired
  on the same node. `BlendScore` weights the audit 0.6 and the critic 0.4 — the audit is
  reproducible, the critic sees what rules cannot; neither is allowed to be the whole verdict.
  A critic with no score of its own leaves the audit score standing.

### 4. The pass — `internal/design/service_critique.go`

`Service.Critique(ctx, id, CritiqueOptions) (CritiqueReport, error)` renders, audits, folds in
judgement, scores, records, publishes `design.critique` and gates. Two rules it enforces:

- **The audit never edits what it audits.** `systemUsage` answers the design-system half
  read-only; `ApplySystem` would have linked the stylesheet, and an audit that mutates its
  subject cannot be run twice. Tested by running it twice and comparing.
- **A pass that could not render never passes.** Without a browser (or with `SkipRender`) the
  design-system checks still run and say so, but `Rendered=false` forces the decision to a
  stop with a reason — it does not become an "iterate" either, because no amount of editing
  the files makes a missing browser render them. `AuditInput.Rendered` also silences every
  render-dependent rule: "the document has no title" fired because nobody loaded the document
  is a finding about the audit, not the artifact.

### 5. Surfaces

- **Tool `design_critique`** (`internal/llm/tools/design_critique.go`), actions `run` | `show`.
  Registered in `DesignToolNames`, `builtin_names.go` and `agent.DesignTools`, so the MCP
  server exports it too. Its description carries the loop: fix the files, commit with
  `design_versions`, critique again. It accepts the critic's own `score`, `summary` and
  `issues`, and reports which rules were folded so nine failures do not read as one.
- **API**: `POST /api/v1/design/artifacts/{id}/critique` (run, `record:false` for a dry look,
  `policy` override) and `GET …/critique?version=N` (never-critiqued is `{"exists":false}` plus
  the settings, not an error).
- **CLI**: `pando design critique [artifact]` with `--version`, `--policy`, `--no-render`,
  `--no-record`, `--json`. A policy typo is rejected before any database or browser work.
- **WebUI**: the Inspector column gained a Structure | Issues tab pair. `IssuePanel.tsx` shows
  the score, the verdict, the reason and the findings worst-first; a finding with a node id is
  clickable straight to the selection. `designStore` gained `critique`, `critiqueDecision`,
  `critiqueSettings`, `fetchCritique`, `runCritique`, and refreshes on the `design.critique`
  SSE event. i18n keys under `design.critique.*` in `en.json` and `es.json`.
- **TUI**: version rows now show their score, and `c` expands the findings of the current
  version's last pass. Read-only on purpose — the critique already travels with the version
  list, and running a pass renders a browser, which is not what a list view should do on a
  keystroke.

### 6. Regression fixtures — the exit criterion

`internal/design/fixtures/critique/{landing,deck,runtime}/{v1,vn}.html`, embedded and driven by
`TestCritiqueFixturesImproveFromV1ToVN`: each brief is created, critiqued, fixed, committed and
critiqued again. It asserts the named rules fire on v1 (a fixture that stops breaking them has
stopped testing anything), that the score **improves**, that those exact rules are gone in vN
rather than traded for different ones, and that vN passes its own gate. Per-fixture policy: the
deck and the landing page run standard, the runtime fixture runs strict — a single warning is
not meant to fail a standard gate, so the fixture states the policy it needs instead of the
audit being bent to make it fail.

`internal/design/imagediff.go` adds `ComparePNG(before, after, tolerance) ImageDiff`
(fraction changed, max channel delta, size mismatch = full difference). A size change is a
regression on its own: comparing overlapping corners of two different layouts would report a
small number for a large change. `TestRenderIsStableEnoughForPerceptualDiff` proves the
premise a golden-screenshot check rests on — two renders of the same artifact differ by
<0.1% — and that a repainted band does register.

## Bugs found and fixed while smoke-testing

1. **`--no-render` fired render-dependent rules.** A headless pass reported "the document has
   no elements" (blocking) about a document nobody loaded. Fixed with `AuditInput.Rendered`
   plus `TestRulesThatNeedARenderDoNotFireWithoutOne`.
2. **A headless pass could reach PASS.** 10/10 on the design-system checks alone was
   presented as a verdict. Now it stops with a reason, unless the policy does not gate.
3. **A failed request could not be named.** `network.EventLoadingFailed` carries only a request
   id, so findings read `failed to load  (net::ERR_FILE_NOT_FOUND)`. `browserSession` now maps
   request id → URL (`EventRequestWillBeSent`, cleared on finish/fail, bounded at 500), so the
   finding names the file. A finding nobody can locate is a finding nobody can fix.
4. **`deck.page-break` was a warning.** It breaks a documented export path — a deck whose PDF
   runs slides together is broken — so it is an error, in both the issue and `ruleSeverity`.

## Files

New: `internal/design/audit.go`, `critique.go`, `service_critique.go`, `imagediff.go`,
`fixtures/critique/**`, `audit_test.go`, `critique_test.go`, `critique_fixtures_test.go`,
`imagediff_test.go`, `e2e_p8_test.go`; `internal/llm/tools/design_critique.go` + test;
`internal/api/design_critique_test.go`; `web-ui/src/components/design/IssuePanel.tsx`.

Modified: `internal/design/{model.go (Issue.Code), renderer.go (NodeFacts, Width),
render_script.go, browser.go}`, `internal/config/config.go` (`design.critique.policy`),
`internal/llm/tools/{design.go, builtin_names.go}`, `internal/llm/agent/tools.go`,
`internal/api/{handlers_design.go, routes.go}`, `cmd/design.go` + test,
`internal/tui/page/design.go` + test, `web-ui` store, `InspectorPanel.tsx`, `en/es.json`.

## Verification

`go build ./...`, `go vet`, `gofmt` clean on every touched file. Green: `./internal/design/...`,
`./internal/api`, `./internal/llm/tools`, `./internal/llm/agent`, `./internal/llm/prompt`,
`./internal/tui/page`, `./cmd`, `./internal/config`, `./internal/db`, `./internal/skills/...`,
`./internal/mesnada/acp`, `./internal/app`. Frontend `bun run build` clean, `bun run lint`
with the same 4 pre-existing warnings.

Real-binary smoke in a throwaway project: `design create --skill landing-page` then
`design critique` → strict policy from the template, 8.5/10 ITERATE naming the missing
`system.css`; after `design system init` → 10.0/10 PASS. A deliberately broken deck fired ten
findings across contrast, control name, image alt, page break, network failure, heading order,
missing h1, tap target, document title and a hardcoded value, scoring 0.0 ITERATE.
`--json`, `--no-render`, an unknown artifact and a bad `--policy` all behave.

## Next — P9 (optional, post-MVP)

Remaining kinds (mobile, document, dashboard, diagram); PPTX export and motion/video
(decision 9 deferral); Figma import/export; direct-manipulation canvas (decision 2 revisited).
Also still open from the tuning note in §12.1: the default
`design.critique.{max_rounds,threshold}` were left at 3 and 8.0 and can now be tuned against
the fixture set.
