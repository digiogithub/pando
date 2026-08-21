---
created_at: 2026-08-21T14:36:50.495352563Z
updated_at: 2026-08-21T14:36:50.495352563Z
tags:
    - change
    - extensions
    - tools
    - commands
---
# P1 — Extension tools, tool middleware, CLI and slash commands

Date: 2026-08-21
Status: DONE
Plan: [[pando/analysis/extension_system_enterprise_analysis]] (phase P1)
Builds on: [[pando/changes/extension-system-p0-foundations]]

## What was done

Phase P1 makes the P0 contract actually reach users: tools contributed by an
extension now reach the model, extensions can filter and intercept every tool
call (core tools included), and they can add both `pando ext …` CLI subcommands
and in-session slash commands.

### Contract additions — `pkg/extension` (still stdlib only)

`pkg/extension/tool.go`:

- `ToolMiddleware` — base interface carrying only `Priority() int`.
- `ToolFilter` — `FilterTools([]Tool) []Tool`, runs once per tool-set build.
- `ToolInterceptor` — `InterceptTool(ctx, ToolCall, next ToolFunc)`, wraps every
  call.
- `ToolFunc` — the next link of an interceptor chain.

Ordering rule: **lower priority runs closer to the tool**. Filters run in
ascending priority; interceptors are nested so that the *highest* priority ends
up outermost and observes what the lower ones did. Ties break on extension ID so
the order is identical in every process for a given build.

`pkg/extension/command.go` (new):

- `Flag`, `Flags` (with `Bool`/`String`/`Int`/`StringSlice` accessors),
  `Command` (recursive via `Subcommands`), `CommandProvider`.
- `SlashCommand`, `SlashResult{Prompt, Output}`, `SlashCommandProvider`.

Neither mentions cobra: an out-of-tree module must not have to agree with core
on a third-party dependency version to add a subcommand. Core adapts.

`pkg/extension/manager.go`:

- `Manager.Instance(id)` returns a loaded instance.
- `Preview[T]` instantiates every *registered* extension **without
  provisioning** and returns those implementing T.
- The enable decision was factored out of `Load` into `Manager.enabled` +
  `disabledSet`, so there is one rule, not two.

### Host wiring

| File | Role |
|---|---|
| `internal/extensions/tools.go` | Adapters both ways + `ApplyTools(mgr, coreTools)`: adds provider tools, runs filters, wraps interceptors |
| `internal/llm/agent/extension_tools.go` | `SetExtensionManager` / `ExtensionManager` + `applyExtensionTools` |
| `internal/llm/agent/tool_discovery.go` | Calls `applyExtensionTools` as step 0 of `ApplyToolDiscovery` |
| `internal/extensions/commands.go` | `Commands()` builds the cobra tree; `commandRunner` loads config+manager lazily |
| `cmd/extensions.go` | New `pando ext` parent command; mounts extension subcommands |
| `internal/commands/extension.go` | `SetExtensionManager`, `ExtensionCommands`, `IsExtensionCommand`, `RunExtension` |
| `internal/commands/registry.go` | `AllCommands` appends extension commands; `Parse` accepts them |
| `internal/api/handlers_chat.go` | `default:` branch of the slash switch tries `RunExtension` first |
| `internal/mesnada/acp/slash_commands.go` + `goal_commands.go` | `slashCommandExtension` kind, advertised in `availableCommands`, executed by `processExtensionCommand` |
| `internal/tui/components/dialog/extension_commands.go` | Palette entries under a new `CommandCategoryExtensions` |
| `internal/app/app.go` | Wires the manager into both `agent` and `commands` |

## Design decisions worth recording

- **Middleware sees core tools too.** `ApplyTools` converts the whole tool list
  into `extension.Tool` before running filters. A policy extension that could
  only filter *other extensions'* tools would be pointless — the reason to have
  one is to constrain what the model can reach.
