# LLM Cache Control — Implementation Plan

**Date**: 2026-04-24  
**Status**: Planned  
**Scope**: Global control of LLM prompt cache — config, TUI, Web-UI  

---

## Context

### Current caching state per provider

| Provider | Mechanism | Code-controlled | Effect of disableCache |
|---|---|---|---|---|
| **Anthropic** | `CacheControl: {type: "ephemeral"}` on messages, tools and system | Yes — `anthropicOptions.disableCache` | Real: removes headers, ~90% savings on hits |
| **OpenAI** | Automatic/server-side (prefix ≥1024 tokens) | No — `openaiOptions.disableCache` exists unused | None — no API to disable it |
| **Gemini** | Implicit (Gemini 2.5+, automatic) + Explicit API (unused) | No — `geminiOptions.disableCache` exists unused | None for implicit; would control explicit (future) |
| **Bedrock** | Always disabled (passes `WithAnthropicDisableCache()`) | Yes — hardcoded | N/A |
| **Copilot** | Not implemented | No | N/A |

### Key files
- `internal/llm/provider/anthropic.go`: `convertMessages()`, `convertTools()`, `preparedMessages()` — CacheControl active
- `internal/llm/provider/openai.go`: `disableCache` declared but unused
- `internal/llm/provider/gemini.go`: `disableCache` declared but unused
- `internal/llm/provider/provider.go`: `NewProvider()`, `NewProviderFromAccount()` — entry points
- `internal/llm/agent/agent.go`: line ~1246 — `needsExtraOpts` path creates providers directly
- `internal/config/config.go`: `Config` struct, `UpdateXxx()` pattern
- `internal/tui/page/settings.go`: `buildGeneralSection()`, `persistSetting()`
- `internal/api/handlers_settings.go`: `SettingsResponse`, `handlePutSettings()`
- `web-ui/src/stores/settingsStore.ts`: `DEFAULTS`, store
- `web-ui/src/components/settings/GeneralSettings.tsx`: General section toggles

---

## Design

### Config key
```go
type LLMCacheConfig struct {
    Enabled bool `json:"enabled" toml:"Enabled"`
}
```
Field in `Config`: `LLMCache LLMCacheConfig`  
Default: `true` (cache enabled by default)  
**Note**: Since the zero-value of bool in Go is `false`, it must be explicitly initialized to `true` in the config loading path (before unmarshal or as post-processing).

### Propagation
`config.LLMCache.Enabled = false` → providers receive `WithXxxDisableCache()` → Anthropic omits CacheControl headers → cost reduced to standard pricing.

---

## Phases

### Phase 1 — Config struct + UpdateLLMCache
**Fact ID**: `llm_cache_phase1_config`

- Add `LLMCacheConfig` struct at the end of config types
- Add `LLMCache LLMCacheConfig` to `Config`
- Add `UpdateLLMCache(enabled bool) error` (same pattern as `UpdateAutoCompact`)
- Ensure default `true`: in the config loading function, after unmarshal, if there is no explicit override, force `cfg.LLMCache.Enabled = true`. The cleanest way is to use `initDefaults()` before unmarshal or verify post-unmarshal if the field was read from the file.

### Phase 2 — Provider factory wiring
**Fact ID**: `llm_cache_phase2_provider_wiring`

- In `provider.go`: add helper `CacheDisabledOptions(providerName)` that reads `config.Get().LLMCache.Enabled`
- Update `NewProviderFromAccount()` to call the helper
- Update `needsExtraOpts` path in `agent.go` to apply cache options
- Bedrock: no changes (always disabled)

### Phase 3 — OpenAI + Gemini disableCache implementation  
**Fact ID**: `llm_cache_phase3_openai_gemini_impl`

- Document with comments in `openai.go` and `gemini.go` that the flag is wired but server-side caching from these providers cannot be disabled via API
- The flag is prepared for when providers add this capability

### Phase 4 — TUI settings
**Fact ID**: `llm_cache_phase4_tui_settings`

- In `buildGeneralSection()`: add toggle with key `"llmCache.enabled"`, label "LLM Prompt Cache"
- In `persistSetting()`: add case for `"llmCache.enabled"` → call `config.UpdateLLMCache()`
- Location: "General" section under "Core" group

### Phase 5 — Web-UI + API backend
**Fact ID**: `llm_cache_phase5_webui_api`

- `handlers_settings.go`: add `LLMCacheEnabled bool` to response, `*bool` to request, wire in GET and PUT
- `web-ui/src/types/index.ts`: add `llm_cache_enabled: boolean` to `SettingsConfig`
- `web-ui/src/stores/settingsStore.ts`: add to `DEFAULTS` with value `true`
- `web-ui/src/components/settings/GeneralSettings.tsx`: add Toggle in toggle section
- Add i18n keys for label and description

### Phase 6 — Tests + Documentation
**Fact ID**: `llm_cache_phase6_tests_docs`

- Go tests in `internal/llm/provider/anthropic_test.go` and `internal/config/config_test.go`
- Python integration tests in `tests/test_llm_cache_config.py`
- Docs in KB: `pando/docs/llm-cache.md`

---

## Config file examples

**TOML**:
```toml
[LLMCache]
  Enabled = false  # Disables prompt caching (mainly affects Anthropic)
```

**JSON**:
```json
{
  "llmCache": {
    "enabled": false
  }
}
```

---

## Important notes

1. **Only Anthropic has real effect**: OpenAI and Gemini use automatic server-side caching with no API to disable it. The flag is prepared for the future.
2. **Bedrock doesn't change**: always disables cache with `WithAnthropicDisableCache()`.
3. **Default = true**: cache enabled by default, aligned with current behavior.
4. **No restart required**: the flag is read on each provider creation (per session), so the change applies in the next session without restart.
