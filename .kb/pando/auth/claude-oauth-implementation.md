# Claude OAuth Implementation in Pando

## Last updated: 2026-03-16 (v2)
## Claude Code CLI version referenced: 2.1.76
## Pando file: `internal/auth/claude.go`

---

## Overview

Pando implements the PKCE OAuth2 flow to authenticate with Claude (claude.ai). The implementation
must be kept in sync with the claude-code CLI to avoid authentication failures when Anthropic
updates their OAuth endpoints.

---

## Key Constants (to verify against new claude-code versions)

```go
ClaudeClientID          = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
ClaudeAuthorizeURL      = "https://claude.ai/oauth/authorize"
ClaudeTokenURL          = "https://platform.claude.com/v1/oauth/token"
ClaudeProfileURL        = "https://api.anthropic.com/api/oauth/profile"
ClaudeManualRedirectURL = "https://platform.claude.com/oauth/code/callback"
ClaudeSuccessURL        = "https://claude.ai/oauth/code/success?app=claude-code"
ClaudeOAuthBetaHeader   = "oauth-2025-04-20"  // DP constant in claude-code
```

In claude-code cli.js these map to the `d2A` config object (function `P7()`):
- `CLIENT_ID` → `ClaudeClientID`
- `CLAUDE_AI_AUTHORIZE_URL` → `ClaudeAuthorizeURL`
- `TOKEN_URL` → `ClaudeTokenURL`
- `BASE_API_URL` + `/api/oauth/profile` → `ClaudeProfileURL`
- `MANUAL_REDIRECT_URL` → `ClaudeManualRedirectURL`
- `CLAUDEAI_SUCCESS_URL` → `ClaudeSuccessURL`

---

## Scopes (CRITICAL - causes "Invalid request format" if wrong)

Pando must use the same scope set as claude-code's `ed1` constant, which is the
union of `l2A` (console scopes) and `U11` (claude.ai scopes):

```
l2A = ["org:create_api_key", "user:profile"]
U11 = ["user:profile", "user:inference", "user:sessions:claude_code", "user:mcp_servers", "user:file_upload"]
ed1 = union = ["org:create_api_key", "user:profile", "user:inference", "user:sessions:claude_code", "user:mcp_servers", "user:file_upload"]
```

Current Pando value (preserves insertion order of ed1):
```
"org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
```

**The `org:create_api_key` scope is required** by `https://claude.ai/v1/oauth/{orgUUID}/authorize`
(the internal claude.ai frontend API). Without it the browser authorization step fails with:
```json
{"type":"error","error":{"type":"invalid_request_error","message":"Invalid request format"}}
```

### How to check scopes in a new cli.js version:
```bash
python3 << 'EOF'
import re
with open('/home/sevir/.local/lib/node_modules/@anthropic-ai/claude-code/cli.js','r',errors='replace') as f:
    c=f.read()
for m in re.findall(r'(?:ZV|pp|JYK|l2A|U11|ed1)=[^\n;]{0,200}', c):
    print(m[:200])
EOF
```

---

## Authorization URL construction (TWO URLs — matches claude-code GZ1)

Claude-code's `startOAuthFlow` generates two URLs using the same PKCE parameters:

1. **Manual URL** — shown in terminal, uses `ClaudeManualRedirectURL` as redirect_uri.
   If the user opens this, they see the code on platform.claude.com and paste it.
2. **Automatic URL** — opened in browser, uses `http://localhost:{port}/callback`.
   If it works, the code arrives at the local server silently.

Both URLs have the same structure:
```
https://claude.ai/oauth/authorize?
  code=true               ← non-standard, required by Anthropic (GZ1 always adds it)
  &client_id=...
  &response_type=code
  &redirect_uri=<manual or automatic>
  &scope=<ed1 scopes>
  &code_challenge=<S256>
  &code_challenge_method=S256
  &state=<random 32 bytes base64url>
```

Optional params added by claude-code when available: `orgUUID`, `login_hint`, `login_method`

