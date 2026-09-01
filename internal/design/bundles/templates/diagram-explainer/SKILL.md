---
name: diagram-explainer
description: A diagram or chart that explains a real mechanism or a real dataset, drawn from values rather than decoration.
version: 1.0.0
author: Pando
license: MIT
when-to-use: The brief is to explain how something works or what some numbers say — architecture, a flow, a comparison, a trend.
when-not-to-use: There is nothing to explain and the picture would be decoration; leave it out.
od:
  mode: template
  surface: web
  category: explanation
  scenario: One diagram or chart, at a size that reads on a slide and in a document, built from the user's real values.
  example_prompt: Diagram how a backup runs end to end — agent, queue, worker, object store, restore path — and mark where the two failure modes we saw last month occur.
  preview:
    type: page
    viewport:
      width: 1280
      height: 800
  design_system:
    requires: false
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

# Diagram and chart

A diagram earns its place when the picture says something the sentence cannot.
If a sentence would do, write the sentence.

## Decide what kind of picture it is

- **Mechanism** — how a thing works: boxes for parts, arrows for what actually
  moves between them, labelled with what moves. Not a generic three-tier stack.
- **Sequence** — what happens in what order, with the actor on each step.
- **Comparison** — bars, for values across categories.
- **Trend over time** — a line, with time on the horizontal axis.
- **Part of a whole** — a stacked bar. Not a pie chart, unless there are two
  slices and the point is "roughly half".
- **Relationship between two measures** — a scatter plot.

Pick from what the data is, not from what looks impressive.

## Rules for diagrams

- Every arrow is labelled with what travels along it. An unlabelled arrow means
  "related somehow", which is the thing a diagram is supposed to fix.
- Every box is a real component with the name the code or the org uses.
- Direction is consistent: one axis of flow, left-to-right or top-to-bottom,
  never both at once.
- Mark what is uncertain or out of scope explicitly (dashed, greyed, labelled)
  rather than leaving it out and implying it does not exist.
- Six to nine boxes is a diagram. Fifteen is a map, and it needs to become two
  diagrams or a table.

## Rules for charts

- **Never invent a number.** Every value comes from the user. If a value is
  missing, the chart shows the gap rather than an estimate.
- Bar charts start at zero. Line charts may not, but must say so on the axis.
- Label the values directly on the marks when there are few enough; a legend is
  a lookup table the reader has to hold in their head.
- One idea per chart. Two y-axes are two charts.
- Units and the period in the axis title, the source under the chart.
- Colour separates series and nothing else; a sequence of values gets one hue at
  varying lightness, and no chart needs more than about five distinguishable
  colours. Never encode meaning in colour alone — pattern, label or order too.

## Drawing it

Draw with HTML and CSS — boxes, grid, borders, a couple of rules — or with SVG
kept to lines, rectangles, circles and text. Both survive scaling and stay
editable. Do not hand-draw illustrative artwork; see `craft/anti-ai-slop.md`.

Size the canvas so it reads at slide size and at document size: 1280×800 is a
good default, type no smaller than 14px inside it.

## Finish

`design_render`, then read the diagram out loud as a sentence following the
arrows. If the sentence is not true, the diagram is not either. `design_export`
as `png` to drop it into a deck or a document.
