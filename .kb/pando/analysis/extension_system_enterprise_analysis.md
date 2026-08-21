# Pando Extension System — Analysis and Recommendation (Pando Enterprise)

Date: 2026-08-21
Status: Reviewed and accepted. All open questions resolved (§8.6). **P0 IMPLEMENTED 2026-08-21** —
see [[pando/changes/extension-system-p0-foundations]].
Author: analysis session on request "how to build Pando Enterprise with private modules"

Related: [[analysis_copilotkit_agui_integration]], [[project_custom_engines_plan]], [[feature_mcp_client_authentication]],
[[project_memory_system_plan]], [[project_llm_proxy_plan]], [[pando/plans/wails_desktop_app_plan]]

---

## 0. Executive summary

Goal: ship an MIT open-source `pando` core plus a closed-source **Pando Enterprise** build that can add
private tools, private HTTP/REST APIs, private WebUI components (up to a full alternative frontend layer),
and private behaviours (notably: pushing memory/remembrances to a company-shared external remembrances
backend).

Three industry models were studied:

| Model | Boundary | Distribution | Best at |
|---|---|---|---|
| **Caddy + xcaddy** | compile-time, in-process, global `init()` registry | custom binary built per customer/plugin set | zero-overhead extension, full Go API surface, easy monetisation of a build |
| **HashiCorp go-plugin** (Terraform, Packer, Vault, Nomad) | run-time, out-of-process, gRPC | separately downloaded, versioned, checksummed plugin binaries | isolation, crash containment, language independence, third-party ecosystem, license separation at the binary level |
| **remembrances-mcp** (own precedent, already shipped) | Caddy-style registry **plus** Go build tag `commercial` | one binary per variant (`-commercial` suffix), plus an `xremembrances build --with` builder | exactly the OSS-core/commercial-modules split we want |

**Recommendation for Pando: adopt the remembrances-mcp / Caddy model as the primary mechanism**
(compile-time module registry + `enterprise` build tag + an `xpando` builder), and explicitly *do not*
adopt go-plugin as the main path. Pando's extension surface is deep, stateful and streaming
(`tools.BaseTool` with streaming permissions, `session.Service`, `pubsub` event buses, `http.Handler`s,
LSP/agent internals). Serialising that across gRPC would be a large, permanently-maintained protocol
surface for very little benefit, and Terraform/Packer only pay that price because they need an *untrusted
third-party ecosystem*. Pando Enterprise is *first-party* code — trust is not the problem, licensing and
build separation are, and a build tag solves those directly.

Keep `go-plugin` in reserve for one narrow case (see §7.4): untrusted customer-authored extensions.
For third-party non-privileged tooling, Pando already has a better answer: MCP servers.

---

## 1. The Caddy / xcaddy model

### 1.1 Caddy core

Caddy's whole extension model is four small pieces:

```go
type Module interface { CaddyModule() ModuleInfo }

type ModuleInfo struct {
    ID  ModuleID        // namespaced: "http.handlers.file_server"
    New func() Module   // factory
}

func init() { caddy.RegisterModule(MyModule{}) }
```

Lifecycle: `New()` → JSON unmarshal of the module's own config subtree → `Provision(ctx)` →
`Validate()` → *use* → `Cleanup()`. Optional capability interfaces (`Provisioner`, `Validator`,
`CleanerUpper`) are discovered by type assertion, so a module implements only what it needs.

Two properties matter and both are cheap:

- **Namespaced IDs** (`http.handlers.*`) let the host ask "give me everything in this namespace" without
  knowing concrete types — the basis of pluggable subsystems.
- **`init()`-based registration** means *importing the package is the installation*. A blank import
  `_ "github.com/x/y"` is the entire plugin-enable mechanism.

### 1.2 xcaddy

xcaddy is deliberately dumb, and that is its strength. `xcaddy build v2.8.4 --with github.com/x/y@v1.2.0`:

1. creates a temp dir, `go mod init caddy`;
2. generates a `main.go` that blank-imports the standard module set plus every `--with` module and calls
   `caddycmd.Main()`;
3. `go get` for the Caddy version *and* each plugin — passing the caddy module@version along so a plugin
   cannot silently upgrade the core;
4. `go mod edit -replace` for every `--with mod=/local/path` or `--replace` flag (relative paths resolved
   to absolute);
5. `go mod tidy`, then `go build` with `GOOS`/`GOARCH`/`GOARM`/`CGO_ENABLED` passed through;
6. deletes the temp dir (`XCADDY_SKIP_CLEANUP=1` to keep it).

Private modules need nothing special from xcaddy: the underlying `go get` honours `GOPRIVATE`,
`GONOSUMDB`, `.netrc`/SSH rewrites, and `--replace` covers the local-checkout case. Running `xcaddy`
inside a plugin's own module directory builds-and-runs Caddy with that module replaced — the dev loop.

**Take-away for Pando:** the builder is ~150 lines. The hard part is not the builder, it is designing the
registry and the capability interfaces so a module can do something useful without patching core.

---

## 2. remembrances-mcp: the model already implemented in-house

This is the most relevant prior art because it is *our own*, and it already solves the OSS/commercial
split. Layout:

```
pkg/modules/            module.go, types.go, registry.go, manager.go, middleware_chain.go   (~500 LOC)
modules/standard/       core_tools.go, fact_tools.go, kb_tools.go, code_*_tools.go          (MIT)
modules/commercial/     db-sync-server/, webui/                                             (closed)
cmd/remembrances-mcp/modules_commercial.go   -> //go:build commercial + blank imports
cmd/xremembrances/main.go                    -> the xcaddy clone (140 LOC)
```

### 2.1 The registry (`pkg/modules/registry.go`)

Identical shape to Caddy: package-level `map[ModuleID]ModuleInfo` behind an RWMutex,
`RegisterModule(instance)` panics on empty ID / nil factory / duplicate ID, plus `GetModule`,
`ListModules`, and `GetModulesByNamespace(prefix)`.

### 2.2 Base interfaces (`pkg/modules/module.go`)

`Module` (metadata only) + optional `Provisioner`, `Validator`, `CleanerUpper` — and notably
`StorageWrapperProvider`, which is the decorator hook:

