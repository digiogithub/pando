---
created_at: 2026-07-27T05:31:17.353959586Z
updated_at: 2026-07-27T05:31:17.353959586Z
tags:
    - feature
    - mcp
    - oauth
    - auth
    - complete
---
# Feature COMPLETA: autenticación de clientes MCP (OAuth 2.1 + estática + mTLS)

Fecha: 2026-07-27. Estado: **implementado de inicio a fin (P1-P6 + superficies + docs)**.
Plan original: [[mcp_client_authentication_oauth_plan]]. Upgrade de librería:
[[mcp_go_upgrade_0_57]]. Docs de fases: [[mcp_client_auth_phase1_static]],
[[mcp_client_auth_phase2_oauth_storage]], [[mcp_client_auth_phase3_oauth_flow]],
[[mcp_client_auth_phase4_spec_gaps]], [[mcp_client_auth_phase5_tls_client_credentials]],
[[mcp_client_auth_phase5_surfaces]], [[mcp_auth_phase6_security_hardening]],
[[mcp_client_auth_docs_and_schema]].

## Qué se implementó

Dependencia: `mark3labs/mcp-go` v0.45.0 -> **v0.57.0** (aporta RFC 8707 `resource`,
parseo de `WWW-Authenticate: resource_metadata`, discovery RFC 9728/8414/OIDC, PKCE,
DCR y refresh).

- **P1 — auth estática** (`internal/config/mcp_auth.go`): `MCPServer.Auth *MCPAuth` con
  `Type = none|bearer|basic|header|oauth|oauth_client_credentials`, `AuthHeaders()`
  (headers explícitos ganan sobre los derivados), `Validate()`. Secretos cifrados con AGE
  en `.pando.toml` (Token, Password, ClientKeyPassword, OAuth.ClientSecret).
- **P2 — almacenamiento y manager** (`internal/mcpauth`): `Store` sobre
  `GlobalConfigDir()/mcp-auth.json` (0600, escritura atómica, lock entre procesos,
  override `PANDO_MCP_AUTH_FILE`), `ServerTokenStore` (implementa `transport.TokenStore`),
  `Manager` con `OAuthConfig`, `InvalidateIfDiskChanged` (disk-watch por mtime),
  `PersistClientRegistration` (persistencia DCR que la librería no hace), `Do401`
  (deduplicación de 401 concurrentes), `Logout`, `HasTokens`.
- **P3 — flujo interactivo**: `CallbackServer` en loopback (puerto 19876 por defecto,
  path `/mcp/oauth/callback`, timeout 5 min, `state` obligatorio), `Manager.Login`/`Status`,
  detección de entorno no interactivo, CLI `pando mcp list|login|logout|status`
  (`--no-browser`, `--manual`, `--timeout`, `--yes`, `--force`), y aviso accionable en el
  agente: `MCP server %q requires authorization. Run: pando mcp login %s`.
- **P4 — huecos de la spec que la librería no cubre**: validación `iss` RFC 9207
  (`validateIssuer`, comparación exacta RFC 3986 §6.2.1, sin normalizar), parseo de
  `WWW-Authenticate` + step-up por `403 insufficient_scope` con unión de scopes y tope de
  2 intentos (`ScopeCapturingTransport`), expiración/invalidación del registro DCR ante
  `invalid_client`. Bug de seguridad corregido de paso: el callback trataba `?error=`
  antes de validar `state`.
- **P5 — mTLS y headless**: `ClientCert`/`ClientKey`/`ClientKeyPassword`/`CACert`/
  `SkipTLSVerify` aplicados también a las peticiones OAuth; grant `client_credentials`
  con `CanonicalResourceURI` (RFC 8707) y re-minteo al expirar (mcp-go solo refresca con
  refresh_token, por eso no se reutiliza su transporte OAuth aquí).
- **Superficies**: REST (`GET/PUT /api/v1/config/mcp-servers` con bloque auth enmascarado +
  `authStatus`; `POST /api/v1/mcp/{name}/login|logout`, `GET .../status`), TUI (settings con
  tipo/estado y acciones Login/Logout) y WebUI (selector de tipo, campos, badge de estado,
  botones Login/Logout).
- **P6 — endurecimiento**: valores sensibles del store (`accessToken`, `refreshToken`,
  `clientSecret`) cifrados con AGE por-valor (`age1:`), compatible hacia atrás con ficheros
  en claro y con fallback a texto plano + warning si no hay clave; `--manual` exige `state`
  cuando se pega la URL completa, valida `iss` si viene, y pide confirmación explícita
  (o `--force`) al pegar solo el código.
- **Docs**: `docs/mcp-authentication.md`, sección en README, y `auth` añadido al generador
  de esquema (`cmd/schema/main.go` + `pando-schema.json`).

## Verificación

- `go build ./...` limpio; `go test ./internal/... ./cmd/...` **todo verde**;
  `go test -race ./internal/mcpauth/...` limpio; `gofmt` limpio en lo tocado.
- **E2E real** contra un servidor de autorización + resource server de prueba
  (stub en Go, `pando mcp login stub --no-browser`): registro dinámico (DCR) ->
  authorize con `code_challenge_method=S256`, `state` y `resource=http://127.0.0.1:18099/mcp`
  -> callback loopback -> canje de código con `resource` y `code_verifier` -> tokens
  persistidos **cifrados** (`age1:…`, cero apariciones del token en claro en el fichero).
  `mcp list` pasa a `ok`, `mcp status` sale 0, `mcp logout` vuelve a `needs login`.
  Callback con `state` incorrecto -> **HTTP 400** (CSRF rechazado).
- Nota de diagnóstico: el `resource` solo se envía cuando el servidor publica su Protected
  Resource Metadata en la ruta RFC 9728 con inserción de path
  (`/.well-known/oauth-protected-resource/<path>`). Si un servidor no publica PRM, mcp-go
  omite `resource` — limitación de la librería, no de Pando.

## Limitaciones conocidas

- Client ID Metadata Documents (draft OAuth) no soportado: se usa DCR o cliente
  pre-registrado.
- Claves PKCS#8 cifradas (PBES2) para mTLS no las descifra la stdlib de Go: error
  accionable que sugiere `openssl pkcs8`.
- El lock entre procesos del store es best-effort (fichero centinela).
- `mcp-auth.json` y `.pando.toml` comparten la misma clave AGE (mismo modelo de amenaza).
