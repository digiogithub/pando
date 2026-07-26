---
created_at: 2026-07-26T21:18:23.071477666Z
updated_at: 2026-07-26T21:18:23.071477666Z
tags:
    - plan
    - mcp
    - oauth
    - auth
    - research
---
# Plan: MCP client authentication (OAuth 2.1 + otros) en Pando

Fecha: 2026-07-26. Estado: investigación + diseño (sin implementar todavía).

## 1. Qué dice la especificación MCP (draft / 2025-06-18)

- La autorización es OPCIONAL y solo aplica a transportes HTTP. STDIO **no** debe usar
  este flujo: las credenciales se toman del entorno (`env`).
- El servidor MCP actúa como **OAuth 2.1 Resource Server**; el cliente MCP como cliente
  OAuth 2.1 público con PKCE (S256) obligatorio.
- Descubrimiento:
  1. Petición sin token -> `401` con cabecera
     `WWW-Authenticate: Bearer resource_metadata="https://…/.well-known/oauth-protected-resource", scope="…"`.
  2. Cliente descarga **Protected Resource Metadata** (RFC 9728) y obtiene
     `authorization_servers[]` + `scopes_supported`.
  3. Cliente descarga metadata del AS: RFC 8414
     (`/.well-known/oauth-authorization-server`) **o** OpenID Connect Discovery
     (`/.well-known/openid-configuration`). El cliente DEBE soportar ambos.
- Registro de cliente, por prioridad: Client ID Metadata Documents (URL https como
  `client_id`, draft-ietf-oauth-client-id-metadata-document-00) > cliente pre-registrado
  (`client_id` en config) > **Dynamic Client Registration RFC 7591** (`POST /register`,
  marcado como *deprecated* pero necesario por compatibilidad).
- Parámetros obligatorios en authorize + token: PKCE `code_challenge`/`code_verifier`,
  y **`resource`** (RFC 8707) con la URI canónica del servidor MCP (sin fragmento,
  sin barra final). El cliente DEBE enviarlo siempre.
- Validación de la respuesta de autorización: `state` (CSRF) y `iss` (RFC 9207)
  comparado con el issuer registrado del AS antes de canjear el código.
- Uso del token: `Authorization: Bearer <token>` en **cada** petición HTTP; nunca en query.
- Errores: `401` = falta/expiró token; `403` + `error="insufficient_scope"` + `scope="…"`
  = step-up (reautorizar con la unión de scopes previos + los del reto, reintentos limitados);
  `400` = petición malformada.
- Refresh tokens: incluir `refresh_token` en `grant_types`; opcionalmente `offline_access`
  en scopes si el AS lo anuncia.

## 2. Cómo lo implementa opencode (TypeScript)

Ficheros: `packages/opencode/src/mcp/{index.ts,auth.ts,oauth-provider.ts,oauth-callback.ts,browser.ts}`,
config en `packages/core/src/config/mcp.ts`.

- Usa el SDK oficial `@modelcontextprotocol/sdk`: implementa la interfaz
  `OAuthClientProvider` (`McpOAuthProvider`) y deja que el SDK haga descubrimiento,
  DCR, PKCE y refresh. El transporte (`StreamableHTTPClientTransport` / `SSEClientTransport`)
  recibe `authProvider` y lanza `UnauthorizedError`.
- Config por servidor remoto: `oauth: { client_id, client_secret, scope, callback_port,
  redirect_uri } | false` (`false` desactiva OAuth explícitamente).
- Persistencia: `~/.local/share/opencode/mcp-auth.json` (modo `0600`, lock de fichero),
  entradas por nombre de servidor con `{tokens, clientInfo, codeVerifier, oauthState, serverUrl}`.
  `getForUrl()` invalida credenciales si cambió la URL del servidor.
- Callback: servidor HTTP local `127.0.0.1:19876/mcp/oauth/callback` (configurable),
  timeout 5 min, valida `state` (rechaza si falta o no coincide), páginas HTML de éxito/error,
  se apaga cuando no quedan flujos pendientes.
- Estados del servidor MCP expuestos a la UI: `connected | disabled | failed | needs_auth |
  needs_client_registration`. Si el error de auth menciona `registration`/`client_id`,
  informa "el servidor no soporta DCR, añade clientId".
- Flujo en dos fases: `McpOAuthPendingProvider` (subclase que guarda tokens en memoria y
  solo hace `commit()` a disco cuando el flujo termina bien) + `startAuth` / `authenticate`
  / `finishAuth` / `removeAuth`. Guarda el transporte pendiente en `pendingOAuthTransports`
  para poder llamar a `transport.finishAuth(code)`.
- Abre el navegador; si falla publica evento `BrowserOpenFailed` y muestra la URL.
- Auto-descubrimiento: si no hay bloque `oauth` en la config igualmente intenta OAuth.

