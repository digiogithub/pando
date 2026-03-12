# Plan de Implementación: Pando MCP Gateway & Lua Hooks

Este plan detalla la implementación de la funcionalidad de MCP Gateway (basada en el proyecto `panorganon`) de forma nativa en `pando`. Además, incorpora extensibilidad mediante hooks en Lua usando la biblioteca `gopherlua` y añade un sistema de estadísticas para auto-exponer las tools de uso frecuente.

## Objetivos Generales
- Integrar la lógica de MCP Gateway de `panorganon` dentro de `pando`, de modo que todos los servidores MCP configurados pasen por este proxy antes de llegar al modelo.
- Sustituir la exposición directa de todas las tools por un catálogo. Las tools son accedidas inicialmente a través de tools proxy como `query_catalog` y `call_tool`.
- Medir la recencia y la frecuencia de visualización/uso de las tools (tabla SQLite `mcp_tool_usage_stats`). Las tools más frecuentemente usadas se considerarán "favoritas" y serán expuestas explícitamente y directamente al modelo (bypass del proxy). Las que dejen de usarse se eliminarán de esta exposición directa.
- Inyectar el modelo de `luafilters` en el ciclo de vida del agente. Particularmente antes y después de interactuar con las tools (inputs/outputs).
- Incluir hooks Lua para varios eventos de etapa temprana:
  - Leer y modificar el *System Prompt*.
  - Al restaurar o al iniciar una sesión.
  - Al iniciar una conversación o cuando el usuario establece un nuevo prompt.
  - Al finalizar la generación/respuesta del agente.
- Los módulos Lua creados en cada hook tendrán total la información de origen, para así mutarla o tomar acciones side-effect.

## Fases de Implementación y Detalle (Facts en Remembrances)

- **Fase 1: Core MCP Gateway e Infraestructura Lua**
  - Integra la base de gopherlua.
  - ID en remembrances de la fact (Key): `pando_mcp_gateway_phase1`

- **Fase 2: Registro de Tools y Estadísticas (Favoritas)**
  - Implementa estadísticas de tools y exposición tipo "catálogo" con la exposición directa selectiva sólo para las top N tools.
  - ID en remembrances de la fact (Key): `pando_mcp_gateway_phase2`

- **Fase 3: Hooks Lua en el Flujo de Pando**
  - Extender los callbacks en el pipeline interno (system prompt, session start, conversation start, user prompt, agent response finishing).
  - ID en remembrances de la fact (Key): `pando_mcp_gateway_phase3`

- **Fase 4: Integración final con Mesnada y UI**
  - Explotación de los logs, métricas y hooks dentro de la interfaz. Finalización del flujo MCP y subagentes.
  - ID en remembrances de la fact (Key): `pando_mcp_gateway_phase4`