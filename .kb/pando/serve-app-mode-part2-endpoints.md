# Análisis de la Implementación del Servidor Pando — Parte 2: Endpoints API REST Detallados

## 4. Endpoints de la API REST

Los endpoints se registran en `internal/api/routes.go`. Todos los handlers están en ficheros separados dentro de `internal/api/`.

### Sistema y Proyecto (`handlers_base.go`)
- **GET /health** → `handleHealth()` — Devuelve `{"status":"healthy","version":"..."}`
- **GET /api/v1/project** → `handleProject()` — CWD y versión
- **GET /api/v1/project/context** → `handleProjectContext()` — Contexto del proyecto

### Sesiones (`handlers_sessions.go`)
- **GET /api/v1/sessions** → Lista todas las sesiones con estado `is_running`
- **GET /api/v1/sessions/{id}** → Obtiene sesión con todos sus mensajes
- **DELETE /api/v1/sessions/{id}** → Elimina sesión y sus mensajes
- **PATCH /api/v1/sessions/{id}** → Actualiza título de sesión
- **GET /api/v1/sessions/{id}/stream** → SSE stream de eventos de sesión con replay

### Chat / LLM (`handlers_chat.go`)
- **POST /api/v1/chat** → Chat síncrono: envía prompt, espera respuesta completa
- **GET|POST /api/v1/chat/stream** → Chat asíncrono con streaming SSE:
  - La ejecución del agente se desacopla del HTTP (corre en background)
  - El cliente puede reconectarse via `GET /sessions/{id}/stream` y recibir replay de eventos
  - Usa `BackgroundSessionManager` para gestionar el ciclo de vida

### BackgroundSessionManager (`background_runner.go`)
- **Fichero:** `internal/api/background_runner.go`
- Gestiona ejecuciones de agente en background independientes de conexiones HTTP
- Buffer circular de 500 eventos por sesión (replay para reconexiones)
- TTL de 10 minutos para sesiones completadas (luego garbage collection)
- Soporta múltiples suscriptores SSE por sesión (subscribers lentos se saltan)
- Cancelación de sesiones activas

### Herramientas MCP (`handlers_tools.go`)
- **GET /api/v1/tools** → Lista herramientas MCP disponibles con nombre, descripción, parámetros y campos requeridos

### Archivos (`handlers_files.go`)
- **GET /api/v1/files** → Lista archivos de una sesión/proyecto
- **GET /api/v1/files/rename** → Renombra archivo
- **GET /api/v1/files/search** → Busca archivos textualmente
- **GET /api/v1/files/raw/{path}** → Contenido raw de archivo
- **GET /api/v1/files/{path}** → Metadatos de archivo por ruta
- **GET /api/v1/fs/browse** → Navegación del sistema de archivos (browse directorios)

### Configuración (`handlers_config.go`)
- **GET/PUT /api/v1/settings** → Ajustes generales (lectura/escritura de config)
- **GET /api/v1/settings/providers** → Proveedores LLM disponibles
- **GET/PUT /api/v1/config/providers** → CRUD de proveedores (API keys enmascaradas en GET)
- **GET/PUT /api/v1/config/agents** → Configuración de agentes LLM
- **GET/PUT /api/v1/config/mcp-servers** → Servidores MCP
- **DELETE /api/v1/config/mcp-servers/{name}** → Eliminar servidor MCP
- **POST /api/v1/config/mcp-servers/{name}/reload** → Recargar servidor MCP
- **GET/PUT /api/v1/config/mcp-gateway** → Configuración MCP Gateway
- **GET/PUT /api/v1/config/lsp** → Configuración de servidores LSP
- **DELETE /api/v1/config/lsp/{language}** → Eliminar LSP para lenguaje
- **GET/PUT /api/v1/config/tools** → Configuración de herramientas internas
- **GET /api/v1/config/browsers** → Configuración de navegadores
- **GET/PUT /api/v1/config/openlit** → Configuración de OpenLit (observabilidad)
- **GET/PUT /api/v1/config/bash** → Configuración de shell
- **GET/PUT /api/v1/config/extensions** → Extensiones de herramienta
- **GET/PUT /api/v1/config/services** → Servicios
- **GET/PUT /api/v1/config/evaluator** → Configuración del evaluador (self-improvement)
- **GET /api/v1/config/provider-accounts** → Lista cuentas de proveedores
- **POST /api/v1/config/provider-accounts** → Crea cuenta de proveedor
- **GET /api/v1/config/provider-types** → Lista tipos de proveedor disponibles
- **GET/PUT/DELETE /api/v1/config/provider-accounts/{id}** → CRUD cuenta específica
- **POST /api/v1/config/provider-accounts/{id}/test** → Prueba conexión
- **POST /api/v1/config/api-server/regenerate-token** → Regenera token de API
- **GET /api/v1/config/init-status** → Estado de inicialización de configuración
- **POST /api/v1/config/generate** → Genera configuración inicial

