# Writing a Pando extension

An extension is a Go package compiled into the Pando binary. It registers itself
at `init()` time, the manager provisions it at startup, and core subsystems find
what it can do by asking for the interfaces it implements.

Before writing one, check
[docs/extension-mechanisms.md](extension-mechanisms.md) — an MCP server, a Lua
hook or a skill is cheaper than a compile-time dependency, and most ideas fit
one of them.

The contract lives in `pkg/extension`. That package imports nothing but the
standard library, which is what lets a private module in another repository
depend on it. Nothing under `internal/` is reachable from an extension, by
design: `internal/extensions` is the only bridge between the two, and core is
free to refactor behind it.

## The smallest extension

```go
package hello

import (
	"context"
	"fmt"

	"github.com/digiogithub/pando/pkg/extension"
)

type Hello struct {
	greeting string
}

func (h *Hello) ExtensionInfo() extension.Info {
	return extension.Info{
		ID:          "tools.acme.hello",
		Name:        "Hello",
		Description: "Says hello.",
		Version:     "1.0.0",
		Author:      "ACME",
		License:     extension.LicenseMIT,
		New:         func() extension.Extension { return &Hello{} },
	}
}

func (h *Hello) Provision(_ context.Context, host extension.HostServices) error {
	h.greeting = host.String("Greeting", "hello")
	return nil
}

func (h *Hello) Validate() error {
	if h.greeting == "" {
		return fmt.Errorf("Greeting must not be empty")
	}
	return nil
}

func init() { extension.Register(&Hello{}) }
```

The registry stores the **factory**, never the instance you registered. Each
manager builds its own instances through `New`, so an extension may hold state
without two managers (a CLI command and a running app, say) sharing it by
accident.

### IDs

Dot-separated, most general segment first, lowercase `[a-z0-9_-]`:

```
tools.acme.jira
api.acme.audit
memory.sink.corp
ui.acme.dashboard
```

The first segment is what the extension mainly does. It is the namespace core
uses to talk about groups of extensions, and it is what license entitlements
match on (`memory.*`).

An invalid ID, a missing factory or a duplicate registration **panics at
`init()`**. That is deliberate: these are build mistakes, and a binary that
starts with a silently missing feature is worse than one that refuses to start.

## Lifecycle

```
Register (init)  →  Provision  →  Validate  →  Start  →  … running …  →  Stop  →  Cleanup
```

- **Provision** receives `HostServices`, including this extension's own config
  subtree. Do configuration reading and object construction here. Returning an
  error aborts this extension only.
- **Validate** rejects bad configuration. It runs after Provision, and a failure
  triggers `Cleanup` so a half-built extension does not leak.
- **Start / Stop** (`Lifecycle`) is for background work. Both must return
  promptly — start a goroutine, do not block in `Start`.
- **Cleanup** releases resources. It runs on shutdown and on unload.

Every one of these is optional. An extension that only declares tools implements
none of them.

A failing extension never aborts the others, and never stops Pando from
starting. Its error is recorded in its `Status` and logged. One broken optional
feature must not take down the editor.

## Configuration

```toml
[Extensions.Entries."tools.acme.hello"]
Enabled = true

[Extensions.Entries."tools.acme.hello".Config]
Greeting = "hola"
```

`HostServices.Raw` is that `Config` table, and `Bool`/`String`/`Int` are typed
readers over it with defaults. Read the config in `Provision`, validate it in
`Validate`, and never read it again — configuration is a startup input, not a
live channel.

Two rules decide whether an extension loads at all:

- **Writing a `Config` table is itself an opt-in.** Configuring an extension
  without setting `Enabled = true` still loads it: configuring something you did
  not want on is a rarer mistake than forgetting the flag.
- **`[Extensions] Disabled = ["id"]` always wins.** It is the stronger switch,
  and it also turns off extensions that would otherwise load by default.

Bundled MIT extensions are on by default. Everything else must be switched on
explicitly, so that adding a private module to a build never silently changes
behaviour.

## Capabilities

Implement the interface, and the subsystem finds you. There is no registration
step per capability.

| Interface | What it does |
|---|---|
| `ToolProvider` | Adds tools the model can call |
| `ToolFilter` | Removes tools from the set (policy, allow-lists) |
| `ToolInterceptor` | Wraps every tool call (audit, quotas, redaction) |
| `CommandProvider` | Adds `pando ext <name>` CLI commands |
| `SlashCommandProvider` | Adds slash commands to the chat surfaces |
| `HTTPEndpointProvider` | Mounts routes under `/api/ext/<base>/` |
| `HTTPMiddlewareProvider` | Wraps the HTTP stack (auth, headers) |
| `EventSubscriber` | Receives internal events (sessions, messages, …) |
| `MemorySink` | Observes remembrance and KB writes |
| `RemembranceSearchWrapper` | Adds to or reorders remembrance search results |
| `FrontendProvider` | Ships WebUI assets and panels |
| `FrontendOverlay` / `FrontendReplacer` | Overrides parts of, or all of, the WebUI |
| `LicenseProvider` | Gates non-MIT extensions (one per build) |

