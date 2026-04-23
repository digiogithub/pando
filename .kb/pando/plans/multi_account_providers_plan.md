# Plan: Sistema Multi-Cuenta de Proveedores en Pando

**Creado**: 2026-04-23  
**Estado**: planificado  
**Objetivo**: Permitir múltiples cuentas por proveedor, añadir proveedores OpenAI-compatibles con URL, API key y headers custom, y actualizar TUI + Web-UI para gestionarlos.

## Contexto y motivación

La arquitectura actual usa `Config.Providers map[models.ModelProvider]Provider`, que solo permite **una cuenta por tipo de proveedor**. Para soportar dos cuentas Anthropic, dos OpenAI, etc., se necesita un modelo de "cuentas nombradas" donde cada cuenta tiene un ID único y un tipo de proveedor.

### Estructura actual (limitante)
- `Config.Providers map[models.ModelProvider]Provider` → clave = tipo ("anthropic"), valor = {APIKey, BaseURL, Disabled, UseOAuth}
- Un solo proveedor del mismo tipo en todo el sistema
- No hay soporte para headers HTTP adicionales

### Estructura propuesta
- `Config.ProviderAccounts []ProviderAccount` → lista de cuentas nombradas
- Cada cuenta: `{ID, DisplayName, Type, APIKey, BaseURL, ExtraHeaders, Disabled, UseOAuth}`
- Nuevo tipo de proveedor: `openai-compatible` para endpoints OpenAI-API-compatibles genéricos
- Model selector muestra `cuenta: modelo` cuando hay múltiples cuentas del mismo tipo

---

## Fases de implementación

### Fase 1: Modelo de datos ProviderAccount y migración de config
**Fact ID**: `multi_account_providers_phase1_data_model`

- Nuevo struct `ProviderAccount` en `internal/config/config.go`
- Reemplazar `Config.Providers` por `Config.ProviderAccounts []ProviderAccount`
- Migración automática en `Load()`: si `providerAccounts` vacío y `providers` tiene datos → auto-migrar
- Funciones CRUD: `AddProviderAccount`, `UpdateProviderAccount`, `DeleteProviderAccount`, `GetProviderAccounts`, `GetProviderAccount`
- Nuevo `ProviderOpenAICompatible ModelProvider = "openai-compatible"` en `models/models.go`
- Tests de migración en `config_test.go`

### Fase 2: Capa de proveedor y registro de modelos
**Fact ID**: `multi_account_providers_phase2_provider_layer`

- `providerClientOptions` gana campo `extraHeaders map[string]string`
- Nueva función `NewProviderFromAccount(account config.ProviderAccount, ...)` en `provider.go`
- Soporte `ProviderOpenAICompatible` en el switch de `NewProvider`
- `Model` struct gana campo `AccountID string`
- Nueva función `RefreshProviderModelsForAccount(ctx, account)` en `registry.go`
- Lógica de prefijo de model ID: si 1 cuenta del tipo → prefijo = tipo (retrocompat); si 2+ → prefijo = account.ID
- Función `DisplayLabel(allAccounts []ProviderAccount) string` en `Model`

### Fase 3: Wiring en App, Agent y selector de modelos
**Fact ID**: `multi_account_providers_phase3_wiring`

- `app.refreshDynamicModels()` itera `ProviderAccounts` en lugar de `Providers`
- Nueva función `config.ResolveProviderAccount(model)` para lookups por modelo
- Reemplazar accesos directos `cfg.Providers[model.Provider]` → `config.ResolveProviderAccount(model)`
- `handleListModels` incluye `accountId` en `ModelInfo` y usa `DisplayLabel()`
- Actualizar `handlers_models.go` para operar con cuentas

### Fase 4: REST API para gestión de cuentas de proveedor
**Fact ID**: `multi_account_providers_phase4_rest_api`

Nuevas rutas en `routes.go`:
```
GET    /api/v1/config/provider-accounts         → lista todas las cuentas
POST   /api/v1/config/provider-accounts         → crear cuenta
GET    /api/v1/config/provider-accounts/{id}    → obtener cuenta por ID
PUT    /api/v1/config/provider-accounts/{id}    → actualizar cuenta
DELETE /api/v1/config/provider-accounts/{id}    → eliminar cuenta
POST   /api/v1/config/provider-accounts/{id}/test → test conectividad
GET    /api/v1/config/provider-types            → lista tipos soportados con metadatos
```

