# Plan de Integración de Nuevas Características en Pando

## Estado Actual del Proyecto

### Pando (github.com/digiogithub/pando)
- **Lenguaje**: Go 1.24, fork de OpenCode/Crush
- **TUI**: bubbletea (charmbracelet) + lipgloss
- **Config**: Viper, soporta `.pando.json` y `.pando.toml` (global `~/.pando.*` + local `.pando.*`)
- **Páginas TUI**: Solo 2 — ChatPage, LogsPage
- **Diálogos**: Help, Quit, Session, Command, Model, Theme, Filepicker, Init, Permissions, MultiArguments
- **Providers LLM**: Anthropic, OpenAI, Copilot, Gemini, Groq, OpenRouter, XAI, Azure, Bedrock, VertexAI
- **Herramientas**: bash, edit, file, glob, grep, fetch, ls, patch, write, view, diagnostics, sourcegraph
- **MCP**: via mcp-go (mark3labs/mcp-go)
- **DB**: SQLite (ncruces/go-sqlite3)
- **CLI**: spf13/cobra
- **NO tiene**: skills, configuración TUI, orquestador de subagentes

### Mesnada (github.com/sevir/mesnada) v4.1.1
- **Lenguaje**: Go 1.23.4
- **TUI**: bubbletea (charmbracelet) — TUI independiente con dashboard
- **Config**: YAML/JSON (`~/.mesnada/config.yaml`)
- **Servidor HTTP**: Gin + stdlib mux, endpoints MCP JSON-RPC, SSE, REST API, WebUI embebida
- **Orquestador**: Completo con spawn, cancel, pause, resume, retry, dependencias entre tareas
- **Agent Spawners**: Copilot, Claude, Gemini, OpenCode, Ollama (Claude/OpenCode), Mistral, ACP
- **Store**: File-based JSON store para persistencia de tareas
- **Personas**: Sistema de personas via markdown
- **ACP**: Agent Communication Protocol via coder/acp-go-sdk
- **Tools MCP**: spawn_agent, get_task, list_tasks, wait_task, wait_multiple, cancel_task, pause_task, resume_task, delete_task, get_stats, get_task_output, set_progress, acp_session_control

### Arquitectura Skills VTCode (referencia)
- **3 niveles de carga**: Metadata (~50 tokens), Instructions (<5K tokens), Resources (on-demand)
- **SKILL.md**: YAML frontmatter + Markdown body
- **Descubrimiento**: Múltiples rutas con precedencia (user > project)
- **Tipos**: Traditional (SKILL.md), CLI Tool (executable), Hybrid
- **Routing**: via metadata (description, when-to-use, when-not-to-use)
- **Invocación**: Implícita (model-driven), Explícita (/skill), Programática (CLI)

---

## FASE 1: Interfaz TUI de Configuración

**Objetivo**: Añadir una página de Settings en la TUI de Pando que permita visualizar y modificar la configuración.

### Tareas:

#### 1.1 — Crear PageID y estructura base
- Añadir `SettingsPage` a `internal/tui/page/page.go`
- Crear `internal/tui/page/settings.go` con el modelo bubbletea básico
- Registrar la nueva página en `internal/tui/tui.go` (mapa de pages + init)
- **Delegable a subagente**: SÍ

#### 1.2 — Crear componentes de settings
- Crear `internal/tui/components/settings/` con componentes:
  - `settings.go` — Componente principal con secciones navegables
  - `section.go` — Sección genérica con campos editables
  - `field.go` — Campo de formulario (text input, toggle, dropdown)
- Implementar navegación por secciones con Tab/Shift+Tab
- **Delegable a subagente**: SÍ

#### 1.3 — Implementar secciones de configuración
- **Sección General**: tema, autoCompact, debug, shell
- **Sección Providers**: listar providers con estado (activo/disabled), editar API keys
- **Sección Agents/Models**: editar modelo por agente (coder, summarizer, task, title)
- **Sección MCP Servers**: listar, añadir, editar, eliminar MCP servers
- **Sección LSP**: listar y editar configuraciones LSP
- **Delegable a subagente**: SÍ (subdividir por sección)

#### 1.4 — Integrar persistencia de config
- Usar `updateCfgFile()` existente para guardar cambios
- Añadir validación en tiempo real al editar
- Mostrar feedback visual (toast/status) al guardar
- **Delegable a subagente**: SÍ

#### 1.5 — Añadir navegación y keybinding
- Añadir keybinding `ctrl+g` para abrir Settings
- Añadir comando "Settings" al command dialog (ctrl+k)
- **Delegable a subagente**: SÍ

---

## FASE 2: Sistema de Skills (estilo VTCode)

**Objetivo**: Implementar un sistema de skills con carga progresiva de 3 niveles siguiendo la arquitectura de VTCode.

