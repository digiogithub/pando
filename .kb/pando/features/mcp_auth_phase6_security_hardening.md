---
created_at: 2026-07-27T05:26:43.112383763Z
updated_at: 2026-07-27T05:26:43.112383763Z
---
# MCP Client Auth — Phase 6: Security Hardening (2026-07-27)

Follows [[pando/features/mcp_client_authentication]] (Phases 1-5). Three tasks, all complete.

## Task 1 — AGE-at-rest encryption for `mcp-auth.json`

- Added exported thin wrappers in `internal/config/agecrypto.go`: `EncryptSecretString`,
  `DecryptSecretString`, `IsEncryptedSecretString`. They just call the existing unexported
  `encryptSecretString`/`decryptSecretString` — no new crypto or key-management logic, same
  AGE identity (`~/.config/pando/keys/<set>/config.age.txt`) and same `"age1:"`-prefixed
  envelope `.pando.toml` secrets use.
- New `internal/mcpauth/crypto.go`: `encryptEntryValues(name, Entry) (Entry, ok bool)` and
  `decryptEntryValues(name, Entry) Entry`. Per-value encryption only —
  `Tokens.AccessToken`, `Tokens.RefreshToken`, `ClientInfo.ClientSecret` — everything else
  (ServerURL, expiry, scopes, client id, issuer, timestamps) stays in clear JSON, matching the
  `.pando.toml` per-value convention (not whole-file encryption).
- Wired into `internal/mcpauth/store.go`: `load()` calls `decryptEntryValues` on every entry
  after unmarshal; `save()` calls `encryptEntryValues` on a *copy* of the doc right before
  marshal (never mutates the caller's in-memory `fileFormat`).
- Backwards compatible: `decryptSecretString`/`DecryptSecretString` is a no-op on a value
  without the `age1:` prefix, so a legacy plaintext file loads unchanged; the next `save()`
  (any write) re-encrypts it — no explicit migration step.
- Fails open: if AGE key material can't be loaded/generated (fresh machine, unwritable config
  home), `encryptSecretString` errors, `encryptEntryValues` logs one `logging.Warn` per
  affected field and leaves that field's value in clear rather than failing the write.
- Isolated failure on read: if one entry's ciphertext fails to decrypt (wrong/rotated key,
  corrupt payload), `decryptEntryValues` logs and nils out just that entry's `Tokens` or
  `ClientInfo` (not the whole `Store.load()`); every other server entry is untouched.
- `0600`/`0700` permissions and the existing atomic temp-file+rename write are unchanged.
- Docs: `docs/mcp-authentication.md` "Encryption at rest" section rewritten — was previously
  documented as **not** encrypted; now documents the per-field envelope, backwards
  compatibility, fail-open fallback, and isolated-entry-failure behavior.

## Task 2 — `pando mcp login --manual` hardening

- `LoginPrompt.ManualCode` signature changed:
  `func(ctx) (code, state, iss string, err error)` (was `(code, state string, err error)`) —
  breaking change, only two call sites existed (`cmd/mcp.go`, tests), both updated.
- `internal/mcpauth/login.go` `Login()` now feeds the manual path's `iss` into the same
  `validateIssuer` call the HTTP callback path uses (previously always empty/skipped).
- `cmd/mcp.go` `parseManualCodeInput` reworked: returns `(code, state, iss string,
  wasBareCode bool, err error)`.
  - Full redirect URL: `state` is now **required** — a URL with `code` but no `state` is a
    hard error (was previously silently accepted as an empty state, the same lenient path as
    a bare code). `iss` is extracted from the URL's `iss` query param when present.
  - Bare code (not a parseable URL): `wasBareCode = true`, `state`/`iss` empty — unchanged
    semantics, but now gated explicitly (see below).
