---
created_at: 2026-08-25T21:02:21.292628126Z
updated_at: 2026-08-25T21:02:21.292628126Z
tags:
    - change
    - extensions
    - licensing
    - hardening
    - docs
    - enterprise
---
# P6 — Licensing, hardening, docs

Date: 2026-08-25
Phase P6 of [[pando/analysis/extension_system_enterprise_analysis]] §9, the last phase of the
enterprise extension roadmap. Builds on [[pando/changes/extension-system-p0-foundations]] and
[[pando/changes/extension-system-p5-memory-capability]].

## What was changed

Three things: an Ed25519 entitlement gate, panic/timeout containment around every remaining
extension entry point, and the two documents §8.5 asked for.

### 1. Licensing

**`pkg/extension/license.go` (new, ~330 LOC)** — core owns the format, the verification and the
gate; the enterprise module owns the trusted keys and where the license comes from.

- `Entitlement` matches an extension ID exactly, by namespace (`memory.*`) or `*`.
- `LicenseClaims` — customer, issuedAt, expiresAt (zero = perpetual), entitlements, notes.
- `SignedLicense` — `{keyId, claims (raw JSON), signature (base64 Ed25519)}`.
- `VerifyLicense(data, keys)` / `SignLicense(claims, keyID, priv)`.
- `LicenseStatus` — what the CLI, API and UI render. Carries no key material.
- `LicenseProvider` interface: `Entitled(Info) error` + `LicenseStatus()`.
- `LicenseGate` / `NewUnlicensedGate` — a ready-made provider implementation enterprise modules
  embed so every build answers identically.

**Signature covers the claims bytes with `json.Compact` applied, and nothing else.** The first
attempt signed `json.Marshal(claims)` and wrote the envelope with `MarshalIndent`, which re-indents
an embedded `json.RawMessage` — every license failed its own verification. Compaction is the only
normalisation: key order, values and unknown fields survive untouched, so a field added by a newer
issuer does not invalidate an older reader's check (covered by
`TestVerifyLicenseAcceptsUnknownClaimFields`).

**`pkg/extension/manager.go`** — the gate:
- `Status.Unlicensed` is separate from `Status.Err`. A load failure is a bug report; an unlicensed
  extension is a question for whoever owns the contract.
- `resolveOrder` previews instances, type-asserts `LicenseProvider`, and pins those ahead of
  everything else.
- `entitled(info)` runs before `missingDependencies`. MIT is never gated; a provider is never gated
  by itself; a **panicking** provider blocks rather than opens the gate.
- `adoptLicenseProvider` takes the first one and warns about later ones — two gates disagreeing
  would make "is this licensed" depend on load order.
- `LicenseStatus() (LicenseStatus, bool)` — the bool distinguishes "no licensing in this build"
  from "licensing says X".
- `Cleanup`/`Unload` clear the adopted provider.

### 2. Hardening

**`internal/extensions/guard.go` (new)** — one file holding the whole policy, with three helpers:
`guardValue` (panic → zero, false), `guardErr` (panic → error), `guardDeclarative` (panic + a 30s
deadline).

Gaps closed: `coreTool.Run` (a panicking extension tool used to crash the agent when no interceptor
was configured), `coreTool.Info`, `ToolProvider.Tools`, `Tool.Info`, `EventSubscriber.Topics`
(now no-events instead of guessing "everything"), `EventSubscriber.HandleEvent` (was panic-guarded,
now also bounded — delivery is sequential, so one blocking handler silenced every subscriber after
it), `FrontendProvider.Panels`, `FrontendProvider.AssetPath`, `CommandProvider.Commands` (both call
sites) and `Command.Run`.

Deliberately **not** bounded: extension tool `Run`. It may take minutes for the same reasons `bash`
does; cancellation there is the agent's context, which is the mechanism that knows when the user
gave up.

### 3. Surfacing

- `pando extensions list` gained an `unlicensed` state and a license block under the table.
  A build with no provider prints nothing there — an empty "License: —" would suggest something is
  missing.
- **Breaking:** `pando extensions list --json` now emits `{"extensions": [...], "license": {...}}`
  instead of a bare array.
- `GET /api/v1/extensions/license` — `{gated, status?, unlicensed[]}`. Answers on every build, like
  the memory endpoint and for the same reason.

### 4. Documentation

- `docs/extension-authoring.md` (new) — lifecycle, IDs, config gates, the capability table, what the
  host guarantees (and what it does not), licensing, testing, gotchas.
