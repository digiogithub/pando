# Plan de Integración Pando v2 — 4 Fases, 25 Tareas

## Info del Proyecto
- **Módulo Go**: `github.com/digiogithub/pando`
- **Go version**: 1.24.0
- **Working dir**: /www/MCP/Pando/pando
- **Mesnada source**: /www/MCP/mesnada (github.com/sevir/mesnada)

## Índice de Tareas (fact IDs)

| ID | Fase | Tarea | Dependencias | Fact Key |
|----|------|-------|-------------|----------|
| T1.1 | 1 | Crear PageID Settings y estructura base | — | task-1.1 |
| T1.2 | 1 | Crear componentes de settings | T1.1 | task-1.2 |
| T1.3 | 1 | Implementar secciones de configuración | T1.2 | task-1.3 |
| T1.4 | 1 | Integrar persistencia de config | T1.3 | task-1.4 |
| T1.5 | 1 | Añadir navegación y keybinding | T1.1 | task-1.5 |
| T2.1 | 2 | Crear paquete core de skills | — | task-2.1 |
| T2.2 | 2 | Implementar descubrimiento de skills | T2.1 | task-2.2 |
| T2.3 | 2 | Implementar carga progresiva 3 niveles | T2.1 | task-2.3 |
| T2.4 | 2 | Integrar skills en sistema de prompts | T2.1, T2.3 | task-2.4 |
| T2.5 | 2 | Implementar invocación de skills | T2.1, T2.4 | task-2.5 |
| T2.6 | 2 | Añadir gestión de skills a TUI Settings | T2.5, T1.3 | task-2.6 |
| T2.7 | 2 | Soporte CLI Tool Skills | T2.1 | task-2.7 |
| T3.1 | 3 | Migrar paquetes core de Mesnada | — | task-3.1 |
| T3.2 | 3 | Migrar servidor HTTP/MCP | T3.1 | task-3.2 |
| T3.3 | 3 | Añadir config de Mesnada al .toml | T3.1 | task-3.3 |
| T3.4 | 3 | Integrar inicio servidor HTTP en app | T3.1, T3.2, T3.3 | task-3.4 |
| T3.5 | 3 | Crear página TUI Orchestrator Dashboard | T3.4, T1.1 | task-3.5 |
| T3.6 | 3 | Registrar tools Mesnada en LLM registry | T3.1, T3.4 | task-3.6 |
| T3.7 | 3 | Añadir dependencias Go necesarias | T3.1 | task-3.7 |
| T3.8 | 3 | Integrar WebUI embebida | T3.2 | task-3.8 |
| T3.9 | 3 | Añadir config Mesnada a TUI Settings | T3.4, T1.3 | task-3.9 |
| T4.1 | 4 | Añadir --yolo/--allow-all-tools flag | — | task-4.1 |
| T4.2 | 4 | Añadir soporte stdin para prompt | — | task-4.2 |
| T4.3 | 4 | Añadir soporte PANDO_PROMPT env var | — | task-4.3 |
| T4.4 | 4 | Auto-approve global para MCP tools | T4.1 | task-4.4 |

---

## FASE 1: Interfaz TUI de Configuración

### T1.1 — Crear PageID Settings y estructura base
- Añadir `SettingsPage PageID = "settings"` a `internal/tui/page/page.go`
- Crear `internal/tui/page/settings.go` con modelo bubbletea que implementa `tea.Model`
- Registrar la nueva página en `internal/tui/tui.go` en el mapa de pages
- Inicializar la página en `New()` function

### T1.2 — Crear componentes de settings
- Crear directorio `internal/tui/components/settings/`
- `settings.go` — Componente principal con lista de secciones navegables (sidebar + content)
- `section.go` — Sección genérica con lista de campos
- `field.go` — Tipos de campo: TextInput, Toggle (bool), Select (dropdown)
- Implementar navegación Tab/Shift+Tab entre secciones, Up/Down entre campos, Enter para editar