- New `pando mcp login --force` flag. When `--manual` recovers a bare code, the CLI prints an
  explicit warning ("cannot be checked against Pando's CSRF state...") and requires an
  interactive `y`/`yes` confirmation before calling `Login`; `--force` skips the prompt for
  scripted use. Full-URL paste (the preferred path) never prompts.
- Docs: `docs/mcp-authentication.md` — new "Pasting the full redirect URL vs. a bare code"
  subsection, `--force` flag documented, "Authorizing on a headless box" updated to mention
  the confirmation/`--force` trade-off.

## Task 3 — doc-only godoc fix

- `internal/config/mcp_auth.go`: `MCPOAuthConfig.RedirectURI`/`CallbackPort` godoc now states
  both are no-ops for `MCPAuthOAuthClientCredentials` (that grant never opens a browser or
  starts a callback server). Comment only, no behavior change.

## Tests added

- `internal/mcpauth/main_test.go`: package-wide `TestMain` redirects `$HOME` to a disposable
  temp dir for every test in the package — required once Store started transparently touching
  real AGE key material on every Set/Get; without it, `go test ./internal/mcpauth/...` would
  read/write key files under the developer's real `~/.config/pando/keys`. Verified clean
  (`ls -la ~/.config/pando/keys` timestamps unchanged after the full test run).
- `internal/mcpauth/store_crypto_test.go`: encrypted round-trip (raw on-disk bytes assert NO
  plaintext secrets, non-secret fields stay in clear, `age1:` prefix present); legacy
  plaintext file still loads and is upgraded on next write; no-AGE-key fallback (unwritable
  `~/.config`, simulated via `chmod 0500`, skipped under root); one corrupt/undecryptable
  entry doesn't break a sibling entry; 0600 perms preserved in all cases.
- `internal/mcpauth/login_manual_test.go`: full-URL-with-state validates; state mismatch
  rejected (`CSRF` in error); bare-code empty-state still accepted at the `Manager.Login`
  layer (CLI is what gates it); `iss` recovered from a pasted URL is validated and a mismatch
  aborts before token exchange (`mix-up` in error, zero token-endpoint calls).
- `cmd/mcp_test.go` (new file): unit tests for `parseManualCodeInput` — full URL w/ state,
  full URL w/ iss, full URL missing state (rejected), full URL missing code (rejected), OAuth
  `error` param surfaced, bare code (`wasBareCode=true`), empty input.
- Existing `internal/mcpauth/login_test.go`'s `ManualCode` closure updated to the new 4-value
  signature (return `""` for iss).

## Verification

- `go build ./...` — clean.
- `go vet ./internal/mcpauth/... ./internal/config/... ./cmd/...` — clean.
- `go test ./internal/mcpauth/... ./internal/config/... ./cmd/... ./internal/api/...` — all
  pass.
- `go test -race ./internal/mcpauth/...` — pass, no races.
- `go test ./internal/llm/agent ./internal/api` (CLAUDE.md verified command) — pass.
- `gofmt -l` on every touched `.go` file — clean.

## Residual caveats for a reviewer

- The AGE key itself is not per-server-scoped: any process that can decrypt `.pando.toml`
  secrets can also decrypt `mcp-auth.json` entries (by design — same key, same threat model
  as the rest of Pando's at-rest encryption). This is not new exposure, just extending the
  existing model to a second file.
- "No AGE key available" fallback only actually triggers on a key-generation/read failure
  (permission denied, read-only home) — `loadOrCreateAgeKeyManager` auto-generates a key on
  first use, so a merely-missing key is not a real-world fallback trigger, only a genuinely
  broken/inaccessible config home is.
- The cross-process file lock (`mcp-auth.json.lock`, pre-existing, unchanged) is still
  best-effort (a stale lock over 2s is force-cleared); this phase didn't touch that mechanism.
- `--force` on `pando mcp login` skips only the bare-code CSRF-state confirmation; it does not
  and should not bypass the full-URL missing-state hard error — that failure mode still
  requires re-pasting a complete URL.
