---
id: PANDO-T-0008
type: task
title: Fix hook_template_section contract mismatch between builder and documented Lua example
status: backlog
priority: medium
parent: PANDO-US-0072
author: mcp
labels: [prompt, lua, bug]
estimate: 2
created: 2026-09-29T21:28:50Z
updated: 2026-09-29T21:28:50Z
---

## Description

Found while implementing PANDO-US-0072. The prompt builder passes the section input to `hook_template_section` under `ctx.parameters.*` and only reads back a top-level `section_content` field from the returned table. `docs/lua-hooks-example.lua` uses `ctx.section_name` directly, which never matches, so hooks written from the documentation silently do nothing.

## Acceptance Criteria

- [ ] Decide one contract (flat `ctx.section_name`/`ctx.section_content` recommended, keeping `ctx.parameters` for compatibility) and implement it in the builder.
- [ ] `docs/lua-hooks-example.lua` and the KB prompt-template docs match the contract.
- [ ] A test runs the documented example hook and asserts the section is modified.

## Notes

The US-0072 variant tests use the currently working `ctx.parameters` shape.
