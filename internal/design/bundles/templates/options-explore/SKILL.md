---
name: options-explore
description: Several design directions side by side in one artifact, grouped by round so every iteration stays comparable.
version: 1.0.0
author: Pando
license: MIT
when-to-use: The user asked for options, variations, or "show me a few directions" — and will want more rounds after seeing them.
when-not-to-use: One direction is already agreed; build that one properly instead of padding it with alternatives.
od:
  mode: template
  surface: web
  category: exploration
  scenario: Present two to four directions per round, keep earlier rounds intact, and let the user name one to continue with.
  example_prompt: Show me three directions for the pricing section of a self-hosted backup tool — one table-led, one card-led, one single-plan — at full fidelity.
  preview:
    type: page
    viewport:
      width: 1600
      height: 1000
  design_system:
    requires: false
  craft:
    requires:
      - process
      - typography
      - color
      - layout
      - anti-ai-slop
  critique:
    policy: standard
---

# Options

Options are for comparison, so everything about this template exists to keep the
comparison honest: one artifact, one page, every round preserved.

## The structure

The page is a stack of rounds, **newest first**. Each round is one
`<section class="round">` holding its options side by side in a wrapping row.

Every option gets:

- A **stable id** of the form `{round}{letter}` — `1a`, `1b`, `2a` — on its
  wrapper element, so `#1b` scrolls it into view.
- That id shown as a **visible badge**, because the user needs to be able to say
  "go with 1b" in chat.
- A **one-line description** of what makes it different, and what it costs.

When the user asks for more, insert a **new** `<section class="round">` above
the existing ones and leave every earlier round exactly as it is. Never edit an
old option to "improve" it: the user is comparing against what they already saw.

## Rules

- **Two to four options per round.** One is not a choice; six is a way of
  avoiding one.
- **Vary one dimension at a time.** Three options that differ in layout *and*
  palette *and* type teach nothing, because no one can say which change did it.
  Name the dimension in the round's heading.
- **Every option is real.** Same content, same fidelity, same design system.
  An option built worse than its siblings is not an option, it is a straw man.
- **Say the trade-off.** An option with no cost named has not been thought
  about.
- Options within a round share a width so they compare visually; if one needs a
  different width to make its point, say so in its description.

## Method

1. Name the decision the round is meant to settle, and put it in the round's
   heading.
2. Pick the dimension that decision turns on.
3. Build each option with the same content, changing only that dimension.
4. Write each trade-off line last, when you can see them next to each other.

## Finish

`design_render`, then `design_present` with `view: "canvas"`. Ask which id to
continue with, and say plainly which one you would pick and why — a designer who
presents three options with no recommendation has handed the work back.
