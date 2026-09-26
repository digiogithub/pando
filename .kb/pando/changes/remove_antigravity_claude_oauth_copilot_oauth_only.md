---
created_at: 2026-09-28T21:17:39.01661567Z
updated_at: 2026-09-28T21:17:39.01661567Z
tags:
    - change
    - providers
    - oauth
    - copilot
    - anthropic
---
# Change: remove Antigravity + Claude OAuth, Copilot OAuth-only (2026-09-28)

## Why
Anthropic no longer allows Claude subscription use outside its own products — API key only. Antigravity (Google OAuth, multi-account, gemini-cli pool) dropped; Gemini is API-key only (no other Gemini OAuth existed). GitHub Copilot: OAuth (device flow / discovered GitHub OAuth tokens) is the official and ONLY mode — no manual token, no selector.

## Removed (files)
internal/oauth/ (antigravity), internal/llm/provider/antigravity_{provider,oauth,scheduler,failover}.go(+tests), internal/llm/models/antigravity.go, internal/api/handlers_antigravity_oauth.go, internal/auth/claude.go, internal/stats/claude.go, internal/tui/page/antigravity_commands.go, internal/tui/components/dialog/{claude_login,claude_stats,add_provider_test}.go.

## Backend edits
- provider.go: no antigravity context/wrappers, no WithUseOAuth/WithOAuthAccount, no implicit Claude OAuth when API key empty. anthropic.go: no oauthToken / bearer branch (claudeCodeHeaders kept on API-key path).
- copilot.go: `loadCopilotCredentials(savedToken, configuredBaseURL)` — no apiKey fallback. Token discovery (GitHubOAuthTokenCandidates: env, pando session, legacy editor files, gh CLI) and device flow kept.
- models/registry/normalize/modelsdev/prompt family: ProviderAntigravity gone. fetcher: Anthropic API-key only.
- config.go: ProviderAccount OAuth fields (OAuthRefreshToken, OAuthAccessToken, OAuthExpiry, ProjectID, Email, OAuthState, OAuthCodeVerifier, OAuthRedirectURI, ReauthRequired) and UseOAuth (Provider + ProviderAccount) removed; hasClaudeCredentials, UpdateProviderOAuth removed; Copilot credential checks = hasCopilotCredentials() only; AddProviderAccount/UpdateProviderAccount clear APIKey for copilot. agecrypto no OAuth tokens. Old configs with `useOAuth` still load (non-strict decode).
- API: only Copilot auth routes remain (login/logout/status); anthropic login/stats + antigravity routes gone; anthropic SupportsOAuth=false; copilot APIKey cleared on create/update/legacy PUT.
- ACP: OpenClaudeUsage, `_pando.openClaudeUsage`, `claude/usage` intercept removed (cmd/root.go, app.go, acp).
- pando_setup: copilot credential shown as `github oauth`. pando-schema.json regenerated. docs/sandbox-coverage.md rows removed. .pando.toml `useOAuth` line removed.

## TUI
Antigravity entries/blocks/actions removed; Claude login dialog + palette commands claude:login/logout/stats:* removed. Copilot settings: read-only `Auth: GitHub OAuth (official)` row + `Login with GitHub (device flow)` action (StartCopilotLoginMsg); apiKey rejected for copilot; add-provider zeroes API key when type does not require one.

## WebUI
ProviderAccountsSettings: no antigravity, no "Use OAuth" switch/useOAuth; TYPES_WITHOUT_API_KEY=[copilot,vertexai]; copilot note + "Login with GitHub" button (startCopilotLogin); never sends apiKey for copilot. commandLauncher: Claude/antigravity commands gone, auth commands only for copilot (fixes bogus vertexai status/logout). QuickMenu anthropic icons removed. Types: useOAuth removed.

## Kept (unrelated)
Antigravity IDE MCP config format (internal/mesnada/mcpconv, mesnada/agent/mcpconfig.go), mesnada gemini-cli engine, Vertex AI ADC, MCP-server OAuth feature.

## Verification
`go build ./...`, `go vet ./...` clean; `go test ./internal/... ./cmd/...` green except `internal/config` TestSetupStatusFreshDirectoryNeedsSetup / TestPrepareSetupScopeGlobalWritesGlobalFile — they belong to concurrent, unrelated setup-wizard work (new internal/config/setup.go/setup_test.go), not this change. web-ui `bun run typecheck` clean. Not manually exercised in the UIs.
