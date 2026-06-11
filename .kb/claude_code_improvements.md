# Anthropic Provider — Pending Improvements (vs claude-code-cli)

Analyzed on 2026-04-04 comparing `internal/llm/provider/anthropic.go` from pando
with `src/services/api/withRetry.ts` and `src/services/api/client.ts` from claude-code-cli.

The improvements listed below are NOT implemented because they require broader architectural
changes. Those already implemented (backoff, x-should-retry, 408/409, context overflow,
beta claude-code-20250219) are in the commit from that same date.

---

## 1. OAuth token refresh on 401 mid-session

**What claude-code-cli does:**
In the retry loop, when a 401 or a 403 with the message
"OAuth token has been revoked" is received, it calls `handleOAuth401Error(failedAccessToken)`
which forces access token renewal using the refresh token, and then
recreates the client with the new token before retrying.

**Why it wasn't implemented:**
In pando, the Anthropic client is created once in `newAnthropicClient()` with the token
that existed at that time. To renew mid-session you would need to:
1. Detect the 401 in `shouldRetry` and mark it as retryable.
2. In the `send`/`stream` loop, before the next attempt, call
   `auth.GetValidClaudeToken()` → `auth.SaveClaudeCredentials()` and recreate
   `a.client` with the new token via `option.WithAuthToken(newToken)`.

Relevant functions in pando:
- `internal/auth/claude.go`: `GetValidClaudeToken`, `RefreshClaudeToken`, `LoadClaudeCredentials`, `SaveClaudeCredentials`
- `internal/llm/provider/anthropic.go`: field `a.options.oauthToken` and `a.client`

**Impact:** Long sessions (>1h) with OAuth will fail with 401 when the access token
expires if the Go SDK doesn't auto-renew. The refresh token has a long lifetime.

---

## 2. Handling broken TCP connections (ECONNRESET / EPIPE)

**What claude-code-cli does:**
Detects `APIConnectionError` errors whose `code` field is `ECONNRESET` or `EPIPE`
(stale keep-alive connections). Calls `disableKeepAlive()` so the next
attempt doesn't reuse the TCP connection and reconnects from scratch.

**Why it wasn't implemented:**
The Go Anthropic SDK doesn't directly expose the underlying network error code
in `*anthropic.Error`. You would need to inspect the `errors.Unwrap()` chain looking for
`*net.OpError` with `Code == syscall.ECONNRESET / EPIPE`, then create a new
`http.Client` without keep-alive (`DisableKeepAlives: true`) for that attempt.

**Impact:** Low in normal use (short sessions), higher in very long sessions
where keep-alive connections expire at the TCP level.

---

## 3. Automatic model fallback after 3 consecutive 529 errors

**What claude-code-cli does:**
Maintains a `consecutive529Errors` counter. After `MAX_529_RETRIES=3` consecutive 529
errors, it throws `FallbackTriggeredError(originalModel, fallbackModel)` which the
caller catches to restart the request with a different model
(e.g., Opus → Sonnet). The fallback only applies to `isNonCustomOpusModel` or when
`FALLBACK_FOR_ALL_PRIMARY_MODELS` is enabled.

**Why it wasn't implemented:**
Pando doesn't have the concept of "fallback model" at the provider level. It would require:
1. Adding a `fallbackModel models.Model` field in `providerClientOptions`.
2. Making `shouldRetry` return a new signal type (e.g., `(retry bool, newModel *models.Model, delay int64, err error)`).
3. Having the agent (`internal/llm/agent/`) manage the model change and notify the user.

**Impact:** When the model is overloaded, pando currently exhausts retries and fails.
With fallback, it would continue with an alternative model transparently.

---

## References

- claude-code-cli: `src/services/api/withRetry.ts` — functions `withRetry`, `shouldRetry`, `getRateLimitResetDelayMs`
- claude-code-cli: `src/services/api/client.ts` — `getAnthropicClient`, OAuth handling
- pando: `internal/llm/provider/anthropic.go` — `shouldRetry`, `send`, `stream`
- pando: `internal/auth/claude.go` — `RefreshClaudeToken`, `GetValidClaudeToken`