- Nuevo archivo `internal/api/handlers_provider_accounts.go`
- Retrocompatibilidad: `GET/PUT /api/v1/config/providers` sigue funcionando (lee/escribe en ProviderAccounts)
- Test endpoint devuelve `{ok: bool, modelCount: int, error?: string}`

### Fase 5: Panel de configuración TUI
**Fact ID**: `multi_account_providers_phase5_tui`

- Nueva sección "Provider Accounts" en `internal/tui/page/settings.go`
- Nuevo dialog `internal/tui/components/dialog/provider_account_dialog.go`
  - Formulario: ID, DisplayName, Type, APIKey, BaseURL, ExtraHeaders (lista dinámica key-value), Disabled, UseOAuth
  - Acción "Test" inline desde el dialog
- Teclas en la sección: `a`/`+` añadir, `e`/`Enter` editar, `d`/`Delete` eliminar, `t` testear, `space` toggle
- Selector de modelos en chat: usa `DisplayLabel()` → "mywork: Claude Sonnet 4.5"

### Fase 6: Panel de configuración Web-UI
**Fact ID**: `multi_account_providers_phase6_webui`

- `web-ui/src/api/providerAccounts.ts` — cliente API tipado
- `web-ui/src/components/settings/ProviderAccountsSection.tsx` — tabla de cuentas
- `web-ui/src/components/settings/ProviderAccountDialog.tsx` — modal add/edit con headers dinámicos
- `web-ui/src/components/ModelSelector.tsx` — actualizar labels con cuenta
- Validación de ID (slug `/^[a-z0-9-]+$/`)
- Test de conectividad inline con indicador de carga
- Hot-reload via SSE `config_changed`

---

## Consideraciones de diseño

### Retrocompatibilidad
- Configs existentes con `providers: { anthropic: {apiKey: ...} }` se migran automáticamente al cargar
- Las cuentas migradas reciben `id = string(providerType)` (ej: "anthropic")
- Model IDs no cambian si solo hay 1 cuenta del tipo (retrocompat total)
- Los endpoints API legacy siguen funcionando mapeando sobre `ProviderAccounts`

### Esquema de Model ID con múltiples cuentas
```
# Caso 1: Solo 1 cuenta Anthropic → retrocompatible
anthropic.claude-sonnet-4-5  (igual que ahora)

# Caso 2: 2 cuentas Anthropic → nuevo prefijo por account ID
work.claude-sonnet-4-5
personal.claude-sonnet-4-5

# Caso 3: OpenAI-compatible custom
my-llm.gpt-4o   (si el endpoint reporta "gpt-4o")
```

### Tipos de proveedor soportados
| Tipo | Requiere APIKey | Requiere BaseURL | Soporta Headers custom |
|------|-----------------|------------------|------------------------|
| anthropic | Sí (o OAuth) | No | No |
| openai | Sí | No | No |
| openai-compatible | Sí/Opcional | Sí (requerido) | Sí |
| ollama | No | Opcional | No |
| copilot | OAuth | No | No |
| gemini | Sí | No | No |
| groq | Sí | No | No |
| openrouter | Sí | No | No |
| xai | Sí | No | No |
| azure | Sí | Sí | No |
| bedrock | AWS credentials | No | No |
| vertexai | GCP credentials | No | No |

---

## Ejemplo de config TOML resultante

```toml
[[providerAccounts]]
id = "anthropic"
type = "anthropic"
apiKey = "sk-ant-..."
displayName = "Anthropic (default)"

[[providerAccounts]]
id = "anthropic-work"
type = "anthropic"
apiKey = "sk-ant-work..."
displayName = "Anthropic Work"

[[providerAccounts]]
id = "local-llm"
type = "openai-compatible"
baseUrl = "http://localhost:1234/v1"
displayName = "Local LM Studio"

[[providerAccounts]]
id = "my-vllm"
type = "openai-compatible"
baseUrl = "http://my-server:8000/v1"
apiKey = "vllm-key"
displayName = "Production vLLM"

[providerAccounts.my-vllm.extraHeaders]
  "X-Custom-Header" = "value"
  "Authorization" = "Bearer special-token"
```
