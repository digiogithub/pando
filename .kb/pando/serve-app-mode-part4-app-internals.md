# Análisis de la Implementación del Servidor Pando — Parte 4: App Interna y Servicios

## 6. App — Núcleo de la Aplicación

### `internal/app/app.go`
- **Fichero:** `/www/MCP/Pando/pando/internal/app/app.go` (~1500 líneas)
- **Estructura `App`:**
```go
type App struct {
    Sessions    session.Service      // Servicio de sesiones
    Messages    message.Service      // Servicio de mensajes
    History     history.Service      // Servicio de historial de archivos
    Permissions permission.Service   // Servicio de permisos (auto-aprobación)
    CoderAgent  agent.Service        // Agente LLM principal

    Projects            *project.Service       // Gestión de proyectos
    ProjectManager      *project.Manager       // Manager de proyectos (ciclo de vida)
    Snapshots           *snapshot.Service      // Snapshots de sesiones
    LSPClients          map[string]*lsp.Client // Clientes LSP por lenguaje
    SkillManager        *skills.SkillManager   // Gestor de skills
    MesnadaOrchestrator *mesnadaOrch.Orchestrator // Orquestador Mesnada
    CronService         *cronjob.Service       // Servicio de cron jobs
    MesnadaServer       *mesnadaServer.Server  // Servidor HTTP del orquestador
    Remembrances        *rag.RemembrancesService // Servicio RAG/KB/code indexing
    LuaManager          *luaengine.FilterManager // Manager de filtros Lua
    MCPGateway          *mcpgateway.Gateway    // Gateway de servidores MCP con favoritos
    Evaluator           *evaluator.EvaluatorService // Auto-mejora (self-improvement)

    IPCBus       *ipc.Bus    // Bus ZMQ (solo primaria)
    IPCIsPrimary bool        // Es instancia primaria?
}
```

### Inicialización en `New()` (proceso secuencial):

1. **Servicios base:** Sessions, Messages, History, Projects, Permissions
2. **ProjectManager:** Manager de proyectos con capacidades de ciclo de vida
3. **Auto-registro global:** Registra el CWD como proyecto global para descubrimiento entre instancias
4. **Theme:** Inicializa tema según configuración
5. **Skills:** Si está habilitado, descubre y carga skills de los paths configurados
6. **LSP Clients:** Inicializa clientes LSP en background (omitido en modo ACP)
7. **Modelos dinámicos:** Refresca modelos de proveedores configurados
8. **OpenLit:** Inicializa OpenLit para observabilidad (tracing distribuido)
9. **Remembrances (RAG):**
   - Servicio de búsqueda semántica (KB, code indexing, eventos)
   - Sincroniza documentos KB desde disco
   - Indexa sesiones automáticamente
   - **Context Enricher:** Busca contexto relevante antes de cada prompt de usuario
10. **Lua Filter Manager:** Filtros Lua para personalizar comportamiento del agente (si está habilitado)
11. **Snapshots:** Servicio de snapshots (auto-cleanup configurable)
12. **Evaluator:** Sistema de auto-mejora con selección UCB de templates y skills
13. **Browser Registry:** Inicializa registro de navegadores para web browsing
14. **MCP Gateway:** Gateway que gestiona servidores MCP con sistema de favoritos por uso
15. **Mesnada Orchestrator:** Orquestador de sub-agentes (si está habilitado) con:
    - Servicio de cron jobs
    - Servidor ACP (Agent Communication Protocol) HTTP opcional
    - Servidor HTTP embebido del orquestador
16. **CoderAgent:** Agente LLM principal con todas las herramientas configuradas
17. **Persona Manager:** Gestor de personas (built-in + definidas por usuario)
18. **Persona Selector:** Selección automática de persona

### `Shutdown()` — Apagado ordenado:
1. MesnadaServer → CronService → MesnadaOrchestrator
2. Snapshot cleanup → Browser sessions → Watchers → LSP clients
3. OpenLit shutdown → Project manager → IPC bus

### `SetupIPC(bus)`:
Configura el bus ZMQ como publisher IPC para el servicio de sesiones (`session.SetIPCPublisher(bus)`), permitiendo que eventos de sesión se transmitan a otras instancias.

---

## 7. Mecanismo de Streaming SSE en Chat

El flujo de chat en streaming funciona así:

1. **POST /api/v1/chat/stream** recibe `{sessionId, prompt}`
2. Crea/obtiene sesión con `getOrCreateSession()`
3. Establece headers SSE (`text/event-stream`)
4. Envía evento `session` con `{sessionId, running:true}`
5. **Submit** al `BackgroundSessionManager` que ejecuta el agente en un goroutine con `context.Background()` (independiente del HTTP)
6. **Subscribe** al canal de eventos de la sesión
7. `streamSessionEvents()` itera eventos del canal:
   - `contentDelta` → SSE evento `content` con el delta de texto
   - `toolCall` → SSE evento `tool_call` con nombre y parámetros
   - `toolResult` → SSE evento `tool_result` con resultado/error
   - `response` → SSE evento `done` (finalización)
   - `error` → SSE evento `error`
   - Manejo especial de llamadas a herramientas con entradas pendientes (tool-use con confirmación)

8. Si el cliente se desconecta, el agente sigue ejecutándose en background
9. **Reconexión:** `GET /api/v1/sessions/{id}/stream` permite reconectarse y recibir replay de eventos bufferizados

---

## 8. Relación entre Modos y Funcionalidades