Tool middleware sees **every** tool, core tools included — a policy extension
that could only filter other extensions' tools would be useless. `Priority()`
orders middleware; ties break on extension ID, so the order is identical in
every process for a given build.

A core tool always wins a name clash. An extension cannot shadow `bash` or
`edit`; the attempt is logged and dropped.

## What the host guarantees

Extensions run **in-process, unsandboxed, with full access**. There is no
security boundary here; there is a reliability one, and it works in one
direction only.

- **Every call from core into extension code is panic-guarded.** A panicking
  extension loses its turn — its tools are dropped, its event is skipped, its
  command exits with an error — and Pando keeps running. See
  `internal/extensions/guard.go`.
- **Declarative calls have a deadline** (30s): listing tools, naming panels,
  answering which topics you want, handling an event. These must return
  promptly by contract. Past the deadline the *caller* is released and your
  result is ignored — a wedged goroutine cannot be killed in Go, so it is
  abandoned. Do not do slow work in a declaration.
- **Tool `Run` has no deadline.** It may legitimately take minutes, for the same
  reasons `bash` does. Honour the context you are given: that is how the host
  tells you the user gave up.

None of this makes a bad extension safe. It makes a *broken* one survivable.

## Licensing

Non-MIT extensions can be gated by an entitlement check. The rules:

- MIT extensions are never gated.
- A build with no `LicenseProvider` compiled in gates nothing, and logs that it
  is not gating. The absence of the gate is a fact about how the binary was
  built, not something a customer did; refusing to start every enterprise module
  over a packaging mistake would turn it into an outage.
- A `LicenseProvider` is never gated by its own check, and loads before
  everything else.
- A refused extension gets `Status.Unlicensed` and shows as `unlicensed` in
  `pando extensions list`, separately from a load error. The two need different
  answers: one is a bug report, the other is a question for whoever owns the
  contract.

Core owns the format and the verification (`pkg/extension/license.go`: Ed25519
over the license claims, entitlements matched exactly, by namespace `ns.*`, or
`*`). The enterprise module owns the trusted public keys and decides where the
license file comes from. **No public key belongs in the OSS core** — a key
anyone can rebuild core with is a key anyone can mint entitlements against.

Embed `extension.LicenseGate` rather than reimplementing the expiry and
entitlement rules, so every build answers the same way:

```go
claims, err := extension.VerifyLicense(data, trustedKeys)
if err != nil {
	return extension.NewUnlicensedGate(path, err), nil
}
return extension.NewLicenseGate(claims, path), nil
```

This is entitlement gating, not tamper protection. A binary handed to a customer
is in their hands. The protection that matters is that enterprise source lives
in a private module.

## Testing

Use a private registry so your tests never see whatever the rest of the binary
registered:

```go
reg := extension.NewRegistry()
reg.Register(&Hello{})
mgr := extension.NewManager(extension.Options{
	Registry: reg,
	Entries:  map[string]extension.Entry{"tools.acme.hello": {Enabled: true}},
})
if err := mgr.Load(context.Background()); err != nil {
	t.Fatal(err)
}
defer mgr.Cleanup()
```

Test what happens when your extension fails, not only when it works: an
extension is optional by definition, and how it degrades is part of its
behaviour.

## Building

Extensions are linked in at build time. Composing a binary from the core plus
private modules is `xpando`'s job — see
[docs/extension-builds.md](extension-builds.md).

## Gotchas

- **`//go:embed` cannot cross module boundaries.** A private module must embed
  its own assets and hand them over as an `fs.FS`.
- **Viper splits dotted keys.** `[Extensions.Entries."a.b.c"]` decodes to nested
  maps; `internal/config` puts the segments back together. Do not read
  `Entries` directly.
- **Do not blank-import a private module from the public repo.** It would force
  a `require` on a module OSS users cannot fetch, and break `go mod tidy` for
  them. Composition happens in a generated main module.
- **Adding a field to `HostServices` is backwards compatible. Removing or
  retyping one is not.** The same goes for `MemoryEvent`: widening it later
  means a coordinated release of core and every private module.

## See also

- [docs/extension-mechanisms.md](extension-mechanisms.md) — extension, MCP, Lua, or skill?
- [docs/extension-builds.md](extension-builds.md) — `xpando` and the build matrix
- [docs/extension-frontend.md](extension-frontend.md) — panels and assets
- [docs/extension-memory.md](extension-memory.md) — the memory capability
