# P0 — Extension system foundations (`pkg/extension`) + `alchemai-agent` module

Date: 2026-08-21
Status: DONE
Plan: [[pando/analysis/extension_system_enterprise_analysis]] (phase P0)

## What was done

Phase P0 of the Pando Enterprise extension plan: the compile-time extension
registry, its lifecycle manager, the host adapter, configuration, and the CLI —
plus the `go.mod` and contract-probe of the private enterprise module. No
existing behaviour changes: a build with no extensions behaves exactly as before.

### New public contract — `pkg/extension` (MIT, stdlib only)

| File | Contents |
|---|---|
| `pkg/extension/doc.go` | Package contract and the rules that keep it usable out-of-tree |
| `pkg/extension/extension.go` | `ID` (namespaced, validated), `Info`, `License`, `Extension`, `Provisioner`, `Validator`, `CleanerUpper`, `Status` |
| `pkg/extension/host.go` | `HostServices` (+ typed `Bool`/`String`/`Int` config readers), `ConfigView`, `Lifecycle` |
| `pkg/extension/registry.go` | `Registry` type + package-level `Register`/`Get`/`List`/`ByNamespace`/`Len` |
| `pkg/extension/manager.go` | `Manager`: load ordering, lifecycle, panic containment, `Capability[T]` generic discovery |
| `pkg/extension/tool.go` | Tool contract (`ToolInfo`, `ToolCall`, `ToolResponse`, `Tool`, `ToolProvider`) |

The package imports **only the standard library**. That is a hard requirement,
not a preference: `go mod tidy` in the enterprise module pulled in nothing, which
confirms the contract is self-contained.

Design points worth recording:

- **Registration is `init()`-time**, Caddy-style. `Register` panics on an invalid
  ID, a missing factory, or a duplicate — a malformed extension is a programmer
  error and must fail loudly at startup, not silently disappear.
- **The registry stores factories, never instances**, so each `Manager` gets its
  own objects and tests can use an isolated `NewRegistry()`.
- **Capabilities are discovered by type assertion** (`Capability[ToolProvider](mgr)`),
  which makes adding a capability interface a backwards-compatible change.
- **Non-MIT extensions do not load by default.** Adding a private module to a
  build never silently changes behaviour: an `Enterprise`-licensed extension must
  be enabled in configuration. Bundled MIT extensions default to on.
- **Panic containment**: `Provision`, `Start`, `Stop` and `Cleanup` are all
  wrapped. A panicking extension is recorded as failed and the others still load.
  Test `TestManagerPanicInProvisionIsContained` pins this.
- **Load order follows `RequiresExtensions`** via a stable depth sort; a
  dependency cycle degrades to ID order and surfaces as a clear
  "requires X, which is not loaded" error instead of hanging.
- `Manager.Load` returns joined errors but callers treat them as non-fatal: one
  broken optional feature must not stop Pando from starting.

### Host side — `internal/extensions`

`internal/extensions/host.go` adapts core to the contract: `configView`
implements `ConfigView` (with `Lookup` resolving a dotted path against the JSON
form of the config, so this package does not have to enumerate core settings),
and `NewManager`/`Load` build the manager from `config.Get()`.

The split exists because `pkg/extension` may not import `internal/...`: Go
forbids that from another module, so any such import would quietly make the
whole contract unusable from `alchemai-agent`.

### Configuration — `internal/config`

New `ExtensionsConfig` (`[Extensions]`) with `Disabled []string` and
`Entries map[string]any`, plus `ExtensionEntry` and the `ExtensionEntries()`
flattener.

**Bug found and fixed while wiring this up:** Viper uses `.` as its key
delimiter, so `[Extensions.Entries."memory.sink.corp"]` is parsed into *nested*
maps (`memory` → `sink` → `corp`). A plain `map[string]ExtensionEntry` decodes
that to nothing at all — silently. Since dotted IDs are the core of the design,
`Entries` is a raw tree and `ExtensionEntries()` reassembles the dotted keys: a
node is an entry when it carries `enabled` or `config`, every other key is a
further ID segment, so `a.b` and `a.b.c` can both be configured. Keys are matched
case-insensitively because Viper lowercases them. Regression test:
`internal/config/extensions_test.go:TestExtensionEntriesFlattensViperNesting`.

