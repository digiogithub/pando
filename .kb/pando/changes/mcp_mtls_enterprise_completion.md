---
created_at: 2026-07-27T20:00:05.580485279Z
updated_at: 2026-07-27T20:00:05.580485279Z
tags:
    - change
    - mcp
    - mtls
    - tls
    - config
---
# Change: finish MCP mTLS for enterprise deployments (config-only)

Date: 2026-07-27. Follow-up to [[mcp_client_authentication]]; field reference in
[[mcp_server_config_fields]].

## Motivation

The phase-5 mTLS support was already wired into every HTTP transport (sse, streamable-http,
the OAuth handler's own client, and the client_credentials token client), but three gaps made
real corporate deployments impossible or forced `SkipTLSVerify`:

1. Passphrase-protected client keys in PKCS#8 PBES2 — the default output of `openssl genpkey`
   / `openssl pkcs8 -topk8`, and what corporate PKI hands operators — could not be used at all;
   Go's standard library only decrypts legacy RFC 1423 PEM. Previous behaviour was an error
   telling the operator to decrypt the key out-of-band.
2. `CACert` **replaced** the system trust store, so trusting an internal CA silently broke every
   public endpoint in the same flow (typically the authorization server).
3. A server reached by IP address, internal alias or tunnel could only be used by turning
   verification off entirely (`SkipTLSVerify`) — there was no way to pin the verification
   hostname or the TLS version range.

Explicit scope constraint from the user: TOML configuration only, no TUI/WebUI surfaces.

## What changed

- **New `internal/mcpauth/pkcs8.go`**: PBES2 (RFC 8018 §6.2) decryption of PKCS#8
  `ENCRYPTED PRIVATE KEY` blocks — PBKDF2 (HMAC-SHA1/224/256/384/512) or scrypt KDF, with
  AES-128/192/256-CBC or DES-EDE3-CBC. Unsupported schemes name the offending algorithm;
  a padding failure is reported as "the ClientKeyPassword is probably wrong". Uses stdlib
  `crypto/pbkdf2` (Go 1.26) and `golang.org/x/crypto/scrypt`, which was already in the module
  graph — no new module dependency (it moved from indirect to direct in `go.mod`).
- **`internal/mcpauth/tls.go`**: `decryptPEMKey` now routes `ENCRYPTED PRIVATE KEY` blocks to the
  new decryptor; `loadCACertPool(path, exclusive)` merges the configured CA into
  `x509.SystemCertPool()` by default (falling back to CA-only with a warning if the system store
  is unreadable) and pins to the CA alone when exclusive; `BuildTLSConfig` gained `ServerName`,
  `MinVersion`/`MaxVersion` handling plus `parseTLSVersion` and an inverted-range check, and its
  "no TLS options configured" short-circuit now accounts for the new fields.
- **`internal/config/mcp_auth.go`**: new `MCPAuth` fields `CACertExclusive`, `TLSServerName`,
  `MinTLSVersion`, `MaxTLSVersion`; `validateTLS` rejects `CACertExclusive` without `CACert`,
  unsupported TLS versions (1.0/1.1 refused per RFC 8996) and `Min > Max`. Version parsing is
  duplicated in `parseTLSVersionName` on purpose so `internal/config` does not depend on
  `internal/mcpauth` (the dependency runs the other way).
- **Schema**: `cmd/schema/main.go` + hand-applied identical edits to `pando-schema.json`
  (regenerating the whole file was rejected: the model enum is emitted in nondeterministic map
  order and would have churned ~150 unrelated lines). Verified equal to generator output with
  `jq`-diff of the auth subtree.
- **Docs**: `docs/mcp-authentication.md` — rewritten mTLS section (config-only note, encrypted-key
  formats, CA trust semantics, hostname/version knobs, "applies to OAuth traffic too"), three new
  troubleshooting entries, updated at-rest table and capability list.

None of the new fields are secrets, so the AGE at-rest set is unchanged.

## Verification

- `go build ./...` clean; `gofmt` clean on all touched files (the single offender,
  `cmd/test_ollama_main/main.go`, is pre-existing and untouched).
- Full `go test ./internal/... ./cmd/...` green after `go clean -testcache`;
  `go test -race ./internal/mcpauth/...` green.
- New `internal/mcpauth/pkcs8_test.go` with **real OpenSSL 3.0 fixtures** committed under
  `internal/mcpauth/testdata/mtls/` (aes-256-cbc+SHA256, aes-128-cbc+SHA1, scrypt, des-ede3-cbc,
  all derived from one key pair): each variant must decrypt to byte-identical PKCS#8 DER of the
  reference plaintext key; wrong password produces the passphrase-specific error.
- `TestEnterpriseMTLSEndToEnd`: live `httptest` TLS server requiring a client certificate, with a
  server certificate carrying **only** a DNS SAN that does not match the dial address — the
  request succeeds over TLS 1.3 with `ClientCert` + encrypted PKCS#8 `ClientKey` + `CACert` +
  `TLSServerName` + `MinTLSVersion`, and fails when `TLSServerName` is removed, proving the
  override (not accidental leniency) is what makes it verify.
- `TestLoadCACertPoolSystemRootsMerged` pins merge-vs-exclusive semantics;
  `TestMCPAuthValidateTLSOptions` covers the new config validation cases.
- Stale test `TestBuildTLSConfig_PKCS8EncryptedKeyGivesActionableError` (asserted the old
  "unsupported" behaviour) rewritten as
  `TestBuildTLSConfig_MalformedPKCS8EncryptedKeyGivesActionableError`.

## Known limitations after this change

- PKCS#12 (`.p12`/`.pfx`) bundles are not accepted; convert with `openssl pkcs12 -in x.p12 …`.
- PBES1 and non-CBC PBES2 ciphers are not implemented (they are obsolete/rare); the error names
  the algorithm and suggests re-encrypting with `-v2 aes-256-cbc`.
- TLS 1.0/1.1 cannot be re-enabled through config, by design.
