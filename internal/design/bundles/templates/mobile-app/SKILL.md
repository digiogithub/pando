---
name: mobile-app
description: Mobile app screens in a device frame — a flow of screens, or one screen at high fidelity.
version: 1.0.0
author: Pando
license: MIT
when-to-use: "The brief is a phone or tablet interface: an app screen, a flow between screens, a mobile prototype."
when-not-to-use: The brief is a responsive website; design that at its real widths instead of inside a phone.
od:
  mode: template
  surface: web
  category: product
  scenario: A row of phone-sized screens showing one flow, at the fidelity the question needs.
  example_prompt: "Design the onboarding flow for a habit tracker: welcome, pick three habits, set a reminder time, and the first day's home screen. iOS, light mode."
  preview:
    type: page
    viewport:
      width: 1600
      height: 1000
  design_system:
    requires: true
  craft:
    requires:
      - process
      - typography
      - color
      - layout
      - interaction
      - anti-ai-slop
  critique:
    policy: strict
---

# Mobile app

A phone screen is small, held in one hand, used in motion, and read at arm's
length. Every constraint below comes from that, not from taste.

## Ask first

Platform (iOS or Android), light or dark, and phone or tablet. The two platforms
have different navigation models, different status bars and different type
scales, and a screen that mixes them reads as neither.

## The device frame

The scaffold ships a plain frame at 390×844 (iPhone-class) with a status bar and
a home indicator. Lay screens out as a row of frames, one per step of the flow,
each captioned with the step and what advances it.

Keep the frame plain: a rounded rectangle, a notch or pill, a status bar. A
photoreal phone render draws attention away from the screen inside it.

## Rules

- **Safe areas are real.** Nothing important within the status bar (top 44–54px)
  or under the home indicator (bottom 34px). The scaffold reserves both.
- **Touch targets 44×44px minimum**, with real space between adjacent ones. A
  row of small icons at the edge of the screen is unusable in a moving train.
- **One primary action per screen**, reachable by a thumb — the bottom half of
  the screen, not the top corner.
- **Type**: 17px body on iOS, 16sp on Android; never below 13px for anything a
  user must read. The scale is tighter than a web page's: two or three sizes.
- **Navigation belongs to the platform.** iOS: back at the top left, tab bar at
  the bottom. Android: a top app bar, a bottom navigation bar or a rail. Do not
  invent a third model.
- **Show the states.** A flow of five happy-path screens hides every decision
  that matters. Include at least the empty state and one error or loading state
  from `craft/interaction.md`.
- **Content is real**, not lorem: a habit list with three plausible habits tells
  you the row height is wrong. Placeholder text does not.

## Method

1. List the steps of the flow, one line each, before drawing anything.
2. Build the frames in order, left to right, with the transition labelled
   between them.
3. Add the non-happy states last, once the main path holds.

## Finish

`design_render`, then `design_inspect` the primary action on each screen and
check it clears 44px. Present with `design_present` using `view: "canvas"` so
the whole flow is readable at once.