```go
type StorageWrapperProvider interface {
    Module
    WrapStorage(primary storage.FullStorage) storage.FullStorage
}
```

`ModuleInfo` carries `Version`, `Author`, **`License`** — the commercial webui module declares
`License: "Commercial"`. Cheap, and it makes `list modules` self-documenting.

### 2.3 Capability interfaces (`pkg/modules/types.go`)

This is the actual design work, and it maps almost one-to-one onto what Pando Enterprise needs:

- `ToolProvider` — adds MCP tools (`Tools() []ToolDefinition`)
- `StorageProvider` / `EmbedderProvider` — pluggable backends by type string
- `ToolMiddleware` — `Wrap(next ToolHandler) ToolHandler` + `Priority() int` + `ToolFilter() []string`
- `ToolTransformer`, `ToolValidator`, `ResponseEnricher` — narrower hooks over tool req/res
- `ConfigProvider` — extra config sources
- `HTTPEndpointProvider` — `Routes() []HTTPRoute` + `BasePath()`
- `HTTPAuthProvider`, `WebhookHandler` — auth middleware, inbound webhooks

Two patterns worth copying verbatim: **`Priority()` + `ToolFilter()`** on every interceptor (deterministic
ordering, scoped application), and **`BasePath()`** on HTTP providers (no route collisions between modules).

### 2.4 Manager and wiring (`pkg/modules/manager.go`, `cmd/remembrances-mcp/main.go:389+`)

`ModuleConfig` is the *host services struct* handed to every module on `Provision`: storage, embedder,
code embedder, KB path/chunking, indexer config, logger. `ModuleManager.LoadModule` runs
factory → Provision → Validate (cleanup on validation failure) → store instance.

Host wiring order in `main.go`:

1. build `ModuleManager` with `ModuleConfig`
2. `loadModules(ctx, mm, cfg)` — a hardcoded `defaultModules` list, then everything under `cfg.Modules`,
   minus `cfg.DisableModules`; conditional defaults (`tools.kb` only when a KB path is set)
3. `storageInstance = modManager.WrapStorage(storageInstance)` — **decorators applied after load**
4. `registerModuleTools(...)` — iterate `GetToolProviders()`, `srv.RegisterTool(def.Tool, def.Handler)`
5. `HTTPTransport.RegisterModuleRoutes(mm.GetHTTPEndpointProviders())`

Config is `modules: { <id>: { enabled: bool, config: map } }` plus a `disable_modules` list, so *core*
modules can also be switched off — the same mechanism serves "add commercial feature" and "strip a
feature for a hardened build".

### 2.5 Commercial gating — the important bit

The commercial modules are compiled in by **one 8-line file**:

```go
//go:build commercial

package main

import (
    _ "github.com/madeindigio/remembrances-mcp/modules/commercial/db-sync-server"
    _ "github.com/madeindigio/remembrances-mcp/modules/commercial/webui"
)
```

Makefile:

```make
MODULE_TAGS       ?=
GO_BUILD_TAGS      = $(strip $(MODULE_TAGS))
EMBEDDED_GO_TAGS   = $(strip $(EMBEDDED_TAGS) $(MODULE_TAGS))
OSX_DIST_SUFFIX   := $(if $(MODULE_TAGS),-commercial,)

build-commercial: MODULE_TAGS=commercial
build-commercial: build
```

Every build/dist/release rule already threads `$(if $(GO_BUILD_TAGS),-tags "$(GO_BUILD_TAGS)",)`, so the
matrix `{cpu, cuda, openvino, embedded} × {oss, commercial}` falls out of one variable, and artifacts get
a `-commercial` suffix (`dist-variants/remembrances-mcp-darwin-aarch64-commercial.zip`).

Note what is *not* done: the commercial code currently lives in the same repo, gated only by the tag. That
is fine for internal use, but it does not stop a source recipient from reading it — see §8.1.

### 2.6 The commercial WebUI module — precedent for Pando's WebUI need

`modules/commercial/webui/webui.go` is a self-contained UI shipped as a module:

```go
//go:embed static templates
var embeddedFiles embed.FS

func init() { modules.RegisterModule(WebUIModule{}) }

func (m *WebUIModule) BasePath() string { return "/admin" }
func (m *WebUIModule) Routes() []modules.HTTPRoute { ... }
```

So: a *closed* module carries its own embedded frontend assets and mounts them under its own base path,
with zero core changes. This is directly reusable for Pando (§7.3).

### 2.7 `cmd/xremembrances`

A faithful, minimal xcaddy clone (140 LOC): temp dir → generate `main.go` with
`remembrances "<base>/cmd/remembrances-mcp"` + `_ "<base>/modules/standard"` + one blank import per
`--with` → `go mod init` → `go get` base and each module → `go mod tidy` → `go build -o <output>`.

Gaps vs xcaddy, worth closing if this pattern is promoted: no `--replace`, no build-tag passthrough
(so it cannot currently produce a commercial build), no `GOOS/GOARCH/CGO` passthrough, no version pinning
of the base module against plugin upgrades, no `run`/dev mode.

---

## 3. The HashiCorp model (go-plugin, Terraform, Packer)

### 3.1 go-plugin mechanics

- Host calls `plugin.NewClient(&ClientConfig{Cmd: exec.Command(binary), ...})`; the plugin is a **separate
  OS process**.
- Plugin prints a handshake line on stdout:
  `CORE-PROTOCOL-VERSION | APP-PROTOCOL-VERSION | NETWORK-TYPE | NETWORK-ADDR | PROTOCOL`.
  `HandshakeConfig` also carries a magic cookie so a stray binary cannot be mistaken for a plugin.
- Transport: `net/rpc` (Go-only, yamux-multiplexed over one TCP conn) or **gRPC** (cross-language,
  HTTP/2 multiplexing). `VersionedPlugins` lets one host speak several protocol majors.
- **Brokers** (`MuxBroker` / `GRPCBroker`) give bidirectional calls: the plugin can call back into
  host-provided services over additional brokered connections keyed by ID.
- Security: `AutoMTLS` (host mints an ephemeral cert, passes the public half via `PLUGIN_CLIENT_CERT`,
  mutual auth both ways) and `SecureConfig` checksum verification of the plugin binary.
