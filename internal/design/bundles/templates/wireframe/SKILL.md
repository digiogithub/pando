---
name: wireframe
description: Low-fidelity screens and storyboards for exploring structure before anyone argues about colour.
version: 1.0.0
author: Pando
license: MIT
when-to-use: The brief is still open — several structures are plausible and nobody has committed to one.
when-not-to-use: The structure is settled and the user wants the finished look; build the real thing instead.
od:
  mode: template
  surface: web
  category: exploration
  scenario: Explore several layouts or a whole flow before committing to a visual direction.
  example_prompt: Wireframe three ways to lay out the settings screen of a backup tool — a single scrolling page, a sidebar of sections, and a wizard — plus the flow from "add a source" to "first backup running".
  preview:
    type: page
    viewport:
      width: 1440
      height: 900
  design_system:
    requires: false
  craft:
    requires:
      - process
      - layout
      - typography
      - content
  critique:
    policy: lenient
---

# Wireframe

Wireframes exist to make structure arguable. Their value is that they are cheap
to throw away, so make many and hold none of them precious.

## Rules of the fidelity

The scaffold ships a deliberately flat palette — greys, one dashed placeholder
style, one type family. Keep it:

- **No colour** beyond the greys, except a single accent used only to mark the
  primary action on each screen.
- **No imagery.** An image is a labelled box: `[ product shot ]`, in monospace.
- **No final copy.** Real headings (they carry the structure), summarised body
  ("three lines about pricing"). Do not write finished marketing copy into a
  wireframe: it invites feedback on the words instead of the layout.
- **No shadows, gradients, rounded-corner styling, or icon sets.** Anything that
  reads as "designed" moves the conversation to the wrong subject.

## Structure

Lay screens out as a grid of `<figure class="screen">` cards on one page, each
with a caption. Two arrangements, both supported by the scaffold:

- **Alternatives** — the same screen done three ways, side by side, labelled
  `A`, `B`, `C`, with a one-line note under each saying what it trades away.
- **Storyboard** — the steps of one flow, left to right, numbered, with the
  action that moves the user to the next step named on the connector.

Say what you are showing in the page header: the question these wireframes are
meant to settle.

## Method

1. List the screens or options before drawing any. If two options differ only in
   spacing, they are one option.
2. Draw the smallest set that makes the difference visible. Three alternatives
   is usually right; six is a way of avoiding a decision.
3. Under each option, state its trade-off in one line. An option with no
   trade-off named has not been thought about.
4. Mark exactly one primary action per screen. If you cannot, the screen has no
   job yet.

## Finish

`design_render`, then present with `design_present` using `view: "canvas"` so
the options sit side by side and can be compared at a glance. Ask which
direction to take before building anything at full fidelity.