### Tareas:

#### 2.1 — Crear paquete core de skills
- Crear `internal/skills/types.go` — Tipos: SkillMetadata, SkillInstruction, SkillResource, Skill
- Crear `internal/skills/parser.go` — Parser de SKILL.md (YAML frontmatter + markdown)
- Crear `internal/skills/manager.go` — SkillManager con cache LRU para instrucciones
- **Delegable a subagente**: SÍ

#### 2.2 — Implementar descubrimiento de skills
- Crear `internal/skills/discovery.go` — Scanner de filesystem
- Rutas de búsqueda con precedencia:
  1. `~/.pando/skills/` (user global)
  2. `.pando/skills/` (project local)
  3. `~/.claude/skills/` (compatibilidad Claude)
  4. `.claude/skills/` (compatibilidad Claude project)
- Soporte para descubrimiento recursivo
- **Delegable a subagente**: SÍ

#### 2.3 — Implementar carga progresiva de 3 niveles
- **Nivel 1**: Cargar metadata de todas las skills al inicio (~50 tokens/skill)
- **Nivel 2**: Cargar instrucciones bajo demanda con cache LRU, eviction al 80% de contexto
- **Nivel 3**: Cargar recursos solo cuando el modelo los solicite explícitamente
- Crear `internal/skills/context.go` — Gestión de contexto y eviction
- **Delegable a subagente**: SÍ

#### 2.4 — Integrar skills en el sistema de prompts
- Modificar `internal/llm/prompt/prompt.go` para inyectar metadata de skills activas
- Modificar `internal/llm/prompt/coder.go` para incluir instrucciones de skills activadas
- Añadir campo `Skills` a la configuración (`internal/config/config.go`)
- **Delegable a subagente**: SÍ

#### 2.5 — Implementar invocación de skills
- Añadir slash command `/skills` (list, info, activate, deactivate)
- Integrar en el command dialog de la TUI
- Routing automático basado en metadata (when-to-use)
- **Delegable a subagente**: SÍ

#### 2.6 — Añadir gestión de skills a TUI Settings
- Añadir sección "Skills" en la página de Settings (Fase 1)
- Mostrar skills descubiertas con estado, nivel de carga, y controles
- Permitir activar/desactivar skills desde la TUI
- **Delegable a subagente**: SÍ

#### 2.7 — Soporte CLI Tool Skills
- Crear `internal/skills/tool_bridge.go` — Bridge para ejecutables CLI como skills
- Registrar CLI tools descubiertos en el tool registry de Pando
- **Delegable a subagente**: SÍ

---

## FASE 3: Integración de Mesnada (Orquestador de Subagentes)

**Objetivo**: Integrar la funcionalidad completa de Mesnada dentro del binario de Pando, incluyendo orquestador, servidor HTTP, spawners de agentes, y protocolo ACP.

### Tareas:

#### 3.1 — Migrar paquetes core de Mesnada a Pando
- Copiar y adaptar (cambiar module paths) los siguientes paquetes:
  - `internal/mesnada/orchestrator/` ← mesnada/internal/orchestrator/
  - `internal/mesnada/agent/` ← mesnada/internal/agent/ (todos los spawners)
  - `internal/mesnada/store/` ← mesnada/internal/store/
  - `internal/mesnada/persona/` ← mesnada/internal/persona/
  - `internal/mesnada/acp/` ← mesnada/internal/acp/
  - `pkg/mesnada/models/` ← mesnada/pkg/models/
- Adaptar imports a `github.com/digiogithub/pando/internal/mesnada/...`
- **Delegable a subagente**: SÍ (puede ser tarea mecánica)

#### 3.2 — Migrar servidor HTTP/MCP
- Copiar y adaptar:
  - `internal/mesnada/server/` ← mesnada/internal/server/ (server.go, tools.go, api.go, gin.go, ui.go)
  - `internal/mesnada/mcpconv/` ← mesnada/internal/mcpconv/
