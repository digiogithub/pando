---
name: deck-basic
description: "Plain, readable slide deck: one idea per slide, big type, print-correct PDF."
version: 1.0.0
author: Pando
license: MIT
when-to-use: The brief is a talk, a review or a pitch that will be presented live.
when-not-to-use: The brief is a document to be read alone — write a page instead.
od:
  mode: template
  surface: deck
  category: presentation
  scenario: A deck presented live and exported to PDF afterwards.
  example_prompt: "A ten-slide internal review of last quarter's incident response: what broke, how long each outage lasted, the three fixes we shipped, and what we still owe."
  preview:
    type: deck
    viewport:
      width: 1280
      height: 720
  design_system:
    requires: true
  craft:
    requires:
      - process
      - content
      - typography
      - layout
      - anti-ai-slop
  critique:
    policy: standard
---

# Basic deck

One idea per slide. The slide is the caption; the speaker is the content.

## Method

1. Write the outline as a list of slide *titles* first. If two titles say the same thing,
   the slides merge. If one title needs "and", it splits.
2. Then fill each slide. A slide holds a title plus at most three lines, or a title plus one
   image, or a title plus one number.

## Rules

- Body text starts at 24px. The audience is metres away.
- No bullet list longer than three items. A five-item list is two slides.
- No slide notes rendered on the slide. Speaker notes go in a `<aside class="notes" hidden>`.
- Every slide is a `<section class="slide" data-slide="N">` — the renderer and the PDF export
  both index on it, so the attribute is not decoration.
- Keep the print styles the scaffold ships. PDF export prints one slide per page and that
  only works while `@page` and the page breaks are intact.

## Finish

Export to PDF with `design_export` and count the pages. Pages != slides means the print
styles were edited into something else.
