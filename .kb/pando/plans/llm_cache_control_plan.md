# LLM Cache Control — Plan de Implementación

**Fecha**: 2026-04-24  
**Estado**: Planificado  
**Scope**: Control global de caché de prompts LLM — config, TUI, Web-UI  

---

## Contexto

### Estado actual del caching en cada proveedor

| Proveedor | Mecanismo | Controlado por código | Efecto de disableCache |
|---|---|---|---|
| **Anthropic** | `CacheControl: {type: "ephemeral"}` en mensajes, tools y system | Sí — `anthropicOptions.disableCache` | Real: elimina headers, ~90% ahorro en hits |
| **OpenAI** | Automático/server-side (prefijo ≥1024 tokens) | No — `openaiOptions.disableCache` existe sin usar | Ninguno — no hay API para desactivarlo |
| **Gemini** | Implícito (Gemini 2.5+, automático) + Explicit API (no usado) | No — `geminiOptions.disableCache` existe sin usar | Ninguno para implícito; controlaría explícito (futuro) |
| **Bedrock** | Siempre desactivado (pasa `WithAnthropicDisableCache()`) | Sí — hardcoded | N/A |
| **Copilot** | No implementado | No | N/A |

### Archivos clave
- `internal/llm/provider/anthropic.go`: `convertMessages()`, `convertTools()`, `preparedMessages()` — CacheControl activo
- `internal/llm/provider/openai.go`: `disableCache` declarado pero sin efecto
- `internal/llm/provider/gemini.go`: `disableCache` declarado pero sin efecto
- `internal/llm/provider/provider.go`: `NewProvider()`, `NewProviderFromAccount()` — puntos de entrada
- `internal/llm/agent/agent.go`: línea ~1246 — path `needsExtraOpts` crea providers directamente
- `internal/config/config.go`: struct `Config`, patrón `UpdateXxx()`
- `internal/tui/page/settings.go`: `buildGeneralSection()`, `persistSetting()`
- `internal/api/handlers_settings.go`: `SettingsResponse`, `handlePutSettings()`
- `web-ui/src/stores/settingsStore.ts`: `DEFAULTS`, store
- `web-ui/src/components/settings/GeneralSettings.tsx`: toggles de la sección General

---

## Diseño

### Config key
```go
type LLMCacheConfig struct {
    Enabled bool `json:"enabled" toml:"Enabled"`
}
```
Campo en `Config`: `LLMCache LLMCacheConfig`  
Default: `true` (caché activado por defecto)  
**Nota**: Como el zero-value de bool en Go es `false`, se debe inicializar explícitamente a `true` en el path de carga del config (antes de hacer unmarshal o como post-process).

### Propagación
`config.LLMCache.Enabled = false` → providers reciben `WithXxxDisableCache()` → Anthropic omite CacheControl headers → coste reducido a precio estándar.

---

## Fases

### Phase 1 — Config struct + UpdateLLMCache
**Fact ID**: `llm_cache_phase1_config`

- Añadir `LLMCacheConfig` struct al final de los tipos de config
- Añadir `LLMCache LLMCacheConfig` a `Config`
- Añadir `UpdateLLMCache(enabled bool) error` (mismo patrón que `UpdateAutoCompact`)
- Asegurar default `true`: en la función de carga del config, después del unmarshal, si no hay override explícito, forzar `cfg.LLMCache.Enabled = true`. La forma más limpia es usar `initDefaults()` antes del unmarshal o verificar post-unmarshal si el campo fue leído del archivo.

### Phase 2 — Provider factory wiring
**Fact ID**: `llm_cache_phase2_provider_wiring`

- En `provider.go`: añadir helper `CacheDisabledOptions(providerName)` que lee `config.Get().LLMCache.Enabled`
- Actualizar `NewProviderFromAccount()` para llamar al helper
- Actualizar path `needsExtraOpts` en `agent.go` para aplicar las opciones de cache
- Bedrock: sin cambios (siempre desactivado)

### Phase 3 — OpenAI + Gemini disableCache implementation  
**Fact ID**: `llm_cache_phase3_openai_gemini_impl`

- Documentar con comentarios en `openai.go` y `gemini.go` que el flag está wired pero el caching server-side de estos proveedores no se puede desactivar vía API
- El flag queda preparado para cuando los proveedores añadan esta capacidad

### Phase 4 — TUI settings
**Fact ID**: `llm_cache_phase4_tui_settings`

- En `buildGeneralSection()`: añadir toggle con key `"llmCache.enabled"`, label "LLM Prompt Cache"
- En `persistSetting()`: añadir case para `"llmCache.enabled"` → llamar `config.UpdateLLMCache()`
- Ubicación: sección "General" bajo grupo "Core"

### Phase 5 — Web-UI + API backend
**Fact ID**: `llm_cache_phase5_webui_api`

- `handlers_settings.go`: añadir `LLMCacheEnabled bool` a respuesta, `*bool` al request, wire en GET y PUT
- `web-ui/src/types/index.ts`: añadir `llm_cache_enabled: boolean` a `SettingsConfig`
- `web-ui/src/stores/settingsStore.ts`: añadir a `DEFAULTS` con valor `true`
- `web-ui/src/components/settings/GeneralSettings.tsx`: añadir Toggle en sección de toggles
- Añadir claves i18n para label y descripción

### Phase 6 — Tests + Documentación
**Fact ID**: `llm_cache_phase6_tests_docs`

- Go tests en `internal/llm/provider/anthropic_test.go` y `internal/config/config_test.go`
- Python integration tests en `tests/test_llm_cache_config.py`
- Docs en KB: `pando/docs/llm-cache.md`

---

## Config file examples

**TOML**:
```toml
[LLMCache]
  Enabled = false  # Desactiva caching de prompts (afecta principalmente a Anthropic)
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

## Notas importantes

1. **Solo Anthropic tiene efecto real**: OpenAI y Gemini usan caching automático server-side sin API para desactivarlo. El flag queda preparado para el futuro.
2. **Bedrock no cambia**: siempre desactiva cache con `WithAnthropicDisableCache()`.
3. **Default = true**: caching activado por defecto, alineado con el comportamiento actual.
4. **Sin restart requerido**: el flag se lee en cada creación de provider (por sesión), por lo que el cambio aplica en la siguiente sesión sin restart.
