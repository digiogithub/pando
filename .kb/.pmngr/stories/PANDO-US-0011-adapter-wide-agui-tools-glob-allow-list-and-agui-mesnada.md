---
id: PANDO-US-0011
type: story
title: Adapter-wide [AGUI] Tools glob allow-list and [AGUI] Mesnada switch
status: done
priority: critical
parent: PANDO-EP-0002
milestone: PANDO-M-0001
author: claude
labels: [agui, config, security]
estimate: 3
created: 2026-09-13T21:14:53Z
updated: 2026-09-14T00:00:00Z
---

## Description

As an embedding host, I want a single adapter-wide tool allow-list on the AG-UI adapter, so that I can expose Pando as a restricted product assistant instead of a full coder with shell and filesystem access.

`internal/agui/agentpool.go:82-91` builds every AG-UI agent with `agent.CoderAgentToolsWithMesnada(...)` and hands the whole slice to `agent.NewAgent` (`agentpool.go:118-124`). Add `Tools []string` (glob allow-list, empty = today's behaviour) and `Mesnada bool` (default true) to `AGUIConfig` (`internal/config/config.go:1137`), carry them through `ConfigFromApp` (`internal/agui/deps.go:93-127`), and apply a **subtractive** filter on `t.Info().Name` (`path.Match` glob semantics) to the slice returned at `agentpool.go:91`, before `agent.NewAgent`. `Mesnada = false` must drop every `mesnada_*` tool even when no `Tools` list is given.

The bypass is the load-bearing part. `ToolDiscovery` is visibility, not authorization: a deferred tool stays executable through the single `tool_search` tool, which carries a remote executor for the whole MCP catalog (`internal/llm/tools/tool_discovery.go:103-106`). The filter must therefore run *after* `ApplyToolDiscovery`, and when an explicit `Tools` list is configured either `tool_search` is dropped entirely or its catalog and executor are narrowed to the allowed set. Frontend tools from `RunAgentInput.tools` are still appended on top (`agentpool.go:110-116`); extend the existing `reserved` name guard there so a frontend tool cannot claim a name the allow-list denies.

Do NOT reuse `filterToolsByNames` (`internal/llm/agent/tools.go:85-101`): it force-includes `alwaysIncludedTools` = bash/edit/view/glob/grep/write/patch/ls (`tools.go:45-61`) and so cannot exclude bash. Do NOT use `DeferredSources`/`NonDeferredTools` as an allow-list, do NOT touch the process-wide `InternalTools` booleans (`config.go:689-785`) which are shared with the TUI and Web UI, do NOT widen `config.KnownAgentNames`, and do NOT treat `AutoApprove=false` as a substitute boundary.

## Acceptance Criteria

- [ ] With `[AGUI] Tools = ["gintrack__*", "kb_search_documents"]`, the tool schema handed to the model for a run contains no `bash`, `edit`, `write`, `patch`, `browser` or `mesnada_*` entry — asserted in `internal/agui/agentpool_test.go`.
- [ ] Regression test with the same list proves a denied tool is unreachable through `tool_search`: it is neither returned by a search nor executable via the deferred-tool executor.
- [ ] `[AGUI] Mesnada = false` removes every `mesnada_*` tool with no `Tools` list present.
- [ ] An absent/empty `Tools` list leaves the toolset byte-identical to today; existing configs load and behave unchanged.
- [ ] A frontend tool declared in `RunAgentInput.tools` is still registered on top of the allow-list, and one declared with a denied name (e.g. `bash`) is refused rather than registered.

## Notes

Unblocks git-in-track's backlog assistant (**GIT-EP-0018**): with this key alone, one `pando agui-serve` process per profile already gives a restricted assistant, which is why it ships first and at critical priority. Decision of 2026-09-13: place the config key where the profile work absorbs it later as a per-profile field, so no migration is needed. Prerequisite for the rest of PANDO-EP-0002; no dependencies of its own. Size **S** (~80 LOC + tests), contained in `internal/agui` plus one config struct.
