---
name: html-email
description: A send-ready HTML email that survives Outlook, Gmail and every client that ignores modern CSS.
version: 1.0.0
author: Pando
license: MIT
when-to-use: The output will be sent as an email — announcement, newsletter, receipt, invitation.
when-not-to-use: The output is a web page people visit; email constraints would only make it worse.
od:
  mode: template
  surface: web
  category: email
  scenario: One email, one message, rendered the same in clients whose CSS support stopped twenty years ago.
  example_prompt: An email announcing that the self-hosted backup tool now supports S3-compatible storage — what changed, how to enable it, and where the docs are. Plain, no marketing tone.
  design_system:
    requires: false
  preview:
    type: page
    viewport:
      width: 800
      height: 1000
  craft:
    requires:
      - process
      - content
      - typography
      - color
  critique:
    policy: standard
---

# HTML email

Email clients are not browsers. Everything you know about modern layout is
unavailable, and the constraints below override normal instincts — including
what the design system says about layout.

## The hard constraints

- **Tables for layout.** `<table role="presentation">` nested as deeply as the
  layout needs. No flexbox, no grid, no float. Outlook's engine is Word's.
- **Inline styles on every element.** A `<style>` block in the head is stripped
  or ignored by several major clients. Write the styles inline; keep an optional
  `<style>` block only for `@media` tweaks that are pure enhancement.
- **600px content width.** Wider is cut off in preview panes. The outer table is
  100% wide with a fixed-width table centred inside it.
- **Web-safe fonts only**, with a real stack: Arial, Helvetica, Georgia,
  Verdana, Tahoma, Times. A web font is a bonus that most recipients never see,
  so the design must hold in the fallback.
- **Background colours on cells, not on the body.** Never rely on a background
  image; several clients drop them.
- **No JavaScript, no forms, no `position`, no negative margins.** They are
  stripped, and a stripped rule leaves a broken layout behind.
- **Images off by default.** Every image needs meaningful `alt` text, explicit
  `width` and `height`, and `display:block`. The email must make complete sense
  with every image blocked — so the message never lives inside an image.

## Structure

The scaffold ships the standard skeleton: preheader, header, body, call to
action, footer. Keep it.

- **Preheader**: 40–90 characters of hidden text after `<body>`. It is what the
  inbox shows next to the subject line, and an email without one shows the first
  words of the header instead.
- **One call to action**, as a bulletproof button (a table cell with a
  background colour and a padded `<a>`), not an image.
- **Footer**: who is sending, why the recipient is getting it, and an
  unsubscribe link for anything that is not transactional. This is a legal
  requirement in most jurisdictions, not a design choice.

## Accessibility and etiquette

- `<table role="presentation">` on every layout table, so screen readers do not
  announce a grid.
- Set `lang` on `<html>`, and a real `<title>` — some clients use it.
- Body text 14–16px, line height 1.5, and a 4.5:1 contrast ratio. People read
  email on phones in daylight.
- Link colour defined explicitly; the default blue on a coloured background is
  frequently unreadable.
- Dark mode: several clients invert colours on their own. Avoid pure white
  backgrounds behind dark logos, and test that inverted text stays legible.

## Finish

`design_render`, then read it at 375px wide as well as 600. Check that the
message survives with images blocked. `design_export` as `html` produces the
file to paste into a sending tool.