### GZ1 in cli.js (to audit new versions):
```bash
python3 << 'EOF'
with open('/home/sevir/.local/lib/node_modules/@anthropic-ai/claude-code/cli.js','r',errors='replace') as f:
    c=f.read()
idx=c.find('function GZ1(')
print(c[idx:idx+800] if idx>=0 else 'NOT FOUND')
EOF
```

---

## Login Flow (matches claude-code's startOAuthFlow + XGY)

```
1. Generate PKCE verifier (32 random bytes base64url) and challenge (sha256 → base64url)
2. Generate state (32 random bytes base64url)  ← claude-code hW4() uses 32 bytes
3. Start local callback server on localhost:0 (random port)
4. Build manual URL (ClaudeManualRedirectURL)
5. Build automatic URL (localhost redirect)
6. Print to stdout: "Opening browser to sign in…"
7. Print to stdout: "If the browser didn't open, visit: {manualURL}"
8. Open browser with automaticURL (xdg-open / open)
9. Print to stdout: "Or paste the authorization code here: "
10. Race: local callback server OR stdin manual input → code + redirectURI used
11. Token exchange with the redirect_uri that was actually used
12. Fetch profile (/api/oauth/profile) → extract org UUID, display name, subscription type
```

---

## PKCE Generation

```go
// Code verifier: 32 random bytes → base64url (no padding), 43 chars
// Code challenge: sha256(verifier_string) → base64url, 43 chars
// State: 32 random bytes → base64url (matches claude-code hW4), 43 chars
```

Claude-code functions:
- `LW4()` → code verifier: `Fy8(randomBytes(32))`
- `RW4(verifier)` → challenge: `Fy8(sha256.update(verifier).digest())`
- `hW4()` → state: `Fy8(randomBytes(32))`
- `Fy8(buf)` → base64url: `buf.toString("base64").replace(+,-).replace(/,_).replace(=,"")`

---

## Token Exchange

Claude-code function: `by8(code, state, verifier, port, isManual, expiresIn)`

**POST** `https://platform.claude.com/v1/oauth/token`

Body:
```json
{
  "grant_type": "authorization_code",
  "code": "<auth_code>",
  "redirect_uri": "<the redirect_uri actually used — localhost or platform.claude.com>",
  "client_id": "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
  "code_verifier": "<verifier>",
  "state": "<state>"
}
```

Headers: **ONLY** `Content-Type: application/json`
- ⚠️ NO `anthropic-beta` header
- ⚠️ NO `Accept` header

### How to audit by8 in a new version:
```bash
python3 << 'EOF'
with open('/home/sevir/.local/lib/node_modules/@anthropic-ai/claude-code/cli.js','r',errors='replace') as f:
    c=f.read()
idx=c.find('async function by8(')
print(c[idx:idx+600] if idx>=0 else 'NOT FOUND')
EOF
```

---

## Token Refresh

Claude-code function: `QQ6(refreshToken, {scopes})`

**POST** `https://platform.claude.com/v1/oauth/token`

Body: `{grant_type: "refresh_token", refresh_token, client_id, scope: scopes.join(" ")}`
Headers: **ONLY** `Content-Type: application/json` (no `anthropic-beta`)

---

## Profile Fetch

Claude-code function: `Kg(accessToken)` calls `/api/oauth/profile`

**GET** `https://api.anthropic.com/api/oauth/profile`
Headers: `Authorization: Bearer <token>`, `Content-Type: application/json`
⚠️ NO `anthropic-beta` header

**Response structure** (nested):
```json
{
  "account": { "uuid": "...", "display_name": "...", "email": "...", "created_at": "..." },
  "organization": {
    "uuid": "...", "organization_type": "claude_max|claude_pro|claude_enterprise|claude_team",
    "rate_limit_tier": "...", "has_extra_usage_enabled": true,
    "billing_type": "...", "subscription_created_at": "..."
  }
}
```

### organization_type → subscriptionType mapping (claude-code fZ1):
```
"claude_max" → "max", "claude_pro" → "pro",
"claude_enterprise" → "enterprise", "claude_team" → "team", default → ""
```

