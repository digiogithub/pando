# Claude Code CLI — API & Authentication Analysis

> Analyzed from: `/home/sevir/.local/lib/node_modules/@anthropic-ai/claude-code/cli.js` (v2.1.76)
> Date: 2026-03-16

---

## 1. Authentication — OAuth2 PKCE Flow

Claude Code uses **OAuth2 PKCE** (Proof Key for Code Exchange) to authenticate with claude.ai/platform.claude.com. There is no API key required by default — the user logs in with their Claude.ai account.

### OAuth Endpoints

| Key | URL |
|-----|-----|
| Authorize (platform) | `https://platform.claude.com/oauth/authorize` |
| Authorize (claude.ai) | `https://claude.ai/oauth/authorize` |
| Token exchange | `https://platform.claude.com/v1/oauth/token` |
| Create API key | `https://api.anthropic.com/api/oauth/claude_cli/create_api_key` |
| Roles | `https://api.anthropic.com/api/oauth/claude_cli/roles` |
| Profile | `https://api.anthropic.com/api/oauth/profile` |
| Success redirect | `https://platform.claude.com/oauth/code/success?app=claude-code` |

### OAuth Client IDs

- **claude.ai (consumer):** `9d1c250a-e61b-44d9-88ed-5944d1962f5e`
- **console/platform:** `22422756-60c9-4084-8eb7-27705fd5cf9a`

### OAuth Scopes

```
user:file_upload
user:inference
user:mcp_servers
user:profile
user:sessions:claude_code
```

The full combined scope string: `user:file_upload user:inference user:mcp_servers user:profile user:sessions:claude_code`

### Login Flow Steps

1. Generate PKCE `code_verifier` and `code_challenge` (SHA-256 hash, base64url encoded)
2. Open browser to `AUTHORIZE_URL` with params: `client_id`, `response_type=code`, `scope`, `redirect_uri`, `code_challenge`, `code_challenge_method=S256`, `state` (nonce)
3. User authenticates in browser; browser redirects back to local callback (or `MANUAL_REDIRECT_URL`)
4. Exchange `code` + `code_verifier` at `TOKEN_URL` for `access_token` + `refresh_token`
5. Store tokens in `~/.claude/.credentials.json`

### Environment Variables (alternative auth)

| Variable | Purpose |
|----------|---------|
| `CLAUDE_CODE_OAUTH_TOKEN` | Direct access token injection |
| `CLAUDE_CODE_OAUTH_REFRESH_TOKEN` | Refresh token (requires `CLAUDE_CODE_OAUTH_SCOPES`) |
| `CLAUDE_CODE_OAUTH_SCOPES` | Space-separated scopes for the refresh token |
| `CLAUDE_CODE_OAUTH_CLIENT_ID` | Override the OAuth client ID |
| `CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR` | FD to read token from |
| `ANTHROPIC_API_KEY` | Classic API key (bypasses OAuth entirely) |

---

## 2. Credential Storage

### File: `~/.claude/.credentials.json`

```json
{
  "claudeAiOauth": {
    "accessToken": "sk-ant-oat01-...",
    "refreshToken": "sk-ant-ort01-...",
    "expiresAt": 1773617325246,
    "scopes": ["user:file_upload", "user:inference", "user:mcp_servers", "user:profile", "user:sessions:claude_code"],
    "subscriptionType": "pro",
    "rateLimitTier": "default_claude_ai"
  },
  "organizationUuid": "05022630-..."
}
```

- `accessToken` prefix: `sk-ant-oat01-`
- `refreshToken` prefix: `sk-ant-ort01-`
- `expiresAt`: Unix timestamp in milliseconds
- Token refresh happens automatically before expiry (5-minute early refresh threshold: 300,000ms)
- Custom oauth file suffix (env `CLAUDE_CODE_CUSTOM_OAUTH_URL`): `.claude-custom-oauth.json` (otherwise `.credentials.json`)

---

## 3. Authenticated API Calls

### Headers

**When using OAuth token:**
```
Authorization: Bearer <accessToken>
anthropic-beta: <OAUTH_BETA_HEADER>
```

**When using API key:**
```
x-api-key: <apiKey>
```

### Base URL

`https://api.anthropic.com` (default)

### Inference Endpoint

Standard Anthropic Messages API: `POST /v1/messages`

### Token Refresh

Standard OAuth2 refresh flow:
```
POST TOKEN_URL
{
  "grant_type": "refresh_token",
  "refresh_token": "<refreshToken>",
  "client_id": "<CLIENT_ID>"
}
```

Response includes new `access_token`, `refresh_token`, `expires_in`.

---

## 4. Context Compaction (Auto-compact)

### Modes

| Mode | Value |
|------|-------|
| Disabled | `"DISABLED"` |
| Enabled | `"ENABLED"` |
| Full (aggressive) | `"ENABLED_FULL"` |

### Trigger Logic

