# Which extension mechanism do I use?

Pando has five ways to add behaviour. They overlap enough that picking the wrong
one is easy and expensive — the wrong choice usually only shows up later, as a
feature that cannot reach the data it needs or a plugin that has to be rebuilt
for every release.

This page is the decision rule. The per-mechanism detail lives elsewhere; this
is only about choosing.

## The short rule

| You want to… | Use |
|---|---|
| Add a tool the model can call, in any language, out of process | **MCP server** |
| Change how Pando behaves at a specific moment, with a small script | **Lua hook** |
| Add a prompt, a workflow, or a repeatable procedure | **Skill or slash command** |
| Describe a new provider/model shape | **Custom engine template** |
| Reach a core interface, or ship inside the binary | **Extension** (`pkg/extension`) |

When two of them would work, take the one higher in that table. It is ordered by
cost: an MCP server is a process you can restart, an extension is a compile-time
decision baked into a binary someone has to rebuild.

## The five, and what each one actually is

**MCP server** — a separate process speaking the Model Context Protocol. It sees
tool calls and nothing else. It cannot read Pando's configuration, touch a
session, filter another extension's tools, or add a UI panel. That isolation is
the point: an MCP server is how untrusted or third-party code gets in. Any
language, no rebuild, per-project configuration.

**Lua hook** — a script Pando runs at a named point (prompt assembly, session
events). Small, hot-reloadable, no build step. It is scripting, not an
architecture: when a hook starts needing state across calls, or wants to talk to
a service with retries, it has outgrown the mechanism.

**Skill / slash command** — Markdown plus frontmatter. Everything a skill does,
the model does. Reach for it whenever the answer is really "the model should
approach this differently", which is more often than it first looks.

**Custom engine template** — YAML describing a provider/model shape. Use it for
a new endpoint that speaks a protocol Pando already knows.

**Extension** — a Go package compiled into the binary, registered in
`pkg/extension`. It is the only mechanism with access to core interfaces: tool
middleware, HTTP routes, the event bus, the remembrance layer, the frontend
asset tree, the configuration overlay, licensing. It is also the only one that can crash the process, which
is why every entry point into extension code is wrapped
(`internal/extensions/guard.go`).

## Choose an extension only when one of these is true

1. **It needs a core interface.** Filtering the tool set, wrapping remembrance
   search, mounting an authenticated HTTP route, subscribing to the internal
   event bus, imposing configuration and locking keys against local edits. No
   other mechanism can see any of these.
2. **It must ship in the binary.** Enterprise modules distributed as one
   executable, with no separate process to deploy and nothing extra to install.
3. **It is first-party and privileged.** Extensions run in-process with full
   access. There is no sandbox. If you would not merge the code into core, do
   not run it as an extension — run it as an MCP server.

If none of the three holds, one of the other four mechanisms is a better answer,
and will cost less to maintain.

## Why not "just make everything an extension"

An extension is a build-time decision. Adding one means rebuilding and
redistributing a binary; changing its API means a coordinated release of core
and every private module. That is an acceptable price for the capabilities above
and a bad one for a tool that could have been an MCP server.

The inverse mistake is just as common: an MCP server that keeps growing
Pando-shaped requirements — needing session state, needing to see other tools,
needing a UI — is an extension that has not admitted it yet.

## Untrusted code

Extensions have no security boundary: an extension can do anything Pando can do.
MCP is the mechanism with a boundary, because it is a separate process speaking
a narrow protocol. Third-party code belongs there, not in `pkg/extension`.

## See also

- [docs/extension-authoring.md](extension-authoring.md) — how to write one
- [docs/extension-builds.md](extension-builds.md) — composing binaries with `xpando`
- [docs/extension-frontend.md](extension-frontend.md) — contributing UI
- [docs/extension-memory.md](extension-memory.md) — observing and augmenting remembrances
