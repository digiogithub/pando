---
created_at: 2026-08-03T08:25:18.593326036Z
updated_at: 2026-08-03T08:25:18.593326036Z
tags:
    - fix
    - security
    - dependencies
    - codeql
    - dependabot
---
# Minimal hardening pass for GitHub CodeQL + Dependabot alerts (2026-08-03)

## Motivation

Ran `.ai/scripts/code-analysis.sh` (CodeQL code-scanning alerts) and
`.ai/scripts/dependabot.sh` against `digiogithub/pando`. Goal was explicitly a
*minimal* pass: fix only the alerts that represent a real, reachable defect,
without a broad refactor.

Alert inventory at the time: 85 open CodeQL alerts (8 critical, 77 high) and
~60 open Dependabot alerts (7 critical, all `golang.org/x/crypto`).

## Triage outcome

**Fixed (real, reachable):**

| Alert | Rule | Site |
|---|---|---|
| 92 | `go/reflected-xss` | `internal/mcpauth/callback.go:305` |
| 61 | `go/reflected-xss` | `internal/tui/page/antigravity_commands.go:78` |
| 73-76 | `go/incorrect-integer-conversion` | `internal/api/terminal_pty.go:87,88,233,234` |
| 37, 38 | `go/unsafe-unzip-symlink` | `internal/runtime/embedded/unpack.go` |
| 72 | `js/insecure-randomness` | `web-ui/src/stores/terminalStore.ts:60` |

**Assessed as false positives, left alone:**

- The ~65 `go/path-injection` alerts across `internal/mesnada/**`,
  `internal/rag/kb`, `internal/skills`, `internal/luaengine`,
  `internal/project`, `internal/runtime/embedded/store.go`: paths come from
  local config / the user's own workspace, which is the tool's whole purpose.
  Pando is a local developer CLI, not a multi-tenant service.
- `internal/api/handlers_files.go:382` — already validates `filepath.IsAbs`
  after `filepath.Clean`; the directory-browser endpoint is intentional.
- `internal/runtime/embedded/unpack.go:32` (`go/zipslip`) — `safeJoin` already
  contains the traversal check; CodeQL does not model the helper.
- `internal/llmproxy/handlers_embeddings.go:128` (`go/request-forgery`) —
  `baseURL` comes from `resolveEmbeddingsEndpoint(account, providerType)`,
  i.e. configured provider accounts, not request data.
- `internal/auth/copilot.go` and `internal/skills/catalog/*`
  (`go/request-forgery`) — same reasoning: configured endpoints.
- `internal/llm/tooldiscovery/policy.go:81`, `internal/runtime/events.go:65`
  (`go/uncontrolled-allocation-size`) — sizes are config-derived and already
  clamped by `min`.
- `internal/mesnada/server/api.go:175` (`go/incorrect-integer-conversion`) —
  `limit` already bounded by `size-start`.
- `internal/lsp/client.go:71`, `internal/llm/tools/browser_session.go:353`
  (`go/command-injection`) — commands come from LSP/browser config the user
  owns.

## Changes

### 1. `internal/mcpauth/callback.go` — reflected XSS

`writeCallbackPage` interpolated `detail` into an HTML template with
`fmt.Fprintf`. `detail` is built from the `error` and `error_description`
query parameters of the OAuth redirect, so a crafted link to the local
callback port injected arbitrary markup. Added `html` import and wrapped the
interpolation in `html.EscapeString(detail)`.

### 2. `internal/tui/page/antigravity_commands.go` — reflected XSS

Same class in the Antigravity Google OAuth callback handler: `errParam` from
`r.URL.Query().Get("error")` written straight into the response. Added `html`
import, wrapped in `html.EscapeString`.

### 3. `internal/api/terminal_pty.go` — integer conversion / overflow

`newPTYSession` and `ptySession.resize` converted client-supplied `cols`/`rows`
to `uint16` and computed pixel sizes as `cols*8` / `rows*16` with no upper
bound. Values arriving from the HTTP terminal API could wrap around to a tiny
or nonsensical window size. Added:

- `const maxPTYDim = 2000` — keeps `cols*8` and `rows*16` inside `uint16`.
- `clampPTYDim(v, def int) int` — falls back to `def` when `v < 2`, caps at
  `maxPTYDim`.