### T1.3 — Implementar secciones de configuración
- **General**: tema (select), autoCompact (toggle), debug (toggle), shell.path (text), shell.args (text)
- **Providers**: lista de providers con estado activo/disabled, editar API keys (text con mask)
- **Agents/Models**: selector de modelo por agente (coder, summarizer, task, title)
- **MCP Servers**: tabla CRUD de MCP servers (name, command, args, type, url)
- **LSP**: lista de configuraciones LSP (language, command, args, enabled)

### T1.4 — Integrar persistencia de config
- Usar `config.updateCfgFile()` existente para guardar cambios al presionar Save
- Validación en tiempo real: verificar API keys no vacías, modelos válidos
- Feedback visual via `util.ReportInfo/ReportError` en la status bar
- Hot-reload de config tras guardar (re-read viper)

### T1.5 — Añadir navegación y keybinding
- Añadir `Settings key.Binding` con `ctrl+g` al keyMap en tui.go
- Registrar comando "Settings" en el command dialog (ctrl+k) con handler
- Manejar navegación Settings→Chat con Esc

---

## FASE 2: Sistema de Skills (estilo VTCode)

### T2.1 — Crear paquete core de skills
- Crear `internal/skills/` package
- `types.go`: SkillMetadata (name, description, version, author, when-to-use, when-not-to-use, allowed-tools, user-invocable), SkillInstruction (markdown body), SkillResource (path, content), Skill (metadata + state)
- `parser.go`: Parser de SKILL.md — extraer YAML frontmatter entre `---`, body como markdown
- `manager.go`: SkillManager con: LoadAll(), GetMetadata(), GetInstructions(), EvictLRU(), Recall()
- Cache LRU para instrucciones con capacidad configurable

### T2.2 — Implementar descubrimiento de skills
- `discovery.go`: Scanner de filesystem con rutas de precedencia:
  1. `~/.pando/skills/` (user global)
  2. `.pando/skills/` (project local)
  3. `~/.claude/skills/` (compatibilidad)
  4. `.claude/skills/` (compatibilidad project)
- Buscar `**/SKILL.md` recursivamente en cada ruta
- Skills de rutas superiores override same-named de inferiores
- Devolver lista ordenada por precedencia

### T2.3 — Implementar carga progresiva 3 niveles
- `context.go`: ContextManager con tracking de tokens usados
- Nivel 1: metadata siempre cargada (~50 tokens/skill) — en `SkillManager.LoadAll()`
- Nivel 2: instrucciones bajo demanda — `SkillManager.GetInstructions(name)` con cache LRU
- Nivel 3: recursos solo cuando solicitados — `SkillManager.GetResource(name, path)`
- Eviction al 80% de ventana de contexto configurable
- `EstimateTokens(text)` helper para estimar tokens (~4 chars/token)

### T2.4 — Integrar skills en sistema de prompts
- Modificar `internal/llm/prompt/prompt.go` para aceptar `SkillManager`
- Inyectar metadata de skills activas en system prompt (Nivel 1)
- Inyectar instrucciones de skill activada en user context (Nivel 2)
- Añadir campo `Skills SkillsConfig` a `config.Config` con `paths []string`, `enabled bool`
- Añadir sección `[skills]` al schema .toml

### T2.5 — Implementar invocación de skills
- Crear slash command handler para `/skills` en `internal/tui/components/dialog/commands.go`
- Subcomandos: `/skills list`, `/skills info <name>`, `/skills activate <name>`, `/skills deactivate <name>`
- Routing automático: en `agent.go`, antes de enviar prompt, evaluar metadata `when-to-use` contra prompt
- Activar skills que coincidan automáticamente

### T2.6 — Añadir gestión de skills a TUI Settings
- Nueva sección "Skills" en la página de Settings (depende de T1.3)
- Mostrar skills descubiertas: nombre, versión, estado (loaded/unloaded), nivel de carga
- Toggle para activar/desactivar skills
- Vista previa de metadata/instrucciones

