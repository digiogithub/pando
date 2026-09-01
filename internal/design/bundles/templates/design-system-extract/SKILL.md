---
name: design-system-extract
description: Build a project's design system from existing code, a live URL, an image or a written style guide.
version: 1.0.0
author: Pando
license: MIT
when-to-use: A project needs a design system before any artifact is built, or the existing one has drifted.
when-not-to-use: The design system is already committed and current.
od:
  mode: workflow
  category: system
  scenario: Turn an existing look into tokens the agent can obey.
  example_prompt: Extract our design system from the marketing site at https://example.com, then apply it to the landing page artifact.
  design_system:
    requires: false
  craft:
    requires:
      - process
      - color
      - typography
  critique:
    policy: none
---

# Extract a design system

A design system is three committed files in `designer/_system/`: `tokens.json` (the source of
truth), `system.css` (generated), and `DESIGN.md` (the written contract).

## Pick the source honestly

| Source | Use when | What it cannot tell you |
|---|---|---|
| `code` | The project already has stylesheets or components | Nothing about intent — only what is written |
| `url` | The look lives on a page that is already deployed | Nothing about states you did not render |
| `image` | Only a screenshot or a mockup exists | Typography and spacing: colours only |
| `text` | A written style guide or brand document exists | Anything the document does not state |

## Method

1. `design_system(action: "extract", source: ..., target: ...)`. This returns a proposal and
   writes nothing.
2. **Read the notes.** They say what the source could not answer. A role with no plausible
   candidate is deliberately left at its accessible default; do not fill it with a guess.
3. Correct the proposal with the user before committing. Extraction measures; it does not
   know which colour is the brand.
4. `design_system(action: "set", ...)` to commit. This writes all three files and regenerates
   the token table inside `DESIGN.md` without touching the prose around it.
5. `design_system(action: "apply", artifact_id: ...)` for each artifact. It links the
   stylesheet and *reports* hardcoded literals a token would cover — it never rewrites them,
   because the same hex can be the brand colour in one place and a deliberate one-off in
   another.

## Rules

- Declared CSS custom properties win over anything inferred: a declaration is a definition.
- Do not invent tokens the source does not support. An honest six-token system beats a
  fabricated thirty-token one.
- Write the prose in `DESIGN.md` yourself: the token table is generated, the contract is not.
