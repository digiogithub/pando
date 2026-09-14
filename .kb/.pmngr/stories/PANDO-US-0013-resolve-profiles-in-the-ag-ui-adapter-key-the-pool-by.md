---
id: PANDO-US-0013
type: story
title: Resolve profiles in the AG-UI adapter, key the pool by profile, list them in /info
status: done
priority: high
parent: PANDO-EP-0002
milestone: PANDO-M-0001
author: claude
labels: [agui, config]
estimate: 5
created: 2026-09-13T21:14:53Z
updated: 2026-09-13T21:14:53Z
---

## Description

As a browser client, I want `POST {path}/<profile>` to run a declared profile with its own toolset, so that several restricted assistants share one `agui-serve` process.

`internal/agui/runtime.go:104-117` (`resolveAgent`) currently 404s anything outside `KnownAgentNames`; change it to return `(baseAgentName, *AGUIProfile)` so a declared profile resolves to its `Base` plus its overrides, and leave the 404 for undeclared names. `internal/agui/deps.go:93-127` carries the profiles through `ConfigFromApp`, and `allowsAgent` (`deps.go:130-137`) must accept profile names alongside the pruned `Agents` set (`deps.go:117-122`). In `internal/agui/agentpool.go:58-129`, key the pool by profile name rather than base agent name (the key already hashes frontend tools at `agentpool.go:173-192` — extend, do not replace, that key) and apply the profile's `Tools`/`DenyTools`/`Mesnada` through the filter added by the allow-list story, with the adapter-wide `[AGUI] Tools` as the fallback when a profile declares none. `internal/agui/server.go:177-184` (`/info`, with `modelDescriptor` at `server.go:198-217`) lists each profile and its effective model.

Do NOT instantiate an agent to answer `/info` — resolve the model from config only. Do NOT let a profile's `Base` leak as the route name, and do NOT widen `KnownAgentNames`.

## Acceptance Criteria

- [ ] `POST {path}/backlog-assistant` runs the profile end to end; an undeclared name still answers 404 (`internal/agui/server_test.go`).
- [ ] Two profiles over the same `Base` get two distinct pooled `agent.Service` instances with different toolsets, and neither evicts the other on a pool lookup.
- [ ] `GET {path}/info` lists every profile with its effective model, and the test asserts no agent was instantiated while serving it.
- [ ] A profile's tool allow-list is enforced on the run exactly as the adapter-wide list is, including the `tool_search` bypass regression.
- [ ] Built-in agent names keep working unchanged when no profile is declared.

## Notes

Depends on the `[AGUI.Profiles.<name>]` schema story and on the adapter-wide allow-list story (both PANDO-EP-0002). Blocks the per-profile persona/model story. Pool eviction wart noted in the source report (`agentpool.go:140-154` can evict an entry serving a run) is out of scope here — it belongs to the operability epic. Size **M** (~200 LOC + tests), all inside `internal/agui`.