Both `newPTYSession` and `resize` now clamp before the conversion.

### 4. `internal/runtime/embedded/unpack.go` — symlink escape

`tar.TypeSymlink` entries called `os.Symlink(header.Linkname, targetPath)`
without validating `Linkname`. `safeJoin` only guarded the symlink's *own*
location, not its target, so an OCI layer could plant a link resolving outside
the extracted rootfs. Added `checkLinkTarget(root, linkPath, target)`:

- rejects an empty target;
- absolute targets are re-based on the image root via `safeJoin`;
- relative targets are resolved against `filepath.Dir(linkPath)`;
- the cleaned result must equal the root or sit under `root + separator`.

The `tar.TypeLink` (hardlink) branch already used `safeJoin` on `Linkname` and
was left unchanged.

### 5. `web-ui/src/stores/terminalStore.ts` — insecure randomness

`makeId` used `Math.random().toString(36)`. Terminal ids address live PTY
sessions, so it now draws 8 bytes from `crypto.getRandomValues` and hex-encodes
them.

### 6. Go dependency bumps

```
filippo.io/edwards25519   v1.1.0  -> v1.1.1
github.com/ulikunitz/xz   v0.5.9  -> v0.5.15
github.com/xuri/excelize/v2 v2.9.0 -> v2.11.0
golang.org/x/crypto       v0.49.0 -> v0.53.0   (clears 7 critical alerts)
golang.org/x/image        v0.38.0 -> v0.41.0
golang.org/x/net          v0.52.0 -> v0.56.0
google.golang.org/grpc    v1.80.0 -> v1.82.1
```

Advisories required `x/crypto >= 0.52.0` and `x/net >= 0.55.0`, but
`excelize v2.11.0` pins higher minimums (`x/crypto v0.53.0`, `x/net v0.56.0`),
so those were used. `go mod tidy` also pulled transitive bumps of
`x/sync`, `x/sys`, `x/term`, `x/text`, `richardlehane/mscfb`,
`richardlehane/msoleps`, `xuri/efp`, `xuri/nfp`, `genproto`, and added
`tiendc/go-deepcopy`.

**Gotcha:** `go get` with multiple module arguments is atomic. The first
attempt included `github.com/docker/docker@v29.3.1+incompatible`, which does
not exist on the module proxy, and the failure silently discarded *every*
other upgrade — `go.mod` was unchanged even though `go build` succeeded. Always
re-grep `go.mod` after a multi-module `go get`.

### 7. npm

`npm audit fix` in `web-ui/` (removed 37, changed 46 packages) and at the repo
root. Cleared the transitive `brace-expansion`, `fast-uri`, `postcss`, `vite`,
and `dompurify` advisories.

## Deliberately not done

- **`github.com/docker/docker`** (alerts 23-31, high/medium): advisory wants
  `v29.3.1`, but Docker publishes v29 only under the `+incompatible` scheme and
  no `v29.x` tag resolves through the module proxy — newest available is
  `v28.5.2+incompatible`, already in use. Not fixable without switching to the
  split `docker/docker/client` module.
- **`react-router`** (alerts 34-39, 73-78): remaining fix needs the 7.x -> 8.x
  major, out of scope for a minimal pass. `npm audit fix` took it as far as the
  7.x line allows; 2 high advisories remain.
- **`sharp`** (alerts 69, 84): fix requires `--force` to `0.35.3`, a breaking
  change. Root `package.json` pins `^0.34.5`.
- **`examples/copilotkit/package-lock.json`** (`postcss`, `@hono/node-server`,
  `uuid`, `@ai-sdk/provider-utils`): example project, not shipped.

## Verification

- `go build ./...` — clean.
- `go vet ./internal/mcpauth ./internal/api ./internal/runtime/embedded ./internal/tui/page` — clean.
- `go test ./internal/...` — full suite passes, no failures.
- `npx tsc --noEmit` in `web-ui/` — clean.
- `npm run build` in `web-ui/` — succeeds.

Related: [[feature_mcp_client_authentication]], [[fix_webui_basic_auth]],
[[feature_lsp_ondemand_install_activation]].