### How to audit Kg and fZ1 in a new version:
```bash
python3 << 'EOF'
with open('/home/sevir/.local/lib/node_modules/@anthropic-ai/claude-code/cli.js','r',errors='replace') as f:
    c=f.read()
for fn in ['async function Kg(', 'async function fZ1(']:
    idx=c.find(fn)
    if idx>=0: print(f'--- {fn} ---\n', c[idx:idx+600])
EOF
```

---

## Post-login API calls (claude-code wc6 handler)

After getting the access token, claude-code makes additional calls. Pando currently
implements only the profile fetch. The full wc6 sequence is:

1. `Kg(accessToken)` → profile → `hZ6({accountUuid, emailAddress, organizationUuid, ...})`
2. `$f6(tokens)` → store credentials to `~/.claude/.credentials.json`
3. `xy8(accessToken)` → GET `https://api.anthropic.com/api/oauth/claude_cli/roles`
   → stores organization_role, workspace_role, organization_name
4. If `user:inference` in scopes: `CU4()` → GET `/api/organization/claude_code_first_token_date`
5. Else: `uy8(accessToken)` → POST `https://api.anthropic.com/api/oauth/claude_cli/create_api_key`

Pando does #1 only. Items #3-5 are for telemetry/API key creation and not critical for auth.

---

## anthropic-beta Header Usage

`anthropic-beta: oauth-2025-04-20` IS used when making inference/API requests:
```js
// Used when building Authorization headers for API calls, NOT in OAuth flow
headers: {Authorization: `Bearer ${token}`, "anthropic-beta": DP}
```

Do NOT send in: token exchange, token refresh, or profile fetch.

---

## Manual Code Entry

When the browser-based automatic flow fails (browser POST to claude.ai internal API fails),
the user can use the manual URL which redirects to `platform.claude.com/oauth/code/callback`.
That page shows the code which the user can paste into the terminal.

Pando now accepts:
- Raw authorization code pasted directly
- Full callback URL pasted (code and state extracted automatically)

---

## How to audit a new claude-code CLI version (full script)

```bash
python3 << 'EOF'
import re, json

cli = '/home/sevir/.local/lib/node_modules/@anthropic-ai/claude-code/cli.js'

# Version
with open(cli.replace('cli.js','package.json')) as f:
    print('Version:', json.load(f)['version'])

with open(cli, 'r', errors='replace') as f:
    c = f.read()

# URLs
for m in re.findall(r'(?:CLIENT_ID|TOKEN_URL|CLAUDE_AI_AUTHORIZE|MANUAL_REDIRECT|BASE_API_URL|SUCCESS_URL):[^,}]{0,100}', c):
    print(m[:120])

# Scopes
for m in re.findall(r'(?:ZV|pp|JYK|l2A|U11|ed1)=[^\n;]{0,200}', c):
    print(m[:200])

# Key functions
for fn in ['function GZ1(', 'async function by8(', 'async function Kg(', 'async function fZ1(', 'async function wc6(']:
    idx = c.find(fn)
    if idx >= 0:
        print(f'\n=== {fn} ===')
        print(c[idx:idx+500])
EOF
```

---

## Known Issues Fixed (2026-03-16)

| Issue | Symptom | Fix |
|-------|---------|-----|
| Missing `org:create_api_key` scope | Browser POST to `/v1/oauth/{orgUUID}/authorize` → `"Invalid request format"` | Added to `ClaudeOAuthScopes` |
| Flat `ClaudeProfile` struct | DisplayName and orgUUID never parsed | Updated to nested struct matching API |
| `anthropic-beta` in token exchange/refresh | Potential rejection | Removed, matches claude-code `by8`/`QQ6` |
| `anthropic-beta` in profile fetch | Mismatch with claude-code | Removed, matches claude-code `Kg` |
| OrganizationUUID not saved after login | `creds.OrganizationUUID` always empty | Extracted from `profile.Organization.UUID` |
| Single URL, no terminal output | Browser failure = silent hang | Two URLs generated; manual URL always printed |
| No manual code entry | Browser callback required | stdin scanner accepts code or full URL |
| State uses 16 bytes | Mismatch with claude-code 32 bytes | Updated `generateState` to use 32 bytes |
