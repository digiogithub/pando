# Implementation Plan: Claude Provider Enhancement in Pando

> Date: 2026-03-16
> Based on: `research/claude-code-api.md`

---

## Overview

Enhance the Claude/Anthropic provider in Pando to support:
1. **OAuth2 PKCE login** with claude.ai account (like Claude Code CLI does)
2. **Token reuse** from existing Claude Code installation (`~/.claude/.credentials.json`)
3. **Model list refresh** via API
4. **Usage statistics** (local stats-cache.json + API metrics)
5. **Ctrl+P command palette** actions: login, logout, show stats, show subscription

---

## Phase 1 — OAuth2 PKCE Authentication (`internal/auth/claude.go`)

### Files to create/modify

- **NEW:** `internal/auth/claude.go` — Claude OAuth2 PKCE implementation
- **MODIFY:** `internal/llm/provider/anthropic.go` — support Bearer token auth
- **MODIFY:** `internal/config/config.go` — add Claude OAuth config fields

### Implementation

```go
// internal/auth/claude.go

const (
    ClaudeClientID        = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
    ClaudeAuthorizeURL    = "https://claude.ai/oauth/authorize"
    ClaudeTokenURL        = "https://platform.claude.com/v1/oauth/token"
    ClaudeProfileURL      = "https://api.anthropic.com/api/oauth/profile"
    ClaudeOAuthBetaHeader = "oauth-2025-04-20"  // verify exact value
    
    ClaudeOAuthScopes = "user:file_upload user:inference user:mcp_servers user:profile user:sessions:claude_code"
)

type ClaudeCredentials struct {
    ClaudeAiOauth   *ClaudeOAuthToken `json:"claudeAiOauth"`
    OrganizationUUID string           `json:"organizationUuid,omitempty"`
}

type ClaudeOAuthToken struct {
    AccessToken      string   `json:"accessToken"`
    RefreshToken     string   `json:"refreshToken"`
    ExpiresAt        int64    `json:"expiresAt"`   // Unix ms
    Scopes           []string `json:"scopes"`
    SubscriptionType string   `json:"subscriptionType"`
    RateLimitTier    string   `json:"rateLimitTier"`
}

// Key functions:
// - Login() — full PKCE flow, opens browser
// - LoadCredentials() — loads ~/.claude/.credentials.json or pando's own file
// - SaveCredentials(creds) — saves to pando's credential file
// - RefreshToken(creds) — exchange refresh token for new access token
// - IsExpired(creds) — checks expiresAt with 5-min buffer
// - GetValidToken(creds) — returns valid token, auto-refreshing if needed
// - GetProfile(token) — fetch user profile from ClaudeProfileURL
// - Logout() — delete credential file
```

### PKCE Flow

1. Generate `codeVerifier` (32 random bytes, base64url)
2. Compute `codeChallenge = base64url(sha256(codeVerifier))`
3. Generate `state` nonce
4. Start local HTTP server on `127.0.0.1:0` (random port)
5. Open browser to: `ClaudeAuthorizeURL?client_id=...&response_type=code&scope=...&redirect_uri=http://127.0.0.1:<port>/callback&code_challenge=...&code_challenge_method=S256&state=...`
6. On callback: verify `state`, extract `code`
7. POST to `ClaudeTokenURL`: `{grant_type: "authorization_code", code, redirect_uri, client_id, code_verifier}`
8. Store response in `~/.config/pando/auth/claude.json`

### Token File Priority (read order)

