---
name: print-flier
description: A single fixed-size canvas — flier, poster, social post, banner — laid out once and exported as an image or PDF.
version: 1.0.0
author: Pando
license: MIT
when-to-use: "The output has one fixed size and does not reflow: a poster, a flier, a social graphic, an ad, an infographic."
when-not-to-use: The output is read at any width, or runs to more than one page.
od:
  mode: template
  surface: web
  category: print
  scenario: One canvas at an exact size, designed to be printed or posted rather than browsed.
  example_prompt: An A4 flier for a Saturday repair café at the community centre — what to bring, what volunteers can fix, the time and the address. No stock photos.
  preview:
    type: page
    viewport:
      width: 794
      height: 1123
  design_system:
    requires: false
  craft:
    requires:
      - process
      - content
      - typography
      - color
      - layout
      - print
      - anti-ai-slop
  critique:
    policy: strict
---

# Flier and fixed canvas

One surface, one size, one message, seen from further away than a web page.

## Ask first if the size is unclear

A4 or Letter? Portrait or landscape? Printed or posted online? A square for
Instagram or a 1.91:1 for a link card? These change the layout completely, and
guessing wastes the build. The scaffold is set to A4 portrait at 96dpi
(794×1123px); change the two custom properties at the top of the stylesheet to
retarget it.

## The hierarchy is the design

A poster is read in three passes, from across a room and then closer:

1. **The hook** — five words or fewer, at a size that carries across the room.
   Usually 60–150px on an A4 canvas.
2. **The what and when** — the two or three facts a reader needs to act.
3. **The details** — address, price, small print, a link. Readable at arm's
   length, no smaller than 8pt.

If a reader cannot get pass 1 in a second and pass 2 in five, the hierarchy is
wrong, not the copy.

## Rules

- **One idea.** A flier advertising three things advertises nothing.
- **One loud element.** Type, a colour field, or one image — not all three.
- **Margins are structural.** Keep at least 12mm of quiet edge; nothing
  meaningful within 5mm of the trim. See `craft/print.md` for bleed.
- **No hand-drawn SVG imagery.** A labelled placeholder box and a request for
  the real asset beats an approximation. Flat colour fields, rules and simple
  geometry are yours to use.
- **Contrast for a printer, not a screen.** Large saturated backgrounds print
  darker and muddier than they look; keep body text on a light ground.
- **Never invent a fact**: no made-up prices, dates, phone numbers or
  organisation names.

## Method

1. Set the canvas size before anything else.
2. Place the hook, the facts and the details as three blocks, and get the
   hierarchy right in greyscale before adding any colour at all.
3. Add colour last, and only where it does a job.

## Finish

`design_render`, then `design_export` — `png` for posting, `pdf` for printing.
Check the export at 25% zoom: at that size only the hook should still be
legible, and it must be. Then `design_present`.