- `autoCompactThreshold`: percentage of context window (configurable, default `-1` = disabled)
- When `current_tokens >= contextWindow * autoCompactThreshold` → triggers compaction
- Buffer: `hh1 = 500` tokens buffer added to threshold calculation
- Stats tracked: `preCompactTokenCount`, `postCompactTokenCount`, `truePostCompactTokenCount`

### Compaction Process

1. Detect `compact_boundary` marker in conversation history (stored as binary marker in session files)
2. If auto-compact: use last portion of conversation before the boundary
3. Send **summarization request** to the model with this system prompt structure:

```
You are operating as part of a context management system for an AI assistant. Your task is to create a 
structured summary of the conversation that will be placed at the start of the context window where 
the conversation history will be replaced with this summary. Your summary should be structured, 
concise, and actionable. Include:

1. Task Overview
   - The user's core request and success criteria
   - Any clarifications or constraints they specified

2. Current State
   - What has been completed so far
   - Files created, modified, or analyzed (with paths if relevant)
   - Key outputs or artifacts produced

3. Important Discoveries
   - Technical constraints or requirements uncovered
   - Decisions made and their rationale
   - Errors encountered and how they were resolved

4. Next Steps
   - Remaining work to be done
   - Pending decisions or blockers
```

4. Replace earlier conversation with the summary prefixed by:
   `"The conversation below has been summarized. The summary below covers the earlier portion of the conversation.\n\n<summary>"`
5. Continue with remaining recent messages after the compaction point

### Manual Compaction

User can trigger via `/compact` slash command. `/clear` wipes context entirely.

---

## 5. Usage Statistics

### Local Cache: `~/.claude/stats-cache.json`

```json
{
  "version": 2,
  "lastComputedDate": "2026-02-22",
  "dailyActivity": [
    {
      "date": "2026-01-31",
      "messageCount": 2794,
      "sessionCount": 3,
      "toolCallCount": 444
    }
  ],
  "dailyModelTokens": [
    {
      "date": "2026-01-31",
      "tokensByModel": {
        "claude-sonnet-4-5": { "inputTokens": 123456, "outputTokens": 7890 }
      }
    }
  ],
  "modelUsage": {
    "claude-sonnet-4-5": {
      "inputTokens": 1000000,
      "outputTokens": 50000,
      "cacheReadInputTokens": 200000,
      "cacheCreationInputTokens": 30000
    }
  },
  "totalSessions": 42,
  "totalMessages": 15234,
  "longestSession": { "date": "2026-02-02", "messageCount": 3031 },
  "firstSessionDate": "2026-01-30",
  "hourCounts": { "9": 120, "14": 340 },
  "totalSpeculationTimeSavedMs": 12345678,
  "shotDistribution": {}
}
```

### Online Stats URLs

- **Personal usage:** `https://claude.ai/settings/usage`
- **Admin/org usage:** `https://claude.ai/admin-settings/usage`
- **API metrics:** `https://api.anthropic.com/api/claude_code/metrics`
- **Org metrics check:** `https://api.anthropic.com/api/claude_code/organizations/metrics_enabled`

### In-session Metrics (tracked in memory)

- `totalCostUSD`
- `totalAPIDuration`, `totalAPIDurationWithoutRetries`
- `totalInputTokens`, `totalOutputTokens`
- `totalCacheReadInputTokens`, `totalCacheCreationInputTokens`
- `totalToolDuration`, `totalToolCount`
- `totalWebSearchRequests`
- `modelUsage` (per-model breakdown)

---

## 6. Model List (extracted from cli.js)

| Model ID | Notes |
|----------|-------|
| `claude-haiku-4-5-20251001` | Haiku 4.5 |
| `claude-sonnet-4-5-20250929` | Sonnet 4.5 |
| `claude-sonnet-4-20250514` | Sonnet 4 |
| `claude-opus-4-20250514` | Opus 4 |
| `claude-3-7-sonnet-20250219` | Sonnet 3.7 (extended thinking) |
| `claude-3-5-sonnet-20241022` | Sonnet 3.5 v2 |
| `claude-3-5-haiku-20241022` | Haiku 3.5 |

---

## 7. Key Implementation Notes

### For OAuth login in Pando

1. Implement PKCE: generate random `code_verifier` (43-128 chars, base64url), compute `code_challenge = BASE64URL(SHA256(code_verifier))`
2. Open browser to authorize URL with `response_type=code`
3. Listen on a local HTTP server for the callback (or show a code for manual entry)
4. Exchange code for tokens via POST to TOKEN_URL
5. Store tokens in `~/.config/pando/claude-credentials.json` (or read existing `~/.claude/.credentials.json`)
6. Auto-refresh when `expiresAt - now < 300000ms`

### For API calls

Use `Authorization: Bearer <accessToken>` header instead of `x-api-key`.
The Anthropic Go SDK supports custom options — pass `option.WithAuthToken(accessToken)` or use a custom HTTP client that injects the Bearer header.

### For reading existing Claude Code tokens

Claude Code stores tokens in `~/.claude/.credentials.json`. Pando can read this file directly to reuse existing login sessions — no need for the user to re-authenticate if they already use Claude Code.