### App wiring — `internal/app/app.go`

- New field `App.Extensions *extension.Manager` (always non-nil).
- Loaded at the end of `New()`, after every service exists, then `Start(ctx)`.
- `Shutdown()` calls `Stop(ctx)` with a 5s timeout, then `Cleanup()`.

### CLI — `cmd/extensions.go`

`pando extensions list` (with `--json`) prints ID, version, license, state
(`loaded` / `disabled` / `error` / `registered`) and description, headed by the
build variant. It builds a throwaway manager so it reports exactly what a real
run would load.

### Version — `internal/version`

Added `Variant` (empty = standard build, `enterprise` = composed with private
modules), set via `-ldflags`. Informational only.

### Enterprise module — `/www/MCP/Alchemai/alchemai-agent`

- `go.mod`: `module github.com/digiogithub/alchemai-agent`, go 1.26, requires
  `github.com/digiogithub/pando v0.647.6` with a dev `replace` to
  `../../Pando/pando`. No `go.sum` yet — the single dependency is path-replaced.
- `compat/compat.go`: a probe type implementing **every** interface of the
  contract, with `var _ = ...` assertions. No product code; its job is to stop
  compiling the moment core changes the contract. This is the compile-time half
  of the nightly compat job the analysis asked for.
- `README.md`: layout, jj workflow, the `GOPRIVATE` + SSH-alias `insteadOf`
  requirement, and how an enterprise binary is composed.

## Design decision taken during implementation

**The public repo contains no reference to `alchemai-agent` — not even a
build-tagged blank-import file.** The analysis originally sketched
`cmd/pando/extensions_enterprise.go` with `//go:build enterprise`. That is wrong
for a separate private module: a blank import forces a `require` line in the
public `go.mod`, and `go mod tidy` for OSS users would then try to fetch a repo
they cannot read. The xcaddy model avoids this entirely — the enterprise binary
is a *generated* main module that imports both core and the private module. The
`enterprise` build tag survives only as a variant label, not as a composition
mechanism.

**Consequence for the WebUI (updates §7.3 of the analysis):** `//go:embed` cannot
reach files in another module, so the "swap the embedded asset root with a build
tag" option is not viable for a frontend that lives in `alchemai-agent`. The
enterprise frontend must be embedded *inside* the enterprise module and handed to
core as an `fs.FS` — i.e. the `FrontendReplacer` capability, not a build tag.

## Verification

- `go build ./...` — clean.
- `go vet ./pkg/extension ./internal/extensions ./cmd` — clean.
- `go test ./pkg/extension` — 20 tests: ID validation, registry duplicate/invalid/nil-factory panics, sorted `List`, `ByNamespace` prefix safety, lifecycle order, validate-failure cleanup, panic containment, `Disabled` overriding `Enabled`, config-implies-enabled, non-MIT default-off, config reaching the extension, dependency ordering, missing dependency, Start/Stop/Cleanup reverse order, idempotent Cleanup, Unload, `Capability[T]` discovery, statuses.
- `go test ./internal/config` — including the three new Viper-nesting tests.
- `go test ./internal/api ./internal/llm/agent ./internal/app` — all pass, no regression.
- **Cross-module build**: `alchemai-agent` builds and vets against the local core.
- **End-to-end composition**: a throwaway module importing
  `github.com/digiogithub/pando/cmd` plus a demo enterprise extension built with
  `-X ...version.Variant=enterprise`, then:
  - with no config → extension listed as `disabled` (non-MIT default-off works);
  - with `[Extensions.Entries."memory.sink.corp"] Enabled = true` and a Config
    table → `loaded`, and `Provision` received the endpoint;
  - with `Disabled = ["memory.sink.corp"]` alongside `Enabled = true` → `disabled`
    (the strong switch wins).

## Not in this phase

`ToolProvider` is declared but **not consumed by the agent yet** — that is P1.
An extension can implement it today and its tools will not reach the model. This
is called out in `pkg/extension/tool.go` so the gap is visible rather than silent.
