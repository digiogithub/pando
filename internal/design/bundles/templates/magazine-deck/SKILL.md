---
name: magazine-deck
description: Editorial deck with full-bleed imagery, asymmetric layouts and pull quotes.
version: 1.0.0
author: Pando
license: MIT
when-to-use: The deck has to look designed — a pitch, a brand story, a conference talk.
when-not-to-use: The deck is an internal status update. Use deck-basic; it is faster to read.
od:
  mode: template
  surface: deck
  category: presentation
  scenario: A deck whose visual quality is part of the argument.
  example_prompt: "A twelve-slide pitch for a coffee roastery opening its first physical shop: the origin story, the roasting method, the numbers, the ask."
  preview:
    type: deck
    viewport:
      width: 1600
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

# Magazine deck

Editorial layout: asymmetry, generous margins, deliberate contrast between quiet and loud
slides.

## Slide vocabulary

Use these layout classes and vary them; three identical slides in a row lose the reader.

- `.slide--title` — full-bleed opener.
- `.slide--split` — text on one side, image or figure on the other. Alternate which side.
- `.slide--quote` — one pull quote, large, nothing else.
- `.slide--figure` — one number or one image at full bleed with a caption.

## Rules

- Rhythm matters: after a loud slide, a quiet one. Every slide loud is every slide flat.
- Pull quotes are real quotes from the user's material. Never invent an attributed quote.
- Images the user did not supply are placeholders and must look like placeholders. Do not
  fetch stock imagery from the network — the preview and the export run offline.
- Type sizes come from the display end of the scale. This is the one place a second font
  family is justified, and only for headings.
- Keep the print styles intact: PDF export prints one slide per page.

## Finish

Look at the deck at 25% zoom as a contact sheet. If every slide has the same silhouette, the
layouts were not varied.
