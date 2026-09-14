---
created_at: 2026-09-14T15:53:49.849560485Z
updated_at: 2026-09-14T15:53:49.849560485Z
tags:
    - feature
    - agui
    - config
    - security
---

# PANDO-US-0011: AG-UI adapter-wide Tools glob allow-list + Mesnada switch

Status: implemented 2026-09-14.

## What changed

- `internal/config/config.go` (`AGUIConfig` struct, ~line 1137): added
  `Tools []string` (glob allow-list, `toml:"Tools"`, empty = no restriction)
  and `Mesnada *bool` (`toml:"Mesnada"`). `Mesnada` is a pointer — mirroring
  `TUI.NerdFonts` elsewhere in the same file — because a plain `bool` cannot
  distinguish "key absent" from "explicitly false", and the story's scope
  rule ("work inside `AGUIConfig` and nothing else in config.go") ruled out
  registering a `viper.SetDefault("agui.mesnada", true)` in `setDefaults()`
  the way `RequireToken`/`FrontendTools`/`HumanInTheLoop` do. Resolution to
  the documented `true` default happens in `agui.ConfigFromApp` instead.
- `internal/agui/deps.go`: `Config` struct gained `Tools []string` and
  `Mesnada bool` (already resolved, non-pointer). `ConfigFromApp` defaults
  `Mesnada` to `true` then overrides it with `*c.Mesnada` when the config
  field is non-nil; `Tools` passes through as-is.