- Mantener endpoints: /mcp, /mcp/sse, /health, /api/*, /ui
- **Delegable a subagente**: SÍ

#### 3.3 — Añadir configuración de Mesnada al .toml de Pando
- Extender `internal/config/config.go` con nuevas secciones:
```toml
[mesnada]
enabled = true

[mesnada.server]
host = "127.0.0.1"
port = 9767

[mesnada.orchestrator]
store_path = "~/.pando/mesnada/tasks.json"
log_dir = "~/.pando/mesnada/logs"
max_parallel = 5
default_engine = "copilot"
default_mcp_config = ""
persona_path = "~/.pando/mesnada/personas"

[mesnada.tui]
enabled = true
webui = true

[mesnada.acp]
enabled = false

[mesnada.engines.copilot]
default_model = "gpt-4.1"
[[mesnada.engines.copilot.models]]
id = "gpt-4.1"
description = "GPT-4.1 via GitHub Copilot"
```
- **Delegable a subagente**: SÍ

#### 3.4 — Integrar inicio del servidor HTTP en Pando
- Modificar `internal/app/app.go` para inicializar el orquestador de Mesnada
- Levantar servidor HTTP en goroutine de background cuando `mesnada.enabled = true`
- Registrar shutdown en el ciclo de vida de la app
- Exponer el orquestador al resto de componentes de Pando
- **Delegable a subagente**: SÍ

#### 3.5 — Crear página TUI de Orchestrator Dashboard
- Crear `internal/tui/page/orchestrator.go` — Nueva página con dashboard de tareas
- Componentes:
  - Lista de tareas con estado, progreso, engine, modelo
  - Panel de detalle de tarea seleccionada
  - Acciones: spawn, cancel, pause, resume, delete
- Añadir keybinding `ctrl+m` para acceder al dashboard
- **Delegable a subagente**: SÍ

#### 3.6 — Registrar tools de Mesnada en el registry LLM de Pando
- Crear `internal/llm/tools/mesnada.go` con herramientas:
  - spawn_agent, get_task, list_tasks, wait_task, cancel_task, etc.
- Registrar en `internal/llm/tools/tools.go`
- Permitir que el agente coder de Pando orqueste subagentes directamente
- **Delegable a subagente**: SÍ

#### 3.7 — Añadir dependencias Go necesarias
- Añadir a go.mod:
  - `github.com/gin-gonic/gin` (HTTP framework para API/WebUI)
  - `github.com/coder/acp-go-sdk` (ACP protocol)
  - `gopkg.in/yaml.v2` (ya lo tiene viper indirectamente)
- Resolver conflictos de versiones con dependencias existentes
- **Delegable a subagente**: SÍ

#### 3.8 — Integrar WebUI embebida
- Copiar `ui/` (assets web embebidos) de mesnada
- Adaptar embed.go para incluir en el binario de Pando
- **Delegable a subagente**: SÍ

#### 3.9 — Añadir configuración de Mesnada a la TUI Settings (Fase 1)
- Sección "Mesnada/Orchestrator" en Settings
- Configurar: host, port, max_parallel, default_engine, personas
- Gestionar engines y modelos disponibles
- Gestionar agentes ACP
- **Delegable a subagente**: SÍ

---

## Orden de Ejecución Recomendado

```
FASE 1 (TUI Config) ──────────────────────────────────────────→
FASE 2 (Skills)      ─────────────────────────→ (depende 2.6 de Fase 1)
FASE 3 (Mesnada)     ────────────────────────────────────────→ (depende 3.5, 3.9 de Fase 1)
```

- **Fase 1 y Fase 2 (2.1-2.5)** pueden ejecutarse en paralelo
- **Fase 2.6** depende de que Fase 1 esté completa (sección skills en settings)
- **Fase 3.5 y 3.9** dependen de Fase 1 (páginas TUI y settings)
- **Fase 3.1-3.4** son independientes y pueden empezar en paralelo con Fases 1 y 2

## Subdivisión para Subagentes Mesnada

Cada tarea marcada como "Delegable a subagente: SÍ" puede ser asignada a un subagente de mesnada con:
- **Engine**: copilot
- **Modelo**: gpt-4.1 (o gpt-4.6 si disponible)
- **WorkDir**: /www/MCP/Pando/pando
- **MCP Config**: Acceso a tools de búsqueda y edición

### Agrupación sugerida de tareas paralelas:

**Batch 1 (paralelo)**:
- Subagente A: Fase 1.1 + 1.2 (estructura base settings)
- Subagente B: Fase 2.1 + 2.2 (core skills + discovery)
- Subagente C: Fase 3.1 (migración de paquetes mesnada)

**Batch 2 (tras Batch 1)**:
- Subagente D: Fase 1.3 (secciones de config)
- Subagente E: Fase 2.3 + 2.4 (carga progresiva + prompts)
- Subagente F: Fase 3.2 + 3.3 (servidor HTTP + config)

**Batch 3 (tras Batch 2)**:
- Subagente G: Fase 1.4 + 1.5 (persistencia + keybindings)
- Subagente H: Fase 2.5 + 2.6 + 2.7 (invocación + TUI + CLI tools)
- Subagente I: Fase 3.4 + 3.7 (integración app + dependencias)

**Batch 4 (final)**:
- Subagente J: Fase 3.5 (dashboard TUI orchestrator)
- Subagente K: Fase 3.6 (tools LLM mesnada)
- Subagente L: Fase 3.8 + 3.9 (WebUI + settings mesnada)