### T2.7 — Soporte CLI Tool Skills
- `tool_bridge.go`: Detectar directorios con ejecutable `tool` + `README.md`
- Crear BaseTool wrapper que ejecuta el CLI tool
- Registrar CLI tools descubiertos en el tool registry via `CoderAgentTools()`
- Leer `schema.json` si existe para validar argumentos

---

## FASE 3: Integración de Mesnada (Orquestador de Subagentes)

### T3.1 — Migrar paquetes core de Mesnada
- Crear estructura `internal/mesnada/` en Pando
- Copiar y adaptar (rewrite imports a `github.com/digiogithub/pando/internal/mesnada/...`):
  - `internal/mesnada/orchestrator/` ← /www/MCP/mesnada/internal/orchestrator/
  - `internal/mesnada/agent/` ← /www/MCP/mesnada/internal/agent/ (todos los spawners)
  - `internal/mesnada/store/` ← /www/MCP/mesnada/internal/store/
  - `internal/mesnada/persona/` ← /www/MCP/mesnada/internal/persona/
  - `internal/mesnada/acp/` ← /www/MCP/mesnada/internal/acp/
  - `internal/mesnada/mcpconv/` ← /www/MCP/mesnada/internal/mcpconv/
  - `pkg/mesnada/models/` ← /www/MCP/mesnada/pkg/models/
- Adaptar config imports internos

### T3.2 — Migrar servidor HTTP/MCP
- Copiar y adaptar:
  - `internal/mesnada/server/` ← /www/MCP/mesnada/internal/server/ (server.go, tools.go, api.go, gin.go, ui.go)
