---
id: PANDO-US-0014
type: story
title: Per-profile persona, prompt and model override via SetSessionLLMOverrides
status: backlog
priority: medium
parent: PANDO-EP-0002
milestone: PANDO-M-0001
author: claude
labels: [agui]
estimate: 2
created: 2026-09-13T21:14:53Z
updated: 2026-09-13T21:14:53Z
---

## Description

As a deployment owner, I want each profile to carry its own persona, extra system prompt and model, so that a backlog assistant and a docs assistant in one process do not share the adapter-wide `[AGUI] Persona`.

`Persona` is adapter-wide today (`internal/agui/deps.go:76-79`) and applied per session in `internal/agui/runtime.go:163-171` through `agent.SetSessionLLMOverrides`, which already carries `Model`, `Persona` and `PersonaScoped` (`internal/llm/agent/session_overrides.go:23-38`). Resolve the profile's `Model`/`Persona`/`Prompt` at the same point and pass them instead of the adapter-wide values, falling back to `[AGUI] Persona` when the profile declares none. Because the overrides are per session and the session is per thread, two threads on two profiles in one process must not see each other's values.

Do NOT add a second override mechanism, do NOT mutate the shared `config.Agent` entry for the base name, and do NOT make the override sticky across a thread that later resolves to a different profile.

## Acceptance Criteria

- [ ] Two threads running two profiles concurrently in one process resolve different personas and different models; asserted in `internal/agui/runtime_test.go` with the two runs interleaved, not serialized.
- [ ] A profile with no `Persona` falls back to adapter-wide `[AGUI] Persona`; with neither set, behaviour is unchanged from today.
- [ ] A profile `Prompt` reaches the session's system text and is absent for runs on other profiles.
- [ ] A profile `Model` is reflected by `GET {path}/info` and by the model actually used for the run.

## Notes

Depends on the profile-resolution story (PANDO-EP-0002). The mechanism already exists, so this is mostly wiring plus a concurrency test. Size **S**.