- Logging: plugin stdout/stderr is streamed to the host; structured if the host uses `hclog`.
- Lifecycle: host owns the process, kills it on `Client.Kill()`; `ReattachConfig` supports attaching to an
  already-running plugin (the dev workflow, e.g. `TF_REATTACH_PROVIDERS`).

### 3.2 Terraform: the distribution layer on top

The interesting part of Terraform is not the RPC, it's the **supply chain**:

- `providercache.Installer` resolves `required_providers` version constraints against a registry
  (`registry.terraform.io`), a filesystem mirror, an HTTP mirror, or a multi-source combination.
- `.terraform.lock.hcl` pins exact versions **and** cryptographic checksums; mismatch = hard failure.
- Protocol majors are encoded in the protobuf package name (`tfplugin5`, `tfplugin6`), so one plugin binary
  can serve both and host/plugin negotiate at handshake.
- `terraform-plugin-framework` / `sdkv2` sit between provider authors and the wire protocol, so core can
  evolve without breaking providers.
- Private/enterprise: **filesystem mirrors** (`terraform.d/plugins`), **dev overrides** (local path,
  bypasses registry+lock), and **unmanaged providers** (`TF_REATTACH_PROVIDERS`).

### 3.3 Packer: the naming/discovery discipline

- One binary can expose *many* components (builders, provisioners, post-processors, data sources).
- Discovery is strict since v1.11: only `$PACKER_PLUGIN_PATH` (default `~/.packer.d/plugins`), with the
  naming convention
  `packer-plugin-<name>_<version>_<api_version>_<os>_<arch>[.exe]` plus a sibling `..._SHA256SUM` file.
- `required_plugins` in HCL + `packer init` installs/verifies.
- Packer explicitly *moved all plugins out of core* (v1.10/v1.11) and documented the reasons: independent
  SemVer for the SDK (a real API promise), smaller core, predictable loading, structured metadata enabling
  `packer init`.

### 3.4 Costs of the out-of-process model

- **Process overhead** per plugin (spawn, handshake, TLS, teardown).
- **Serialisation boundary**: every type crossing it needs a proto definition; rich Go interfaces,
  streaming callbacks, `context` semantics and error wrapping all have to be re-expressed. In Terraform's
  case this *is* the product surface, so it is worth it.
- **Interface stability becomes a contract**: once third parties ship binaries, protocol changes need
  version negotiation and an SDK shim layer forever.
- Debugging is harder (two processes, brokered connections, reattach dance).

---

## 4. Side-by-side, judged against Pando's requirements

Requirements: (R1) new tools, (R2) new REST/HTTP APIs, (R3) new WebUI components and optionally a whole
alternative frontend, (R4) behaviour overrides (e.g. memory pushed to a shared corporate remembrances
service), (R5) closed-source code not shipped in the OSS repo, (R6) reproducible "Pando Enterprise" builds.

| Criterion | Caddy/xcaddy (compile-time) | go-plugin (out-of-process) |
|---|---|---|
| R1 tools with rich host access (`permission.Service`, streaming, `history`, LSP) | native, zero cost | every dependency needs a brokered gRPC service |
| R2 HTTP routes | `http.Handler` registered directly | must proxy requests over RPC, or the plugin serves its own port |
| R3 WebUI assets | `embed.FS` in the module, or full asset-root swap by tag | awkward — assets must be streamed or served separately |
| R4 decorate core services (storage/memory sink) | interface wrapping, in-process (`WrapStorage` precedent) | very hot path over RPC; unattractive |
| R5 closed source | private Go module + `GOPRIVATE`; source never in OSS repo, only a build-tagged import file | binary-only distribution, strongest separation |
| R6 reproducible builds | one `go build -tags enterprise`; SBOM/version stamping trivial | needs registry + lockfile + checksums to be reproducible |
| Crash isolation | none (a bad module can panic the process) | strong |
| Third-party untrusted ecosystem | weak (source-level trust required) | strong |
| Maintenance cost | low — no protocol, refactor freely | high — permanent protocol + SDK |

Pando's own extension points are *exactly* the ones in the left column: `tools.BaseTool`
(`internal/llm/tools/tools.go:79`), `CoderAgentTools(...)` (`internal/llm/agent/tools.go:114`),
`http.ServeMux` routes (`internal/api/routes.go:9+`), an embedded WebUI (`internal/api/ui_assets_app.go`),
`rag.RemembrancesService` / `rag.Store`, `pubsub` event buses, `session.Service`, `permission.Service`.

**Verdict: compile-time.** Also note Pando *already* has the out-of-process story covered by MCP —
`internal/mcpclient`, `internal/mcpgateway`, `internal/mcpauth` (OAuth 2.1 / mTLS / client_credentials).
Anything that only needs "a tool with an isolated implementation" should be an MCP server, not a
go-plugin. Building a second out-of-process plugin protocol next to MCP would be redundant.

---

## 5. What Pando Enterprise needs to hook (concrete inventory)

| Need | Current core location | Extension hook required |
|---|---|---|
| Private tools | `internal/llm/agent/tools.go:114 CoderAgentTools`, `:187 CoderAgentToolsWithMesnada`, `:359`, `:390` | `ToolProvider` consulted inside the tool assembly, before `ApplyToolDiscovery` |
| Tool interception (audit, DLP, redaction, quota) | tool execution in `internal/llm/agent` | `ToolMiddleware` with `Priority()` + `ToolFilter()` |
| Private REST APIs | `internal/api/routes.go` | `HTTPEndpointProvider` with `BasePath()`, registered in `routes.go` after core routes |
| Enterprise auth (SSO/OIDC/SAML, RBAC) | `internal/auth`, `internal/api` basic auth | `HTTPAuthProvider` returning middleware + `AuthInfo{UserID, Roles}` |
| New WebUI panels | `web-ui/` React app, embedded via `internal/api/ui_assets_app.go` | `FrontendProvider` — `fs.FS` of assets + panel manifest (§7.3) |
| Whole alternative frontend | same embed | build-tag swap of the embedded asset root (§7.3c) |
| Shared corporate memory | `internal/rag` (`Store`, `RemembrancesService`), memory subsystem | `MemorySink` / store decorator, `WrapStore`-style (§7.5) |
| Telemetry/compliance export | `internal/observability`, `internal/pubsub` | `EventSubscriber` capability |
| Licensing/entitlements | — | `LicenseProvider` + gate in module load (§8.2) |
| Model/provider policy (allow-list, private gateway) | `internal/llm/provider`, `internal/llmproxy` | `ProviderPolicy` interceptor |
| Config | `internal/config` | `ConfigProvider` + `[Extensions]` config table |