### Container Runtime (`handlers_container.go`)
- **GET /api/v1/container/capabilities** → Capacidades del runtime
- **GET /api/container/config** → Configuración del contenedor (ruta legacy sin /v1)
- **GET /api/v1/container/sessions** → Sesiones activas en contenedores
- **POST /api/v1/container/sessions/{sessionId}/stop** → Detiene sesión en contenedor
- **GET /api/v1/container/events** → Eventos del runtime de contenedores
- **GET /api/v1/container/images** → Lista imágenes disponibles
- **DELETE /api/v1/container/images/{ref...}** → Elimina imagen
- **POST /api/v1/container/images/gc** → Garbage collection de imágenes

### Eventos y Notificaciones SSE
- **GET /api/v1/config/events** → SSE para hot-reload de configuración
- **GET /api/v1/notifications/stream** → SSE para notificaciones de usuario (errores LLM, errores de herramientas, diagnósticos LSP)

### Remembrances (RAG + Code Indexing)
- **GET /api/v1/remembrances/projects** → Proyectos indexados
- **POST /api/v1/remembrances/projects/index** → Indexa un proyecto de código

### Skills
- **GET /api/v1/skills/installed** → Skills instalados
- **GET /api/v1/skills/catalog** → Catálogo de skills disponibles
- **POST /api/v1/skills/install** → Instala un skill
- **DELETE /api/v1/skills/{name}** → Desinstala skill

### Logs
- **GET /api/v1/logs** → Obtiene logs históricos
- **GET /api/v1/logs/stream** → SSE streaming de logs en tiempo real

### Orchestrator (Mesnada)
- **GET /api/v1/orchestrator/tasks** → Lista tareas del orquestador
- **POST /api/v1/orchestrator/tasks** → Crea nueva tarea
- **GET /api/v1/orchestrator/tasks/{id}** → Obtiene tarea por ID
- **DELETE /api/v1/orchestrator/tasks/{id}** → Elimina tarea
- **POST /api/v1/orchestrator/tasks/{id}/cancel** → Cancela tarea

### Terminal
- **POST /api/v1/terminal/exec** → Ejecuta comando en terminal y devuelve resultado

### Snapshots (`handlers_snapshots.go`)
- **GET /api/v1/snapshots/count** → Cuenta de snapshots
- **GET /api/v1/snapshots** → Lista snapshots
- **POST /api/v1/snapshots** → Crea snapshot
- **GET /api/v1/snapshots/{id}** → Obtiene snapshot por ID
- **POST /api/v1/snapshots/{id}/apply** → Aplica snapshot
- **POST /api/v1/snapshots/{id}/revert** → Revierte snapshot
- **DELETE /api/v1/snapshots/{id}** → Elimina snapshot

### Evaluator
- **GET /api/v1/evaluator/metrics** → Métricas de evaluación
- **GET /api/v1/evaluator/templates** → Templates de prompt
- **GET /api/v1/evaluator/skills** → Skills del evaluador
- **GET /api/v1/evaluator/sessions** → Sesiones evaluadas

### Models
- **GET /api/v1/models** → Lista modelos disponibles
- **PUT /api/v1/models/active** → Establece modelo activo

### Personas
- **GET /api/v1/personas** → Lista personas
- **GET /api/v1/personas/active** → Obtiene persona activa
- **PUT /api/v1/personas/active** → Establece persona activa

### CronJobs
- **GET /api/v1/cronjobs** → Lista cron jobs
- **POST /api/v1/cronjobs** → Crea cron job
- **PUT/DELETE /api/v1/cronjobs/{name}** → Actualiza/elimina cron job
- **POST /api/v1/cronjobs/{name}/run** → Ejecuta cron job inmediatamente

### Projects
- **GET /api/v1/projects** → Lista proyectos
- **POST /api/v1/projects** → Crea proyecto
- **GET /api/v1/projects/active** → Obtiene proyecto activo
- **GET /api/v1/projects/events** → Eventos de proyecto
- **GET /api/v1/projects/{id}** → Obtiene proyecto por ID
- **DELETE /api/v1/projects/{id}** → Elimina proyecto
- **PATCH /api/v1/projects/{id}** → Renombra proyecto
- **POST /api/v1/projects/{id}/activate** → Activa proyecto
- **POST /api/v1/projects/{id}/deactivate** → Desactiva proyecto
- **POST /api/v1/projects/{id}/init** → Inicializa proyecto

### Instancias (observación y control remoto vía IPC) (`handlers_instances.go`)
- **GET /api/v1/instances** → Lista todas las instancias Pando vivas
- **GET /api/v1/instances/{id}** → Obtiene instancia por ID
- **GET /api/v1/instances/{id}/stream** → Proxy del PUB stream ZMQ como SSE
- **GET /api/v1/instances/{id}/sessions** → Lista sesiones remotas via RPC `session.list`
- **GET /api/v1/instances/{id}/sessions/{sid}** → Obtiene sesión remota via RPC `session.get`
- **GET /api/v1/instances/{id}/sessions/{sid}/stream** → Proxy PUB stream filtrado por session_id
- **DELETE /api/v1/instances/{id}/sessions/{sid}/cancel** → Interrumpe generación LLM remota via RPC `session.interrupt`
- **POST /api/v1/instances/{id}/sessions/{sid}/message** → Envía mensaje a sesión remota via RPC `message.send`
