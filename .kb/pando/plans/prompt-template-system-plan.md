# Plan de Implementación: Sistema de Templates de Prompts para Pando

## Resumen Ejecutivo

Rediseño completo del sistema de generación de system prompts de Pando, migrando de prompts hardcoded en constantes Go a un sistema modular de templates (.md.tpl) con composición por capas, secciones condicionales basadas en capabilities disponibles, optimización por proveedor de LLM, y hookabilidad completa vía Lua scripts.

## Análisis de Referencia

### Fuentes analizadas:
1. **Crush** (Go) — Template-based con PromptDat struct, coder.md.tpl/task.md.tpl, MCP instructions injection, skills XML, context files loop. Lazy async building.
2. **OpenCode** (TypeScript) — Layered composition (provider → environment → skills → instructions), provider-specific prompts por familia de modelo, múltiples modos (build/plan/explore/compaction), plugin hooks para transformación.
3. **Claude Code** — Hierarchical composition (Identity → Responsibilities → Process → Quality → Output → Edge Cases), frontmatter metadata, example-driven triggering, hook event types para lifecycle, agent archetypes.
4. **Pando actual** — Multi-agent (coder/task/title/summarizer), provider-specific (Anthropic vs OpenAI), Lua hooks (6 tipos), skills injection, MCP tool filtering, persona system para sub-agentes.

### Mejores características seleccionadas:
- **De Crush**: Ficheros .md.tpl separados, PromptDat struct, async building, template engine Go
- **De OpenCode**: Composición por capas, provider-specific prompts, múltiples modos (plan/explore), capability awareness
- **De Claude Code**: Estructura jerárquica de secciones, edge case handling, quality standards, prompt validation
- **De Pando**: Sistema Lua existente, hooks de lifecycle, MCP filtering, persona system

## Arquitectura Final

```
internal/llm/prompt/
├── templates/                      # Templates embebidos (embed.FS)
│   ├── base/                       # Secciones reutilizables
│   │   ├── identity.md.tpl
│   │   ├── environment.md.tpl
│   │   ├── conventions.md.tpl
│   │   ├── workflow.md.tpl
│   │   ├── tone.md.tpl
│   │   └── tools_policy.md.tpl
│   ├── agents/                     # Template por tipo de agente
│   │   ├── coder.md.tpl
│   │   ├── task.md.tpl
│   │   ├── planner.md.tpl         # NUEVO
│   │   ├── explorer.md.tpl        # NUEVO
│   │   ├── title.md.tpl
│   │   └── summarizer.md.tpl
│   ├── providers/                  # Optimizaciones por proveedor
│   │   ├── anthropic.md.tpl
│   │   ├── openai.md.tpl
│   │   ├── gemini.md.tpl
│   │   └── ollama.md.tpl
│   ├── capabilities/               # Secciones condicionales
│   │   ├── remembrances.md.tpl
│   │   ├── orchestration.md.tpl
│   │   ├── web_search.md.tpl
│   │   ├── code_indexing.md.tpl
│   │   └── lsp.md.tpl
│   └── context/                    # Contexto dinámico
│       ├── git.md.tpl
│       ├── project.md.tpl
│       ├── skills.md.tpl
│       └── mcp_instructions.md.tpl
├── data.go                         # PromptData struct
├── registry.go                     # Template loading, caching, override
├── builder.go                      # Composition pipeline + CapabilityDetector
├── prompt.go                       # Entry point (refactored)
├── builder_test.go                 # Unit tests
└── integration_test.go             # Integration tests con Lua

# Nuevos hooks Lua:
luaengine/types.go  →  HookTemplateSection, HookCapabilityCheck, HookProviderSelect, HookPromptCompose
luaengine/functions.go → pando_get_config, pando_get_git_status, pando_list_mcp_servers, pando_list_tools, pando_render_template, pando_load_file
```

## Pipeline de Composición

```
1. Load identity template (base/identity.md.tpl)
2. Select provider template → hook_provider_select → render
3. Select agent template (agents/{name}.md.tpl) → render
4. Render environment section (base/environment.md.tpl)
5. Detect capabilities → hook_capability_check per capability → render matching templates
6. Render context sections (git, project files, skills, MCP instructions)
7. Apply hook_template_section to EACH section
8. Apply hook_prompt_compose to reorder/add/remove sections
9. Join all sections
10. Apply hook_system_prompt (backward compatible)
11. Return final prompt
```

## Fases de Implementación

### Fase 1: Infraestructura de Templates y PromptBuilder
**Fact ID**: prompt_system_plan_phase1
**Alcance**: PromptData struct, TemplateRegistry, PromptBuilder, refactor de GetAgentPrompt
**Dependencias**: Ninguna
**Prioridad**: CRÍTICA — base para todo lo demás

### Fase 2: Templates de Agentes y Secciones Base
**Fact ID**: prompt_system_plan_phase2
**Alcance**: Migración de prompts hardcoded a .md.tpl, nuevos agentes planner/explorer
**Dependencias**: Fase 1
**Prioridad**: ALTA — funcionalidad core

### Fase 3: Templates por Proveedor
**Fact ID**: prompt_system_plan_phase3
**Alcance**: Templates específicos para Anthropic, OpenAI, Gemini, Ollama
**Dependencias**: Fase 2
**Prioridad**: MEDIA — optimización

### Fase 4: Templates de Capabilities Condicionales
**Fact ID**: prompt_system_plan_phase4
**Alcance**: CapabilityDetector, templates para remembrances/mesnada/web/code/lsp
**Dependencias**: Fase 1
**Prioridad**: ALTA — diferenciador clave de Pando

### Fase 5: Integración Avanzada de Lua Hooks
**Fact ID**: prompt_system_plan_phase5
**Alcance**: Nuevos hooks (template_section, capability_check, prompt_compose, provider_select), funciones pando_*
**Dependencias**: Fases 1, 4
**Prioridad**: MEDIA — extensibilidad

### Fase 6: Testing, Documentación y Templates de Contexto
**Fact ID**: prompt_system_plan_phase6
**Alcance**: Templates de contexto, test suite, documentación, ejemplos
**Dependencias**: Fases 1-5
**Prioridad**: ALTA — calidad y mantenibilidad

## Orden de Ejecución Recomendado

```
Fase 1 ─────┬──→ Fase 2 ──→ Fase 3
             │
             └──→ Fase 4 ──→ Fase 5
                                │
                    Fase 6 ←────┘
```

Las fases 2-3 y 4-5 pueden ejecutarse en paralelo tras completar la Fase 1.

## Principios de Diseño

1. **Backward Compatible**: Los prompts generados deben ser equivalentes a los actuales hasta que se opte por las nuevas features
2. **Opt-in Complexity**: Capabilities, providers y hooks solo se activan cuando están disponibles
3. **Override Pattern**: Templates externos sobrescriben embebidos; Lua hooks pueden modificar cualquier sección
4. **Minimal Token Usage**: Templates de Ollama significativamente más cortos; capabilities solo se incluyen si están activas
5. **Testable**: Cada componente con tests unitarios; integration tests para la cadena completa
6. **Composable**: Secciones independientes que se componen en pipeline

## Fecha del plan: 2026-03-16
## Estado: APROBADO PENDIENTE DE IMPLEMENTACIÓN