---

## 6. Recommended architecture for Pando

**Adopt the remembrances-mcp module system, ported to Pando, with the gaps closed.** Concretely:

```
pkg/extension/                 # public, MIT, stable API for extension authors
    extension.go               # Extension, ExtensionInfo, Provisioner, Validator, CleanerUpper
    registry.go                # global init()-time registry, namespaced IDs
    manager.go                 # lifecycle + HostServices + typed getters
    capabilities.go            # ToolProvider, HTTPEndpointProvider, FrontendProvider, MemorySink, ...
    middleware.go              # ordered interceptor chain (Priority + Filter)

extensions/standard/           # MIT, optional-but-bundled core features moved behind the registry
cmd/pando/extensions_enterprise.go   # //go:build enterprise  -> blank imports (the ONLY core change)
cmd/xpando/                    # the builder (xcaddy clone, with the gaps fixed)
```

and, in a **separate private repository** `github.com/digiogithub/alchemai-agent`
(module path under `GOPRIVATE`), the actual closed modules. See §6.7 for that repo's current state.

### 6.1 Why `pkg/` and not `internal/`

Extension authors (and the enterprise repo) must import the interfaces. `internal/` forbids that. Only the
*interfaces plus the value types they need* go to `pkg/extension`; host implementations stay in `internal/`.
This forces a deliberate, reviewable API surface — the same discipline the packer-plugin-sdk split bought
HashiCorp, without the RPC.

### 6.2 Core API shape

```go
package extension

type ID string   // namespaced: "tools.acme.jira", "api.acme.audit", "memory.sink.corp"

type Info struct {
    ID          ID
    Name        string
    Description string
    Version     string
    Author      string
    License     string        // "MIT" | "Enterprise" | ...
    Requires    []string      // min core version constraint, other extension IDs
    New         func() Extension
}

type Extension interface{ ExtensionInfo() Info }

// optional
type Provisioner interface{ Provision(ctx context.Context, h HostServices) error }
type Validator   interface{ Validate() error }
type CleanerUpper interface{ Cleanup() error }
```

`HostServices` is the single struct handed to every extension (the `ModuleConfig` idea, but with Pando's
services). Keep it interfaces-only so core internals stay swappable:

```go
type HostServices struct {
    Raw         map[string]any       // this extension's own config subtree
    Config      ConfigView           // read-only view over internal/config
    Logger      *slog.Logger
    Sessions    SessionService       // narrow interfaces, not internal/session directly
    Permissions PermissionService
    History     HistoryService
    Pubsub      EventBus
    Remembrance RemembranceService
    LSP         LSPProvider
    WorkingDir  string
    Version     string
}
```

**Rule: `HostServices` fields are interfaces declared in `pkg/extension`, satisfied by `internal/*` types.**
That is what keeps the enterprise repo compiling across core refactors.

### 6.3 Capability interfaces (first cut)

```go
type ToolProvider interface { Extension; Tools(h HostServices) []tools.BaseTool }

type ToolMiddleware interface {
    Extension
    WrapTool(next ToolRunner) ToolRunner
    Priority() int
    ToolFilter() []string          // empty = all
}

type HTTPEndpointProvider interface {
    Extension
    BasePath() string              // "/api/ext/acme"
    Routes() []Route               // Method, Path, http.HandlerFunc, Middlewares, Description
}

type HTTPAuthProvider interface {
    Extension
    Middleware() func(http.Handler) http.Handler
    Authenticate(*http.Request) (*AuthInfo, error)
}

type FrontendProvider interface {
    Extension
    Assets() fs.FS                 // served under /ext/<id>/
    Panels() []PanelManifest       // id, title, icon, mount point, entry module URL
}

type FrontendReplacer interface {   // whole alternative frontend layer
    Extension
    RootAssets() (fs.FS, bool)     // takes over "/" when ok
}

type MemorySink interface {
    Extension
    OnMemoryWrite(ctx context.Context, ev MemoryEvent) error
    OnMemoryDelete(ctx context.Context, key string) error
}

type RemembranceStoreWrapper interface {   // the WrapStorage pattern
    Extension
    WrapStore(primary RemembranceStore) RemembranceStore
}

type EventSubscriber interface {
    Extension
    Topics() []string
    OnEvent(ctx context.Context, topic string, payload any)
}

type CommandProvider interface { Extension; Commands() []*cobra.Command }   // new CLI subcommands
type SlashCommandProvider interface { Extension; SlashCommands() []SlashCommand }

type ConfigProvider interface { Extension; LoadConfig() (map[string]any, error) }
type LicenseProvider interface { Extension; Entitlements() []string; Verify() error }
```

Start with `ToolProvider`, `HTTPEndpointProvider`, `FrontendProvider`, `MemorySink`/`RemembranceStoreWrapper`,
`EventSubscriber`, `ConfigProvider`. The rest can be added later — adding a capability interface is
non-breaking by construction (type assertion), which is the main structural advantage of this design.

### 6.4 Configuration

Follow the remembrances-mcp shape, adapted to Pando's TOML config:

```toml
[Extensions]
Disabled = ["tools.acme.legacy"]

[Extensions.Entries."memory.sink.corp"]
Enabled = true
[Extensions.Entries."memory.sink.corp".Config]
Endpoint = "https://remembrances.corp.internal"
ProjectID = "pando"
Mode = "async"
```

Same two-list semantics: an explicit `Disabled` list beats everything (lets a hardened build strip even
core features), and an entry with config but `Enabled = false` is skipped.

### 6.5 `cmd/xpando` — the builder

> **Decision update (2026-08-21, see §8.6-Q3):** `xpando` is *internal* build tooling — customers never run
> it. No registry, lockfile, checksum or signature machinery is needed; keep it at xcaddy scope.

