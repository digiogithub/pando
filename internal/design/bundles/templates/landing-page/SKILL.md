---
name: landing-page
description: Single-page marketing site with a hero, a proof section and one call to action.
version: 1.0.0
author: Pando
license: MIT
when-to-use: The brief is a product, service or event page whose job is one conversion.
when-not-to-use: The brief needs several pages, an app UI, or a document to read.
od:
  mode: template
  surface: web
  category: marketing
  scenario: Launch page for a product, service or event.
  example_prompt: Build a landing page for a self-hosted backup tool for small teams. One price, no free tier. The audience is sysadmins who distrust cloud storage.
  preview:
    type: page
    viewport:
      width: 1440
      height: 900
  design_system:
    requires: true
  craft:
    requires:
      - process
      - content
      - typography
      - color
      - layout
      - anti-ai-slop
  critique:
    policy: strict
---

# Landing page

Build one page that makes one promise and asks for one action.

## Before writing markup

1. Read `craft/anti-ai-slop.md` first. Most of this template's failure modes are listed
   there, and they are easier to avoid than to remove.
2. Read the design system contract (`designer/_system/DESIGN.md`). Every colour, size and gap
   comes from a token; if a value you need is missing, add the token rather than a literal.
3. Name the single action the page asks for. If you cannot name it, ask the user before
   building.

## Structure

The scaffold ships the skeleton. Sections, in order:

- **Hero** — one headline stating what the thing does for whom, one supporting sentence, one
  button. No gradient, no floating shapes, no emoji.
- **Proof** — the concrete reason to believe: what it does, how, what it costs. Use the
  user's real facts. Never invent a customer, a logo, a testimonial or a number.
- **Close** — restate the action. Same button label as the hero, not a different one.

Add a section only when the brief supplies content for it. An empty section is worse than a
missing one.

## Rules

- Content width caps at `65ch` for prose. Full-bleed is for background colour only.
- The call to action is the only element using `--color-accent`.
- Test at 375px before declaring it done: nothing may scroll horizontally.
- Body text keeps a contrast ratio of at least 4.5:1 against the surface behind it.

## Finish

Run `design_render`, then `design_inspect` on the hero and the button. Fix what the console
reports before asking the user to look.
