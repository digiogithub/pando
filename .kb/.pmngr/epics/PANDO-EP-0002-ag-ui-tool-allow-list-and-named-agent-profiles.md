---
id: PANDO-EP-0002
type: epic
title: AG-UI tool allow-list and named agent profiles
status: backlog
priority: critical
milestone: PANDO-M-0001
labels: [agui, config, security]
created: 2026-09-13T21:11:26Z
updated: 2026-09-13T21:11:26Z
---

## Description

AG-UI agent names are a closed seven-name enum (`internal/config/config.go:73-90`) and `internal/agui/agentpool.go:82-91` builds every one of them with the identical full coder toolset (bash, edit, write, browser, mesnada) regardless of the name. No path filters tools: `RunAgentInput.tools` only adds, `[AGUI] Persona` and `AutoApprove` are adapter-wide, and `ToolDiscovery` is visibility rather than authorization because a deferred tool stays executable through `tool_search` (`internal/llm/tools/tool_discovery.go:103-106`). A product that wants a "backlog assistant" with only its own MCP tools and KB search cannot have one.

Two steps, in order. First, an adapter-wide `[AGUI] Tools` allow-list (with `Mesnada` off) applied as a subtractive filter before `agent.NewAgent`; small, and it unblocks embedding hosts immediately with one `agui-serve` process per profile. Second, named profiles `[AGUI.Profiles.<name>]` carrying `Base`, `Model`, `Persona`, `Prompt`, `Tools`, `DenyTools` and `Mesnada`, resolved by the adapter and listed by `/info`, so several restricted assistants share one process.

## Acceptance Criteria

- [ ] `[AGUI] Tools` (glob allow-list) and `[AGUI] Mesnada` exist; a run under a list naming only `gintrack__*` and `kb_search_documents` exposes no `bash`, `edit`, `write`, `browser` or `mesnada_*` in the model's tool schema, and a regression test proves `tool_search` cannot reach a denied tool.
- [ ] Frontend tools declared in `RunAgentInput.tools` are still added on top of the allow-list and cannot shadow a denied name.
- [ ] `[AGUI.Profiles.<name>]` parses, validates (`Base` must be a known agent, unknown keys refused) and round-trips through the config API; `KnownAgentNames` and existing configs are unchanged.
- [ ] `POST {path}/<profile>` runs the profile; an undeclared name still answers 404; two profiles over the same `Base` get distinct pooled instances; `GET /info` lists each profile with its model without instantiating an agent.
- [ ] Two threads on two profiles in one process resolve different personas and models concurrently (reusing `SetSessionLLMOverrides`).

## Notes

Decision of 2026-09-13: ship the allow-list first as the load-bearing half, placing the config key where the profile work later absorbs it. Do not ship an embedded assistant with the full coder toolset and only `AutoApprove=false` as protection. Evidence in `report-agui-server.md` §1.