Port `cmd/xremembrances/main.go`, closing the gaps found in §2.7:

```
xpando build [core-version]
    --with  module[@version][=/local/path]     # repeatable, blank-imported
    --replace module[@version]=replacement     # go.mod replace only, no import
    --tags  enterprise,cuda                    # ← build tags (missing in xremembrances)
    --output ./pando-enterprise      # binary name; source module is alchemai-agent
    --ldflags "-X ...Version=..."
    --embed-ui /path/to/dist                   # optional alternative frontend (see §7.3c)
```

Pipeline: temp dir → generate `main.go` importing `github.com/digiogithub/pando/cmd` and calling
`cmd.Execute()` (already exported at `cmd/root.go:1111`, so no core change needed) → `go mod init` →
`go get` core@version **and** each `--with` (passing the core module@version to prevent plugin-driven
core upgrades) → `go mod edit -replace` for each replacement → `go mod tidy` →
`go build -tags "..." -ldflags "..."` with `GOOS/GOARCH/GOARM/CGO_ENABLED` passed through → cleanup
(skippable via `XPANDO_SKIP_CLEANUP=1`).

Private modules: nothing special — `GOPRIVATE=github.com/digiogithub/alchemai-agent` plus SSH/`.netrc`
credentials, exactly as xcaddy does it. Add a `--replace` for local enterprise checkouts (the dev loop).

Caveat: the generated module builds the WebUI from the *published* core module, i.e. the already-embedded
`internal/api/webui/dist`. So `xpando` produces a binary with the stock frontend unless the enterprise
module supplies its own assets (`FrontendProvider`/`FrontendReplacer`) or `--embed-ui` is used. That is a
deliberate consequence of `embed` being compile-time and directory-scoped; §7.3 handles it.

### 6.7 The enterprise repository: `alchemai-agent` (created 2026-08-21)

The private repo now exists locally at **`/www/MCP/Alchemai/alchemai-agent`**.

| Fact | Value |
|---|---|
| Remote | `git@github.com-josedigio:digiogithub/alchemai-agent.git` |
| Go module path (to declare) | `github.com/digiogithub/alchemai-agent` |
| VCS | **jj (Jujutsu), git-colocated** — `.jj` + `.git` side by side, same as the Pando repo; use the jj workflow, not raw git |
| License | proprietary — `Copyright (c) 2026 Digio. All rights reserved.` (NOT MIT; the opposite of core) |
| State at creation | empty apart from `LICENSE` and `.gitignore` (`.pando/*`); no `go.mod` yet; one commit `chore: initial repo` |

Consequences and setup notes:

- **`go.mod` must declare `module github.com/digiogithub/alchemai-agent`**, matching the remote, or
  `go get` from the build pipeline will not resolve.
- The remote uses an **SSH host alias** (`github.com-josedigio`), so any machine or CI runner that builds
  the enterprise variant needs both:
  ```sh
  export GOPRIVATE=github.com/digiogithub/alchemai-agent
  git config --global url."git@github.com-josedigio:digiogithub/".insteadOf "https://github.com/digiogithub/"
  ```
  plus the matching `Host github.com-josedigio` block in `~/.ssh/config`. Without the `insteadOf` rewrite
  the Go toolchain will try HTTPS and fail on a private repo.
- Naming: the **module/repo** is `alchemai-agent`, while the **build variant** stays `enterprise`
  (`-tags enterprise`, artifact suffix `-enterprise`). Keep the two vocabularies distinct in the Makefile
  so a future second private module does not force a rename of the tag.
- `cmd/pando/extensions_enterprise.go` in the public repo therefore blank-imports
  `github.com/digiogithub/alchemai-agent/...` packages. OSS users cannot fetch that module, which is what
  makes `go build -tags enterprise` fail for them — make the failure message explicit rather than a raw
  Go module resolution error.
- Being git-colocated under jj means the standard jj repo rules from the Pando project apply (detached
  HEAD is normal, never `git stash`).
- Suggested initial layout, mirroring `modules/commercial/` in remembrances-mcp:
  ```
  alchemai-agent/
      go.mod                       # module github.com/digiogithub/alchemai-agent
      memory/corpsync/             # MemorySink + RemembranceStoreWrapper (P5)
      ui/                          # enterprise frontend (dist embedded) (P4)
      api/                         # private HTTP endpoints (P2)
      tools/                       # private tools (P1)
      controlplane/                # ControlPlaneClient contract (P5/P6)
      internal/license/            # entitlement verification (P6)
  ```
  Each leaf package registers itself in `init()` against `pkg/extension`, so enabling it is a blank import.

### 6.6 Makefile / release matrix

Copy the remembrances-mcp variable discipline verbatim:

```make
MODULE_TAGS   ?=
GO_BUILD_TAGS  = $(strip $(MODULE_TAGS))
DIST_SUFFIX   := $(if $(findstring enterprise,$(MODULE_TAGS)),-enterprise,)

build: web-ui-embedded
	go build $(if $(GO_BUILD_TAGS),-tags "$(GO_BUILD_TAGS)",) -ldflags '$(LDFLAGS)' -o pando$(DIST_SUFFIX) .

build-enterprise: MODULE_TAGS=enterprise
build-enterprise: build
```

Every existing `release-*` rule needs the same `$(if $(GO_BUILD_TAGS),...)` insert. Stamp the variant into
the binary (`-X .../version.Variant=enterprise`) so `pando --version`, the TUI footer, the WebUI and
support tickets all agree on what is running.

---

## 7. Design details for the specific enterprise needs

### 7.1 Private tools

`CoderAgentTools` (`internal/llm/agent/tools.go:114`) already assembles conditionally from
`cfg.InternalTools`. Insert one call before `ApplyToolDiscovery`:

```go
base = append(base, extmgr.ProvidedTools(host)...)   // from all loaded ToolProviders
result := appendLuaTools(base)
return ApplyToolDiscovery(result, gateway)
```

Same insert in `CoderAgentToolsWithMesnada`, and (scoped by capability filter) in `TaskAgentTools` /
`ContextEnricherAgentTools`. Because extension tools are plain `tools.BaseTool`, they inherit permissions,
tool-discovery gating, savings accounting and the tool-metadata registry for free.