- **Adapters do not stack.** `asExtensionTool`/`asCoreTool` unwrap their own
  round trip by type assertion. Without that, every tool-set rebuild (once per
  turn) would add another layer of wrappers.
- **A core tool always wins a name clash.** An extension declaring `bash` is
  logged and dropped, never allowed to shadow it. Same rule for slash commands:
  an extension cannot redefine `/compact`.
- **`ApplyToolDiscovery` is the single choke point.** Both coder tool-set paths
  end there, and extension tools join *before* discovery classifies anything, so
  they can be deferred behind `tool_search` like any other tool. The restricted
  sets (`TaskAgentTools`, `ContextEnricherAgentTools`) deliberately do not get
  extension tools: they are read-only by construction.
- **Panic containment everywhere.** A panicking filter is ignored and the
  unfiltered set survives (the safe failure: it is what core would have offered
  anyway); a panicking interceptor yields an error `ToolResponse`; a panicking
  slash command becomes an error the surface reports.
- **CLI help must not start Pando.** The cobra tree is built in `init()` from
  `Preview`, which is why `Preview` ignores configuration: at that point none has
  been read. Whether an extension is enabled is checked when a command runs.
  `commandRunner` then loads configuration itself if nothing else has — without
  that, `pando ext …` reported every extension as disabled (found in the
  end-to-end test, fixed).
- **Commands mount under `pando ext`, never at top level**, so an extension can
  never shadow a core command and `pando ext --help` is an exact list of what
  the build added.

## Deviation from the plan

The plan proposed proving the API by moving an existing optional core tool (e.g.
Sourcegraph) behind the registry as a standard extension. That was **not** done:
the tool is gated by `[InternalTools] SourcegraphEnabled`, and moving it would
either silently change that flag's meaning for existing users or require
honouring both switches, which buys nothing. The API is proven instead by the
unit tests below plus a real composed binary (see Verification).

## Verification

- `go build ./...`, `go vet` on every touched package — clean.
- `go test ./pkg/extension ./internal/extensions ./internal/commands ./internal/llm/agent ./internal/api ./internal/config ./internal/mesnada/... ./internal/tui/...` — all pass.
- New tests:
  - `internal/extensions/tools_test.go` (13): nil manager is identity, provider
    tools appear and run, core wins name clash, filters see core tools, filter
    order follows priority, interceptor wraps every tool, interceptor can refuse,
    interceptor nesting order, panicking filter/interceptor contained, tool error
    propagates, adapter round trip does not stack.
  - `internal/commands/extension_test.go` (6): no manager, listing + `Parse`,
    built-ins win collisions, routing to the owning extension, unknown name not
    claimed, panic contained.
  - `internal/llm/agent/extension_tools_test.go` (2): extension tools reach the
    agent through `ApplyToolDiscovery`, and no manager means no change.
  - Test types are split per capability on purpose — one struct with every
    method implements every interface, and the manager would then hand
    `ApplyTools` a "filter" whose function is nil.
- **End-to-end**: a throwaway module importing `github.com/digiogithub/pando/cmd`
  plus a demo `tools.demo.corp` Enterprise extension (tool + nested CLI command +
  slash command), built with `-X ...version.Variant=enterprise`:
  - `pando extensions list` → `disabled` with no config, `loaded` with it;
  - `pando ext --help` lists `demo` without provisioning anything;
  - `pando ext demo hello world --loud` → `hello from https://corp.internal args=[world] loud=true`;
  - with the config removed → `Error: extension tools.demo.corp is not loaded`.
- `alchemai-agent/compat` extended to assert all six new interfaces; the module
  builds and vets against local core.

## Not in this phase

- The TUI editor's own `/name` typing path is not routed through
  `commands.RunExtension`; extension commands are reachable there from the
  command palette. ACP and the WebUI do route typed commands.
- Slash commands invoked from the TUI palette always run with empty arguments —
  the palette cannot collect them.
- `HTTPEndpointProvider`, `HTTPAuthProvider` and `EventSubscriber` are P2.
