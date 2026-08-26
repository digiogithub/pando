---
name: web-prototype
description: Clickable multi-screen prototype of an application flow, no backend.
version: 1.0.0
author: Pando
license: MIT
when-to-use: The brief is an app flow to be walked through and reacted to, not a page to read.
when-not-to-use: The brief is marketing copy, a document, or a single static screen.
od:
  mode: template
  surface: web
  category: product
  scenario: Walk a stakeholder through an application flow before writing the real thing.
  example_prompt: "Prototype the onboarding flow for a payroll app: sign up, connect a bank account, add two employees, run the first payroll. Show the empty states."
  preview:
    type: page
    viewport:
      width: 1280
      height: 832
  design_system:
    requires: true
  craft:
    requires:
      - layout
      - color
      - anti-ai-slop
  critique:
    policy: standard
---

# Web prototype

Build the *flow*, not the backend. Every screen is real markup; every transition is a link
or a class toggle.

## Method

1. List the screens the brief implies, in order, before writing any markup. Confirm the list
   with the user if it is longer than five.
2. One `<section class="screen" id="...">` per screen in a single document. Navigation is
   plain anchors — no router, no build step, nothing to install.
3. State that must persist across screens goes in `sessionStorage` inside a `try/catch`. A
   prototype must still render when storage is unavailable.

## What to include

- The **empty state** of every list. It is the screen users actually meet first and the one
  most prototypes skip.
- The **error state** of every form that can fail.
- Realistic content. Three rows of plausible data beat twenty rows of "Item 1".

## What to leave out

- Authentication, real network calls, real payments.
- Animation beyond a 150ms transition. Motion hides flow problems.

## Rules

- Every interactive element is reachable by keyboard and shows a visible focus ring at 3:1
  contrast.
- Buttons that do nothing yet must say so, not fail silently.
- Never fabricate a user's real data. Use obviously synthetic names.

## Finish

Render each screen with `design_render` and check the console. A prototype with a JavaScript
error is a demo that will break in front of the stakeholder.
