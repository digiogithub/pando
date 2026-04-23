# Plan: Internal Tools Implementation for Pando

## Objetivo
Implementar como herramientas internas nativas de Pando las funcionalidades de:
1. **md-fetch** (Go): fetch de URLs con conversión HTML→Markdown y detección de JSON
2. **Google Search** (hyper-mcp plugin Rust): búsqueda via Google Custom Search API
3. **Brave Search** (hyper-mcp plugin Rust): búsqueda via Brave Search API
4. **Context7** (hyper-mcp plugin Rust): resolución de IDs de librerías y obtención de docs
5. **Perplexity Search** (hyper-mcp plugin Rust): búsqueda con IA y citaciones

## Análisis de Fuentes

### md-fetch (/www/MCP/Pando/md-fetch)
- **Arquitectura Go**: Fetcher → Browser (Chrome/Firefox/Lynx/w3m/Curl) → Converter (HTML→Markdown)
- **Detección de tipo**: HTML → Markdown, JSON → pretty-printed ```json block, Plaintext → texto
- **Pando ya tiene** `fetch.go` con HTML→Markdown pero sin JSON detection
- **Mejora a incorporar**: Detección JSON por Content-Type y por contenido (starts with { o [)

### Google Search (/www/MCP/hyper-mcp/examples/plugins/google-search/src/lib.rs)
- **API**: GET https://www.googleapis.com/customsearch/v1
- **Config requerida**: GOOGLE_API_KEY, GOOGLE_SEARCH_ENGINE_ID
- **Params**: query, num(1-10), start(1-91), safe, lr, gl, cr, date_restrict, site_search, search_type
- **Respuesta raw**: JSON con searchInformation, items (title, link, displayLink, snippet), spelling

### Brave Search (/www/MCP/hyper-mcp/examples/plugins/brave-search/src/lib.rs)
- **API**: GET https://api.search.brave.com/res/v1/web/search
- **Auth**: Header X-Subscription-Token: API_KEY
- **Config requerida**: BRAVE_API_KEY
- **Params**: query, count(1-20), offset, country, search_lang, ui_lang, safesearch, freshness, result_filter
- **Respuesta raw**: JSON con query, web.results (title, url, description, page_age), discussions

### Context7 (/www/MCP/hyper-mcp/examples/plugins/context7/src/lib.rs)
- **API**: https://context7.com/api (no requiere API key)
- **Header**: X-Context7-Source: mcp-server
- **Tool 1** `c7_resolve_library_id`: GET /v1/search?query={name} → results[]{title, id, description, totalSnippets, stars}
- **Tool 2** `c7_get_library_docs`: GET /v1/{lib_id}/?context7CompatibleLibraryID={id}&topic={t}&tokens={n}
- **Respuesta docs**: Markdown directo

### Perplexity Search (/www/MCP/hyper-mcp/examples/plugins/perplexity-search/src/lib.rs)
- **API**: POST https://api.perplexity.ai/chat/completions
- **Auth**: Bearer token
- **Config requerida**: PERPLEXITY_API_KEY
- **Params**: query, model(sonar-pro|sonar-reasoning|sonar-deep-research), system_message, max_tokens, temperature, search_recency_filter, return_citations, return_images, return_related_questions
- **Respuesta raw**: OpenAI-compatible chat response con citations[] y search_results[]

## Arquitectura en Pando

### Patrón de tools existente
- Interface `BaseTool` con `Info() ToolInfo` y `Run(ctx, ToolCall) (ToolResponse, error)`
- Registro en `internal/llm/agent/tools.go` → `CoderAgentTools()` y `CoderAgentToolsWithMesnada()`
- Config en `internal/config/config.go` usando Viper (JSON + TOML + env vars)
- Settings TUI en `internal/tui/page/settings.go` → `buildSections()` → nueva sección

## Fases de Implementación

### Fase 1: Config Infrastructure (fact key: internal_tools_plan_phase1)
- Añadir `InternalToolsConfig` struct a config.go
- Añadir `InternalTools InternalToolsConfig` a `Config` struct
- Bindear env vars en `setProviderDefaults()`: PANDO_GOOGLE_API_KEY, GOOGLE_API_KEY, BRAVE_API_KEY, PERPLEXITY_API_KEY, PANDO_GOOGLE_SEARCH_ENGINE_ID
- Añadir defaults en `setDefaults()`
- Añadir `buildInternalToolsSection()` en settings.go con: toggles por tool, API keys (masked), Search Engine ID

### Fase 2: Enhanced Fetch Tool (fact key: internal_tools_plan_phase2)
- Modificar `internal/llm/tools/fetch.go`
- Añadir formato "auto" y "json"
- Detección JSON: por Content-Type (application/json) y por contenido (starts { o [)
- Pretty-print JSON → ```json ... ``` code block
- Usar InternalTools.FetchMaxSizeMB para el límite de tamaño

### Fase 3: Search Tools (fact key: internal_tools_plan_phase3)
- Crear `internal/llm/tools/search_google.go`
- Crear `internal/llm/tools/search_brave.go`
- Crear `internal/llm/tools/search_perplexity.go`
- Respuestas en Markdown estructurado (mejora vs plugins originales que devuelven texto plano)
- Errores HTTP/JSON → devolver en code block legible
- Usar permission.Service para aprobación (action: "web_search")
- Solo registrar si API key está configurada

### Fase 4: Context7 Tool (fact key: internal_tools_plan_phase4)
- Crear `internal/llm/tools/context7.go`
- Dos structs: `context7ResolveTool` y `context7DocsTool`
- Constructor `NewContext7Tools() []BaseTool`
- Solo registrar si Context7Enabled=true
- Sin API key requerida

### Fase 5: Registration & Integration (fact key: internal_tools_plan_phase5)
- Modificar `internal/llm/agent/tools.go`
- Registro condicional basado en config
- Config ejemplos JSON y TOML listos para copiar
- Documentar env vars soportadas

### Fase 6: Tests (fact key: internal_tools_plan_phase6)
- Tests en `tests/` (Python)
- Mock HTTP para las APIs externas
- Cubrir: config loading, JSON detection, cada tool con parámetros obligatorios y opcionales

## Mejoras de Pando vs plugins originales
1. **Respuestas en Markdown estructurado** con headers, bold, etc. (los plugins originales devuelven texto plano)
2. **Respuestas JSON pretty-printed** en code blocks cuando la API devuelve JSON
3. **Config integrada** en el sistema de config de Pando (JSON/TOML/env vars unificados)
4. **Panel TUI** para gestionar API keys y toggles sin editar ficheros
5. **Permission system** integrado con el sistema de permisos de Pando
6. **Context cancellation** respetado en todas las peticiones HTTP

## Archivos a Crear/Modificar
| Archivo | Acción |
|---------|--------|
| internal/config/config.go | Modificar: añadir InternalToolsConfig |
| internal/tui/page/settings.go | Modificar: añadir buildInternalToolsSection |
| internal/llm/tools/fetch.go | Modificar: JSON detection + auto format |
| internal/llm/tools/search_google.go | NUEVO |
| internal/llm/tools/search_brave.go | NUEVO |
| internal/llm/tools/search_perplexity.go | NUEVO |
| internal/llm/tools/context7.go | NUEVO |
| internal/llm/agent/tools.go | Modificar: registro condicional |
| tests/test_internal_tools_*.py | NUEVO (6 ficheros de tests) |
