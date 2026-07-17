---
created_at: 2026-07-17T12:00:02.235969119Z
updated_at: 2026-07-17T12:00:02.235969119Z
tags:
    - documentation
    - webui
    - basic-auth
    - security
    - pando-docs
---
# WebUI Access (Basic Auth) Documentation

## Date
2026-07-17

## What Changed
Created user documentation for WebUI Access / Basic Auth feature in pando-docs (English and Spanish).

## Files Created
- `/www/MCP/Pando/pando-docs/content/en/docs/features/webui-access.md`
- `/www/MCP/Pando/pando-docs/content/es/docs/features/webui-access.md`

## Documentation Coverage

### When Auth Is Required
- Localhost binding → No auth needed
- Non-localhost binding → Auth required if users configured
- Requests from same machine → Always allowed

### Setup Methods
- Web UI: Settings > WebUI Access panel
- Configuration file: `.pando.toml` / `.pando.json`

### User Management
- Add users via Web UI or API
- Delete users (last user deletion disables auth)
- Reveal passwords (age-encrypted in config)

### Security Features
- Passwords stored age-encrypted
- Decrypted only in memory at startup
- HTTPS for all communication
- Constant-time credential comparison

### Configuration Reference
- `Server.Host` - Bind address
- `Server.BasicAuth.Enabled` - Enable/disable auth
- `Server.BasicAuth.Users` - Username/password pairs

## Source Code References
- `internal/api/basicauth.go` - Auth middleware and validation
- `internal/api/handlers_basicauth.go` - API handlers for user management
- `internal/config/config.go:464-477` - BasicAuthConfig struct
- `web-ui/src/components/settings/WebUIAccessSettings.tsx` - Web UI settings panel

## Verification
- Hugo build successful: 108 EN pages, 107 ES pages
- No build errors
