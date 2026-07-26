---
created_at: 2026-07-27T04:19:45.992872005Z
updated_at: 2026-07-27T04:19:45.992872005Z
tags:
    - change
    - mcp
    - oauth
    - dependency
---
# Upgrade mark3labs/mcp-go v0.45.0 -> v0.57.0 (2026-07-26)

Supersede la sección 5 ("Qué ofrece ya mark3labs/mcp-go v0.45.0") de
[[mcp_client_authentication_oauth_plan]].

## Motivación

El plan de autenticación MCP ([[mcp_client_authentication_oauth_plan]]) identificó 6
carencias del cliente OAuth de mcp-go v0.45.0. La versión v0.57.0 (última publicada)
resuelve 4 de ellas, por lo que se actualiza la dependencia antes de implementar.

## Qué cambia en v0.57.0 (respecto a v0.45.0)

Resuelto:
- **RFC 8707 `resource`**: se envía en authorize (`GetAuthorizationURL`), token exchange
  (`ProcessAuthorizationResponse`), refresh (`refreshToken`) y en el registro DCR.
  El valor sale de la Protected Resource Metadata (`resourceURL`).
- **`WWW-Authenticate: … resource_metadata="…"`**: nuevo
  `OAuthHandler.HandleUnauthorizedResponse(resp)`, llamado automáticamente desde los
  transportes streamable-http y SSE en cada 401. Nuevo campo
  `OAuthConfig.ProtectedResourceMetadataURL` + `SetProtectedResourceMetadataURL()`.
- **`MCP-Protocol-Version`** en discovery: ahora `mcp.LATEST_PROTOCOL_VERSION`
  (antes fijo a `2025-03-26`).
- **Discovery más completo**: lista de candidatos de AS metadata (RFC 8414 + OIDC),
  `AuthServerMetadata` ampliado (`code_challenge_methods_supported`, revocation,
  introspection, response_modes, etc.).

Seguridad añadida:
- Validación de origen de la PRM URL anunciada (scheme+host deben coincidir con el
  base URL) — evita que un resource comprometido redirija el discovery a metadata
  del atacante.
- Binding obligatorio `resource` <-> base URL cuando la PRM viene del header 401
  (RFC 9728 §3.3/§7.3); se rechaza si falta el campo `resource`.
- Rechazo de esquemas no http(s) (`javascript:`, `data:`, `file:`) en los campos URL
  de la metadata; cap de 1 MiB al leer documentos de metadata.
- Arreglo de la race `metadataOnce`/`metadataMu` (issue #871): el discovery HTTP ya no
  se hace con el mutex tomado.
- `token_type` case-insensitive (RFC 6749 §5.1); se aceptan respuestas 2xx del token
  endpoint (Supabase devuelve 201).

Sigue faltando (lo implementa Pando):
1. Validación de `iss` en la respuesta de autorización (RFC 9207).
2. Persistencia del registro DCR: `RegisterClient` solo actualiza `h.config.ClientID`
   / `ClientSecret` en memoria; `TokenStore` únicamente guarda tokens. Hay que leer
   `GetClientID()` / `GetClientSecret()` tras el registro y persistirlos.
3. Step-up ante `403 insufficient_scope`.
4. Grant `client_credentials` (solo authorization_code + refresh_token).
5. Servidor de callback local y flujo interactivo (fuera del alcance de la librería).

## Verificación del upgrade

- `go get github.com/mark3labs/mcp-go@v0.57.0` + `go mod tidy`.
- Requiere go >= 1.25.5; Pando declara `go 1.26`. OK.
- Cambios de dependencias transitivas: entran `github.com/google/jsonschema-go v0.4.2` y
  `github.com/santhosh-tekuri/jsonschema/v6 v6.0.2`; salen `invopop/jsonschema`,
  `mailru/easyjson`, `wk8/go-ordered-map/v2`, `bahlo/generic-list-go`, `buger/jsonparser`.
- `go build ./...` limpio; `go vet` limpio en los 3 paquetes que importan mcp-go
  (`internal/mcpclient`, `internal/llm/agent`, `internal/mcpgateway`).
- Tests OK: `internal/llm/agent`, `internal/api`, `internal/mesnada/server`,
  `internal/mcpgateway`, `internal/config`, `internal/mesnada`.
- **Cero cambios de código**: la API usada por Pando (`NewStdioMCPClient`,
  `NewSSEMCPClient`, `NewStreamableHttpClient`, `client.WithHeaders`,
  `transport.WithHTTPHeaders`) no rompió.

## Impacto en el plan

- P2 se simplifica: `client.NewOAuthStreamableHttpClient` / `NewOAuthSSEClient` con un
  `TokenStore` propio ya da discovery + PKCE + DCR + refresh + `resource` conformes.
- P4 se reduce a: `iss` (RFC 9207), persistencia del registro DCR y step-up `403`.