### 7.2 Private HTTP/REST APIs

In `internal/api/routes.go`, after the core registrations:

```go
for _, p := range extmgr.HTTPEndpointProviders() {
    base := strings.TrimSuffix(p.BasePath(), "/")
    for _, r := range p.Routes() {
        h := chain(r.Handler, r.Middlewares...)
        mux.Handle(r.Method+" "+base+r.Path, h)
    }
}
```

Reserve the `/api/ext/` prefix for extensions and **reject any `BasePath()` outside it** — core must be
free to add `/api/v1/*` routes without colliding with a customer build. Apply the core auth/basic-auth
gate to extension routes by default, and let an `HTTPAuthProvider` replace it wholesale.

### 7.3 WebUI: three levels, pick per feature

> **Decision update (2026-08-21, see §8.6-Q2):** the enterprise need is a *different visual layer with
> identical communications*. That makes **(c) asset-root swap** the primary path, and turns "extract the
> API/state layer of `web-ui/` into a reusable package shared by both frontends" into the main P4 task.
> (a) and (b) remain in the API but drop in priority.
>
> **Correction found during P0 implementation:** of the two variants of (c), the *build-tag swap* is **not
> viable** — `//go:embed` cannot reach files in another Go module, and the enterprise frontend lives in
> `alchemai-agent`. The enterprise `dist` must therefore be embedded inside the enterprise module and
> handed to core as an `fs.FS`, i.e. **`FrontendReplacer` is the mechanism**, not a build tag.

Pando embeds a single built React app (`internal/api/ui_assets_app.go`, `//go:embed webui/dist/**`, built
by `make web-ui-embedded`). Three distinct enterprise needs, three mechanisms:

**(a) New panels inside the existing app — recommended default.**
Extension implements `FrontendProvider`: `Assets() fs.FS` (its own `//go:embed dist`, mounted read-only at
`/ext/<id>/`) plus `Panels() []PanelManifest`. Core exposes `GET /api/v1/extensions/ui` returning the
merged manifest. The React shell fetches it at boot and `import(/* @vite-ignore */ url)`s each panel's ESM
entry, rendering it into a named mount point (sidebar item, settings section, chat side panel, status-bar
slot). Requirements: the shell must expose a small stable `window.__PANDO_UI__` contract (React instance or
a framework-agnostic mount function, event bus, auth token, API base) so panels are not rebuilt on every
core bump. Vite `build.rollupOptions.external` + an import map keeps React from being bundled twice.
This is the exact analogue of the remembrances-mcp commercial `webui` module carrying its own
`//go:embed static templates`.

**(b) Overriding/branding existing screens.**
A theme/asset overlay: `FrontendProvider` may declare an overlay `fs.FS` whose files shadow core paths
(`logo.svg`, `theme.css`, `index.html` head injections). Serve with a layered `fs.FS` (overlay first, then
embedded core). Keep the override list explicit in the manifest — a blanket shadow makes core upgrades
undebuggable.

**(c) A completely different frontend layer.**
Two supported ways:
- *Build tag swap*: add `//go:build !enterprise_ui` to `internal/api/ui_assets_app.go` and ship
  `ui_assets_enterprise.go` with `//go:build enterprise_ui` embedding a different `dist` directory. Cost:
  one 8-line core file, mirroring `modules_commercial.go`. Cleanest, fully static.
- *`FrontendReplacer`*: the enterprise module returns a root `fs.FS`, and the static handler prefers it
  over the embedded core assets. No core build-tag file needed; the trade-off is the core dist is still
  compiled into the binary (dead weight, a few MB).
Also keep the existing runtime escape hatch (serving a UI directory from disk) for customers who want to
iterate on their own frontend without rebuilding Go.

### 7.4 Untrusted / customer-authored extensions (the narrow go-plugin case)

If a customer wants to write their *own* extension and we do not want their code inside our binary, the
answer order is:

1. **MCP server** — already supported, already authenticated (`internal/mcpauth`), out-of-process,
   language-agnostic. Covers "custom tools" completely.
2. **Lua** (`internal/luaengine`) — already supported for small in-process hooks, sandboxed
   (no `os.exec`, no arbitrary shell).
3. **go-plugin over gRPC** — only if a customer needs a *privileged, stateful* extension point that MCP
   cannot express (e.g. their own memory sink or auth provider) and they refuse compile-time integration.
   Then: define a small proto for that one capability, use `AutoMTLS` + `SecureConfig` checksums, and
   version the protocol major in the proto package name (`pandoplugin1`) the way Terraform does.
   **Do not** build a general go-plugin surface speculatively — one capability at a time, on demand.

### 7.5 Shared corporate memory (the flagship enterprise behaviour)

Two complementary hooks, both already proven in remembrances-mcp:

- **`MemorySink`** (fire-and-forget observer): every memory/remembrance write and KB document add publishes
  a `MemoryEvent{Scope, Key, Path, Content, Tags, Embedding?, ProjectID, UserID, Timestamp}` to all
  registered sinks. The enterprise sink batches and pushes to the company-shared remembrances-mcp over
  HTTP/MCP with retry, an on-disk spool for offline, and a redaction/DLP filter applied *before* egress.
  Async by default; a `Mode = "sync"` option for compliance builds that must not lose events.
- **`RemembranceStoreWrapper`** (decorator, the `WrapStorage` pattern): the enterprise module wraps the
  local store so *reads* also consult the corporate store and merge results — exactly what
  `modules/commercial/db-sync-server` already does with `MergedStorage` + `QueryMerger` + `SyncQueue` +
  dedup. Strong reuse candidate: that module's sync executor, dedup and merge logic is directly
  transferable, and it already has phase-by-phase tests.

Non-negotiables for this feature, given it exfiltrates project content:
- **opt-in, per project and per scope**, never on by default;
- explicit redaction rules + a dry-run mode that logs what *would* be sent;
- the UI must show, at all times, that remote sync is active and where data goes;
- the spool must be encrypted at rest (AGE is already used for the WebUI basic-auth users file).

### 7.6 Licensing / entitlements

