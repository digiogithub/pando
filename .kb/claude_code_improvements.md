# Anthropic Provider — Mejoras pendientes (vs claude-code-cli)

Analizadas el 2026-04-04 comparando `internal/llm/provider/anthropic.go` de pando
con `src/services/api/withRetry.ts` y `src/services/api/client.ts` de claude-code-cli.

Las mejoras listadas abajo NO están implementadas porque requieren cambios arquitectónicos
más amplios. Las ya implementadas (backoff, x-should-retry, 408/409, context overflow,
beta claude-code-20250219) están en el commit de esa misma fecha.

---

## 1. Refresh de token OAuth en 401 mid-session

**Qué hace claude-code-cli:**
En el bucle de retry, cuando se recibe un 401 o un 403 con mensaje
"OAuth token has been revoked", llama a `handleOAuth401Error(failedAccessToken)`
que fuerza la renovación del access token usando el refresh token, y luego
recrea el cliente con el token nuevo antes de reintentar.

**Por qué no se implementó:**
En pando el cliente Anthropic se crea una vez en `newAnthropicClient()` con el token
que había en ese momento. Para renovar mid-session habría que:
1. Detectar el 401 en `shouldRetry` y marcarlo como retryable.
2. En el bucle de `send`/`stream`, antes del siguiente intento, llamar a
   `auth.GetValidClaudeToken()` → `auth.SaveClaudeCredentials()` y recrear
   `a.client` con el nuevo token vía `option.WithAuthToken(newToken)`.

Funciones relevantes en pando:
- `internal/auth/claude.go`: `GetValidClaudeToken`, `RefreshClaudeToken`, `LoadClaudeCredentials`, `SaveClaudeCredentials`
- `internal/llm/provider/anthropic.go`: campo `a.options.oauthToken` y `a.client`

**Impacto:** Sesiones largas (>1h) con OAuth fallarán con 401 cuando el access token
expire si el SDK de Go no renueva automáticamente. El refresh token tiene vida larga.

---

## 2. Manejo de conexiones TCP rotas (ECONNRESET / EPIPE)

**Qué hace claude-code-cli:**
Detecta errores `APIConnectionError` cuyo campo `code` es `ECONNRESET` o `EPIPE`
(conexiones keep-alive obsoletas). Llama a `disableKeepAlive()` para que el siguiente
intento no reutilice la conexión TCP y reconecta desde cero.

**Por qué no se implementó:**
El SDK de Go de Anthropic no expone directamente el código de error de red subyacente
en `*anthropic.Error`. Habría que inspeccionar la cadena de `errors.Unwrap()` buscando
`*net.OpError` con `Code == syscall.ECONNRESET / EPIPE`, y luego crear un nuevo
`http.Client` sin keep-alive (`DisableKeepAlives: true`) para ese intento.

**Impacto:** Bajo en uso normal (sesiones cortas), mayor en sesiones muy largas
donde las conexiones keep-alive expiran a nivel TCP.

---

## 3. Fallback automático de modelo tras 3 errores 529 consecutivos

**Qué hace claude-code-cli:**
Mantiene un contador `consecutive529Errors`. Tras `MAX_529_RETRIES=3` errores 529
consecutivos, lanza `FallbackTriggeredError(originalModel, fallbackModel)` que el
llamador captura para reiniciar la petición con un modelo diferente
(e.g. Opus → Sonnet). El fallback solo aplica a `isNonCustomOpusModel` o cuando
`FALLBACK_FOR_ALL_PRIMARY_MODELS` está activado.

**Por qué no se implementó:**
Pando no tiene el concepto de "fallback model" en el nivel del provider. Requeriría:
1. Añadir un campo `fallbackModel models.Model` en `providerClientOptions`.
2. Que `shouldRetry` devuelva un nuevo tipo de señal (p.ej. `(retry bool, newModel *models.Model, delay int64, err error)`).
3. Que el agente (`internal/llm/agent/`) gestione el cambio de modelo y notifique al usuario.

**Impacto:** Cuando el modelo está sobrecargado, hoy pando agota los reintentos y falla.
Con fallback, continuaría con un modelo alternativo de forma transparente.

---

## Referencias

- claude-code-cli: `src/services/api/withRetry.ts` — función `withRetry`, `shouldRetry`, `getRateLimitResetDelayMs`
- claude-code-cli: `src/services/api/client.ts` — `getAnthropicClient`, manejo OAuth
- pando: `internal/llm/provider/anthropic.go` — `shouldRetry`, `send`, `stream`
- pando: `internal/auth/claude.go` — `RefreshClaudeToken`, `GetValidClaudeToken`