- Mantener endpoints: /mcp, /mcp/sse, /health, /api/*, /ui
- Adaptar imports de config y orchestrator al nuevo path

### T3.3 — Añadir config de Mesnada al .toml
- Extender `internal/config/config.go` con structs:
  - `MesnadaConfig` con campos: Enabled, Server (host,port), Orchestrator (store_path, log_dir, max_parallel, default_engine, default_mcp_config, persona_path), TUI (enabled, webui), ACP (enabled, default_agent, agents map), Engines map
- Añadir campo `Mesnada MesnadaConfig` a `Config` struct
- Registrar defaults en viper: `mesnada.enabled=false`, `mesnada.server.host=127.0.0.1`, `mesnada.server.port=9767`
- Actualizar `pando-schema.json` con nuevas propiedades

### T3.4 — Integrar inicio servidor HTTP en app
- Modificar `internal/app/app.go`:
  - Añadir campo `Orchestrator` y `MesnadaServer` a App struct
  - En `New()`: si `config.Get().Mesnada.Enabled`, crear orchestrator y server
  - Levantar HTTP server en goroutine de background
  - Registrar shutdown en `App.Shutdown()`
- Exponer orchestrator para uso por otros componentes

### T3.5 — Crear página TUI Orchestrator Dashboard
- Crear `internal/tui/page/orchestrator.go` — nueva página PageID "orchestrator"
- Layout: tabla de tareas (ID, status, engine, model, progress) + panel de detalle
- Acciones con keybindings: (s)pawn, (c)ancel, (p)ause, (r)esume, (d)elete, (l)og
- Input dialog para spawn (prompt, engine, model, work_dir)
- Refresh automático cada 2s para tareas running

### T3.6 — Registrar tools Mesnada en LLM registry
- Crear `internal/llm/tools/mesnada.go` con herramientas adaptadas como BaseTool:
  - `mesnada_spawn_agent`, `mesnada_get_task`, `mesnada_list_tasks`, `mesnada_wait_task`, `mesnada_cancel_task`, `mesnada_get_task_output`, `mesnada_set_progress`
- Cada tool llama directamente al orchestrator (no via HTTP)
- Registrar en `internal/llm/agent/tools.go` dentro de `CoderAgentTools()`
- Solo registrar si mesnada.enabled=true en config

### T3.7 — Añadir dependencias Go necesarias
- `go get github.com/gin-gonic/gin`
- `go get github.com/coder/acp-go-sdk`
- `go get gopkg.in/yaml.v2`
- Resolver conflictos de versiones con dependencias existentes
- Ejecutar `go mod tidy`

### T3.8 — Integrar WebUI embebida
- Copiar `ui/` directory de /www/MCP/mesnada/ui/ a pando
- Crear `internal/mesnada/ui/embed.go` con `//go:embed`
- Conectar en el servidor HTTP para servir assets estáticos en /ui

### T3.9 — Añadir config Mesnada a TUI Settings
- Nueva sección "Mesnada" en la página Settings
- Campos: enabled (toggle), host (text), port (number), max_parallel (number), default_engine (select), persona_path (text)
- Subsección ACP: enabled (toggle), default_agent (select), auto_permission (toggle)

---

## FASE 4: Modo No-Interactivo Mejorado

### T4.1 — Añadir --yolo/--allow-all-tools flag
- En `cmd/root.go init()`: añadir flag `--yolo` (alias `--allow-all-tools`) tipo bool
- Ambos flags son aliases del mismo behavior
- Cuando activo: marcar una variable global/contexto `AllToolsAutoApproved`
- Pasar este flag al App para que lo propague
- En modo no-interactivo (`-p`): `--yolo` auto-aprueba TODO incluyendo MCP tools
- En modo interactivo: `--yolo` también auto-aprueba (útil para scripts con TUI)

### T4.2 — Añadir soporte stdin para prompt
- En `cmd/root.go RunE`: si `prompt == ""` y stdin NO es terminal (piped), leer stdin completo como prompt
- Detectar terminal con `os.Stdin.Stat()` comprobando `ModeCharDevice`
- Leer con `io.ReadAll(os.Stdin)` y trim whitespace
- Si stdin está vacío y no hay `-p`, lanzar modo interactivo normal
- Ejemplo: `echo "Explain Go context" | pando` o `cat prompt.txt | pando`

### T4.3 — Añadir soporte PANDO_PROMPT env var
- En `cmd/root.go RunE`: si `prompt == ""` (ni flag ni stdin), comprobar `os.Getenv("PANDO_PROMPT")`
- Prioridad: `-p` flag > stdin piped > `PANDO_PROMPT` env var > modo interactivo
- Documentar en help text del comando

### T4.4 — Auto-approve global para MCP tools con --yolo
- Modificar `internal/permission/permission.go`:
  - Añadir campo `globalAutoApprove bool` a `permissionService`
  - Añadir método `SetGlobalAutoApprove(bool)` a `Service` interface
  - En `Request()`: si `globalAutoApprove == true`, retornar `true` inmediatamente (antes de cualquier check)
- Modificar `internal/app/app.go`:
  - En `RunNonInteractive()`: si yolo flag activo, llamar `a.Permissions.SetGlobalAutoApprove(true)`
  - Esto cubre TODAS las tools incluyendo MCP, bash, write, edit, etc.
- Modificar `cmd/root.go`: pasar yolo flag a `app.RunNonInteractive()` o via config

---

## Grafo de Dependencias

```
T1.1 ──→ T1.2 ──→ T1.3 ──→ T1.4
  └────→ T1.5       └──────→ T2.6
                     └──────→ T3.9

T2.1 ──→ T2.2
  ├────→ T2.3 ──→ T2.4 ──→ T2.5 ──→ T2.6
  └────→ T2.7

T3.1 ──→ T3.2 ──→ T3.8
  ├────→ T3.3
  ├────→ T3.7
  └──(T3.1+T3.2+T3.3)──→ T3.4 ──→ T3.5
                            ├────→ T3.6
                            └────→ T3.9

T4.1 ──→ T4.4
T4.2 (independiente)
T4.3 (independiente)
```

## Tareas Paralelas Inmediatas (sin dependencias)
T1.1, T2.1, T2.7, T3.1, T3.7, T4.1, T4.2, T4.3