> **Decision update (2026-08-21, see §8.6-Q3):** customers receive compiled binaries only and all builds run
> on our internal pipelines, so licensing is entitlement-gating and anti-accident, not anti-tamper. Safe to
> defer to P6 or later.

Keep it simple and honest: signed license file (Ed25519, public key compiled into the enterprise modules —
not into the OSS core), containing customer, expiry, and an entitlement list. `ModuleManager.LoadExtension`
refuses to provision an extension whose `Info.License != "MIT"` when the entitlement is absent or expired,
and logs it loudly. This is anti-accident, not anti-adversary — an enterprise build handed to a customer is
inherently in their hands; the real protection is that the *source* lives in a private repo (§8.1).

---

## 8. Risks, trade-offs and open questions

### 8.1 Source separation (the one thing remembrances-mcp has not solved)

Today the "commercial" modules sit in the OSS repo behind a build tag. For Pando, do it properly from day
one: enterprise code in `github.com/digiogithub/alchemai-agent` (private), pulled via `GOPRIVATE`. The
only artefact in the public repo is `cmd/pando/extensions_enterprise.go` with `//go:build enterprise` and
blank imports of a module that public users cannot fetch. Consequence: **`go build -tags enterprise` must
fail cleanly with a clear error for OSS users**, and CI must build *both* tag sets on every PR, or the
enterprise build will silently rot.

### 8.2 API stability

The moment enterprise code lives in another repo, `pkg/extension` becomes a versioned contract. Mitigations:
narrow interfaces (never export `internal/*` structs), a `Requires` core-version constraint in `Info`,
SemVer on `pkg/extension` independent of the app, a compatibility test suite in the private repo run in
core CI (nightly, against `main`).

### 8.3 In-process failure modes

A panicking extension takes the process down. Core must wrap every extension entry point
(`Provision`, tool `Run`, HTTP handler, sink callback, event handler) in a recover that disables the
offending extension and reports it, rather than dying. Add a per-extension timeout on sink/event callbacks
so a slow corporate endpoint cannot stall the agent loop.

### 8.4 Build matrix growth

Pando already multiplies platforms × (CLI, desktop/Wails, ACP) and macOS signing/notarisation. Adding an
`enterprise` axis roughly doubles it. Do it with one variable (`MODULE_TAGS`) as remembrances-mcp does, and
keep the artifact suffix mechanical (`-enterprise`). Verify early that the enterprise variant still passes
the macOS entitlements/notarisation path (`scripts/pando.entitlements`, `.pkg` flow) — extra embedded
assets change bundle layout and signing inputs.

### 8.5 Overlap with existing mechanisms

Pando already has *four* extension-ish systems: MCP servers, Lua hooks, custom engine YAML templates, and
skills/slash commands. The extension registry must be positioned as "privileged, first-party, compiled-in",
and the docs must say plainly when to use which — otherwise contributors will ask for a fifth. Suggested
rule of thumb: *needs core interfaces or must ship in the binary* → extension; *just a tool* → MCP;
*small scripted hook* → Lua; *prompt/workflow* → skill or command.

### 8.6 Open questions — resolved

**All five resolved 2026-08-21 (product decisions). Kept here with their consequences.**

1. ~~Repo strategy?~~ **RESOLVED: separate private repo from the start — and it now exists.**
   `github.com/digiogithub/alchemai-agent`, local checkout `/www/MCP/Alchemai/alchemai-agent`, jj-colocated,
   proprietary license, created 2026-08-21 (details in §6.7). Under `GOPRIVATE`, never a commercial subtree
   inside the public repo. Consequences: `pkg/extension` is a
   real versioned contract from P0 (no "we'll tidy the API later" grace period); the public repo contains
   only `cmd/pando/extensions_enterprise.go` (`//go:build enterprise` + blank imports); `go build -tags
   enterprise` must fail with a clear, intentional error for OSS users; CI must build **both** tag sets on
   every PR and the private repo must run a compat job against core `main` nightly, or the enterprise build
   rots silently.
2. ~~Full alternative WebUI layer, or panels + branding overlay?~~ **RESOLVED: a different visual layer,
   same communications.** The enterprise frontend re-skins and re-structures the components under a
   different corporate brand, but the transport/state implementation (REST client, SSE streams, auth token
   flow, event handling) is *identical* to core. Consequences — this is the decisive constraint on the
   frontend work:
   - The API/state layer of `web-ui/` must be **extracted into a reusable, versioned internal package**
     (client + SSE subscriptions + stores/hooks + TypeScript types), separate from the presentation
     components. That extraction is the real P4 deliverable, not the panel loader.
   - The swap happens at the **asset-root level**, not per panel: mechanism (c) from §7.3 —
     `//go:build !enterprise_ui` on `internal/api/ui_assets_app.go` plus an `enterprise_ui` variant
     embedding the enterprise `dist`. `FrontendReplacer` stays as the non-build-tag alternative.
   - Panels (§7.3a) and branding overlay (§7.3b) drop in priority: with a whole alternative frontend, they
     are not the primary path. Keep `FrontendProvider` in the API for extensions that add a panel to the
     *core* UI, but do not gate P4 on the ESM panel-loading contract.
   - Generate the TypeScript API types from one source shared by both frontends so a core API change breaks
     the enterprise build at compile time rather than at runtime.
3. ~~Binary or customer-run `xpando build`?~~ **RESOLVED: customers receive a compiled binary only.**
   Build pipelines run internally with `xpando`. Consequences: no external `GOPRIVATE` credential
   distribution, no public plugin ecosystem to support, no third-party ABI/API promise beyond our own
   pipelines. `cmd/xpando` is therefore **internal build tooling**, which lets it stay small — it does not
   need a registry, lockfile, checksums or signature verification (the Terraform/Packer supply-chain
   machinery is all unnecessary here). Licensing (§7.6) also loses urgency: with binary-only distribution
   the license check is anti-accident/entitlement-gating, not anti-tamper, so it can slip to P6 or later.
4. ~~Corporate memory sink: extension or core feature?~~ **RESOLVED: implementation lives in enterprise,
   separated from core.** Core owns only the *interfaces and the emission points* — `MemorySink`,
   `RemembranceStoreWrapper`, the `MemoryEvent` type, and the calls that publish events on memory/KB
   writes. Everything specific to the corporate backend (transport, batching, retry, spool, dedup, merged
   reads, redaction rules) ships in `alchemai-agent`. Consequence: core must define `MemoryEvent` richly
   enough on the first pass (scope, key, path, content, tags, embedding, project, timestamp, origin) —
   widening it later means a coordinated release of both repos.
