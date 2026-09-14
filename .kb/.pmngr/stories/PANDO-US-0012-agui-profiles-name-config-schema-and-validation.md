---
id: PANDO-US-0012
type: story
title: "[AGUI.Profiles.<name>] config schema and validation"
status: done
priority: high
parent: PANDO-EP-0002
milestone: PANDO-M-0001
author: claude
labels: [agui, config]
estimate: 3
created: 2026-09-13T21:14:53Z
updated: 2026-09-13T21:14:53Z
---

## Description

As a deployment owner, I want to declare named AG-UI agent profiles in config, so that one Pando process can serve several restricted assistants instead of one process per profile.

Add `AGUIProfile{Base, Model, Persona, Prompt, Tools, DenyTools, Mesnada}` and `AGUIConfig.Profiles map[string]AGUIProfile` next to `AGUIConfig` in `internal/config/config.go:1137`. `Base` is required and must satisfy `IsKnownAgent` (`config.go:56-90`); it supplies the model/token configuration the built-in agent already carries. `Tools`/`DenyTools` reuse the glob matcher introduced by the allow-list story; `Mesnada` defaults to true. Validation runs where the other AG-UI config validation runs, raising a named error for an unknown `Base` and refusing unknown keys inside a profile block rather than silently pruning them (config load prunes unknown *agent* keys at `config.go:3114` and `config.go:4358-4400` — profiles must not inherit that silence).

This story is config only: no adapter behaviour changes yet. Do NOT add fields to the `Agent` struct (`config.go:110-121`), do NOT extend `config.KnownAgentNames`, and do NOT let a profile name collide with a known agent name.

## Acceptance Criteria

- [ ] A profile with an unknown `Base` fails config validation with an error naming both the profile and the offending base.
- [ ] An unknown key inside `[AGUI.Profiles.<name>]` is refused with a named error, not dropped.
- [ ] A profile whose name equals a `KnownAgentNames` entry is refused.
- [ ] `KnownAgentNames` and the `Agent` struct are unchanged, and a config with no `Profiles` block loads byte-identically to today (test in `internal/config`).
- [ ] Profiles round-trip through the config API without loss (read-modify-write preserves every field, including an empty `Tools` list distinct from an absent one).

## Notes

Depends on PANDO-EP-0002's allow-list story for the glob matcher (reuse it, do not write a second one). Blocks the profile-resolution and per-profile-override stories in this epic. Size **S**.
