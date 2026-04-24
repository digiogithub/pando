# LLM Prompt Cache Control

**Implemented**: 2026-04-24

## Overview

Global toggle to enable/disable LLM prompt caching. Controlled via `[LLMCache]` in the config file.

## Configuration

**TOML**:
```toml
[LLMCache]
  Enabled = false  # Default: true
```

**JSON**:
```json
{ "llmCache": { "enabled": false } }
```

## Provider behavior

| Provider | Effect when disabled |
|---|---|
| **Anthropic** | Removes `CacheControl: {type: "ephemeral"}` headers → standard pricing |
| **OpenAI** | No effect (server-side automatic caching, no API to disable) |
| **Gemini** | No effect (implicit caching, no API to disable) |
| **Bedrock** | Always disabled (hardcoded) |

## Notes

- Default is `true` (caching enabled).
- Only Anthropic has real effect — other providers cache automatically.
- Change applies on next session creation (no restart required).
- Controlled via TUI Settings → General → LLM Prompt Cache toggle.
- Controlled via Web-UI Settings → General → LLM Prompt Cache toggle.
- REST API: `PUT /api/v1/settings` with `{"llm_cache_enabled": false}`.
