---
id: PANDO-US-0055
type: story
title: P5 Settings shell and Appearance section, migrate settings panels
status: done
priority: medium
parent: PANDO-EP-0010
milestone: PANDO-M-0003
author: mcp
labels: [webui, design, settings, theme]
estimate: 5
created: 2026-09-24T21:01:11Z
updated: 2026-09-24T21:44:58Z
started: 2026-09-24T21:19:14Z
closed: 2026-09-24T21:44:58Z
---

## Description

Rebuild SettingsView as a native settings window, with left category navigation and content in SettingsSection/SettingsRow cards. Keep the mobile master-detail layout.

Add a new Appearance section:
- a light/dark/system segmented control
- a family selector with palette previews
- accent swatches (the first swatch is "theme default")
- a UI font size setting

Replace ThemePicker. Migrate all 23 panels in `components/settings/` to the primitives.

## Acceptance Criteria

- [ ] Appearance changes apply live and persist.
- [ ] Every settings panel still saves correctly.
- [ ] The mobile master-detail layout still works.
- [ ] typecheck passes.