5. ~~Multi-tenant/RBAC: is that an extension (`HTTPAuthProvider` + `AuthInfo.Roles`) or does core need a
   real user model first?~~ **RESOLVED 2026-08-21 (product decision): multi-tenancy is out of scope for
   Pando.** A Pando instance has exactly one internal user; tenancy is handled by an external remote
   control-plane layer sitting above Pando. Consequences:
   - Core needs **no user model, no RBAC tables, no per-resource permissions** — the largest hidden cost in
     the enterprise plan is removed.
   - `HTTPAuthProvider` narrows to two jobs: validating the token/mTLS credential minted by the control
     plane, and optional WebUI SSO. `AuthInfo.Roles` degrades to *identity asserted by the control plane*,
     not internal authorisation.
   - The corporate `MemorySink` emits `UserID`/`ProjectID` as **attribution** (who owns this instance), not
     as an isolation key. Isolation belongs to the shared remembrances backend, not to Pando.
   - This further weakens the case for go-plugin: with no tenancy there is no per-tenant process-isolation
     argument left at all.
   - New item it creates: the **control-plane contract** — how the remote layer injects identity, license
     and config into an instance, and how it reads instance state back. Model it as an enterprise
     capability (`ControlPlaneClient`), scheduled around P5/P6.

---

## 9. Proposed phased roadmap

**P0 — Foundations (no behaviour change). ✅ DONE 2026-08-21** — full record in
[[pando/changes/extension-system-p0-foundations]].
`pkg/extension` (stdlib-only contract: registry, lifecycle, `HostServices`, `Capability[T]`, tool contract);
`internal/extensions` host adapter; `[Extensions]` config with `Disabled` + `Entries`; manager wired into
`internal/app` (`App.Extensions`, started in `New`, stopped in `Shutdown`); `pando extensions list [--json]`;
`version.Variant`. `alchemai-agent` got its `go.mod` and a `compat/` contract probe. Verified end to end by
composing a binary from a separate module with a demo enterprise extension.

Two things learned in the doing, both already folded back into this document:
- No build-tagged blank-import file belongs in the public repo (it would force a `require` on a private
  module and break `go mod tidy` for OSS users). Composition is a generated main module — the xcaddy way.
- `//go:embed` cannot cross module boundaries, which rules out the build-tag frontend swap (see §7.3).
- Viper splits dotted map keys, so `[Extensions.Entries."a.b.c"]` needed an explicit flattener in
  `internal/config`; a naive `map[string]ExtensionEntry` decoded to nothing, silently.

**P1 — Tools + CLI/slash capability.**
`ToolProvider`, `ToolMiddleware` (Priority + Filter), `CommandProvider`, `SlashCommandProvider`. Wire into
`internal/llm/agent/tools.go`. Prove it by moving one existing optional core tool (e.g. the Sourcegraph or
Context7 tool set) behind the registry as a *standard* extension — that is the real test of the API.

**P2 — HTTP + auth capability.**
`HTTPEndpointProvider` (+ `/api/ext/` prefix enforcement), `HTTPAuthProvider`, `EventSubscriber` over
`internal/pubsub`. Prove with a trivial standard extension exposing `/api/ext/demo/ping`.

**P3 — `cmd/xpando` + build matrix.**
Builder with `--with/--replace/--tags/--output/--ldflags` + `GOOS/GOARCH/CGO` passthrough; `MODULE_TAGS`
threaded through every Makefile build/release rule; `Variant` stamped in `internal/version`; CI builds both
tag sets.

**P4 — Frontend capability.**
`FrontendProvider` (assets + panel manifest), `/api/v1/extensions/ui`, dynamic ESM panel loading in the
React shell with a documented `window.__PANDO_UI__` contract, overlay/branding support. Optional
`FrontendReplacer` / `enterprise_ui` tag if §8.6-Q2 says yes.

**P5 — Memory capability + first enterprise module.**
`MemorySink` + `RemembranceStoreWrapper` in core; private repo `alchemai-agent` with the corporate
remembrances sync module, porting `db-sync-server`'s merged-storage/dedup/sync-queue logic. Redaction,
spool, dry-run, visible UI indicator.

**P6 — Licensing, docs, hardening.**
`LicenseProvider` + Ed25519 license verification; panic-recovery and timeout wrappers around every
extension entry point; extension-author guide; "which mechanism do I use" decision doc (§8.5).

Rough sizing: P0–P2 are small and mostly mechanical (the remembrances-mcp code is a working template,
~500 LOC of registry/manager). P3 is a day. P4 is the largest single piece of *new* design (frontend
contract). P5 has the most reusable prior art but the highest security/compliance bar.

---

## 10. Bottom line

- Copy the model we already run in remembrances-mcp — it is Caddy's, it works, and the code is a template
  we own: global `init()` registry, namespaced IDs, `Provision/Validate/Cleanup`, capability interfaces
  discovered by type assertion, `Priority()`+`ToolFilter()` on interceptors, `BasePath()` on HTTP
  providers, decorator wrapping for storage/memory.
- Gate the closed modules with a single build-tagged blank-import file, drive the whole release matrix from
  one `MODULE_TAGS` variable, and ship `cmd/xpando` as the xcaddy-equivalent (with the `--tags`,
  `--replace` and cross-compile gaps that `xremembrances` still has, closed).
- Put the enterprise source in a private module behind `GOPRIVATE` from day one, not behind a tag in the
  public repo. That repo now exists: **`github.com/digiogithub/alchemai-agent`**
  (`/www/MCP/Alchemai/alchemai-agent`, jj-colocated, proprietary license) — see §6.7.
- Do **not** build a go-plugin/gRPC extension protocol as the general mechanism. Pando's extension surface
  is too rich and too stateful, and MCP already covers the untrusted third-party case that would justify
  the cost. Keep go-plugin as a targeted, per-capability option if a customer ever needs a privileged
  out-of-process hook.
