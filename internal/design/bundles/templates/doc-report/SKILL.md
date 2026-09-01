---
name: doc-report
description: A page-style document — report, memo, one-pager, résumé — that reads on screen and prints correctly.
version: 1.0.0
author: Pando
license: MIT
when-to-use: The brief is something to be read alone and probably printed or sent as a PDF.
when-not-to-use: The brief is presented live to an audience — build a deck instead.
od:
  mode: template
  surface: web
  category: document
  scenario: A document that flows across paper pages and exports to PDF with the breaks in the right places.
  example_prompt: "Write the post-incident report for last month's four-hour storage outage as a two-page document: timeline, root cause, the three fixes shipped, and what is still outstanding."
  preview:
    type: page
    viewport:
      width: 816
      height: 1056
  design_system:
    requires: true
  craft:
    requires:
      - process
      - content
      - typography
      - layout
      - print
  critique:
    policy: standard
---

# Document

A document is read alone, at reading distance, usually in one sitting, and often
on paper. Every decision follows from that.

## Before writing

1. Ask the paper size if the brief does not say it — A4 and US Letter are not
   interchangeable, and the scaffold has to be set to one of them.
2. Decide the argument: what the reader knows at the start, what they should
   know at the end, and the order that gets them there. Write the section
   headings first; if they do not read as a coherent outline on their own, the
   document is not ready to be written.
3. Read `craft/print.md`. The break rules are the difference between a document
   and a web page that happens to be tall.

## Structure

The scaffold gives a title block, a flowing body and a running footer. Sections
are `<section>`; the page breaks between them are controlled in CSS, never by
hand-placed spacers.

- **Title block**: title, one-line subtitle, date and author. Nothing else.
- **Body**: headings no deeper than three levels. A fourth level means the
  document wants to be two documents.
- **Tables and figures**: captioned, and never split across a page break.
- **Footer**: page number, and the document's short title. Both on every page.

## Rules

- One column. Two columns is for a printed newsletter, and it makes on-screen
  reading worse.
- Measure 65–75 characters. On a Letter page at 11pt that is roughly the margin
  the scaffold sets — do not widen it to fit more in.
- Body copy 10–12pt. Anything smaller is a footnote, and footnotes are 9pt.
- No colour that carries meaning, because the document will be printed in
  greyscale by someone. Colour may emphasise; a shape, a label or a weight must
  carry the meaning.
- Never invent a number, a date, a name or a quote. See `craft/content.md`.

## Finish

`design_render`, then `design_export` with format `pdf`. Open it and check three
things: no heading stranded at the foot of a page, no table split, and the page
count is what the document actually needs. Then `design_present`.