## 3. Cómo lo implementa hermes-agent (Python)

Ficheros: `tools/mcp_oauth.py` (~1200 líneas), `tools/mcp_oauth_manager.py`,
`tools/mcp_dashboard_oauth.py`, integración en `tools/mcp_tool.py`.

- Usa `mcp.client.auth.oauth2.OAuthClientProvider` del SDK Python, subclaseado como
  `HermesMCPOAuthProvider`, y un **manager singleton** (`MCPOAuthManager`) que es el único
  sitio que instancia providers.
- Aportes propios sobre el SDK:
  - **Disk-watch por mtime**: si otro proceso refresca los tokens en disco, se recarga
    antes del siguiente `async_auth_flow` (equivalente a `invalidateOAuthCacheIfDiskChanged`
    de Claude Code).
  - **Deduplicación de 401**: futures en vuelo por access_token; N llamadas concurrentes
    provocan una sola recuperación.
  - **Poisoned client registration**: ante `invalid_client` desde el token endpoint
    (verificando que la respuesta viene realmente de ese endpoint) marca el registro DCR
    como inválido y fuerza re-registro.
  - Prefetch y persistencia de la metadata OAuth.
- Storage `HermesTokenStorage` por servidor: ficheros separados `tokens` / `client_info` /
  `meta`, escritura atómica `0o600`, `snapshot()`/`restore()`, puerto de redirect cacheado.
- Callback: servidor HTTP local con puerto reservado libre (`_find_free_port`) o puerto
  cacheado; alternativa de **pegar la URL manualmente** (`_paste_callback_reader`) y
  alternativa **dashboard** (`DashboardOAuthFlow`) para entornos sin navegador.
- Detección de entorno no interactivo: si no hay TTY ni tokens cacheados lanza
  `OAuthNonInteractiveError` con el mensaje "run `hermes mcp login <name>` first".
- Otros tipos de auth soportados en `mcp_tool.py`: `headers` arbitrarias (bearer/API key),
  **mTLS** (`client_cert` / `client_key` con passphrase, tests en
  `tests/tools/test_mcp_client_cert.py`), `ssl_verify` configurable, cabecera
  `mcp-protocol-version`, y **stripping de `Authorization` en redirects cross-origin**.
- CLI: `hermes mcp login/remove <name>`.

## 4. Estado actual de Pando

- `internal/config/config.go`: `MCPServer{Command, Env, Args, Type, URL, Headers, Timeout}`.
  Tipos: `stdio`, `sse`, `streamable-http`. Único mecanismo de auth = `Headers`
  (soportan cifrado AGE vía `internal/config/agecrypto.go` + `ResolveMCPServerSecrets`).
- `internal/mcpclient/client.go::New()` crea el cliente con
  `client.NewStdioMCPClient` / `NewSSEMCPClient` / `NewStreamableHttpClient`.
  No hay OAuth, ni store de tokens, ni manejo de 401.
- `internal/llm/agent/mcp-tools.go` crea un cliente **nuevo por llamada** (líneas ~172, ~243):
  cualquier estado de auth debe vivir fuera del cliente (store compartido en disco/memoria).

## 5. Qué ofrece ya `mark3labs/mcp-go v0.45.0` (dependencia actual)

`client/oauth.go` + `client/transport/oauth.go`:

- `client.NewOAuthStreamableHttpClient(baseURL, OAuthConfig, …)` y `NewOAuthSSEClient(…)`.
- `OAuthConfig{ClientID, ClientSecret, ClientURI, RedirectURI, Scopes, TokenStore,
  AuthServerMetadataURL, PKCEEnabled, HTTPClient}`.
- Interfaz `TokenStore` (solo `MemoryTokenStore` de serie) + `Token` con `ExpiresAt`.
- `OAuthHandler`: descubrimiento RFC 9728 -> RFC 8414 -> OIDC -> endpoints por defecto;
  `RegisterClient` (DCR RFC 7591); `GetAuthorizationURL`; `ProcessAuthorizationResponse`;
  refresh automático; `GenerateCodeVerifier/CodeChallenge/State`; `GetExpectedState`.
- `OAuthAuthorizationRequiredError` + `IsOAuthAuthorizationRequiredError` + `GetOAuthHandler`.

**Carencias detectadas en mcp-go que Pando debe cubrir**:
1. No envía el parámetro `resource` (RFC 8707) ni en authorize ni en token.
2. No parsea `WWW-Authenticate: … resource_metadata="…"`; deriva la URL
   `/.well-known/oauth-protected-resource` del baseURL.
3. No valida `iss` (RFC 9207).
4. No persiste el registro DCR (`client_id`/`client_secret` dinámicos) — solo tokens.
5. No maneja `403 insufficient_scope` / step-up.
6. `MCP-Protocol-Version` fijado a `2025-03-26` en las peticiones de metadata.