1. `CLAUDE_CODE_OAUTH_TOKEN` env var (direct token)
2. `ANTHROPIC_API_KEY` env var (API key mode)
3. `~/.config/pando/auth/claude.json` (pando's own credential store)
4. `~/.claude/.credentials.json` (reuse existing Claude Code login — read-only fallback)

---

## Phase 2 — Provider Integration (`internal/llm/provider/anthropic.go`)

### Changes

```go
type anthropicOptions struct {
    useBedrock    bool
    disableCache  bool
    shouldThink   func(userMessage string) bool
    // NEW:
    useOAuthToken bool   // use Bearer token instead of x-api-key
    oauthToken    string // the access token
}
```

Modify `newAnthropicClient` to:
- When `oauthToken != ""`: add `option.WithAuthToken(oauthToken)` (Bearer header)
- Set `anthropic-beta: <OAUTH_BETA_HEADER>` via custom header option

Add `WithAnthropicOAuthToken(token string) AnthropicOption` constructor.

In `NewProvider` (provider.go): when provider is `anthropic` and no `apiKey`, try loading Claude OAuth credentials automatically.

---

## Phase 3 — Model Registry Enhancement (`internal/llm/models/anthropic.go`)

### Add missing models

```go
const (
    // Existing ...
    ClaudeSonnet45  ModelID = "anthropic.claude-sonnet-4-5-20250929"
    ClaudeHaiku45   ModelID = "anthropic.claude-haiku-4-5-20251001"
    ClaudeSonnet46  ModelID = "anthropic.claude-sonnet-4-6"   // latest
    ClaudeOpus46    ModelID = "anthropic.claude-opus-4-6"     // latest
)
```

### Dynamic model fetcher

Optionally implement `FetchAnthropicModels(token string) ([]Model, error)` that calls `GET https://api.anthropic.com/v1/models` with the Bearer token/API key and returns available models with their context windows.

---

## Phase 4 — Usage Statistics (`internal/stats/claude.go`)

### Files to create

- **NEW:** `internal/stats/claude.go` — stats cache reader/writer

### Data structures (matching stats-cache.json format)

```go
type ClaudeStatsCache struct {
    Version         int              `json:"version"`
    LastComputedDate string          `json:"lastComputedDate"`
    DailyActivity   []DailyActivity  `json:"dailyActivity"`
    DailyModelTokens []DailyModelTokens `json:"dailyModelTokens"`
    ModelUsage      map[string]ModelTokens `json:"modelUsage"`
    TotalSessions   int              `json:"totalSessions"`
    TotalMessages   int              `json:"totalMessages"`
    LongestSession  *SessionStat     `json:"longestSession"`
    FirstSessionDate string          `json:"firstSessionDate"`
    HourCounts      map[string]int   `json:"hourCounts"`
}

type DailyActivity struct {
    Date         string `json:"date"`
    MessageCount int    `json:"messageCount"`
    SessionCount int    `json:"sessionCount"`
    ToolCallCount int   `json:"toolCallCount"`
}

type ModelTokens struct {
    InputTokens              int64 `json:"inputTokens"`
    OutputTokens             int64 `json:"outputTokens"`
    CacheReadInputTokens     int64 `json:"cacheReadInputTokens"`
    CacheCreationInputTokens int64 `json:"cacheCreationInputTokens"`
}
```

### Key functions

- `LoadClaudeCodeStats() (*ClaudeStatsCache, error)` — reads `~/.claude/stats-cache.json`
- `LoadPandoStats() (*ClaudeStatsCache, error)` — reads pando's own stats file
- `GetWeeklyStats(cache *ClaudeStatsCache) WeeklySummary`
- `GetTodayStats(cache *ClaudeStatsCache) DailyActivity`
- `EstimateCost(usage ModelTokens, modelID string) float64` — based on published pricing

---

## Phase 5 — Command Palette Actions (Ctrl+P)

### New category

Add `CommandCategoryAccount CommandCategory = "Account"` to `internal/tui/components/dialog/commands.go`.

### New commands

| ID | Title | Description | Category |
|----|-------|-------------|----------|
| `claude:login` | Login with Claude.ai | Authenticate with your claude.ai account via OAuth | Account |
| `claude:logout` | Logout from Claude.ai | Remove saved Claude.ai credentials | Account |
| `claude:stats:daily` | Show today's usage | Display today's message/token stats | Account |
| `claude:stats:weekly` | Show weekly usage | Display this week's usage summary | Account |
| `claude:stats:browser` | Open usage page | Open claude.ai/settings/usage in browser | Account |
| `claude:subscription` | Show subscription | Display subscription type and rate limit tier | Account |
| `claude:models:refresh` | Refresh model list | Fetch latest available Claude models | Models |

### Integration points

In `internal/tui/tui.go` or the commands setup function, register these commands dynamically:
- If not logged in: show `claude:login` 
- If logged in: show `claude:logout`, `claude:stats:*`, `claude:subscription`

### Stats dialog

Create `internal/tui/components/dialog/claude_stats.go` — a simple stats overlay showing:
- Today: N messages, N sessions, N tool calls
- This week: totals + top model used
- All time: totals, first session date
- Cost estimate (if pricing data available)

---

## Phase 6 — Context Compaction in Pando

### Status

Pando doesn't currently implement auto-compaction. The agent loop in `internal/llm/agent/agent.go` sends all messages on each turn. When context fills up, the API returns an error.

### Implementation

1. Track `contextWindowUsed` tokens after each response (from `usage.InputTokens + usage.OutputTokens`)
2. Compare against model's `ContextWindow` (from `models.Model`)
3. When `used > contextWindow * threshold (e.g. 0.85)`:
   - Send summarization request (same prompt as Claude Code)
   - Replace old messages with summary
   - Emit `EventWarning` with type `"context_compacted"` for TUI notification
4. Add config option `AutoCompact bool` and `AutoCompactThreshold float64` (default 0.85)
5. Manual `/compact` command in chat (like Claude Code's `/compact`)

### Summarization prompt (adapted from Claude Code source)

```
Create a structured summary of the conversation to replace the conversation history. Include:
1. Task Overview: user's core request, success criteria, constraints
2. Current State: completed work, files modified (with paths), key outputs
3. Important Discoveries: technical constraints, decisions made, errors resolved
4. Next Steps: remaining work, pending decisions
```

---

## Implementation Order

1. **Phase 1** — OAuth auth (`internal/auth/claude.go`) — foundation for all else
2. **Phase 2** — Provider integration (wire OAuth into anthropic provider)
3. **Phase 4** — Stats reader (independent, no auth needed to read local file)
4. **Phase 5** — Command palette (requires auth + stats)
5. **Phase 3** — Model fetcher (enhancement, can use existing API key flow)
6. **Phase 6** — Context compaction (independent improvement, complex)

---

## Files to Create/Modify Summary

| File | Action | Phase |
|------|--------|-------|
| `internal/auth/claude.go` | CREATE | 1 |
| `internal/llm/provider/anthropic.go` | MODIFY | 2 |
| `internal/llm/models/anthropic.go` | MODIFY | 3 |
| `internal/stats/claude.go` | CREATE | 4 |
| `internal/tui/components/dialog/commands.go` | MODIFY | 5 |
| `internal/tui/components/dialog/claude_stats.go` | CREATE | 5 |
| `internal/tui/tui.go` | MODIFY | 5 |
| `internal/config/config.go` | MODIFY | 1,6 |
| `internal/llm/agent/agent.go` | MODIFY | 6 |

---

## Notes

- The `anthropic-beta` header value for OAuth needs to be verified by inspecting the exact string constant (`DP` in minified code) — likely `"oauth-2025-04-20"` or similar.
- When reading `~/.claude/.credentials.json` as fallback, Pando should treat it **read-only** — never write to Claude Code's credential file.
- The stats-cache.json from Claude Code is also **read-only** from Pando's perspective — Pando maintains its own stats.
- Token refresh should be transparent — handled before any API call if within the 5-minute buffer.