| Característica | TUI (primaria) | TUI (secundaria) | Serve | App | ACP |
|---|---|---|---|---|---|
| **Servidor HTTP REST** | ✗ | ✗ | ✓ | ✓ + WebUI | ✗ |
| **Bus ZMQ (PUB/ROUTER)** | ✓ (primaria) | ✗ | ✓ | ✓ | ✓ |
| **Bridge eventos → ZMQ** | ✓ | ✗ | ✓ | ✓ | ✓ |
| **DBProxy writes** | ✗ | ✓ | ✗ | ✗ | ✗ |
| **Gestión de sesiones** | ✓ | ✓ (lectura local, escritura proxy) | ✓ | ✓ | ✓ |
| **Lock primario** | ✓ | ✗ | ✗ | ✗ | ✗ |
| **Instance Registry** | ✓ | ✓ | ✓ | ✓ | ✓ |
| **Context Enricher** | ✓ | ✓ | ✓ | ✓ | ✓ |
| **Auto-apertura navegador** | ✗ | ✗ | ✗ | ✓ | ✗ |

---

## 9. Resumen de Ficheros Clave

| Fichero | Propósito |
|---------|-----------|
| `cmd/serve.go` | Comando `pando serve` (servidor HTTP sin WebUI) |
| `cmd/app_command.go` | Comando `pando app` (servidor HTTP con WebUI) |
| `cmd/app.go` | Lógica compartida `runAppMode()` |
| `cmd/root.go` | Modo TUI (interactivo, con IPC completo y lock) |
| `internal/api/server.go` | Servidor HTTP: Server struct, NewServer, middleware, HTTP server config |
| `internal/api/routes.go` | Registro de todas las rutas API (+ utilidades writeJSON/writeError) |
| `internal/api/handlers_base.go` | Handlers: health, project, project context |
| `internal/api/handlers_chat.go` | Handlers: chat síncrono, chat stream, session stream |
| `internal/api/handlers_sessions.go` | Handlers CRUD de sesiones |
| `internal/api/handlers_config.go` | Handlers de configuración (proveedores, agentes, LSP, etc.) |
| `internal/api/handlers_container.go` | Handlers de container runtime |
| `internal/api/handlers_cronjobs.go` | Handlers CRUD de cron jobs |
| `internal/api/handlers_evaluator.go` | Handlers del evaluador |
| `internal/api/handlers_extras.go` | Handlers varios |
| `internal/api/handlers_files.go` | Handlers de archivos |
| `internal/api/handlers_instances.go` | Handlers de instancias (proxy IPC → REST) |
| `internal/api/handlers_logs.go` | Handlers de logs |
| `internal/api/handlers_models.go` | Handlers de modelos LLM |
| `internal/api/handlers_notifications.go` | SSE de notificaciones |
| `internal/api/handlers_orchestrator.go` | Handlers del orquestador Mesnada |
| `internal/api/handlers_personas.go` | Handlers de personas |
| `internal/api/handlers_projects.go` | Handlers CRUD de proyectos |
| `internal/api/handlers_provider_accounts.go` | Handlers de cuentas de proveedores |
| `internal/api/handlers_remembrances.go` | Handlers de remembranzas (RAG/index) |
| `internal/api/handlers_settings.go` | Handlers de ajustes |
| `internal/api/handlers_snapshots.go` | Handlers de snapshots |
| `internal/api/handlers_terminal.go` | Handler de terminal |
| `internal/api/handlers_tools.go` | Handler de herramientas MCP |
| `internal/api/handlers_browser_config.go` | Handler de configuración de navegadores |
| `internal/api/handlers_config_events.go` | SSE de eventos de configuración |
| `internal/api/handlers_config_init.go` | Handlers de inicialización/config generate |
| `internal/api/background_runner.go` | BackgroundSessionManager para sesiones asíncronas |
| `internal/api/ui_assets_app.go` | WebUI embebida (embed.FS) |
| `internal/app/app.go` | App struct, New(), Shutdown(), SetupIPC() |
| `internal/ipc/bus.go` | Bus ZMQ (PUB+ROUTER) con JSON-RPC |
| `internal/ipc/client.go` | Cliente ZMQ (SUB+DEALER) con soporte multi-endpoint |
| `internal/ipc/envelope.go` | Envelope estándar para mensajes PUB |
| `internal/ipc/ports.go` | Asignación de puertos (hash FNV + fallback) |
| `internal/ipc/lock_common.go` | LockInfo y rutas de fichero de lock |
| `internal/ipc/lock_unix.go` | flock para primacía de instancia |
| `internal/ipc/options.go` | Timeouts de conexión/llamada |
| `internal/ipc/errors.go` | Errores del protocolo IPC |
| `internal/ipc/protocol/rpc.go` | Constantes y tipos JSON-RPC |
| `internal/ipc/protocol/topics.go` | Constantes de topics PUB |
| `internal/ipc/protocol/payloads.go` | Tipos de payloads para eventos |
| `internal/ipc/bridge/bridge.go` | Bridge eventos in-process → ZMQ PUB |
| `internal/ipc/bridge/handlers.go` | Handlers JSON-RPC registrados en el Bus |
| `internal/ipc/dbproxy/proxy.go` | DBProxy: proxy de escrituras BD via ZMQ |
| `internal/ipc/dbproxy/handlers.go` | Handlers RPC de db.write |
| `internal/instanceregistry/entry.go` | Entry y tipos Mode para instancias |
| `internal/instanceregistry/announce.go` | Anunciar/revocar instancia en /tmp |
| `internal/instanceregistry/registry.go` | Listar/obtener instancias vivas |