## 6. Diseño propuesto para Pando

### Config (`internal/config/config.go`)

```go
type MCPServer struct {
    …
    Auth *MCPAuth `json:"auth,omitempty" toml:"Auth" yaml:"auth"`
}

type MCPAuth struct {
    Type         MCPAuthType       // "none" | "bearer" | "basic" | "header" | "oauth" | "oauth_client_credentials"
    Token        string            // bearer / api key (cifrable con AGE)
    Username     string
    Password     string
    HeaderName   string            // para type=header
    OAuth        *MCPOAuthConfig
    ClientCert   string            // mTLS opcional (fase posterior)
    ClientKey    string
    SkipTLSVerify bool
}

type MCPOAuthConfig struct {
    ClientID     string
    ClientSecret string
    Scopes       []string
    RedirectURI  string
    CallbackPort int
    AuthServerMetadataURL string
    Disabled     bool
}
```

`ResolveMCPServerSecrets` / `agecrypto.go` deben cifrar/descifrar `Token`, `Password`,
`ClientSecret` igual que hoy hacen con `Headers`.

### Nuevo paquete `internal/mcpauth`

- `Store`: persistencia por servidor en `~/.pando/mcp-auth.json` (o en la DB SQLite del
  proyecto), permisos `0600`, escritura atómica + file lock. Entrada:
  `{serverURL, tokens{access,refresh,expiresAt,scope}, clientInfo{clientID,clientSecret,issuedAt,expiresAt}, codeVerifier, state, metadata}`.
  Invalidación si cambia `serverURL` (patrón `getForUrl` de opencode).
- `Manager` singleton (patrón hermes): cachea `*transport.OAuthHandler` por servidor,
  deduplica 401 concurrentes con futures, recarga desde disco si cambia el mtime,
  marca el registro DCR como envenenado ante `invalid_client`.
- `CallbackServer`: HTTP local en `127.0.0.1:<puerto>` (por defecto uno fijo tipo 19876,
  o puerto libre cacheado), path `/mcp/oauth/callback`, timeout 5 min, validación de `state`,
  páginas HTML de éxito/error, apagado cuando no quedan flujos.
- Modo no interactivo: si no hay TTY/navegador y no hay tokens, devolver error claro
  "ejecuta `pando mcp login <name>`" + soporte de pegado manual de la URL de callback.

### Integración

- `internal/mcpclient/client.go::New()`: según `Auth.Type`
  - `bearer`/`basic`/`header` -> inyecta cabecera (unificado con `Headers`).
  - `oauth` -> `client.NewOAuthStreamableHttpClient` / `NewOAuthSSEClient` con
    `TokenStore` = store de Pando y `PKCEEnabled: true`.
  - `stdio` -> solo `Env` (según spec, sin OAuth).
- Detección de `client.IsOAuthAuthorizationRequiredError(err)` en `mcp-tools.go`:
  marcar el servidor con estado `needs_auth` (nuevo estado junto a los actuales) y
  notificar en TUI/WebUI en lugar de fallar en silencio.
- CLI + surfaces: `pando mcp login <name>` / `logout <name>` / `status`, más slash command
  `/mcp` o sección de settings, en TUI, WebUI y ACP (patrón usado por features previas).

### Fases sugeridas

- **P1** Config + AGE + auth estática (`bearer`, `basic`, `header`) — sin OAuth. Rápido y útil ya.
- **P2** `internal/mcpauth` store + manager + integración del cliente OAuth de mcp-go
  (autodescubrimiento, PKCE, DCR, refresh).
- **P3** Callback server local + `pando mcp login/logout/status` + estado `needs_auth`
  propagado a TUI/WebUI/ACP.
- **P4** Cierre de carencias de mcp-go: `resource` (RFC 8707), parseo de
  `WWW-Authenticate.resource_metadata`, validación `iss` (RFC 9207), persistencia del
  registro DCR, step-up por `403 insufficient_scope`.
- **P5** Extras: mTLS (`client_cert`/`client_key`), `oauth_client_credentials` para
  entornos headless/CI, deduplicación de 401 y disk-watch multiproceso.

## 7. Referencias

- Spec: https://modelcontextprotocol.io/specification/draft/basic/authorization
- RFC 9728 (Protected Resource Metadata), RFC 8414 (AS Metadata), RFC 7591 (DCR),
  RFC 8707 (Resource Indicators), RFC 9207 (iss), RFC 6750 (Bearer), OAuth 2.1 draft-13.
- opencode: `packages/opencode/src/mcp/*`
- hermes-agent: `tools/mcp_oauth*.py`, `tools/mcp_tool.py`

Relacionado: [[pando-mcp-gateway-implementation]]
