---
created_at: 2026-10-02T13:21:32.69667191Z
updated_at: 2026-10-02T13:21:32.69667191Z
tags:
    - feature
    - tls
    - security
    - app
    - serve
    - webui
---
# Local CA in the user profile for the auto-generated TLS certificate (2026-10-02)

Related: [[external_access_footer_toggle]], [[project_web_reverse_proxy]].

## Motivation
`pando app` / `pando serve` generated a self-signed certificate under the project's data dir (`.pando/tls/`), so every project had a different certificate and none could be imported as trusted once and for all. The single certificate was also both CA and leaf, valid 10 years (rejected as a leaf by browsers that cap validity), with SANs frozen at generation time.

## Design
`internal/tlsutil/cert.go` now keeps, under `<profile>/tls/` (`config.GlobalConfigDir()`, i.e. `$XDG_CONFIG_HOME/pando` or `~/.config/pando`):
- `ca.crt` / `ca.key`: local ECDSA P-256 CA, 10 years, `MaxPathLenZero`, critical name constraints (DNS `localhost`, `local`; loopback, RFC1918, link-local, CGNAT 100.64/10, `::1`, `fc00::/7`, `fe80::/10`). This is the file to import as trusted. Regenerated only when missing, mismatched with its key, or within 30 days of expiry.
- `server.crt` / `server.key`: leaf signed by the CA, 397 days, SANs `localhost`, `<hostname>.local` and the host's local IPs inside the permitted ranges. `server.crt` holds leaf + CA.
- The leaf is reused while valid, signed by the current CA, matching its key and covering the current names/IPs; otherwise it is reissued by the same CA (previously seen IPs are carried over, max 64). Reissuing does not affect the CA trust import.
- Generation is serialised across processes with a `.lock` directory (10 s timeout, stale after 60 s) and files are written atomically.
- `LoadPinnedLoopbackTLSConfig` is unchanged; because `server.crt` includes the CA, a parent's pin keeps verifying a reissued leaf.

## Files touched
- `internal/tlsutil/cert.go`, `internal/tlsutil/cert_test.go`: CA + leaf, `CertPaths.CAFile`.
- `internal/config/global_projects.go`: `TLSCertDir(fallback)`.
- `cmd/app.go`, `cmd/serve.go`, `cmd/agui_serve.go`, `cmd/desktop.go`, `internal/project/web_instance.go`: use `config.TLSCertDir`; `app`/`serve` print the CA path to import.

## Notes
- Old per-project `.pando/tls/` files are left in place and no longer used.
- Public IPs are not included in the leaf (outside the CA name constraints); use `--tls-cert/--tls-key` for public exposure.
- Explicit `--tls-cert/--tls-key` behaviour is unchanged.

## Verification
- `go test ./internal/tlsutil ./internal/project ./internal/config ./internal/api ./cmd` OK (reuse, reissue keeping CA, verification against CA, name constraints rejecting a public name, expiry thresholds, concurrent callers).
- Live: `pando serve` from two different project dirs with an isolated `XDG_CONFIG_HOME`: identical `ca.crt` and `server.crt` hashes, `curl --cacert ca.crt` verifies `https://localhost` and `https://127.0.0.1`, no `tls/` created under the project `.pando/`.
- Not verified: actual import into a browser / OS trust store, macOS and Windows.