- `docs/extension-mechanisms.md` (new) — §8.5's decision doc: extension vs MCP vs Lua vs skill vs
  engine template, ordered by cost, plus the three conditions that justify an extension and the note
  that extensions have no security boundary (MCP is the mechanism that does).
- README links both.

### 5. alchemai-agent (private module)

- `license/` — `extension.LicenseProvider` over `LicenseGate`. Trusted keys via `-ldflags -X`
  (`linkedKeyID` / `linkedKeyB64`) or a pipeline-regenerated `keys.go`. License located from
  `Config.LicenseFile`, then `$PANDO_LICENSE_FILE`, then `<project>/.pando/license.json`,
  `~/.pando/license.json`, `/etc/pando/license.json`. `Provision` never returns an error: a failure
  there would leave the manager with no provider and therefore *no gate*, exactly backwards.
- `cmd/mklicense` — `keygen` / `sign` / `show`. Private key written 0600.
- `compat/compat.go` — probe extended with `Entitled` / `LicenseStatus` and the `LicenseProvider`
  assertion.
- **`memorysync/transport.go` written** — the file blocked in the P5 session. `payload`, `toPayload`,
  `permanentError`, `client` with `push`/`search`. 401/403 and 4xx except 429 are permanent; 429,
  5xx and transport failures retry. Every search result is labelled `Source: "corporate"` by the
  client, never by the server. `memorysync` now builds and vets clean.

## Two decisions taken beyond §7.6

**A `LicenseProvider` is enabled by default**, even though it is non-MIT and the default rule is
"non-MIT must be switched on explicitly". Left off by default, an operator who enabled an enterprise
module without thinking about the licensing extension would get an ungated build — the exact
accident this mechanism exists to prevent. `[Extensions] Disabled` still switches it off.

**A build with no license provider gates nothing**, and logs that once. The absence of the gate is a
fact about how the binary was built, not something a customer did; refusing to start every
enterprise module over a packaging mistake would turn it into an outage.

## Files touched

Core: `pkg/extension/{license.go,license_test.go,extension.go,manager.go,manager_test.go}`,
`internal/extensions/{guard.go,guard_test.go,tools.go,tools_test.go,events.go,events_test.go,frontend.go,commands.go}`,
`internal/api/{handlers_extensions.go,handlers_extensions_test.go,routes.go}`, `cmd/extensions.go`,
`docs/extension-authoring.md`, `docs/extension-mechanisms.md`, `README.md`.

alchemai-agent: `license/{license.go,keys.go,license_test.go}`, `cmd/mklicense/main.go`,
`memorysync/transport.go`, `memorysync/README.md`, `compat/compat.go`, `README.md`.

## Verification

`go build ./...` clean. `gofmt` clean on everything touched (`cmd/test_ollama_main/main.go` has a
pre-existing diff, untouched). `go vet` clean on the touched packages. Tests pass:
`./pkg/extension ./internal/extensions ./internal/api ./internal/config ./internal/app
./internal/rag/... ./internal/llm/agent ./internal/llm/tools`. 12 new tests in `pkg/extension`,
7 in `internal/extensions`, 2 in `internal/api`, 7 in `alchemai-agent/license`.

**End to end on a composed enterprise binary** (`xpando build --with .../license --with
.../memorysync --ldflags "-X ...linkedKeyID=... -X ...linkedKeyB64=..."`), with licenses minted by
`mklicense`:

| Scenario | Result |
|---|---|
| Standard build | no license block at all; `--json` has no `license` key |
| Enterprise, no license file | `memory.sink.corp` unlicensed; `License: INVALID — no license file configured or found` |
| Valid license entitling `memory.*` | `memory.sink.corp` **loaded**; `License: ACME GmbH, expires 2027-01-31` |
| Valid license entitling only `api.audit.corp` | `unlicensed  license for "ACME GmbH" does not entitle memory.sink.corp` |
| Expired license entitling `*` | `unlicensed  extension: license expired on 2020-01-01` |
| Entitlement edited by hand in the file | `unlicensed  no usable license: extension: license signature does not verify` |

The license provider loaded in every enterprise case without being named in `pando.toml`, confirming
the default-on rule.

Not exercised: the WebUI has no license indicator — the CLI and the API endpoint cover reporting,
and unlike the memory capability nothing leaves the machine, so no always-visible indicator is
required.

## Note

Config file discovery is `<workdir>/.pando.toml` (viper `SetConfigName(".pando")`), **not**
`pando.toml` or `.pando/pando.toml`. Cost some time during verification.
