---
name: dashboard-page
description: "Data-dense dashboard screen: summary figures, one primary chart, one table."
version: 1.0.0
author: Pando
license: MIT
when-to-use: The brief is a screen someone reads every day to answer a recurring question.
when-not-to-use: The brief is a page that persuades, or a one-off report.
od:
  mode: template
  surface: web
  category: product
  scenario: An operational screen that answers one recurring question at a glance.
  example_prompt: "A dashboard for a warehouse team: orders waiting to ship, average pick time today versus last week, and the ten oldest unshipped orders."
  preview:
    type: page
    viewport:
      width: 1440
      height: 1024
  design_system:
    requires: true
  craft:
    requires:
      - process
      - content
      - layout
      - color
      - typography
      - anti-ai-slop
      - interaction
  critique:
    policy: standard
---

# Dashboard page

A dashboard answers a question. Name the question first; every element either helps answer
it or leaves.

## Structure

- **Summary row** — at most four figures. Each carries a comparison ("vs. last week"),
  because a number with nothing to compare it against tells nobody anything.
- **One primary chart** — the shape of the question over time. One chart, not four.
- **One table** — the rows the reader will act on, sorted by the thing that makes them
  urgent. Ten rows, then a link to the rest.

## Rules

- Numbers are tabular: `font-variant-numeric: tabular-nums`, right-aligned in tables.
- Never encode a state in colour alone: add the word ("Late", "Blocked") next to it.
- Charts render as inline SVG. No chart library, no CDN, no network request.
- Empty state is mandatory: what the screen shows on a day with no data.
- All data in the scaffold is synthetic and must be visibly so. Never invent figures and
  present them as the user's own.

## Finish

Check the summary row at 375px: four figures across a phone is four unreadable figures. They
stack.