- `internal/agui/agentpool.go`:
  - `buildLocked` was split: the tool-set construction moved into a new
    `buildToolsLocked(frontendTools []Tool) []tools.BaseTool`, so tests can
    assert on the exact slice handed to `agent.NewAgent` without needing a
    live model provider. `buildLocked` now just calls it and wraps
    `agent.NewAgent`.
  - `buildToolsLocked` now: (1) builds `agentTools` via
    `agent.CoderAgentToolsWithMesnada` (unchanged — this already runs
    `agent.ApplyToolDiscovery` internally), (2) captures a `reserved` name
    set from that *pre-filter* slice, (3) calls the new
    `filterAGUITools(agentTools, p.cfg.Tools, p.cfg.Mesnada)`, (4) does the
    existing HITL `AskUserQuestion` substitution, (5) appends frontend tools
    via the *existing* `reserved` map (now built before filtering, so a
    denied name like `bash` stays reserved even though it is no longer in
    the filtered `agentTools`).
  - New `filterAGUITools(allTools, allow []string, mesnada bool) []tools.BaseTool`:
    a plain subtractive filter on `t.Info().Name`. Deliberately does **not**
    reuse `agent.filterToolsByNames` (`internal/llm/agent/tools.go`), which
    force-includes `alwaysIncludedTools` (bash/edit/view/glob/grep/write/
    patch/ls) — exactly the tools this allow-list must be able to exclude.
    With `allow` empty and `mesnada` true (today's defaults) it returns the
    *same* input slice unchanged (not a copy), satisfying the "byte-identical
    no-op" acceptance criterion.
  - New `aguiToolAllowed(name string, allow []string, mesnada bool) bool`:
    the single predicate. `!mesnada` unconditionally denies any
    `mesnada_*`-prefixed name, independent of `allow` (even if `allow`
    contains `"mesnada_*"` — the switch wins). Once `allow` is non-empty, the
    literal name `"tool_search"` is **always** denied, regardless of whether
    a glob (even `"*"` or `"tool_search"` itself) would otherwise match it.
    Otherwise falls through to `path.Match` against each glob.

## Why tool_search needed special-casing (the load-bearing part)

`agent.ApplyToolDiscovery` (`internal/llm/agent/tool_discovery.go`) — not
`internal/llm/tools/tool_discovery.go`, which does not exist; the story text
had the wrong package path — registers every tool (including MCP catalog
entries that were never a direct `tools.BaseTool` in `allTools`) into a
process-wide shared registry (`SharedDiscoveryRegistry`), then wires a single
`tool_search` tool backed by a `RemoteToolExecutor` that can search and
execute anything in that registry. `ToolDiscovery` is visibility, not
authorization: removing every other denied tool from the visible slice does
nothing if `tool_search` itself survives, since its executor still reaches
the full registry. `filterAGUITools` therefore always strips `tool_search`
once an explicit `Tools` allow-list is configured (chose "drop entirely" over
"narrow its catalog/executor" — the story allowed either, and narrowing would
need a per-adapter scoped registry, out of scope for an ~80 LOC story).

## Frontend-tool reserved-name guard (AC5)

`newFrontendTools` (`internal/agui/frontend_tool.go`) already refused a
client-declared tool whose name collides with an entry in its `reserved`
map. The fix was simply computing that map from `agentTools` **before**
`filterAGUITools` runs, instead of after: a name the allow-list denies (e.g.
`bash`) is still "reserved" even though it is no longer in the filtered
slice, so a page cannot reintroduce it via a frontend-tool proxy. No change
to `frontend_tool.go` was needed.

## Verification

- `go build ./...` — clean.
- `go test ./internal/agui/... ./internal/config/...` — all pass.
- New tests in `internal/agui/agentpool_test.go` (all passing):
  - `TestFilterAGUITools_EmptyAllowListIsNoOp` — byte-identical no-op (same
    backing slice, proven by mutating the input and observing it through the
    output).
  - `TestFilterAGUITools_AllowListIsSubtractiveOnly` — the AC1 example
    (`["gintrack__*", "kb_search_documents"]`) strips bash/edit/write/patch/
    browser_navigate/mesnada_spawn_agent/tool_search.
  - `TestAGUIToolAllowed_MesnadaSwitchWinsOverAllowList` — `Mesnada=false`
    drops `mesnada_*` even when an allow-list would otherwise match it.
  - `TestAGUIToolAllowed_ToolSearchAlwaysDeniedOnceAllowListSet` — `tool_search`
    survives with no allow-list, is always denied once one is set.
  - `TestFilterAGUITools_ToolSearchBypassRegression` — builds a real
    in-memory MCP gateway + catalog row, enables `ToolDiscovery` via
    `config.SetForTests`, calls the *real* `agent.ApplyToolDiscovery`, proves
    the unfiltered `tool_search` really can find `github_create_issue`
    (sanity check the bypass exists), then proves `filterAGUITools` removes
    `tool_search` from the set entirely.
  - `TestBuildToolsLocked_AllowListAndFrontendToolGuard` — exercises the real
    `agentPool.buildToolsLocked` (real tool constructors, not stubs): allow
    list `["glob","grep"]` excludes bash/edit/write/patch; a frontend tool
    named `bash` is refused, one named `showChart` still registers.
  - `TestBuildToolsLocked_ToolSearchNeverReachesBuiltSetUnderAllowList` —
    same bypass-closure proof through the real `buildToolsLocked` path with
    `ToolDiscovery` enabled process-wide.
- Pre-existing, unrelated failures observed when running the full
  `internal/llm/agent` package (not touched by this story): `TestSetAndGetCavemanMode`,
  `TestCavemanActivatesTheSessionPolicyPath`, `TestCavemanSessionPolicyInstructions`,
  `TestApplyToolDiscoveryWithoutManagerIsUnchanged` fail only when the whole
  package runs together (131 pass / 4 fail), but each passes individually —
  a pre-existing test-isolation/global-config-leakage issue in that package,
  not caused by this change. Not chased per task scope.

## Things deliberately NOT done (per story constraints)

- Did not touch `agent.filterToolsByNames` or its `alwaysIncludedTools` set.
- Did not touch `config.InternalTools` booleans (process-wide, shared with
  TUI/Web UI).
- Did not widen `config.KnownAgentNames`.
- Did not touch `internal/rag/`, the SDK, or MCP transport files (other
  agents were editing those concurrently).
- Did not add a `viper.SetDefault("agui.mesnada", ...)` entry or otherwise
  touch `setDefaults()`/`applyDefaultValues()` in config.go — out of the
  allowed scope (`AGUIConfig` struct only); solved via `*bool` instead.

## Related

See [[PANDO-EP-0002]] and the follow-on story `PANDO-US-0012` (named AG-UI
profiles), which is expected to reuse `filterAGUITools`'s glob matcher for
per-profile `Tools`/`DenyTools`.
